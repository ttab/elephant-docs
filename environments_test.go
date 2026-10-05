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
					"split":   {Title: "Split"},
				},
			},
		},
	}

	return conf
}

// deployed is the single deployment shorthand the tests mostly need.
func deployed(version string) APIDeployments {
	return APIDeployments{{Version: version}}
}

func TestLoadEnvironments(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "environments.json")

	err := os.WriteFile(name, []byte(`{
  "tenants": {
    "tt": {"apis": {
      "example": {"version": "v1.2.0", "service": "example2"},
      "split": [
        {"version": "v1.0.0", "service": "one", "services": ["First"]},
        {"version": "v1.1.0", "service": "two", "services": ["Second"]}
      ]
    }},
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

	// A bare object is one deployment, and the service name is what the
	// host derives from.
	single := env.Deployments("tt", "example")
	if len(single) != 1 || single[0].Version != "v1.2.0" {
		t.Fatalf("got the deployments %v, wanted one at v1.2.0", single)
	}

	if single[0].ServiceName("example") != "example2" {
		t.Errorf("got the service %q, wanted the override",
			single[0].ServiceName("example"))
	}

	if !single[0].Serves("Anything") {
		t.Error("a deployment that names no services didn't serve one")
	}

	split := env.Deployments("tt", "split")
	if len(split) != 2 {
		t.Fatalf("got %d deployments of the split API, wanted two",
			len(split))
	}

	if !split[0].Serves("First") || split[0].Serves("Second") {
		t.Errorf("the first deployment serves %v", split[0].Services)
	}

	if split[1].ServiceName("split") != "two" {
		t.Errorf("got the second service %q", split[1].ServiceName("split"))
	}

	if len(env.Deployments("ntb", "example")) != 0 {
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

func TestLoadEnvironmentsRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "environments.json")

	err := os.WriteFile(name, []byte(`{
  "tenants": {"tt": {"apis": {"example": {"verison": "v1.2.0"}}}}
}`), 0o600)
	if err != nil {
		t.Fatalf("write the environments file: %v", err)
	}

	_, err = LoadEnvironments(name)
	if err == nil {
		t.Fatal("a misspelled deployment key was accepted")
	}

	if !strings.Contains(err.Error(), "verison") {
		t.Errorf("got the error %q, wanted it to name the key", err)
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
				"tt": {APIs: map[string]APIDeployments{
					"nope": deployed("v1.0.0"),
				}},
			}},
			wantErr: "unknown API",
		},
		{
			name: "unparseable version",
			env: Environments{Tenants: map[string]TenantConfig{
				"tt": {APIs: map[string]APIDeployments{
					"example": deployed("latest"),
				}},
			}},
			wantErr: "invalid version",
		},
		{
			name: "bad tenant name",
			env: Environments{Tenants: map[string]TenantConfig{
				"TT prod": {APIs: map[string]APIDeployments{
					"example": deployed("v1.0.0"),
				}},
			}},
			wantErr: "the name must be",
		},
		{
			name: "bad service name",
			env: Environments{Tenants: map[string]TenantConfig{
				"tt": {APIs: map[string]APIDeployments{
					"example": {{
						Version: "v1.0.0",
						Service: "Example Two",
					}},
				}},
			}},
			wantErr: "the name must be",
		},
		{
			name: "no deployments",
			env: Environments{Tenants: map[string]TenantConfig{
				"tt": {APIs: map[string]APIDeployments{
					"example": {},
				}},
			}},
			wantErr: "no deployments",
		},
		{
			name: "split without services",
			env: Environments{Tenants: map[string]TenantConfig{
				"tt": {APIs: map[string]APIDeployments{
					"split": {
						{Version: "v1.0.0", Service: "one"},
						{Version: "v1.1.0", Service: "two"},
					},
				}},
			}},
			wantErr: "name the services",
		},
		{
			name: "the same service twice",
			env: Environments{Tenants: map[string]TenantConfig{
				"tt": {APIs: map[string]APIDeployments{
					"split": {
						{
							Version:  "v1.0.0",
							Services: []string{"First"},
						},
						{
							Version:  "v1.1.0",
							Services: []string{"Second"},
						},
					},
				}},
			}},
			wantErr: "listed twice",
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
