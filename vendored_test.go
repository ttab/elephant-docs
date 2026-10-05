package elephantdocs

import (
	"strings"
	"testing"
)

// vendorImporter is a service whose request message references a type from a
// vendored protobuf file.
func vendorImporter(pkg string, service string) string {
	return `syntax = "proto3";

package ` + pkg + `;

import "newsdoc/newsdoc.proto";

service ` + service + ` {
  rpc Get(GetRequest) returns (GetResponse);
}

message GetRequest {
  string uuid = 1;
  newsdoc.Document document = 2;
}

message GetResponse {
  string uuid = 1;
}
`
}

// Two APIs in the same module importing the same vendored file used to leave
// the second one with a dependency whose API name was empty, which rendered
// links to /apis//<version>.
func TestVendoredImportIsNotADependency(t *testing.T) {
	repo := buildRepo(t, testCommit{
		Tag: "v1.0.0",
		Files: map[string]string{
			"first/service.proto": vendorImporter(
				"test.first", "First"),
			"second/service.proto": vendorImporter(
				"test.second", "Second"),
			"rpc/vendor/newsdoc/newsdoc.proto": vendoredNewsdoc,
		},
	})

	conf := ModuleConfig{
		Name: "example.test/module",
		APIs: map[string]APIConfig{
			"first":  {Title: "First"},
			"second": {Title: "Second"},
		},
	}

	module := testModule(t, conf, repo)
	version := module.VersionLookup["v1.0.0"]

	apis, err := collectAPIData(
		map[string]*Module{module.Name: module},
		module, version, version.Commit)
	if err != nil {
		t.Fatalf("collect the API data: %v", err)
	}

	for _, api := range []string{"first", "second"} {
		data, ok := apis[api]
		if !ok {
			t.Fatalf("the API %q was not collected", api)
		}

		for pkg, dep := range data.Dependencies {
			t.Errorf(
				"%s registered %q as a dependency named %q; a vendored file belongs to the module it came from",
				api, pkg, dep.Name)
		}

		if len(data.Vendored) != 1 {
			t.Fatalf("%s indexed %d vendored files, wanted one",
				api, len(data.Vendored))
		}

		if data.Vendored[0].Package != "newsdoc" {
			t.Errorf("%s indexed the package %q",
				api, data.Vendored[0].Package)
		}
	}
}

// A request message that references a vendored type has to render as more
// than an empty object, or the example is a request nobody can send.
func TestRequestSkeletonRendersVendoredTypes(t *testing.T) {
	repo := buildRepo(t, testCommit{
		Tag: "v1.0.0",
		Files: map[string]string{
			"first/service.proto": vendorImporter(
				"test.first", "First"),
			"rpc/vendor/newsdoc/newsdoc.proto": vendoredNewsdoc,
		},
	})

	module := testModule(t, ModuleConfig{
		Name: "example.test/module",
		APIs: map[string]APIConfig{"first": {Title: "First"}},
	}, repo)

	version := module.VersionLookup["v1.0.0"]

	apis, err := collectAPIData(
		map[string]*Module{module.Name: module},
		module, version, version.Commit)
	if err != nil {
		t.Fatalf("collect the API data: %v", err)
	}

	data := apis["first"]

	declSets := [][]ProtoDeclarations{data.Declarations, data.Vendored}

	body := newProtoIndex(declSets...).RequestSkeleton(
		MessageRef{Package: "test.first", Message: "GetRequest"})

	if !strings.Contains(body, `"document"`) {
		t.Fatalf("the skeleton has no document field: %s", body)
	}

	if strings.Contains(body, `"document": {}`) {
		t.Errorf("the vendored message rendered as an empty object: %s",
			body)
	}
}
