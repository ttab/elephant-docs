package elephantdocs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"

	"github.com/Masterminds/semver/v3"
	"github.com/ttab/elephant-docs/internal"
)

// DefaultTenant is the tenant curl examples are shown for until the reader
// picks another one.
const DefaultTenant = "tt"

// hostSuffix is the domain the tenant environments are served under. A
// production host is "<service>.api.<tenant>.<hostSuffix>", and a staging
// host "<service>.api.stage.<tenant>.<hostSuffix>".
const hostSuffix = "ecms.se"

// Environments is the manually maintained record of which version of each API
// a tenant currently runs in production. It is what makes the protocol gate
// honest per environment: a tenant serves Connect for an API when its
// deployed version is at or past the API's connect.from.
//
// Staging is assumed to run the module's latest version.
type Environments struct {
	Tenants map[string]TenantConfig `json:"tenants"`
}

type TenantConfig struct {
	// APIs maps an API name to the deployments that serve it. An API that
	// isn't in the map is not deployed for the tenant.
	APIs map[string]APIDeployments `json:"apis"`
}

// APIDeployments are the services that serve one API for one tenant. Most
// APIs are served by a single deployment and are written as one object; an
// API split across several deployments is written as an array, and then every
// entry names the protobuf services it serves.
type APIDeployments []APIDeployment

// APIDeployment is one service that serves an API for a tenant.
type APIDeployment struct {
	// Version is the version of the declarations module that the deployed
	// service was built against.
	Version string `json:"version"`
	// Service is the deployment name the host derives from. It defaults
	// to the API name, and is only needed where the service is called
	// something else: the eidos API is served by eidos2.
	Service string `json:"service,omitempty"`
	// Services are the protobuf services this deployment serves. It is
	// only needed for an API split across several deployments, where it
	// is what decides which host a method's example is written for.
	Services []string `json:"services,omitempty"`
}

// UnmarshalJSON accepts either a single deployment object or an array of
// them, so that the common case of one deployment per API stays a plain
// object.
func (d *APIDeployments) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))

	dec.DisallowUnknownFields()

	if bytes.HasPrefix(bytes.TrimLeft(data, " \t\r\n"), []byte("[")) {
		var list []APIDeployment

		err := dec.Decode(&list)
		if err != nil {
			return fmt.Errorf("read the deployment list: %w", err)
		}

		*d = list

		return nil
	}

	var single APIDeployment

	err := dec.Decode(&single)
	if err != nil {
		return fmt.Errorf("read the deployment: %w", err)
	}

	*d = APIDeployments{single}

	return nil
}

// ServiceName is the deployment name the API's host derives from.
func (d APIDeployment) ServiceName(api string) string {
	if d.Service == "" {
		return api
	}

	return d.Service
}

// Serves reports whether the deployment serves a protobuf service. A
// deployment that names no services serves the whole API.
func (d APIDeployment) Serves(service string) bool {
	if len(d.Services) == 0 {
		return true
	}

	return slices.Contains(d.Services, service)
}

// LoadEnvironments reads and validates the environments file. A file that
// isn't there is not an error: the site is then rendered without deployment
// information, and with curl examples for the default tenant.
func LoadEnvironments(envPath string) (_ Environments, outErr error) {
	var env Environments

	f, err := os.Open(envPath)
	if os.IsNotExist(err) {
		return Environments{}, nil
	} else if err != nil {
		return Environments{}, fmt.Errorf("open environments file: %w", err)
	}

	defer internal.Close("environments file", f, &outErr)

	dec := json.NewDecoder(f)

	dec.DisallowUnknownFields()

	err = dec.Decode(&env)
	if err != nil {
		return Environments{}, fmt.Errorf("unmarshal environments: %w", err)
	}

	return env, nil
}

var (
	tenantNameExp  = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	serviceNameExp = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	protoNameExp   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Validate checks that the tenants name known APIs, parseable versions and
// host-safe service names, and that a split API says which deployment serves
// what.
func (e Environments) Validate(conf Config) error {
	known := make(map[string]bool)

	for _, mod := range conf.Modules {
		for api := range mod.APIs {
			known[api] = true
		}
	}

	for _, tenant := range slices.Sorted(maps.Keys(e.Tenants)) {
		if !tenantNameExp.MatchString(tenant) {
			return fmt.Errorf(
				"tenant %q: the name must be lowercase letters, digits and dashes",
				tenant)
		}

		tc := e.Tenants[tenant]

		for _, api := range slices.Sorted(maps.Keys(tc.APIs)) {
			if !known[api] {
				return fmt.Errorf(
					"tenant %q: unknown API %q", tenant, api)
			}

			err := tc.APIs[api].validate(api)
			if err != nil {
				return fmt.Errorf("tenant %q: API %q: %w",
					tenant, api, err)
			}
		}
	}

	return nil
}

func (d APIDeployments) validate(api string) error {
	if len(d) == 0 {
		return fmt.Errorf(
			"no deployments, drop the %q entry instead", api)
	}

	seen := make(map[string]bool)

	for _, dep := range d {
		_, err := semver.NewVersion(dep.Version)
		if err != nil {
			return fmt.Errorf("invalid version %q: %w", dep.Version, err)
		}

		name := dep.ServiceName(api)

		if !serviceNameExp.MatchString(name) {
			return fmt.Errorf(
				"service %q: the name must be lowercase letters, digits and dashes",
				name)
		}

		if seen[name] {
			return fmt.Errorf("the service %q is listed twice", name)
		}

		seen[name] = true

		for _, s := range dep.Services {
			if !protoNameExp.MatchString(s) {
				return fmt.Errorf(
					"service %q: %q is not a protobuf service name",
					name, s)
			}
		}

		if len(d) > 1 && len(dep.Services) == 0 {
			return fmt.Errorf(
				"service %q: an API split across deployments has to name the services each one serves",
				name)
		}
	}

	return nil
}

// TenantNames lists the configured tenants, the default tenant first and the
// rest in alphabetical order.
func (e Environments) TenantNames() []string {
	names := slices.Sorted(maps.Keys(e.Tenants))

	idx := slices.Index(names, DefaultTenant)
	if idx > 0 {
		names = slices.Insert(slices.Delete(names, idx, idx+1),
			0, DefaultTenant)
	}

	return names
}

// Deployments are the services that serve an API for a tenant in production,
// empty when the tenant doesn't run it.
func (e Environments) Deployments(tenant string, api string) APIDeployments {
	tc, ok := e.Tenants[tenant]
	if !ok {
		return nil
	}

	return tc.APIs[api]
}

// ProductionHost is the host a tenant serves a deployment on.
func ProductionHost(tenant string, service string) string {
	return fmt.Sprintf("https://%s.api.%s.%s", service, tenant, hostSuffix)
}

// StagingHost is the host a tenant serves the staging deployment on. Staging
// is assumed to run the module's latest version.
func StagingHost(tenant string, service string) string {
	return fmt.Sprintf("https://%s.api.stage.%s.%s",
		service, tenant, hostSuffix)
}
