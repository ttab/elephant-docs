package elephantdocs

import (
	"testing"
)

const vendoredNewsdoc = `syntax = "proto3";

package newsdoc;

message Document {
  string uuid = 1;
}
`

func TestParseProtoFilesProtoRoot(t *testing.T) {
	repo := buildRepo(t, testCommit{
		Tag: "v1.0.0",
		Files: map[string]string{
			"rpc/example/service.proto": serviceProto(
				"test.example", "Examples"),
		},
	})

	module := testModule(t, ModuleConfig{
		Name:      "example.test/module",
		ProtoRoot: "rpc",
		APIs:      map[string]APIConfig{"example": {Title: "Example"}},
	}, repo)

	version := module.VersionLookup["v1.0.0"]

	protos, err := parseProtoFiles(
		version, module.ProtoRoot(), module.VendorPath(), "example")
	if err != nil {
		t.Fatalf("parse proto files: %v", err)
	}

	if len(protos) != 1 {
		t.Fatalf("got %d declarations, wanted 1", len(protos))
	}

	if protos[0].File != "rpc/example/service.proto" {
		t.Errorf("got the file %q, wanted rpc/example/service.proto",
			protos[0].File)
	}

	// Without the proto root the API is invisible, which is the failure
	// the configuration exists to prevent.
	protos, err = parseProtoFiles(version, "", DefaultVendorRoot, "example")
	if err != nil {
		t.Fatalf("parse proto files without a proto root: %v", err)
	}

	if protos != nil {
		t.Errorf("got %d declarations without a proto root, wanted none",
			len(protos))
	}
}

func TestParseProtoFilesRootLayoutFallback(t *testing.T) {
	// A module configured with a proto root that older versions predate
	// still finds those versions in the repository root.
	repo := buildRepo(t, testCommit{
		Tag: "v1.0.0",
		Files: map[string]string{
			"example/service.proto": serviceProto(
				"test.example", "Examples"),
		},
	})

	module := testModule(t, ModuleConfig{
		Name:      "example.test/module",
		ProtoRoot: "rpc",
		APIs:      map[string]APIConfig{"example": {Title: "Example"}},
	}, repo)

	protos, err := parseProtoFiles(module.VersionLookup["v1.0.0"],
		module.ProtoRoot(), module.VendorPath(), "example")
	if err != nil {
		t.Fatalf("parse proto files: %v", err)
	}

	if len(protos) != 1 {
		t.Fatalf("got %d declarations, wanted 1", len(protos))
	}

	if protos[0].File != "example/service.proto" {
		t.Errorf("got the file %q, wanted example/service.proto",
			protos[0].File)
	}
}

func TestParseProtoFilesExcludesVendorRoot(t *testing.T) {
	// A vendor root inside an API directory would otherwise have its
	// declarations rendered as part of that API.
	repo := buildRepo(t, testCommit{
		Tag: "v1.0.0",
		Files: map[string]string{
			"example/service.proto": serviceProto(
				"test.example", "Examples"),
			"example/vendor/newsdoc/newsdoc.proto": vendoredNewsdoc,
		},
	})

	module := testModule(t, ModuleConfig{
		Name:       "example.test/module",
		VendorRoot: "example/vendor",
		APIs:       map[string]APIConfig{"example": {Title: "Example"}},
	}, repo)

	protos, err := parseProtoFiles(module.VersionLookup["v1.0.0"],
		module.ProtoRoot(), module.VendorPath(), "example")
	if err != nil {
		t.Fatalf("parse proto files: %v", err)
	}

	if len(protos) != 1 {
		t.Fatalf("got %d declarations, wanted 1", len(protos))
	}

	if protos[0].Package != "test.example" {
		t.Errorf("got the package %q, wanted test.example",
			protos[0].Package)
	}
}

func TestParseVendoredProto(t *testing.T) {
	repo := buildRepo(t, testCommit{
		Tag: "v1.0.0",
		Files: map[string]string{
			"example/service.proto": serviceProto(
				"test.example", "Examples"),
			"rpc/vendor/newsdoc/newsdoc.proto": vendoredNewsdoc,
		},
	})

	module := testModule(t, ModuleConfig{
		Name: "example.test/module",
		APIs: map[string]APIConfig{"example": {Title: "Example"}},
	}, repo)

	version := module.VersionLookup["v1.0.0"]

	pd, err := parseVendoredProto(
		version, module.VendorPath(), "newsdoc/newsdoc.proto")
	if err != nil {
		t.Fatalf("parse the vendored proto: %v", err)
	}

	if pd == nil {
		t.Fatal("the vendored proto was not found")
	}

	if pd.File != "newsdoc/newsdoc.proto" {
		t.Errorf("got the file %q, wanted the import path", pd.File)
	}

	if pd.Package != "newsdoc" {
		t.Errorf("got the package %q, wanted newsdoc", pd.Package)
	}

	pd, err = parseVendoredProto(
		version, module.VendorPath(), "nothing/here.proto")
	if err != nil {
		t.Fatalf("look for a proto that isn't vendored: %v", err)
	}

	if pd != nil {
		t.Error("got a declaration for a file that isn't vendored")
	}
}

func TestVendorSkipPrefix(t *testing.T) {
	cases := []struct {
		name       string
		apiPath    string
		vendorRoot string
		want       string
	}{
		{"outside", "example", "rpc/vendor", ""},
		{"inside", "example", "example/vendor", "vendor/"},
		{"nested", "rpc/example", "rpc/example/vendor", "vendor/"},
		{"is the api", "example", "example", "/"},
		{"unset", "example", "", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := vendorSkipPrefix(c.apiPath, c.vendorRoot)
			if got != c.want {
				t.Errorf("got %q, wanted %q", got, c.want)
			}
		})
	}
}
