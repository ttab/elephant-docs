package elephantdocs

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/ttab/elephant-docs/internal"
	"github.com/yuin/goldmark"
	"golang.org/x/mod/modfile"
	"golang.org/x/net/html"
	"golang.org/x/sync/errgroup"
)

//go:embed templates
var templateFS embed.FS

//go:embed assets
var assetFS embed.FS

type API struct {
	Name          string
	Title         string
	Version       string
	Module        string
	LatestVersion string
	Data          APIData
}

type APIData struct {
	Declarations []ProtoDeclarations
	Dependencies map[string]API
}

type Module struct {
	Name          string
	Repo          *git.Repository `json:"-"`
	Versions      []*ModuleVersion
	LatestVersion *ModuleVersion
	VersionLookup map[string]*ModuleVersion `json:"-"`
	APIs          map[string]APIConfig
	Include       map[string]IncludeConfig
}

type ModuleVersion struct {
	Tag                string
	Commit             *object.Commit  `json:"-"`
	Version            *semver.Version `json:"-"`
	IsPrerelease       bool
	DependencyVersions map[string]string
	Log                []*object.Commit `json:"-"`
}

func VersionsAtCommit(id plumbing.Hash, versions []*ModuleVersion) []*ModuleVersion {
	var l []*ModuleVersion

	for _, v := range versions {
		if v.Commit.Hash.Equal(id) {
			l = append(l, v)
		}
	}

	return l
}

func Generate(
	ctx context.Context, outDir string, basePath string, conf Config,
	uiPrintln func(format string, a ...any),
) error {
	apiConf := make(map[string]APIConfig)
	modules := make(map[string]*Module)

	rootPath := basePath
	if rootPath == "" {
		rootPath = "/"
	}

	if !strings.HasSuffix(rootPath, "/") {
		rootPath += "/"
	}

	rootURL, err := url.Parse(rootPath)
	if err != nil {
		return fmt.Errorf("invalid base path: %w", err)
	}

	tpl := template.New("templates")

	funcs := template.FuncMap{
		"message_href": func(ref MessageRef) string {
			return fmt.Sprintf("#message-%s", ref.Message)
		},
		"commit_message": func(message string) template.HTML {
			lines := strings.Split(message, "\n")

			for i, l := range lines {
				lines[i] = html.EscapeString(l)
			}

			return template.HTML(strings.Join(lines, "<br/>"))
		},
		"attr": func(name string) template.HTMLAttr {
			return template.HTMLAttr(name)
		},
		"base_path": func() string {
			return basePath
		},
		"abs_url": func(targetUrl string) string {
			target, err := url.Parse(targetUrl)
			if err != nil {
				panic("bad URL: " + targetUrl)
			}

			if target.Scheme != "" {
				return targetUrl
			}

			return rootURL.JoinPath(targetUrl).String()
		},
	}

	tpl.Funcs(funcs)

	tpl, err = tpl.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return fmt.Errorf("parse templates: %w", err)
	}

	for _, mod := range conf.Modules {
		uiPrintln("Cloning %s", mod.Name)

		module, err := newModule(mod)
		if err != nil {
			return fmt.Errorf("create module: %w", err)
		}

		uiPrintln("")

		modules[module.Name] = module

		maps.Copy(apiConf, mod.APIs)
	}

	var apiMenu []MenuItem

	for _, module := range modules {
		for api := range module.APIs {
			conf := apiConf[api]

			apiMenu = append(apiMenu, MenuItem{
				Title: conf.Title,
				HRef: fmt.Sprintf("/apis/%s/%s",
					api, module.LatestVersion.Tag),
			})
		}
	}

	slices.SortFunc(apiMenu, func(a MenuItem, b MenuItem) int {
		return strings.Compare(a.Title, b.Title)
	})

	// Prepend the home item.
	apiMenu = append([]MenuItem{
		{
			Title: "Home",
			HRef:  "/",
		},
	}, apiMenu...)

	jobs := make(chan collectJob)
	results := make(chan collectJob)

	defer close(results)

	grp, gCtx := errgroup.WithContext(ctx)

	// Copy all assets.
	grp.Go(func() error {
		err := os.CopyFS(filepath.Join(outDir), assetFS)
		if err != nil {
			return fmt.Errorf("write assets directory: %w", err)
		}

		return nil
	})

	grp.Go(func() error {
		localTpl, err := tpl.Clone()
		if err != nil {
			return fmt.Errorf("clone templates: %w", err)
		}

		markdown, err := os.ReadFile("docs/README.md")
		if err != nil {
			return fmt.Errorf("read index README.md: %w", err)
		}

		var htmlBuf bytes.Buffer

		err = goldmark.Convert(markdown, &htmlBuf)
		if err != nil {
			return fmt.Errorf("render markdown: %w", err)
		}

		tailwindHTML, err := tailwindify(&htmlBuf)
		if err != nil {
			return fmt.Errorf("add tailwind classes: %w", err)
		}

		page := Page{
			Title: "Start",
			Menu:  apiMenu,
			Contents: MarkdownPage{
				HTML: tailwindHTML,
			},
		}

		err = renderPage(
			outDir,
			localTpl, "markdown_page.html", page)
		if err != nil {
			return fmt.Errorf(
				"render markdown page: %w", err)
		}

		return nil
	})

	// Queue the rendering of each module version.
	grp.Go(func() error {
		defer close(jobs)

		for _, module := range modules {
			for _, version := range module.Versions {
				job := collectJob{
					Module:  module,
					Version: version,
				}

				select {
				case jobs <- job:
				case <-gCtx.Done():
					return ctx.Err()
				}
			}
		}

		return nil
	})

	// Start workers that will render module versions.
	for range 16 {
		grp.Go(func() error {
			for job := range jobs {
				err := renderModuleVersionPages(
					outDir, basePath, modules, job, tpl, funcs,
					apiConf, apiMenu,
				)
				if err != nil {
					return err
				}
			}

			return nil
		})
	}

	// Render landing pages (and changelogs) for each module.
	grp.Go(func() error {
		modTemplate, err := tpl.Clone()
		if err != nil {
			return fmt.Errorf("clone templates: %w", err)
		}

		for _, module := range modules {
			for api := range module.APIs {
				err := renderAPILandingPages(modTemplate, outDir, basePath, apiMenu, module, api)
				if err != nil {
					return fmt.Errorf("render %s landing page: %w",
						api, err)
				}
			}
		}

		return nil
	})

	err = grp.Wait()
	if err != nil {
		return fmt.Errorf("render documentation: %w", err)
	}

	return nil
}

