package elephantdocs

import (
	"slices"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
)

func gatedAPI(t *testing.T, protocols map[string]ProtocolConfig) APIConfig {
	t.Helper()

	conf := Config{
		Modules: []ModuleConfig{
			{
				Name: "example.test/module",
				APIs: map[string]APIConfig{
					"example": {
						Title:     "Example",
						Protocols: protocols,
					},
				},
			},
		},
	}

	err := conf.Validate()
	if err != nil {
		t.Fatalf("validate the config: %v", err)
	}

	return conf.Modules[0].APIs["example"]
}

func TestProtocolsAt(t *testing.T) {
	connectFrom := map[string]ProtocolConfig{
		ProtocolConnect: {From: "v0.25.0"},
	}

	dualStack := map[string]ProtocolConfig{
		ProtocolConnect: {From: "v0.25.0"},
		ProtocolTwirp:   {Until: "v1.0.0"},
	}

	cases := []struct {
		name      string
		protocols map[string]ProtocolConfig
		version   string
		want      []string
	}{
		{
			name:    "ungated is twirp only",
			version: "v0.24.1",
			want:    []string{ProtocolTwirp},
		},
		{
			name:      "before connect",
			protocols: connectFrom,
			version:   "v0.24.1",
			want:      []string{ProtocolTwirp},
		},
		{
			name:      "at connect",
			protocols: connectFrom,
			version:   "v0.25.0",
			want:      []string{ProtocolConnect, ProtocolTwirp},
		},
		{
			name:      "dual stack",
			protocols: dualStack,
			version:   "v0.30.0",
			want:      []string{ProtocolConnect, ProtocolTwirp},
		},
		{
			name:      "after twirp",
			protocols: dualStack,
			version:   "v1.0.0",
			want:      []string{ProtocolConnect},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			conf := gatedAPI(t, c.protocols)

			v, err := semver.NewVersion(c.version)
			if err != nil {
				t.Fatalf("parse the version: %v", err)
			}

			got := protocolsAt(conf, v)
			if !slices.Equal(got, c.want) {
				t.Errorf("got %v, wanted %v", got, c.want)
			}
		})
	}
}

func TestResolveProtocolsNotices(t *testing.T) {
	repo := buildRepo(t,
		testCommit{
			Tag: "v0.24.0",
			Files: map[string]string{
				"example/service.proto": serviceProto(
					"test.example", "Examples"),
			},
		},
		testCommit{
			Tag: "v0.25.0",
			Files: map[string]string{
				"example/exampleconnect/service.connect.go": "package exampleconnect\n",
			},
		},
		testCommit{
			Tag: "v1.0.0",
			Files: map[string]string{
				"example/README.md": "# Example\n",
			},
		},
	)

	conf := gatedAPI(t, map[string]ProtocolConfig{
		ProtocolConnect: {From: "v0.25.0"},
		ProtocolTwirp:   {Until: "v1.0.0"},
	})

	module := testModule(t, ModuleConfig{
		Name: "example.test/module",
		APIs: map[string]APIConfig{"example": conf},
	}, repo)

	env := Environments{Tenants: map[string]TenantConfig{
		"tt":  {APIs: map[string]APIDeployments{"example": deployed("v0.25.0")}},
		"ntb": {APIs: map[string]APIDeployments{}},
	}}

	before := resolveProtocols("example", conf, module,
		module.VersionLookup["v0.24.0"], env)

	if before.Default != ProtocolTwirp {
		t.Errorf("got the default protocol %q before the gate, wanted twirp",
			before.Default)
	}

	// The version is the link text, never repeated in the sentence: the
	// template writes the text, the link and the full stop.
	if len(before.Notices) != 1 ||
		before.Notices[0].LinkText != "v0.25.0" {
		t.Errorf("got the notices %v, wanted one linking v0.25.0",
			before.Notices)
	}

	if strings.Contains(before.Notices[0].Text, "v0.25.0") ||
		strings.HasSuffix(before.Notices[0].Text, ".") {
		t.Errorf("the notice text %q repeats the linked version",
			before.Notices[0].Text)
	}

	if before.Notices[0].HRef != "/apis/example/v0.25.0" {
		t.Errorf("got the notice link %q, wanted it relative to the site root",
			before.Notices[0].HRef)
	}

	dual := resolveProtocols("example", conf, module,
		module.VersionLookup["v0.25.0"], env)

	if !dual.Multiple() || dual.Default != ProtocolConnect {
		t.Errorf("got %d protocols defaulting to %q, wanted connect first",
			len(dual.Available), dual.Default)
	}

	if len(dual.Notices) != 0 {
		t.Errorf("got the notices %v in the dual stack band", dual.Notices)
	}

	// Only the tenant that runs the API gets examples, and the row for
	// the other one says it isn't deployed.
	if !slices.Equal(dual.Tenants, []string{"tt"}) {
		t.Errorf("got the example tenants %v, wanted tt", dual.Tenants)
	}

	if len(dual.Deployments) != 2 {
		t.Fatalf("got %d deployment rows, wanted one per tenant",
			len(dual.Deployments))
	}

	if dual.Deployments[1].Deployed {
		t.Error("a tenant without the API was reported as running it")
	}

	after := resolveProtocols("example", conf, module,
		module.VersionLookup["v1.0.0"], env)

	if after.Multiple() || after.Default != ProtocolConnect {
		t.Errorf("got %d protocols after the twirp removal, wanted connect only",
			len(after.Available))
	}

	if len(after.Notices) != 1 ||
		!strings.Contains(after.Notices[0].Text, "removed in v1.0.0") {
		t.Errorf("got the notices %v after the removal", after.Notices)
	}

	if after.Notices[0].LinkText != "v0.30.0" &&
		after.Notices[0].LinkText != "v0.25.0" {
		t.Errorf("got the link %q, wanted the last dual stack version",
			after.Notices[0].LinkText)
	}
}

