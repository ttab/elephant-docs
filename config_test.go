package elephantdocs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, contents string) string {
	t.Helper()

	dir := t.TempDir()
	name := filepath.Join(dir, "elephant-docs.json")

	err := os.WriteFile(name, []byte(contents), 0o600)
	if err != nil {
		t.Fatalf("write the config file: %v", err)
	}

	return name
}

func TestLoadConfigRejectsUnknownFields(t *testing.T) {
	path := writeConfig(t, `{
  "modules": [
    {
      "title": "Core",
      "name": "example.test/module",
      "protoRootTypo": "rpc",
      "apis": {"example": {"title": "Example"}}
    }
  ]
}`)

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("a misspelled configuration key was accepted")
	}

	if !strings.Contains(err.Error(), "protoRootTypo") {
		t.Errorf("the error doesn't name the unknown field: %v", err)
	}
}

func TestLoadConfigValidatesProtocols(t *testing.T) {
	cases := []struct {
		name      string
		protocols string
		wantErr   string
	}{
		{
			name:      "unparseable from",
			protocols: `{"connect": {"from": "the next one"}}`,
			wantErr:   "invalid from version",
		},
		{
			name:      "unparseable until",
			protocols: `{"twirp": {"until": "soon"}}`,
			wantErr:   "invalid until version",
		},
		{
			name:      "unknown protocol",
			protocols: `{"grpc": {"from": "v1.0.0"}}`,
			wantErr:   "unknown protocol",
		},
		{
			name:      "from after until",
			protocols: `{"connect": {"from": "v2.0.0", "until": "v1.0.0"}}`,
			wantErr:   "is not before until",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeConfig(t, `{
  "modules": [
    {
      "title": "Core",
      "name": "example.test/module",
      "apis": {
        "example": {"title": "Example", "protocols": `+c.protocols+`}
      }
    }
  ]
}`)

			_, err := LoadConfig(path)
			if err == nil {
				t.Fatal("an invalid protocol gate was accepted")
			}

			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("got the error %q, wanted it to mention %q",
					err, c.wantErr)
			}
		})
	}
}

func TestLoadConfigParsesGates(t *testing.T) {
	path := writeConfig(t, `{
  "modules": [
    {
      "title": "Core",
      "name": "example.test/module",
      "protoRoot": "rpc",
      "apis": {
        "example": {
          "title": "Example",
          "protocols": {
            "connect": {"from": "v0.25.0"},
            "twirp": {"until": "v1.0.0", "note": "on the way out"}
          }
        }
      }
    }
  ]
}`)

	conf, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load the config: %v", err)
	}

	mod := conf.Modules[0]

	if mod.APIPath("example") != "rpc/example" {
		t.Errorf("got the API path %q, wanted rpc/example",
			mod.APIPath("example"))
	}

	if mod.VendorPath() != DefaultVendorRoot {
		t.Errorf("got the vendor path %q, wanted %q",
			mod.VendorPath(), DefaultVendorRoot)
	}

	api := mod.APIs["example"]

	connect, ok := api.gate(ProtocolConnect)
	if !ok {
		t.Fatal("the connect gate wasn't parsed")
	}

	if connect.From == nil || connect.From.String() != "0.25.0" {
		t.Errorf("got the connect from version %v, wanted 0.25.0", connect.From)
	}

	twirp, ok := api.gate(ProtocolTwirp)
	if !ok {
		t.Fatal("the twirp gate wasn't parsed")
	}

	if twirp.Until == nil || twirp.Until.String() != "1.0.0" {
		t.Errorf("got the twirp until version %v, wanted 1.0.0", twirp.Until)
	}

	if twirp.Note != "on the way out" {
		t.Errorf("got the twirp note %q", twirp.Note)
	}
}

func TestLoadConfigRejectsAnAbsoluteProtoRoot(t *testing.T) {
	path := writeConfig(t, `{
  "modules": [
    {
      "title": "Core",
      "name": "example.test/module",
      "protoRoot": "/etc",
      "apis": {"example": {"title": "Example"}}
    }
  ]
}`)

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("an absolute proto root was accepted")
	}
}
