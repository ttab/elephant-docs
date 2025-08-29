package elephantdocs

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/ttab/elephantine"
	"golang.org/x/mod/modfile"
	"golang.org/x/sync/errgroup"
)

//go:embed templates
var templateFS embed.FS

//go:embed assets
var assetFS embed.FS

type API struct {
	Name    string
	Title   string
	Version string
	Module  string
	Data    APIData
}

type APIData struct {
	Declarations []ProtoDeclarations
	Dependencies map[string]ProtoHandle
}

type Module struct {
	Name          string
	Repo          *git.Repository `json:"-"`
	Versions      []ModuleVersion
	VersionLookup map[string]ModuleVersion `json:"-"`
	APIs          map[string]APIConfig
	Include       map[string]IncludeConfig
}

type ModuleVersion struct {
	Tag                string
	Commit             *object.Commit  `json:"-"`
	Version            *semver.Version `json:"-"`
	DependencyVersions map[string]string
}

func Generate(
	ctx context.Context, outDir string, conf Config,
	uiPrintln func(format string, a ...any),
) error {
	apiConf := make(map[string]APIConfig)
	modules := map[string]*Module{}

	tpl := template.New("templates")

	tpl, err := tpl.ParseFS(templateFS, "templates/*.html")
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

	for api, conf := range apiConf {
		apiMenu = append(apiMenu, MenuItem{
			Title: conf.Title,
			HRef:  fmt.Sprintf("/apis/%s/", api),
		})
	}

	slices.SortFunc(apiMenu, func(a MenuItem, b MenuItem) int {
		return strings.Compare(a.Title, b.Title)
	})

	jobs := make(chan collectJob)
	results := make(chan collectJob)

	defer close(results)

	grp, gCtx := errgroup.WithContext(ctx)

	grp.Go(func() error {
		err := os.CopyFS(filepath.Join(outDir), assetFS)
		if err != nil {
			return fmt.Errorf("write assets directory: %w", err)
		}

		return nil
	})

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

	for range 16 {
		grp.Go(func() error {
			for job := range jobs {
				module := job.Module
				version := job.Version

				apis, err := collectAPIData(modules, module, version)
				if err != nil {
					return fmt.Errorf("render %s@%s: %w",
						module.Name, version.Tag, err)
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

					d := API{
						Name:    api,
						Title:   conf.Title,
						Version: version.Tag,
						Module:  module.Name,
						Data:    data,
					}

					apiDir := filepath.Join("apis", api)

					versionDir := filepath.Join(
						apiDir, version.Tag,
					)

					menu := slices.Clone(apiMenu)

					for i := range menu {
						menu[i].Active = strings.Trim(menu[i].HRef, "/") == apiDir
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

					err = renderVersionPage(
						versionOutDir,
						tpl, page)
					if err != nil {
						return fmt.Errorf(
							"render version page for %s@%s: %w",
							api, version.Tag, err)
					}
				}
			}

			return nil
		})
	}

	err = grp.Wait()
	if err != nil {
		return fmt.Errorf("render documentation: %w", err)
	}

	return nil
}

func renderVersionPage(outDir string, tpl *template.Template, page Page) (outErr error) {
	err := elephantine.MarshalFile(
		filepath.Join(outDir, "index.json"), page.Contents)
	if err != nil {
		return fmt.Errorf("write API data: %w", err)
	}

	indexPath := filepath.Join(outDir, "index.html")

	file, err := os.Create(indexPath)
	if err != nil {
		return fmt.Errorf("create index.html: %w", err)
	}

	defer elephantine.Close("index.html", file, &outErr)

	err = tpl.ExecuteTemplate(file, "api_version.html", page)
	if err != nil {
		return fmt.Errorf("render page: %w", err)
	}

	return nil
}

type collectJob struct {
	Module  *Module
	Version ModuleVersion
}

func collectAPIData(
	modules map[string]*Module,
	module *Module, version ModuleVersion,
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
				Proto: pd,
			}
		}

		apis[apiName] = protos
	}

	apiData := make(map[string]APIData)

	for apiName, protos := range apis {
		data := APIData{
			Declarations: protos,
			Dependencies: make(map[string]ProtoHandle),
		}

		for _, p := range protos {
			for _, f := range p.Imports {
				h, ok := files[f]
				if !ok {
					return nil, fmt.Errorf("missing dependency %q", f)
				}

				data.Dependencies[h.Proto.Package] = h
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