func TestCheckProtocolGates(t *testing.T) {
	repo := buildRepo(t,
		testCommit{
			Tag: "v0.24.0",
			Files: map[string]string{
				"example/service.proto": serviceProto(
					"test.example", "Examples"),
			},
		},
		testCommit{
			Tag: "v0.25.0",
			Files: map[string]string{
				"example/exampleconnect/service.connect.go": "package exampleconnect\n",
			},
		},
	)

	quiet := func(_ string, _ ...any) {}

	valid := gatedAPI(t, map[string]ProtocolConfig{
		ProtocolConnect: {From: "v0.25.0"},
	})

	module := testModule(t, ModuleConfig{
		Name: "example.test/module",
		APIs: map[string]APIConfig{"example": valid},
	}, repo)

	err := checkProtocolGates(module, quiet)
	if err != nil {
		t.Errorf("a gate backed by the module tree was refused: %v", err)
	}

	early := gatedAPI(t, map[string]ProtocolConfig{
		ProtocolConnect: {From: "v0.24.0"},
	})

	module = testModule(t, ModuleConfig{
		Name: "example.test/module",
		APIs: map[string]APIConfig{"example": early},
	}, repo)

	err = checkProtocolGates(module, quiet)
	if err == nil {
		t.Fatal("a connect gate on a version without the adapters was accepted")
	}

	if !strings.Contains(err.Error(), "exampleconnect") {
		t.Errorf("the error doesn't name the missing directory: %v", err)
	}

	untagged := gatedAPI(t, map[string]ProtocolConfig{
		ProtocolConnect: {From: "v9.9.9"},
	})

	module = testModule(t, ModuleConfig{
		Name: "example.test/module",
		APIs: map[string]APIConfig{"example": untagged},
	}, repo)

	err = checkProtocolGates(module, quiet)
	if err == nil {
		t.Fatal("a connect gate on an untagged version was accepted")
	}
}

