package elephantdocs

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

// renderTemplate renders one of the site's templates the way generation does,
// which is the only way to catch a link that has the base path applied twice:
// the value on the page is relative to the site root, and abs_url is what
// prefixes it.
func renderTemplate(
	t *testing.T, basePath string, name string, page Page,
) string {
	t.Helper()

	funcs, err := templateFuncs(basePath)
	if err != nil {
		t.Fatalf("build the template functions: %v", err)
	}

	tpl := template.New("templates")

	tpl.Funcs(funcs)
	tpl.Funcs(schemaTemplateFuncs())

	tpl, err = tpl.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		t.Fatalf("parse the templates: %v", err)
	}

	var buf bytes.Buffer

	err = tpl.ExecuteTemplate(&buf, name, page)
	if err != nil {
		t.Fatalf("render %s: %v", name, err)
	}

	return buf.String()
}

func gatedTestModule(t *testing.T) (*Module, APIConfig) {
	t.Helper()

	repo := buildRepo(t,
		testCommit{
			Tag: "v0.24.0",
			Files: map[string]string{
				"example/service.proto": serviceProto(
					"test.example", "Examples"),
			},
		},
		testCommit{
			Tag: "v0.99.0",
			Files: map[string]string{
				"example/exampleconnect/service.connect.go": "package exampleconnect\n",
			},
		},
	)

	conf := gatedAPI(t, map[string]ProtocolConfig{
		ProtocolConnect: {From: "v0.99.0"},
	})

	module := testModule(t, ModuleConfig{
		Name: "example.test/module",
		APIs: map[string]APIConfig{"example": conf},
	}, repo)

	return module, conf
}

// The deployed base path is /elephant-docs, and every link the protocol work
// added has to survive it.
func TestVersionPageLinksUnderBasePath(t *testing.T) {
	module, conf := gatedTestModule(t)

	env := Environments{Tenants: map[string]TenantConfig{
		"tt": {APIs: map[string]APIDeployments{
			"example": deployed("v0.99.0"),
		}},
	}}

	set := resolveProtocols("example", conf, module,
		module.VersionLookup["v0.24.0"], env)

	page := Page{
		Title:     "Example",
		Protocols: &set,
		Contents: API{
			Name:      "example",
			Title:     "Example",
			Version:   "v0.24.0",
			Module:    module.Name,
			Protocols: &set,
		},
	}

	out := renderTemplate(t, "/elephant-docs", "api_version.html", page)

	if strings.Contains(out, "/elephant-docs/elephant-docs/") {
		t.Error("a link carries the base path twice")
	}

	for _, want := range []string{
		// The boundary notice, pointing at the first Connect version.
		`href="/elephant-docs/apis/example/v0.99.0"`,
		// The deployed versions table.
		`href="/elephant-docs/apis/example/v0.99.0"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the page has no link %s", want)
		}
	}

	// And the notice reads as one sentence rather than repeating the
	// version it links to.
	if strings.Contains(out, "v0.99.0. v0.99.0") {
		t.Error("the version notice repeats the version")
	}
}

func TestMethodPageUnderBasePath(t *testing.T) {
	module, conf := gatedTestModule(t)

	env := Environments{Tenants: map[string]TenantConfig{
		"tt": {APIs: map[string]APIDeployments{
			"example": deployed("v0.99.0"),
		}},
		"ntb": {APIs: map[string]APIDeployments{
			"example": deployed("v0.24.0"),
		}},
	}}

	set := resolveProtocols("example", conf, module,
		module.VersionLookup["v0.99.0"], env)

	examples := methodExamples(set, "test.example", "Examples", "Get")

	method := MethodPage{
		API:             "example",
		Version:         "v0.99.0",
		Package:         "test.example",
		ServiceName:     "Examples",
		MethodName:      "Get",
		Request:         MessageRef{Message: "GetRequest"},
		Response:        MessageRef{Message: "GetResponse"},
		Protocols:       &set,
		RequestBody:     "{\n  \"uuid\": \"sample-body-marker\"\n}",
		RequestBodyFile: requestBodyFile,
		Examples:        examples,
		ExampleCSS:      exampleCSS(set, examples),
	}

	page := Page{
		Title:     "Get",
		Protocols: &set,
		HeadCSS:   method.ExampleCSS,
		Contents:  method,
	}

	out := renderTemplate(t, "/elephant-docs", "method_page.html", page)

	if strings.Contains(out, "/elephant-docs/elephant-docs/") {
		t.Error("a link carries the base path twice")
	}

	// The body is written once, whatever the reader picks.
	if n := strings.Count(out, "sample-body-marker"); n != 1 {
		t.Errorf("the request body is rendered %d times, wanted once", n)
	}

	if !strings.Contains(out, "-d @"+requestBodyFile) {
		t.Errorf("the examples don't post the shared body")
	}

	// The tenant that hasn't been upgraded gets a notice rather than a
	// Connect invocation.
	if !strings.Contains(out, "https://example.api.tt.ecms.se/test.example.Examples/Get") {
		t.Error("the upgraded tenant has no connect example")
	}

	if strings.Contains(out,
		"https://example.api.ntb.ecms.se/test.example.Examples/Get") {
		t.Error("a connect example was rendered for the tenant before the gate")
	}
}
