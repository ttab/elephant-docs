package elephantdocs

import (
	"errors"
	"fmt"
	"html/template"
	"path"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// ProtocolInfo is a protocol that a rendered version of an API is served
// over. It is resolved once per (API, version) so that templates never have
// to compare versions.
type ProtocolInfo struct {
	// Name is the configuration key, "connect" or "twirp".
	Name string
	// Label is the display name.
	Label string
	// PathPrefix is what precedes the "<package>.<Service>/<Method>" path.
	PathPrefix string
	// Default marks the protocol a reader is shown first.
	Default bool
	// ContentType is the JSON content type for the protocol.
	ContentType string
	// BinaryContentType is the protobuf content type.
	BinaryContentType string
	// Headers are the headers a JSON request sets, in the order a curl
	// example writes them.
	Headers []ProtocolHeader
	// Note is the per-API note from the configuration, if any.
	Note string
	// Comment is what is always true of the protocol, regardless of API.
	Comment string
}

type ProtocolHeader struct {
	Name     string
	Value    string
	Optional bool
}

// ProtocolNotice is a version boundary that the reader of this particular
// version page has to know about.
type ProtocolNotice struct {
	Text     string
	HRef     string
	LinkText string
}

// TenantDeployment is one row of the deployed versions table.
type TenantDeployment struct {
	Tenant string
	// Deployed is false when the API is absent from the tenant, which
	// means it isn't deployed there.
	Deployed bool
	// Version is the module version the tenant runs in production.
	Version string
	// HRef links to that version's page.
	HRef string
	// Protocols are the protocols the deployed version serves.
	Protocols []string
	ProdHost  string
	StageHost string
}

// ProtocolSet is the resolved protocol situation for one version of one API.
type ProtocolSet struct {
	API     string
	Version string
	// Default is the name of the protocol shown first.
	Default string
	// Available are the protocols this version is served over.
	Available []ProtocolInfo
	// Notices are the version boundaries around this version.
	Notices []ProtocolNotice
	// Deployments is the per tenant production deployment of the API.
	Deployments []TenantDeployment
	// Tenants are the tenants examples are rendered for.
	Tenants []string
	// TenantCSS drives the tenant picker: one rule per tenant, so that
	// switching tenant is a data attribute on <html> rather than script.
	TenantCSS template.CSS
	// DocHRef links to the site's protocol reference.
	DocHRef string
}

// Multiple reports whether the reader has a choice to make.
func (ps ProtocolSet) Multiple() bool {
	return len(ps.Available) > 1
}

// PreferredTenant is the tenant examples are shown for when the reader hasn't
// picked one.
func (ps ProtocolSet) PreferredTenant() string {
	if slices.Contains(ps.Tenants, DefaultTenant) {
		return DefaultTenant
	}

	if len(ps.Tenants) > 0 {
		return ps.Tenants[0]
	}

	return DefaultTenant
}

// Has reports whether the set contains a protocol.
func (ps ProtocolSet) Has(name string) bool {
	for _, p := range ps.Available {
		if p.Name == name {
			return true
		}
	}

	return false
}

func protocolTemplate(name string) ProtocolInfo {
	switch name {
	case ProtocolConnect:
		return ProtocolInfo{
			Name:              ProtocolConnect,
			Label:             "Connect",
			PathPrefix:        "",
			ContentType:       "application/json",
			BinaryContentType: "application/proto",
			Headers: []ProtocolHeader{
				{Name: "Authorization", Value: "Bearer $TOKEN"},
				{Name: "Content-Type", Value: "application/json"},
				{
					Name:     "Connect-Protocol-Version",
					Value:    "1",
					Optional: true,
				},
			},
			Comment: "Connect-Protocol-Version is optional:" +
				" curl and a plain fetch work without it.",
		}
	case ProtocolTwirp:
		return ProtocolInfo{
			Name:              ProtocolTwirp,
			Label:             "Twirp (legacy)",
			PathPrefix:        "/twirp",
			ContentType:       "application/json",
			BinaryContentType: "application/protobuf",
			Headers: []ProtocolHeader{
				{Name: "Authorization", Value: "Bearer $TOKEN"},
				{Name: "Content-Type", Value: "application/json"},
			},
		}
	}

	return ProtocolInfo{Name: name, Label: name}
}

// protocolsAt resolves which protocols a version of an API is served over.
//
// An API that declares no protocols is Twirp only, which is how every API was
// documented before the gate existed. Connect has to be declared: the
// declarations carrying a Connect adapter says nothing about whether the
// deployed service mounts it.
func protocolsAt(conf APIConfig, v *semver.Version) []string {
	var available []string

	for _, name := range protocolOrder {
		gate, declared := conf.gate(name)
		if !declared {
			if name == ProtocolTwirp {
				available = append(available, name)
			}

			continue
		}

		if gate.From != nil && v.LessThan(gate.From) {
			continue
		}

		if gate.Until != nil && !v.LessThan(gate.Until) {
			continue
		}

		available = append(available, name)
	}

	return available
}

// resolveProtocols builds the protocol set for one version of an API.
func resolveProtocols(
	api string, conf APIConfig, module *Module,
	version *ModuleVersion, env Environments, basePath string,
) ProtocolSet {
	set := ProtocolSet{
		API:     api,
		Version: version.Tag,
		DocHRef: basePath + "/protocols",
	}

	for i, name := range protocolsAt(conf, version.Version) {
		info := protocolTemplate(name)

		info.Default = i == 0

		if gate, ok := conf.gate(name); ok {
			info.Note = gate.Note
		}

		set.Available = append(set.Available, info)
	}

	if len(set.Available) > 0 {
		set.Default = set.Available[0].Name
	}

	set.Notices = protocolNotices(api, conf, module, version, basePath)
	set.Deployments = tenantDeployments(api, conf, env, basePath)

	for _, d := range set.Deployments {
		if d.Deployed {
			set.Tenants = append(set.Tenants, d.Tenant)
		}
	}

	// Examples still need a host when the environments file says nothing
	// about the API, and the host is derived from the tenant name either
	// way.
	if len(set.Tenants) == 0 {
		set.Tenants = []string{DefaultTenant}
		set.Deployments = nil
	}

	set.TenantCSS = tenantCSS(set.Tenants)

	return set
}

func protocolNotices(
	api string, conf APIConfig, module *Module,
	version *ModuleVersion, basePath string,
) []ProtocolNotice {
	var notices []ProtocolNotice

	versionHRef := func(tag string) string {
		return fmt.Sprintf("%s/apis/%s/%s", basePath, api, tag)
	}

	connect, hasConnect := conf.gate(ProtocolConnect)
	if hasConnect && connect.From != nil && version.Version.LessThan(connect.From) {
		notices = append(notices, ProtocolNotice{
			Text: fmt.Sprintf(
				"This version is served over Twirp only. Connect is available from %s.",
				connect.FromTag),
			HRef:     versionHRef(connect.FromTag),
			LinkText: connect.FromTag,
		})
	}

	twirp, hasTwirp := conf.gate(ProtocolTwirp)
	if hasTwirp && twirp.Until != nil && !version.Version.LessThan(twirp.Until) {
		notice := ProtocolNotice{
			Text: fmt.Sprintf(
				"The /twirp/ paths were removed in %s.", twirp.UntilTag),
		}

		last := lastVersionWith(module, conf, ProtocolTwirp)
		if last != "" {
			notice.Text += " The last version that served them was"
			notice.HRef = versionHRef(last)
			notice.LinkText = last
		}

		notices = append(notices, notice)
	}

	return notices
}

// lastVersionWith is the newest tagged version that is served over a
// protocol, which is what a reader who has landed past a removal wants a link
// to.
func lastVersionWith(module *Module, conf APIConfig, protocol string) string {
	// Module.Versions is sorted newest first.
	for _, v := range module.Versions {
		if v.IsPrerelease {
			continue
		}

		for _, name := range protocolsAt(conf, v.Version) {
			if name == protocol {
				return v.Tag
			}
		}
	}

	return ""
}

func tenantDeployments(
	api string, conf APIConfig, env Environments, basePath string,
) []TenantDeployment {
	var deployments []TenantDeployment

	for _, tenant := range env.TenantNames() {
		d := TenantDeployment{
			Tenant:    tenant,
			ProdHost:  ProductionHost(tenant, api),
			StageHost: StagingHost(tenant, api),
		}

		tag, deployed := env.DeployedVersion(tenant, api)
		if deployed {
			d.Deployed = true
			d.Version = tag
			d.HRef = fmt.Sprintf("%s/apis/%s/%s", basePath, api, tag)

			v, err := semver.NewVersion(tag)
			if err == nil {
				for _, name := range protocolsAt(conf, v) {
					d.Protocols = append(d.Protocols,
						protocolTemplate(name).Label)
				}
			}
		}

		deployments = append(deployments, d)
	}

	return deployments
}

// tenantCSS renders the rules that show the blocks for the selected tenant.
// Tenant names are validated at load, so they are safe to write into a
// selector.
func tenantCSS(tenants []string) template.CSS {
	var b strings.Builder

	b.WriteString("[data-tenant-block]{display:none}")

	for _, t := range tenants {
		fmt.Fprintf(&b,
			"html[data-tenant=%q] [data-tenant-block=%q]{display:revert}",
			t, t)
	}

	return template.CSS(b.String())
}

// checkProtocolGates verifies the gate against the module tree: a claim that a
// version serves Connect is refused when that version doesn't even carry the
// generated adapters, and Twirp code that outlives its removal is a warning.
//
// The gate is a statement about a deployment in another repository, so this
// can only catch the claims that are impossible, not the ones that are merely
// early or late.
func checkProtocolGates(
	module *Module, uiPrintln func(format string, a ...any),
) error {
	for api, conf := range module.APIs {
		connect, ok := conf.gate(ProtocolConnect)
		if ok && connect.From != nil {
			err := checkConnectFrom(module, api, connect)
			if err != nil {
				return err
			}
		}

		twirp, ok := conf.gate(ProtocolTwirp)
		if ok && twirp.Until != nil {
			checkTwirpUntil(module, api, twirp, uiPrintln)
		}
	}

	return nil
}

func checkConnectFrom(module *Module, api string, gate protocolGate) error {
	version, ok := module.VersionLookup[gate.FromTag]
	if !ok {
		return fmt.Errorf(
			"%s: connect.from %q is not a tagged version of %s",
			api, gate.FromTag, module.Name)
	}

	dir := path.Join(module.APIPath(api), api+"connect")

	exists, err := treeHasDir(version, dir)
	if err != nil {
		return fmt.Errorf("%s: look for %s in %s: %w",
			api, dir, gate.FromTag, err)
	}

	if !exists {
		return fmt.Errorf(
			"%s: connect.from is %s, but that version has no %s directory",
			api, gate.FromTag, dir)
	}

	return nil
}

func checkTwirpUntil(
	module *Module, api string, gate protocolGate,
	uiPrintln func(format string, a ...any),
) {
	file := path.Join(module.APIPath(api), "service.twirp.go")

	for _, v := range module.Versions {
		if v.Version.LessThan(gate.Until) {
			continue
		}

		exists, err := treeHasFile(v, file)
		if err != nil || !exists {
			continue
		}

		uiPrintln(
			"warning: %s declares twirp.until %s, but %s still contains %s",
			api, gate.UntilTag, v.Tag, file)

		return
	}
}

func treeHasDir(version *ModuleVersion, dir string) (bool, error) {
	tree, err := version.Commit.Tree()
	if err != nil {
		return false, fmt.Errorf("get commit tree: %w", err)
	}

	_, err = tree.Tree(dir)
	if errors.Is(err, object.ErrDirectoryNotFound) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("look up directory: %w", err)
	}

	return true, nil
}

func treeHasFile(version *ModuleVersion, filePath string) (bool, error) {
	tree, err := version.Commit.Tree()
	if err != nil {
		return false, fmt.Errorf("get commit tree: %w", err)
	}

	_, err = tree.File(filePath)
	if errors.Is(err, object.ErrFileNotFound) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("look up file: %w", err)
	}

	return true, nil
}
