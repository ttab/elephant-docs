package elephantdocs

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/yoheimuta/go-protoparser/v4"
	"github.com/yoheimuta/go-protoparser/v4/parser"
)

type ProtoHandle struct {
	API     string
	Module  string
	Version string
	Proto   ProtoDeclarations
}

type ProtoDeclarations struct {
	File     string
	Package  string
	Imports  []string
	Services []ProtoService
	Messages []ProtoMessage
}

type ProtoService struct {
	Name    string
	Doc     []string
	Methods []ProtoMethod
}

type ProtoMethod struct {
	Name     string
	Doc      []string
	Request  MessageRef
	Response MessageRef
}

type ProtoMessage struct {
	Doc     []string
	Name    string
	Comment string
	Fields  []ProtoField
}

type ProtoField struct {
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

func parseProtoFiles(
	version ModuleVersion, api string,
) ([]ProtoDeclarations, error) {
	tree, err := version.Commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("get tag tree: %w", err)
	}

	apiDir, err := tree.Tree(api)
	if errors.Is(err, object.ErrDirectoryNotFound) {
		return nil, nil
	}

	var protos []ProtoDeclarations

	err = apiDir.Files().ForEach(func(f *object.File) error {
		if !strings.HasSuffix(f.Name, ".proto") {
			return nil
		}

		r, err := f.Reader()
		if err != nil {
			return fmt.Errorf("open %q for reading: %w", f.Name, err)
		}

		defer r.Close()

		pf, err := protoparser.Parse(r, protoparser.WithFilename(f.Name))
		if err != nil {
			return fmt.Errorf("parse %q: %w", f.Name, err)
		}

		pd := createProtoDeclaration(pf)

		pd.File = strings.Join([]string{api, f.Name}, "/")

		protos = append(protos, pd)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse file: %w", err)
	}

	return protos, nil
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
		}
	}

	return fields
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
