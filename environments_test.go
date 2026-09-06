package elephantdocs

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func testConfig() Config {
	conf := Config{
		Modules: []ModuleConfig{
			{
				Title: "Core",
				Name:  "example.test/module",
				APIs: map[string]APIConfig{
					"example": {Title: "Example"},
				},
			},
		},
	}

	return conf
}

func TestLoadEnvironments(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "environments.json")

	err := os.WriteFile(name, []byte(`{
  "tenants": {
    "tt": {"apis": {"example": "v1.2.0"}},
    "ntb": {"apis": {}}
  }
}`), 0o600)
	if err != nil {
		t.Fatalf("write the environments file: %v", err)
	}

	env, err := LoadEnvironments(name)
	if err != nil {
		t.Fatalf("load the environments: %v", err)
	}

	err = env.Validate(testConfig())
	if err != nil {
		t.Fatalf("validate the environments: %v", err)
	}

	names := env.TenantNames()
	if !slices.Equal(names, []string{"tt", "ntb"}) {
		t.Errorf("got the tenants %v, wanted the default tenant first", names)
	}

	tag, ok := env.DeployedVersion("tt", "example")
	if !ok || tag != "v1.2.0" {
		t.Errorf("got the deployed version %q (%t), wanted v1.2.0", tag, ok)
	}

	_, ok = env.DeployedVersion("ntb", "example")
	if ok {
		t.Error("an API absent from a tenant was reported as deployed")
	}

	// A file that isn't there leaves the site without deployment
	// information rather than failing.
	env, err = LoadEnvironments(filepath.Join(dir, "nothing.json"))
	if err != nil {
		t.Fatalf("load a missing environments file: %v", err)
	}

	if len(env.Tenants) != 0 {
		t.Error("a missing environments file produced tenants")
	}
}

func TestEnvironmentsValidate(t *testing.T) {
	cases := []struct {
		name    string
		env     Environments
		wantErr string
	}{
		{
			name: "unknown API",
			env: Environments{Tenants: map[string]TenantConfig{
				"tt": {APIs: map[string]string{"nope": "v1.0.0"}},
			}},
			wantErr: "unknown API",
		},
		{
			name: "unparseable version",
			env: Environments{Tenants: map[string]TenantConfig{
				"tt": {APIs: map[string]string{"example": "latest"}},
			}},
			wantErr: "invalid version",
		},
		{
			name: "bad tenant name",
			env: Environments{Tenants: map[string]TenantConfig{
				"TT prod": {APIs: map[string]string{"example": "v1.0.0"}},
			}},
			wantErr: "the name must be",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.env.Validate(testConfig())
			if err == nil {
				t.Fatal("an invalid environments file was accepted")
			}

			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("got the error %q, wanted it to mention %q",
					err, c.wantErr)
			}
		})
	}
}

func TestEnvironmentHosts(t *testing.T) {
	prod := ProductionHost("tt", "repository")
	if prod != "https://repository.api.tt.ecms.se" {
		t.Errorf("got the production host %q", prod)
	}

	stage := StagingHost("ntb", "index")
	if stage != "https://index.api.stage.ntb.ecms.se" {
		t.Errorf("got the staging host %q", stage)
	}
}
