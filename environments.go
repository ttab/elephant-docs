package elephantdocs

import (
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
// production host is "<api>.api.<tenant>.<hostSuffix>", and a staging host
// "<api>.api.stage.<tenant>.<hostSuffix>".
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
	// APIs maps an API name to the module version tag the tenant runs in
	// production. An API that isn't in the map is not deployed for the
	// tenant.
	APIs map[string]string `json:"apis"`
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

var tenantNameExp = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Validate checks that the tenants name known APIs and parseable versions.
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

			_, err := semver.NewVersion(tc.APIs[api])
			if err != nil {
				return fmt.Errorf(
					"tenant %q: API %q: invalid version %q: %w",
					tenant, api, tc.APIs[api], err)
			}
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

// DeployedVersion is the version tag of an API that a tenant runs in
// production, if it runs it at all.
func (e Environments) DeployedVersion(tenant string, api string) (string, bool) {
	tc, ok := e.Tenants[tenant]
	if !ok {
		return "", false
	}

	tag, ok := tc.APIs[api]

	return tag, ok
}

// ProductionHost is the host a tenant serves an API on.
func ProductionHost(tenant string, api string) string {
	return fmt.Sprintf("https://%s.api.%s.%s", api, tenant, hostSuffix)
}

// StagingHost is the host a tenant serves the staging deployment of an API
// on. Staging is assumed to run the module's latest version.
func StagingHost(tenant string, api string) string {
	return fmt.Sprintf("https://%s.api.stage.%s.%s", api, tenant, hostSuffix)
}
