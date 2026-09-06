package elephantdocs

import (
	"os"
	"testing"

	"github.com/yoheimuta/go-protoparser/v4"
)

func fixtureIndex(t *testing.T) *protoIndex {
	t.Helper()

	f, err := os.Open("testdata/skeleton/fixture.proto")
	if err != nil {
		t.Fatalf("open the fixture: %v", err)
	}

	defer func() {
		err := f.Close()
		if err != nil {
			t.Errorf("close the fixture: %v", err)
		}
	}()

	pf, err := protoparser.Parse(f, protoparser.WithFilename("fixture.proto"))
	if err != nil {
		t.Fatalf("parse the fixture: %v", err)
	}

	return newProtoIndex([]ProtoDeclarations{createProtoDeclaration(pf)})
}

func TestRequestSkeleton(t *testing.T) {
	idx := fixtureIndex(t)

	cases := []struct {
		name    string
		message string
		want    string
	}{
		{
			name:    "scalars",
			message: "Scalars",
			want: `{
  "text": "string",
  "flag": true,
  "count": 1,
  "bigCount": "1",
  "ratio": 1.5,
  "payload": "Ynl0ZXM=",
  "offsetInLog": "1"
}`,
		},
		{
			name:    "collections",
			message: "Collections",
			want: `{
  "tags": [
    "string"
  ],
  "labels": {
    "key": "string"
  },
  "indexed": {
    "1": {
      "text": "string",
      "flag": true,
      "count": 1,
      "bigCount": "1",
      "ratio": 1.5,
      "payload": "Ynl0ZXM=",
      "offsetInLog": "1"
    }
  },
  "items": [
    {
      "text": "string",
      "flag": true,
      "count": 1,
      "bigCount": "1",
      "ratio": 1.5,
      "payload": "Ynl0ZXM=",
      "offsetInLog": "1"
    }
  ]
}`,
		},
		{
			name:    "enums use the first non-zero value",
			message: "WithEnum",
			want: `{
  "status": "STATUS_ACTIVE",
  "history": [
    "STATUS_ACTIVE"
  ]
}`,
		},
		{
			name:    "well known types",
			message: "WellKnown",
			want: `{
  "created": "2026-01-01T12:00:00Z"
}`,
		},
		{
			name:    "cycles stop at the second occurrence",
			message: "Cyclic",
			want: `{
  "id": "string",
  "parent": {}
}`,
		},
		{
			name:    "nesting stops at the depth limit",
			message: "Deep1",
			want: `{
  "next": {
    "next": {
      "next": {
        "next": {
          "next": {}
        }
      }
    }
  }
}`,
		},
		{
			name:    "oneof renders its first member",
			message: "WithOneOf",
			want: `{
  "name": "string",
  "uuid": "string"
}`,
		},
		{
			name:    "message without fields",
			message: "NoFields",
			want:    `{}`,
		},
		{
			name:    "unresolvable message",
			message: "Unresolved",
			want: `{
  "thing": {}
}`,
		},
		{
			name:    "unknown message",
			message: "NotDeclared",
			want:    `{}`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := idx.RequestSkeleton(MessageRef{
				Package: "test.fixture",
				Message: c.message,
			})

			if got != c.want {
				t.Errorf("got:\n%s\n\nwanted:\n%s", got, c.want)
			}
		})
	}
}

func TestJSONFieldName(t *testing.T) {
	cases := map[string]string{
		"uuid":          "uuid",
		"if_match":      "ifMatch",
		"big_count":     "bigCount",
		"offset_in_log": "offsetInLog",
		"a_2_b":         "a2B",
		"already":       "already",
	}

	for in, want := range cases {
		got := jsonFieldName(in)
		if got != want {
			t.Errorf("jsonFieldName(%q) = %q, wanted %q", in, got, want)
		}
	}
}