func TestCheckProtocolGatesWarnsAboutTwirpCode(t *testing.T) {
	repo := buildRepo(t,
		testCommit{
			Tag: "v0.25.0",
			Files: map[string]string{
				"example/service.proto": serviceProto(
					"test.example", "Examples"),
				"example/exampleconnect/service.connect.go": "package exampleconnect\n",
			},
		},
		testCommit{
			Tag: "v1.0.0",
			Files: map[string]string{
				"example/service.twirp.go": "package example\n",
			},
		},
	)

	conf := gatedAPI(t, map[string]ProtocolConfig{
		ProtocolConnect: {From: "v0.25.0"},
		ProtocolTwirp:   {Until: "v1.0.0"},
	})

	module := testModule(t, ModuleConfig{
		Name: "example.test/module",
		APIs: map[string]APIConfig{"example": conf},
	}, repo)

	var warnings []string

	err := checkProtocolGates(module, func(format string, a ...any) {
		warnings = append(warnings, strings.ToLower(format))
		_ = a
	})
	if err != nil {
		t.Fatalf("check the protocol gates: %v", err)
	}

	if len(warnings) != 1 || !strings.Contains(warnings[0], "warning") {
		t.Errorf("got the warnings %v, wanted one about the twirp code",
			warnings)
	}
}

func TestMethodExamples(t *testing.T) {
	set := ProtocolSet{
		API:       "example",
		Default:   ProtocolConnect,
		Available: []ProtocolInfo{protocolTemplate(ProtocolConnect)},
		Tenants:   []string{"tt"},
	}

	examples := methodExamples(set, "test.example", "Examples", "Get")

	if len(examples) != 1 {
		t.Fatalf("got %d examples, wanted one", len(examples))
	}

	e := examples[0]

	want := "curl https://example.api.tt.ecms.se/test.example.Examples/Get"
	if !strings.HasPrefix(e.Command, want) {
		t.Errorf("got the command %q, wanted it to start with %q",
			e.Command, want)
	}

	if !strings.Contains(e.Command, "Connect-Protocol-Version: 1") {
		t.Errorf("the connect example doesn't set the protocol version: %s",
			e.Command)
	}

	// The body is written once per page, so the command reads it from a
	// file rather than carrying a copy.
	if !strings.HasSuffix(e.Command, "-d @"+requestBodyFile) {
		t.Errorf("the command doesn't post the shared body: %s", e.Command)
	}

	if e.StagingHost != "https://example.api.stage.tt.ecms.se" {
		t.Errorf("got the staging host %q", e.StagingHost)
	}
}

// A tenant that runs a version from before the gate must never be shown a
// Connect example, which is what a method page would otherwise do the moment
// one tenant is ahead of another.
func TestMethodExamplesStraddlingTenants(t *testing.T) {
	repo := buildRepo(t,
		testCommit{
			Tag: "v0.24.0",
			Files: map[string]string{
				"example/service.proto": serviceProto(
					"test.example", "Examples"),
			},
		},
		testCommit{
			Tag: "v0.25.0",
			Files: map[string]string{
				"example/exampleconnect/service.connect.go": "package exampleconnect\n",
			},
		},
	)

	conf := gatedAPI(t, map[string]ProtocolConfig{
		ProtocolConnect: {From: "v0.25.0"},
	})

	module := testModule(t, ModuleConfig{
		Name: "example.test/module",
		APIs: map[string]APIConfig{"example": conf},
	}, repo)

	// tt is past the gate, ntb is not.
	env := Environments{Tenants: map[string]TenantConfig{
		"tt":  {APIs: map[string]APIDeployments{"example": deployed("v0.25.0")}},
		"ntb": {APIs: map[string]APIDeployments{"example": deployed("v0.24.0")}},
	}}

	set := resolveProtocols("example", conf, module,
		module.VersionLookup["v0.25.0"], env)

	if !slices.Equal(set.Tenants, []string{"tt", "ntb"}) {
		t.Fatalf("got the example tenants %v", set.Tenants)
	}

	examples := methodExamples(set, "test.example", "Examples", "Get")

	byKey := make(map[string]MethodExample, len(examples))
	for _, e := range examples {
		byKey[e.Key] = e
	}

	if len(byKey) != 4 {
		t.Fatalf("got %d examples, wanted one per protocol and tenant",
			len(byKey))
	}

	connectNTB := byKey[exampleKey(ProtocolConnect, "ntb")]
	if connectNTB.Command != "" {
		t.Errorf("a connect example was written for a tenant before the gate: %s",
			connectNTB.Command)
	}

	if !strings.Contains(connectNTB.Notice, "v0.24.0") ||
		!strings.Contains(connectNTB.Notice, "Connect") {
		t.Errorf("got the notice %q, wanted the deployed version and protocol",
			connectNTB.Notice)
	}

	for _, key := range []string{
		exampleKey(ProtocolConnect, "tt"),
		exampleKey(ProtocolTwirp, "tt"),
		exampleKey(ProtocolTwirp, "ntb"),
	} {
		if byKey[key].Command == "" {
			t.Errorf("%s got a notice rather than a command: %q",
				key, byKey[key].Notice)
		}
	}

	// The request body is hidden for the combination that has no command,
	// and the default combination is the one a reader without JavaScript
	// gets.
	css := string(exampleCSS(set, examples))

	if !strings.Contains(css,
		`html[data-protocol="connect"][data-tenant="ntb"] [data-example-body]{display:none}`) {
		t.Errorf("the body isn't hidden for the tenant without an example: %s",
			css)
	}

	if !strings.Contains(css,
		`html:not([data-protocol]) [data-example]:not([data-example="connect|tt"]){display:none}`) {
		t.Errorf("there is no no-JavaScript fallback in %s", css)
	}
}