func tailwindify(buf *bytes.Buffer) (template.HTML, error) {
	doc, err := html.Parse(buf)
	if err != nil {
		return "", fmt.Errorf("parse HTML: %w", err)
	}

	classes := map[string]string{
		"h1": "mb-4 text-4xl font-extrabold leading-none tracking-tight text-gray-900 md:text-5xl lg:text-6xl",
		"h2": "text-4xl font-extrabold",
		"h3": "text-3xl font-extrabold",
		"h4": "text-2xl font-extrabold",
		"h5": "text-1xl font-extrabold",
		"a":  "uk-link",
		"ul": "uk-list uk-list-circle",
	}

	for n := range doc.Descendants() {
		if n.Type == html.ElementNode {
			class, ok := classes[n.Data]
			if !ok {
				continue
			}

			n.Attr = append(n.Attr, html.Attribute{
				Key: "class",
				Val: class,
			})
		}
	}

	var out bytes.Buffer

	err = html.Render(&out, doc)
	if err != nil {
		return "", fmt.Errorf("render modified HTML: %w", err)
	}

	return template.HTML(out.String()), nil
}

type MarkdownPage struct {
	HTML template.HTML
}

func renderModuleVersionPages(
	outDir string,
	basePath string,
	modules map[string]*Module,
	job collectJob,
	tpl *template.Template,
	funcs template.FuncMap,
	apiConf map[string]APIConfig,
	apiMenu []MenuItem,
) error {
	module := job.Module
	version := job.Version

	apis, err := collectAPIData(modules, module, version)
	if err != nil {
		return fmt.Errorf("render %s@%s: %w",
			module.Name, version.Tag, err)
	}

	localTpl, err := tpl.Clone()
	if err != nil {
		return fmt.Errorf("create local templates: %w", err)
	}

	dir := filepath.Join(outDir, module.Name, version.Tag)

	err = os.MkdirAll(dir, 0o770)
	if err != nil {
		return fmt.Errorf("create version directory: %w", err)
	}

	for api, data := range apis {
		conf, ok := apiConf[api]
		if !ok {
			return fmt.Errorf("missing config for %q", api)
		}

		localFuncs := maps.Clone(funcs)

		localFuncs["message_href"] = apiMessageHRef(data, basePath)

		localTpl.Funcs(localFuncs)

		d := API{
			Name:          api,
			Title:         conf.Title,
			Version:       version.Tag,
			Module:        module.Name,
			LatestVersion: module.LatestVersion.Tag,
			Data:          data,
		}

		apiDir := filepath.Join("apis", api)

		versionDir := filepath.Join(
			apiDir, version.Tag,
		)

		menu := slices.Clone(apiMenu)

		for i := range menu {
			if strings.HasPrefix(menu[i].HRef, "/"+apiDir) {
				menu[i].Active = true

				break
			}
		}

		versionOutDir := filepath.Join(outDir, versionDir)

		err = os.MkdirAll(versionOutDir, 0o770)
		if err != nil {
			return fmt.Errorf("create version dir: %w", err)
		}

		page := Page{
			Title:    d.Title,
			Menu:     menu,
			Contents: d,
			Breadcrumb: []MenuItem{
				{
					Title: "Home",
					HRef:  "/",
				},
				{
					Title: conf.Title,
					HRef:  "/" + apiDir,
				},
				{
					Title: version.Tag,
					HRef:  "/" + versionDir,
				},
			},
		}

		err = renderPage(
			versionOutDir,
			localTpl, "api_version.html", page)
		if err != nil {
			return fmt.Errorf(
				"render version page for %s@%s: %w",
				api, version.Tag, err)
		}
	}

	return nil
}

