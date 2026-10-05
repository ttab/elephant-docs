package elephantdocs

import (
	"fmt"
	"strings"
)

// skeletonMaxDepth is how far into nested messages a generated request body
// goes before it renders an empty object. Deep enough for the request
// messages in the fleet, shallow enough that a pathological message doesn't
// produce a page of JSON.
const skeletonMaxDepth = 5

// protoIndex resolves message and enum references across an API and the
// declarations it depends on. go-protoparser gives us source text rather than
// a descriptor set, so a reference is matched on its fully qualified name and
// then, failing that, on its bare name.
type protoIndex struct {
	messages map[string]indexedMessage
	enums    map[string]ProtoEnum
}

// indexedMessage carries the fully qualified name the message was declared
// under, which is what the cycle guard compares: the same message is referred
// to by its bare name from its own package and by its qualified name from
// another.
type indexedMessage struct {
	Key     string
	Message ProtoMessage
}

func newProtoIndex(declSets ...[]ProtoDeclarations) *protoIndex {
	idx := protoIndex{
		messages: make(map[string]indexedMessage),
		enums:    make(map[string]ProtoEnum),
	}

	for _, decls := range declSets {
		for _, d := range decls {
			for _, m := range d.Messages {
				im := indexedMessage{
					Key:     qualify(d.Package, m.Name),
					Message: m,
				}

				idx.messages[im.Key] = im

				if _, ok := idx.messages[m.Name]; !ok {
					idx.messages[m.Name] = im
				}
			}

			for _, e := range d.Enums {
				idx.enums[qualify(d.Package, e.Name)] = e

				if _, ok := idx.enums[e.Name]; !ok {
					idx.enums[e.Name] = e
				}
			}
		}
	}

	return &idx
}

func qualify(pkg string, name string) string {
	if pkg == "" {
		return name
	}

	return pkg + "." + name
}

func (idx *protoIndex) message(ref MessageRef) (indexedMessage, bool) {
	m, ok := idx.messages[qualify(ref.Package, ref.Message)]
	if ok {
		return m, true
	}

	m, ok = idx.messages[ref.Message]

	return m, ok
}

func (idx *protoIndex) enum(ref MessageRef) (ProtoEnum, bool) {
	e, ok := idx.enums[qualify(ref.Package, ref.Message)]
	if ok {
		return e, true
	}

	e, ok = idx.enums[ref.Message]

	return e, ok
}

// RequestSkeleton renders a sample JSON request body for a message, using the
// protojson encoding: lowerCamelCase field names, 64 bit integers as strings,
// bytes as base64. It is a starting point for a curl invocation, not a valid
// request: the values are placeholders.
func (idx *protoIndex) RequestSkeleton(ref MessageRef) string {
	v := idx.messageValue(ref, 0, nil)

	var b strings.Builder

	v.writeJSON(&b, "")

	return b.String()
}

func (idx *protoIndex) messageValue(
	ref MessageRef, depth int, stack []string,
) jsonValue {
	if wkt, ok := wellKnownValue(ref); ok {
		return wkt
	}

	im, ok := idx.message(ref)
	if !ok {
		return jsonObject{}
	}

	for _, s := range stack {
		if s == im.Key {
			return jsonObject{}
		}
	}

	if depth >= skeletonMaxDepth {
		return jsonObject{}
	}

	stack = append(stack, im.Key)

	obj := make(jsonObject, 0, len(im.Message.Fields))

	for _, f := range im.Message.Fields {
		if len(f.OneOf) > 0 {
			// One member of a oneof can be set, so the skeleton
			// shows the first one.
			variant := f.OneOf[0]

			obj = append(obj, jsonMember{
				Key:   jsonFieldName(variant.Name),
				Value: idx.fieldValue(variant.Type, depth+1, stack),
			})

			continue
		}

		obj = append(obj, jsonMember{
			Key:   jsonFieldName(f.Name),
			Value: idx.fieldValue(f.Type, depth+1, stack),
		})
	}

	return obj
}

func (idx *protoIndex) fieldValue(
	ft FieldType, depth int, stack []string,
) jsonValue {
	switch {
	case ft.MappedBy != "":
		return jsonObject{
			{
				Key:   mapKeySample(ft.MappedBy),
				Value: idx.singleValue(ft, depth, stack),
			},
		}
	case ft.Repeated:
		return jsonArray{idx.singleValue(ft, depth, stack)}
	}

	return idx.singleValue(ft, depth, stack)
}

func (idx *protoIndex) singleValue(
	ft FieldType, depth int, stack []string,
) jsonValue {
	if ft.Scalar != "" {
		return scalarSample(ft.Scalar)
	}

	if ft.Message == nil {
		return jsonRaw("null")
	}

	if e, ok := idx.enum(*ft.Message); ok {
		return jsonRaw(quote(enumSample(e)))
	}

	return idx.messageValue(*ft.Message, depth, stack)
}