// A split API is served by more than one deployment, and a method's example
// has to be written for the host that answers for its service.
func TestMethodExamplesSplitAPI(t *testing.T) {
	repo := buildRepo(t, testCommit{
		Tag: "v0.5.0",
		Files: map[string]string{
			"example/service.proto": serviceProto(
				"test.example", "Examples"),
		},
	})

	conf := gatedAPI(t, nil)

	module := testModule(t, ModuleConfig{
		Name: "example.test/module",
		APIs: map[string]APIConfig{"example": conf},
	}, repo)

	env := Environments{Tenants: map[string]TenantConfig{
		"tt": {APIs: map[string]APIDeployments{
			"example": {
				{
					Version:  "v0.5.0",
					Service:  "other",
					Services: []string{"Other"},
				},
				{
					Version:  "v0.5.0",
					Service:  "examples",
					Services: []string{"Examples"},
				},
			},
		}},
	}}

	set := resolveProtocols("example", conf, module,
		module.VersionLookup["v0.5.0"], env)

	if len(set.Deployments) != 2 {
		t.Fatalf("got %d deployment rows, wanted one per deployment",
			len(set.Deployments))
	}

	if !slices.Equal(set.Tenants, []string{"tt"}) {
		t.Errorf("got the example tenants %v, wanted tt once", set.Tenants)
	}

	examples := methodExamples(set, "test.example", "Examples", "Get")

	if len(examples) != 1 {
		t.Fatalf("got %d examples", len(examples))
	}

	if !strings.Contains(examples[0].Command,
		"https://examples.api.tt.ecms.se/") {
		t.Errorf("got the command %q, wanted the host of the deployment that serves the service",
			examples[0].Command)
	}

	unserved := methodExamples(set, "test.example", "Missing", "Get")
	if unserved[0].Notice == "" {
		t.Error("a service no deployment answers for got an example")
	}
}

// The deployed version of a service built against a commit rather than a tag
// has no page, so the row names it without linking it.
func TestTenantDeploymentUntaggedVersion(t *testing.T) {
	repo := buildRepo(t, testCommit{
		Tag: "v0.5.0",
		Files: map[string]string{
			"example/service.proto": serviceProto(
				"test.example", "Examples"),
		},
	})

	conf := gatedAPI(t, nil)

	module := testModule(t, ModuleConfig{
		Name: "example.test/module",
		APIs: map[string]APIConfig{"example": conf},
	}, repo)

	env := Environments{Tenants: map[string]TenantConfig{
		"tt": {APIs: map[string]APIDeployments{
			"example": deployed("v0.5.1-0.20260605063608-cd9379fcae57"),
		}},
	}}

	set := resolveProtocols("example", conf, module,
		module.VersionLookup["v0.5.0"], env)

	if set.Deployments[0].HRef != "" {
		t.Errorf("an untagged version was linked to %q",
			set.Deployments[0].HRef)
	}

	if set.Deployments[0].VersionNote == "" {
		t.Error("an unlinked version was rendered without a note")
	}
}
