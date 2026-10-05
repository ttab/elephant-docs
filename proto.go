package elephantdocs

import (
	"errors"
	"fmt"
	"html/template"
	"path"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/ttab/elephant-docs/internal"
	"github.com/yoheimuta/go-protoparser/v4"
	"github.com/yoheimuta/go-protoparser/v4/parser"
)

type ProtoHandle struct {
	API     string
	Module  string
	Version string
	// Vendored marks a protobuf file read out of the module's vendored
	// proto root. It resolves the imports of the module's own
	// declarations and is never rendered as part of an API: the module it
	// was copied out of is where it is documented.
	Vendored bool
	Proto    ProtoDeclarations
}

type ProtoDeclarations struct {
	File     string
	Package  string
	Imports  []string
	Services []ProtoService
	Messages []ProtoMessage
	Enums    []ProtoEnum
}

type ProtoService struct {
	Name    string
	Doc     []string
	Methods []ProtoMethod
}

type ProtoMethod struct {
	Name     string
	Doc      []string
	Readme   template.HTML
	Request  MessageRef
	Response MessageRef
}

type ProtoMessage struct {
	Doc     []string
	Readme  template.HTML
	Name    string
	Comment string
	Fields  []ProtoField
}

type ProtoEnum struct {
	Doc    []string
	Readme template.HTML
	Name   string
	Values []ProtoEnumValue
}

type ProtoEnumValue struct {
	Name   string
	Doc    []string
	Number string
}

type ProtoField struct {
	Name  string
	Doc   []string
	Type  FieldType
	OneOf []OneOfVariant `json:",omitempty"`
}

type OneOfVariant struct {
	Name string
	Doc  []string
	Type FieldType
}

type FieldType struct {
	Repeated bool        `json:",omitempty"`
	MappedBy string      `json:",omitempty"`
	Scalar   string      `json:",omitempty"`
	Message  *MessageRef `json:",omitempty"`
}

type MessageRef struct {
	Package string `json:",omitempty"`
	Message string
}

// apiTree opens an API's directory in a version tree. A module that keeps its
// protobuf sources under a proto root is looked up there first, and in the
// repository root after that, so that a module that moved its sources keeps
// its older versions.
//
// The returned path is the directory the files were found in, which is also
// the prefix the files are imported by.
func apiTree(
	tree *object.Tree, protoRoot string, api string,
) (string, *object.Tree, error) {
	candidates := []string{path.Join(protoRoot, api)}
	if protoRoot != "" {
		candidates = append(candidates, api)
	}

	for _, dir := range candidates {
		sub, err := tree.Tree(dir)
		if errors.Is(err, object.ErrDirectoryNotFound) {
			continue
		} else if err != nil {
			return "", nil, fmt.Errorf(
				"look up the %q directory: %w", dir, err)
		}

		return dir, sub, nil
	}

	return "", nil, nil
}

// vendorSkipPrefix is the path prefix, relative to an API directory, that
// holds vendored protobuf files. The default vendor root is outside every API
// directory, but the mage configuration allows moving it, and a vendored file
// documented as part of an API would be both a duplicate declaration and an
// unresolvable import.
func vendorSkipPrefix(apiPath string, vendorRoot string) string {
	if vendorRoot == "" {
		return ""
	}

	if vendorRoot == apiPath {
		// The whole API directory is the vendor root, which means
		// there is nothing of the module's own in it.
		return "/"
	}

	if !strings.HasPrefix(vendorRoot, apiPath+"/") {
		return ""
	}

	return strings.TrimPrefix(vendorRoot, apiPath+"/") + "/"
}

func parseProtoFiles(
	version *ModuleVersion, protoRoot string, vendorRoot string, api string,
) ([]ProtoDeclarations, error) {
	tree, err := version.Commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("get tag tree: %w", err)
	}

	apiPath, apiDir, err := apiTree(tree, protoRoot, api)
	if err != nil {
		return nil, err
	}

	if apiDir == nil {
		return nil, nil
	}

	skip := vendorSkipPrefix(apiPath, vendorRoot)

	var protos []ProtoDeclarations

	err = apiDir.Files().ForEach(func(f *object.File) error {
		if !strings.HasSuffix(f.Name, ".proto") {
			return nil
		}

		if skip != "" && (skip == "/" || strings.HasPrefix(f.Name, skip)) {
			return nil
		}

		pd, err := parseProtoFile(f)
		if err != nil {
			return err
		}

		pd.File = path.Join(apiPath, f.Name)

		protos = append(protos, pd)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse file: %w", err)
	}

	return protos, nil
}

// parseVendoredProto reads an import out of the module's vendored proto root.
// A repository that vendors a protobuf file from a module that isn't itself
// documented here would otherwise fail the whole build on an unresolvable
// import.
func parseVendoredProto(
	version *ModuleVersion, vendorRoot string, importPath string,
) (*ProtoDeclarations, error) {
	if vendorRoot == "" {
		return nil, nil
	}

	tree, err := version.Commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("get tag tree: %w", err)
	}

	f, err := tree.File(path.Join(vendorRoot, importPath))
	if errors.Is(err, object.ErrFileNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("look up the vendored %q: %w", importPath, err)
	}

	pd, err := parseProtoFile(f)
	if err != nil {
		return nil, err
	}

	// Indexed under the path it is imported by, which is the path it had
	// in the repository it was vendored out of.
	pd.File = importPath

	return &pd, nil
}

