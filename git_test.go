package elephantdocs

import (
	"strings"
	"testing"
)

func TestGetChangelogProtoRoot(t *testing.T) {
	repo := buildRepo(t,
		testCommit{
			Message: "add the example service",
			Tag:     "v1.0.0",
			Files: map[string]string{
				"rpc/example/service.proto": serviceProto(
					"test.example", "Examples"),
			},
		},
		testCommit{
			Message: "document the example service",
			Tag:     "v1.1.0",
			Files: map[string]string{
				"rpc/example/README.md": "# Example\n",
			},
		},
		testCommit{
			Message: "unrelated change",
			Tag:     "v1.2.0",
			Files: map[string]string{
				"README.md": "# Module\n",
			},
		},
	)

	conf := ModuleConfig{
		Name:      "example.test/module",
		ProtoRoot: "rpc",
		APIs:      map[string]APIConfig{"example": {Title: "Example"}},
	}

	module := testModule(t, conf, repo)

	versions, err := getChangelog(module, module.ProtoRoot(), "example")
	if err != nil {
		t.Fatalf("get changelog: %v", err)
	}

	if len(versions) != 3 {
		t.Fatalf("got %d versions in the changelog, wanted 3", len(versions))
	}

	var logged int

	for _, v := range versions {
		logged += len(v.Log)
	}

	if logged != 2 {
		t.Errorf("got %d commits in the changelog, wanted the 2 that touched the API",
			logged)
	}

	// The same module read as a repository root layout finds nothing,
	// which is the failure the proto root configuration prevents.
	versions, err = getChangelog(module, "", "example")
	if err != nil {
		t.Fatalf("get changelog without a proto root: %v", err)
	}

	if versions != nil {
		t.Errorf("got %d versions without a proto root, wanted none",
			len(versions))
	}
}

func TestCheckAPIsArePresent(t *testing.T) {
	repo := buildRepo(t, testCommit{
		Tag: "v1.0.0",
		Files: map[string]string{
			"rpc/example/service.proto": serviceProto(
				"test.example", "Examples"),
		},
	})

	present := testModule(t, ModuleConfig{
		Name:      "example.test/module",
		ProtoRoot: "rpc",
		APIs:      map[string]APIConfig{"example": {Title: "Example"}},
	}, repo)

	err := checkAPIsArePresent(present)
	if err != nil {
		t.Errorf("a configured and present API was reported as missing: %v", err)
	}

	missing := testModule(t, ModuleConfig{
		Name: "example.test/module",
		APIs: map[string]APIConfig{"example": {Title: "Example"}},
	}, repo)

	err = checkAPIsArePresent(missing)
	if err == nil {
		t.Fatal("an API that no version declares was accepted")
	}

	if !strings.Contains(err.Error(), "example") {
		t.Errorf("the error doesn't name the API: %v", err)
	}
}