// scalarSample is the sample value for a protobuf scalar type, in the
// protojson encoding: 64 bit integers are strings, and bytes is base64.
func scalarSample(scalar string) jsonValue {
	switch scalar {
	case "double", "float":
		return jsonRaw("1.5")
	case "int32", "sint32", "sfixed32", "uint32", "fixed32":
		return jsonRaw("1")
	case "int64", "sint64", "sfixed64", "uint64", "fixed64":
		return jsonRaw(`"1"`)
	case "bool":
		return jsonRaw("true")
	case "string":
		return jsonRaw(`"string"`)
	case "bytes":
		// Base64 of "bytes".
		return jsonRaw(`"Ynl0ZXM="`)
	}

	return jsonRaw("null")
}

// mapKeySample is the sample key for a map field. JSON object keys are always
// strings, whatever the protobuf key type is.
func mapKeySample(keyType string) string {
	switch keyType {
	case "string":
		return "key"
	case "bool":
		return "true"
	}

	return "1"
}

// enumSample is the first non-zero value of an enum, since the zero value is
// the unspecified sentinel and is omitted from the JSON encoding anyway.
func enumSample(e ProtoEnum) string {
	if len(e.Values) == 0 {
		return ""
	}

	for _, v := range e.Values {
		if v.Number != "0" {
			return v.Name
		}
	}

	return e.Values[0].Name
}

// wellKnownValue covers the google.protobuf types that have their own JSON
// encoding, which the source text parser cannot resolve to a declaration.
func wellKnownValue(ref MessageRef) (jsonValue, bool) {
	if ref.Package != "google.protobuf" {
		return nil, false
	}

	switch ref.Message {
	case "Timestamp":
		return jsonRaw(`"2026-01-01T12:00:00Z"`), true
	case "Duration":
		return jsonRaw(`"1.5s"`), true
	case "FieldMask":
		return jsonRaw(`"field.name"`), true
	case "StringValue":
		return jsonRaw(`"string"`), true
	case "BoolValue":
		return jsonRaw("true"), true
	case "Int32Value", "UInt32Value":
		return jsonRaw("1"), true
	case "Int64Value", "UInt64Value":
		return jsonRaw(`"1"`), true
	case "DoubleValue", "FloatValue":
		return jsonRaw("1.5"), true
	case "BytesValue":
		return jsonRaw(`"Ynl0ZXM="`), true
	case "Empty", "Struct":
		return jsonObject{}, true
	case "Value":
		return jsonRaw("null"), true
	}

	return nil, false
}

// jsonFieldName is the protojson name of a protobuf field: the snake_case
// name with the underscores removed and the letter after each underscore
// upper cased.
func jsonFieldName(name string) string {
	var (
		b  strings.Builder
		up bool
	)

	for _, r := range name {
		if r == '_' {
			up = true

			continue
		}

		if up && r >= 'a' && r <= 'z' {
			b.WriteRune(r - 'a' + 'A')
		} else {
			b.WriteRune(r)
		}

		up = false
	}

	return b.String()
}

func quote(s string) string {
	return `"` + s + `"`
}

// A small ordered JSON value tree. The fields of a message have to keep their
// declaration order, which a map cannot do, and the output is indented for
// reading rather than for parsing.
type jsonValue interface {
	writeJSON(b *strings.Builder, indent string)
}

type jsonRaw string

func (r jsonRaw) writeJSON(b *strings.Builder, _ string) {
	b.WriteString(string(r))
}

type jsonArray []jsonValue

func (a jsonArray) writeJSON(b *strings.Builder, indent string) {
	if len(a) == 0 {
		b.WriteString("[]")

		return
	}

	inner := indent + "  "

	b.WriteString("[\n")

	for i, v := range a {
		b.WriteString(inner)

		v.writeJSON(b, inner)

		if i < len(a)-1 {
			b.WriteString(",")
		}

		b.WriteString("\n")
	}

	b.WriteString(indent + "]")
}

type jsonMember struct {
	Key   string
	Value jsonValue
}

type jsonObject []jsonMember

func (o jsonObject) writeJSON(b *strings.Builder, indent string) {
	if len(o) == 0 {
		b.WriteString("{}")

		return
	}

	inner := indent + "  "

	b.WriteString("{\n")

	for i, m := range o {
		fmt.Fprintf(b, "%s%q: ", inner, m.Key)

		m.Value.writeJSON(b, inner)

		if i < len(o)-1 {
			b.WriteString(",")
		}

		b.WriteString("\n")
	}

	b.WriteString(indent + "}")
}