type APILandingPage struct {
	Name             string
	Version          string
	RedirectLocation string
}

type ChangelogPage struct {
	Module   *Module
	Name     string
	Title    string
	Versions []*ModuleVersion
}

func renderAPILandingPages(
	tpl *template.Template,
	outDir string, basePath string,
	menu []MenuItem,
	module *Module, api string,
) error {
	conf, ok := module.APIs[api]
	if !ok {
		return errors.New("missing API configuration")
	}

	apiDir := filepath.Join("apis", api)

	menu = slices.Clone(menu)

	for i := range menu {
		if strings.HasPrefix(menu[i].HRef, "/"+apiDir) {
			menu[i].Active = true

			break
		}
	}

	apiOutDir := filepath.Join(outDir, apiDir)

	log, err := getChangelog(module, api)
	if err != nil {
		return fmt.Errorf("get module changelog: %w", err)
	}

	changelogPage := Page{
		Title: conf.Title,
		Menu:  menu,
		Contents: ChangelogPage{
			Module:   module,
			Name:     api,
			Title:    conf.Title,
			Versions: log,
		},
		Breadcrumb: []MenuItem{
			{
				Title: "Home",
				HRef:  "/",
			},
			{
				Title: conf.Title,
				HRef:  "/" + apiDir,
			},
			{
				Title: "Changelog",
			},
		},
	}

	err = renderPage(
		filepath.Join(apiOutDir, "changelog"),
		tpl, "api_changelog.html", changelogPage)
	if err != nil {
		return fmt.Errorf(
			"render api page for %s: %w",
			api, err)
	}

	redirectURL := fmt.Sprintf("%s/apis/%s/%s",
		basePath, api, module.LatestVersion.Tag)

	redirectPage := Page{
		Title: conf.Title,
		Menu:  menu,
		MetaTags: []map[string]string{
			{
				"http-equiv": "refresh",
				"content":    fmt.Sprintf("0; url=%s", redirectURL),
			},
		},
		Contents: APILandingPage{
			Name:             api,
			Version:          module.LatestVersion.Tag,
			RedirectLocation: redirectURL,
		},
		Breadcrumb: []MenuItem{
			{
				Title: "Home",
				HRef:  "/",
			},
			{
				Title: conf.Title,
				HRef:  "/" + apiDir,
			},
			{
				Title: "Changelog",
			},
		},
	}

	err = renderPage(
		apiOutDir,
		tpl, "api_redirect.html", redirectPage)
	if err != nil {
		return fmt.Errorf(
			"render redirect page for %s: %w",
			api, err)
	}

	return nil
}

func apiMessageHRef(data APIData, basePath string) func(ref MessageRef) string {
	return func(ref MessageRef) string {
		if ref.Package == "" {
			return fmt.Sprintf("#message-%s", ref.Message)
		}

		for _, decl := range data.Declarations {
			if decl.Package == ref.Package {
				return fmt.Sprintf("#message-%s", ref.Message)
			}
		}

		for dApi, dep := range data.Dependencies {
			for _, decl := range dep.Data.Declarations {
				if decl.Package != ref.Package {
					continue
				}

				return fmt.Sprintf("%s/apis/%s/%s/#message-%s",
					basePath, dApi, dep.Version, ref.Message)
			}
		}

		return ""
	}
}

