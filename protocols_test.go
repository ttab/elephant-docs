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
		"tt":  {APIs: map[string]string{"example": "v0.25.0"}},
		"ntb": {APIs: map[string]string{}},
	}}

	before := resolveProtocols("example", conf, module,
		module.VersionLookup["v0.24.0"], env, "")

	if before.Default != ProtocolTwirp {
		t.Errorf("got the default protocol %q before the gate, wanted twirp",
			before.Default)
	}

	if len(before.Notices) != 1 ||
		!strings.Contains(before.Notices[0].Text, "v0.25.0") {
		t.Errorf("got the notices %v, wanted one naming v0.25.0",
			before.Notices)
	}

	dual := resolveProtocols("example", conf, module,
		module.VersionLookup["v0.25.0"], env, "")

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
		module.VersionLookup["v1.0.0"], env, "")

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
