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
	// Headers are the headers a JSON request sets, in the order a curl
	// example writes them.
	Headers []ProtocolHeader
	// Note is the per-API note from the configuration, if any.
	Note string
	// Comment is what is always true of the protocol, regardless of API.
	Comment string
}

type ProtocolHeader struct {
	Name  string
	Value string
}

// ProtocolNotice is a version boundary that the reader of this particular
// version page has to know about.
type ProtocolNotice struct {
	Text     string
	HRef     string
	LinkText string
}

// TenantDeployment is one row of the deployed versions table: one service
// that serves the API for one tenant. An API split across several deployments
// gets a row per deployment.
type TenantDeployment struct {
	Tenant string
	// Deployed is false when the API is absent from the tenant, which
	// means it isn't deployed there.
	Deployed bool
	// Service is the deployment name the host derives from.
	Service string
	// Services are the protobuf services this deployment serves, empty
	// when it serves the whole API.
	Services []string
	// Version is the module version the deployment runs in production.
	Version string
	// HRef links to that version's page, relative to the site root. It is
	// empty for a version that is not a tag of the module, which is what
	// a service built against an unreleased commit reports.
	HRef string
	// VersionNote says why an unlinked version has no page.
	VersionNote string
	// Protocols are the labels of the protocols the deployment serves.
	Protocols []string
	ProdHost  string

	// protocols are the protocol names behind Protocols, which is what
	// decides whether a method example can be written for this tenant.
	protocols []string
}