func renderPage(
	outDir string,
	tpl *template.Template,
	templateName string,
	page Page,
) (outErr error) {
	err := os.MkdirAll(outDir, 0o700)
	if err != nil {
		return fmt.Errorf("create %q: %w", outDir, err)
	}

	err = internal.MarshalFile(
		filepath.Join(outDir, "index.json"), page.Contents)
	if err != nil {
		return fmt.Errorf("write page data: %w", err)
	}

	indexPath := filepath.Join(outDir, "index.html")

	file, err := os.Create(indexPath)
	if err != nil {
		return fmt.Errorf("create index.html: %w", err)
	}

	defer internal.Close("index.html", file, &outErr)

	err = tpl.ExecuteTemplate(file, templateName, page)
	if err != nil {
		return fmt.Errorf("render page: %w", err)
	}

	return nil
}

type collectJob struct {
	Module  *Module
	Version *ModuleVersion
}

func collectAPIData(
	modules map[string]*Module,
	module *Module, version *ModuleVersion,
) (map[string]APIData, error) {
	dependencies, err := readDepVersions(version.Commit, module.Include)
	if err != nil {
		return nil, fmt.Errorf("resolve dependency versions: %w", err)
	}

	files := map[string]ProtoHandle{}

	for _, dep := range dependencies {
		depMod, ok := modules[dep.Module]
		if !ok {
			return nil, fmt.Errorf("unknown module %q", dep.Module)
		}

		_, ok = depMod.APIs[dep.API]
		if !ok {
			return nil, fmt.Errorf("module %q doesn't expose the API %q",
				dep.Module, dep.API)
		}

		depVersion, ok := depMod.VersionLookup[dep.Version]
		if !ok {
			return nil, fmt.Errorf("no tagged version %q of %q",
				dep.Version, dep.Module)
		}

		protos, err := parseProtoFiles(depVersion, dep.API)
		if err != nil {
			return nil, fmt.Errorf("parse files in dependency %q in %q: %w",
				dep.API, dep.Module, err)
		}

		for _, pd := range protos {
			files[pd.File] = ProtoHandle{
				API:     dep.API,
				Module:  dep.Module,
				Version: dep.Version,
				Proto:   pd,
			}
		}
	}

	apis := map[string][]ProtoDeclarations{}

	for apiName := range module.APIs {
		protos, err := parseProtoFiles(version, apiName)
		if err != nil {
			return nil, fmt.Errorf("parse proto files: %w", err)
		}

		// All APIs are not always present in all versions of a module.
		if len(protos) == 0 {
			continue
		}

		for _, pd := range protos {
			files[pd.File] = ProtoHandle{
				API:     apiName,
				Version: version.Tag,
				Module:  module.Name,
				Proto:   pd,
			}
		}

		apis[apiName] = protos
	}

	apiData := make(map[string]APIData)

	for apiName, protos := range apis {
		data := APIData{
			Declarations: protos,
			Dependencies: make(map[string]API),
		}

		for _, p := range protos {
			for _, f := range p.Imports {
				h, ok := files[f]
				if !ok {
					return nil, fmt.Errorf("missing dependency %q", f)
				}

				data.Dependencies[h.Proto.Package] = API{
					Name:    h.API,
					Version: h.Version,
					Module:  h.Module,
					Data: APIData{
						Declarations: []ProtoDeclarations{
							h.Proto,
						},
					},
				}
			}
		}

		apiData[apiName] = data
	}

	return apiData, nil
}

type depSpec struct {
	API     string
	Module  string
	Version string
}

func readDepVersions(
	commit *object.Commit, include map[string]IncludeConfig,
) (map[string]depSpec, error) {
	if len(include) == 0 {
		return map[string]depSpec{}, nil
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("get tag tree: %w", err)
	}

	modF, err := tree.File("go.mod")
	if err != nil {
		return nil, fmt.Errorf("get go.mod: %w", err)
	}

	rc, err := modF.Reader()
	if err != nil {
	}

	defer func() {
		err := rc.Close()
		if err != nil {
			slog.Error("close go.mod file", "err", err)
		}
	}()

	modData, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("read go.mod: %w", err)
	}

	goMod, err := modfile.Parse("go.mod", modData, nil)
	if err != nil {
		return nil, fmt.Errorf("parse go.mod: %w", err)
	}

	reverseInc := map[string]string{}

	for api, conf := range include {
		reverseInc[conf.From] = api
	}

	deps := make(map[string]depSpec)

	for _, req := range goMod.Require {
		api, included := reverseInc[req.Mod.Path]
		if !included {
			continue
		}

		deps[req.Mod.Path] = depSpec{
			API:     api,
			Module:  req.Mod.Path,
			Version: req.Mod.Version,
		}
	}

	return deps, nil
}