func parseProtoFile(f *object.File) (_ ProtoDeclarations, outErr error) {
	r, err := f.Reader()
	if err != nil {
		return ProtoDeclarations{}, fmt.Errorf(
			"open %q for reading: %w", f.Name, err)
	}

	defer internal.Close(f.Name, r, &outErr)

	pf, err := protoparser.Parse(r, protoparser.WithFilename(f.Name))
	if err != nil {
		return ProtoDeclarations{}, fmt.Errorf("parse %q: %w", f.Name, err)
	}

	return createProtoDeclaration(pf), nil
}

func createProtoDeclaration(pf *parser.Proto) ProtoDeclarations {
	var d ProtoDeclarations

	for _, v := range pf.ProtoBody {
		switch o := v.(type) {
		case *parser.Import:
			l, err := strconv.Unquote(o.Location)
			if err != nil {
				// Should not be possible in parsed proto.
				panic(fmt.Errorf("invalid import %q: %w", o.Location, err))
			}

			d.Imports = append(d.Imports, l)
		case *parser.Package:
			d.Package = o.Name
		case *parser.Service:
			s := ProtoService{
				Doc:     collectComments(o.Comments),
				Name:    o.ServiceName,
				Methods: collectMethods(o),
			}

			d.Services = append(d.Services, s)
		case *parser.Message:
			m := ProtoMessage{
				Doc:    collectComments(o.Comments),
				Name:   o.MessageName,
				Fields: collectFields(o),
			}

			d.Messages = append(d.Messages, m)
		case *parser.Enum:
			e := ProtoEnum{
				Doc:    collectComments(o.Comments),
				Name:   o.EnumName,
				Values: collectEnumValues(o),
			}

			d.Enums = append(d.Enums, e)
		}
	}

	return d
}

var scalars = map[string]bool{
	"double":   true,
	"float":    true,
	"int32":    true,
	"int64":    true,
	"uint32":   true,
	"uint64":   true,
	"sint32":   true,
	"sint64":   true,
	"fixed32":  true,
	"fixed64":  true,
	"sfixed32": true,
	"sfixed64": true,
	"bool":     true,
	"string":   true,
	"bytes":    true,
}

func collectFields(msg *parser.Message) []ProtoField {
	var fields []ProtoField

	for _, v := range msg.MessageBody {
		switch o := v.(type) {
		case *parser.Field:
			field := ProtoField{
				Doc:  collectComments(o.Comments),
				Name: o.FieldName,
			}

			if scalars[o.Type] {
				field.Type = FieldType{
					Scalar: o.Type,
				}
			} else {
				msg := createMessageRef(o.Type)

				field.Type = FieldType{
					Message: &msg,
				}
			}

			field.Type.Repeated = o.IsRepeated

			fields = append(fields, field)
		case *parser.MapField:
			field := ProtoField{
				Doc:  collectComments(o.Comments),
				Name: o.MapName,
			}

			if scalars[o.Type] {
				field.Type = FieldType{
					Scalar: o.Type,
				}
			} else {
				msg := createMessageRef(o.Type)

				field.Type = FieldType{
					Message: &msg,
				}
			}

			field.Type.MappedBy = o.KeyType

			fields = append(fields, field)
		case *parser.Oneof:
			field := ProtoField{
				Doc:  collectComments(o.Comments),
				Name: o.OneofName,
			}

			for _, f := range o.OneofFields {
				variant := OneOfVariant{
					Name: f.FieldName,
					Doc:  collectComments(f.Comments),
				}

				if scalars[f.Type] {
					variant.Type = FieldType{
						Scalar: f.Type,
					}
				} else {
					msg := createMessageRef(f.Type)

					variant.Type = FieldType{
						Message: &msg,
					}
				}

				field.OneOf = append(field.OneOf, variant)
			}

			fields = append(fields, field)
		}
	}

	return fields
}

func collectEnumValues(enum *parser.Enum) []ProtoEnumValue {
	var values []ProtoEnumValue

	for _, v := range enum.EnumBody {
		switch o := v.(type) {
		case *parser.EnumField:
			values = append(values, ProtoEnumValue{
				Name:   o.Ident,
				Doc:    collectComments(o.Comments),
				Number: o.Number,
			})
		}
	}

	return values
}

func collectMethods(srv *parser.Service) []ProtoMethod {
	var methods []ProtoMethod

	for _, v := range srv.ServiceBody {
		switch o := v.(type) {
		case *parser.RPC:
			methods = append(methods, ProtoMethod{
				Doc:      collectComments(o.Comments),
				Name:     o.RPCName,
				Request:  createMessageRef(o.RPCRequest.MessageType),
				Response: createMessageRef(o.RPCResponse.MessageType),
			})
		}
	}

	return methods
}

func createMessageRef(msgType string) MessageRef {
	parts := strings.Split(msgType, ".")
	if len(parts) == 1 {
		return MessageRef{
			Message: msgType,
		}
	}

	return MessageRef{
		Package: strings.Join(parts[0:len(parts)-1], "."),
		Message: parts[len(parts)-1],
	}
}

func collectComments(comments []*parser.Comment) []string {
	var lines []string

	for _, c := range comments {
		for _, l := range c.Lines() {
			lines = append(lines, strings.TrimSpace(l))
		}
	}

	return lines
}