// ProtocolSet is the resolved protocol situation for one version of one API.
type ProtocolSet struct {
	// API is the API the set belongs to, which is the host name to
	// derive an example from when nothing is known about the tenants.
	API string
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
	// SelectionCSS shows the blocks that match the reader's protocol
	// choice, so that switching protocol is a data attribute on <html>
	// rather than script, and so that a reader without JavaScript sees
	// the default protocol rather than nothing.
	SelectionCSS template.CSS
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

// deployment is the tenant's deployment that serves a protobuf service.
func (ps ProtocolSet) deployment(
	tenant string, service string,
) (TenantDeployment, bool) {
	for _, d := range ps.Deployments {
		if d.Tenant != tenant || !d.Deployed {
			continue
		}

		if len(d.Services) == 0 || slices.Contains(d.Services, service) {
			return d, true
		}
	}

	return TenantDeployment{}, false
}

func protocolTemplate(name string) ProtocolInfo {
	switch name {
	case ProtocolConnect:
		return ProtocolInfo{
			Name:       ProtocolConnect,
			Label:      "Connect",
			PathPrefix: "",
			Headers: []ProtocolHeader{
				{Name: "Authorization", Value: "Bearer $TOKEN"},
				{Name: "Content-Type", Value: "application/json"},
				{Name: "Connect-Protocol-Version", Value: "1"},
			},
			Comment: "Connect-Protocol-Version is optional, but has to be" +
				" exactly 1 when it is set: curl and a plain fetch work" +
				" without it. Responses use lowerCamelCase field names," +
				" and requests are accepted with either spelling.",
		}
	case ProtocolTwirp:
		return ProtocolInfo{
			Name:       ProtocolTwirp,
			Label:      "Twirp (legacy)",
			PathPrefix: "/twirp",
			Headers: []ProtocolHeader{
				{Name: "Authorization", Value: "Bearer $TOKEN"},
				{Name: "Content-Type", Value: "application/json"},
			},
			Comment: "Twirp responses use the snake_case field names from" +
				" the .proto file, and requests are accepted with either" +
				" spelling.",
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

// resolveProtocols builds the protocol set for one version of an API. Every
// link it produces is relative to the site root: the templates run them
// through abs_url, which is what prefixes the base path.
func resolveProtocols(
	api string, conf APIConfig, module *Module,
	version *ModuleVersion, env Environments,
) ProtocolSet {
	set := ProtocolSet{API: api}

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

	set.Notices = protocolNotices(api, conf, module, version)
	set.Deployments = tenantDeployments(api, conf, module, env)

	for _, d := range set.Deployments {
		if d.Deployed && !slices.Contains(set.Tenants, d.Tenant) {
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

	set.SelectionCSS = protocolCSS(set)

	return set
}

func protocolNotices(
	api string, conf APIConfig, module *Module,
	version *ModuleVersion,
) []ProtocolNotice {
	var notices []ProtocolNotice

	versionHRef := func(tag string) string {
		return fmt.Sprintf("/apis/%s/%s", api, tag)
	}

	connect, hasConnect := conf.gate(ProtocolConnect)
	if hasConnect && connect.From != nil && version.Version.LessThan(connect.From) {
		notices = append(notices, ProtocolNotice{
			Text: "This version is served over Twirp only." +
				" Connect is available from",
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
	api string, conf APIConfig, module *Module, env Environments,
) []TenantDeployment {
	var deployments []TenantDeployment

	for _, tenant := range env.TenantNames() {
		deps := env.Deployments(tenant, api)
		if len(deps) == 0 {
			deployments = append(deployments,
				TenantDeployment{Tenant: tenant})

			continue
		}

		for _, dep := range deps {
			deployments = append(deployments,
				tenantDeployment(api, conf, module, tenant, dep))
		}
	}

	return deployments
}

func tenantDeployment(
	api string, conf APIConfig, module *Module,
	tenant string, dep APIDeployment,
) TenantDeployment {
	service := dep.ServiceName(api)

	d := TenantDeployment{
		Tenant:   tenant,
		Deployed: true,
		Service:  service,
		Services: dep.Services,
		Version:  dep.Version,
		ProdHost: ProductionHost(tenant, service),
	}

	// A service can be built against a commit rather than a tag, and then
	// there is no version page to link to. Say so rather than linking to a
	// directory generation never writes.
	if _, tagged := module.VersionLookup[dep.Version]; tagged {
		d.HRef = fmt.Sprintf("/apis/%s/%s", api, dep.Version)
	} else {
		d.VersionNote = "not a release of " + module.Name +
			", so it has no page here"
	}

	v, err := semver.NewVersion(dep.Version)
	if err == nil {
		for _, name := range protocolsAt(conf, v) {
			d.protocols = append(d.protocols, name)
			d.Protocols = append(d.Protocols,
				protocolTemplate(name).Label)
		}
	}

	return d
}

// protocolCSS renders the rules that hide the blocks belonging to a protocol
// the reader hasn't selected. The rules only ever hide, so an element with no
// protocol of its own is untouched, and the last rule is the no-JavaScript
// fallback: nothing sets data-protocol then, and the default protocol is what
// shows.
func protocolCSS(set ProtocolSet) template.CSS {
	var b strings.Builder

	for _, p := range set.Available {
		fmt.Fprintf(&b,
			"html[data-protocol=%q] [data-protocol-block]:not([data-protocol-block=%q]){display:none}",
			p.Name, p.Name)
	}

	fmt.Fprintf(&b,
		"html:not([data-protocol]) [data-protocol-block]:not([data-protocol-block=%q]){display:none}",
		set.Default)

	return template.CSS(b.String())
}

// requestBodyFile is what the generated curl invocations post. The request
// body is the same for every protocol and tenant, so the page renders it once
// and the commands read it from a file rather than carrying eight copies of a
// message that runs to tens of kilobytes.
const requestBodyFile = "request.json"

// MethodExample is the invocation of one method for one protocol and one
// tenant.
type MethodExample struct {
	Protocol string
	Tenant   string
	// Key is what the generated CSS matches to show this example when the
	// reader picks the protocol and the tenant.
	Key string
	// Command is the curl invocation, empty when Notice is set.
	Command string
	// StagingHost is the host the same call goes to on staging, which is
	// assumed to run the module's latest version.
	StagingHost string
	// Notice replaces the command when the tenant's deployment cannot be
	// called this way, which is what keeps a method page from writing a
	// Connect example for a tenant that is still on Twirp.
	Notice string
}

func exampleKey(protocol string, tenant string) string {
	return protocol + "|" + tenant
}

// methodExamples renders one invocation per protocol and tenant. A tenant
// whose deployed version doesn't serve the rendered protocol, or that doesn't
// run the service at all, gets a one-line notice instead of a command that
// would fail.
func methodExamples(
	set ProtocolSet, pkg string, service string, method string,
) []MethodExample {
	var examples []MethodExample

	for _, p := range set.Available {
		procedure := fmt.Sprintf("%s/%s.%s/%s",
			p.PathPrefix, pkg, service, method)

		for _, tenant := range set.Tenants {
			examples = append(examples,
				methodExample(set, p, tenant, service, procedure))
		}
	}

	return examples
}

func methodExample(
	set ProtocolSet, p ProtocolInfo,
	tenant string, service string, procedure string,
) MethodExample {
	e := MethodExample{
		Protocol: p.Name,
		Tenant:   tenant,
		Key:      exampleKey(p.Name, tenant),
	}

	// Without an environments file there is nothing to check the example
	// against, and the host is derived from the tenant name.
	if len(set.Deployments) == 0 {
		e.Command = curlCommand(
			ProductionHost(tenant, set.API)+procedure, p)
		e.StagingHost = StagingHost(tenant, set.API)

		return e
	}

	d, ok := set.deployment(tenant, service)

	switch {
	case !ok:
		e.Notice = fmt.Sprintf("%s does not run %s.", tenant, service)
	case !slices.Contains(d.protocols, p.Name):
		e.Notice = fmt.Sprintf(
			"%s runs %s %s, which is not served over %s.",
			tenant, d.Service, d.Version, p.Label)
	default:
		e.Command = curlCommand(d.ProdHost+procedure, p)
		e.StagingHost = StagingHost(tenant, d.Service)
	}

	return e
}

func curlCommand(url string, p ProtocolInfo) string {
	var b strings.Builder

	fmt.Fprintf(&b, "curl %s \\\n", url)

	for _, h := range p.Headers {
		fmt.Fprintf(&b, "  -H %q \\\n", h.Name+": "+h.Value)
	}

	fmt.Fprintf(&b, "  -d @%s", requestBodyFile)

	return b.String()
}

// exampleCSS renders the rules that show the one example matching the
// reader's protocol and tenant, and hide the shared request body when that
// combination has no command to run it with. The last two rules are the
// no-JavaScript fallback, where neither attribute is set.
func exampleCSS(set ProtocolSet, examples []MethodExample) template.CSS {
	var b strings.Builder

	preferred := exampleKey(set.Default, set.PreferredTenant())

	for _, e := range examples {
		fmt.Fprintf(&b,
			"html[data-protocol=%q][data-tenant=%q] [data-example]:not([data-example=%q]){display:none}",
			e.Protocol, e.Tenant, e.Key)

		if e.Command != "" {
			continue
		}

		fmt.Fprintf(&b,
			"html[data-protocol=%q][data-tenant=%q] [data-example-body]{display:none}",
			e.Protocol, e.Tenant)

		if e.Key == preferred {
			b.WriteString(
				"html:not([data-protocol]) [data-example-body]{display:none}")
		}
	}

	fmt.Fprintf(&b,
		"html:not([data-protocol]) [data-example]:not([data-example=%q]){display:none}",
		preferred)

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
