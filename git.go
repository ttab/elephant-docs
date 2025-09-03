package elephantdocs

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/ttab/elephant-docs/internal"
)

func newModule(mod ModuleConfig) (*Module, error) {
	clone := mod.Clone
	if clone == "" {
		mod.Clone = fmt.Sprintf("https://%s", mod.Name)
	}

	repo, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL:      mod.Clone,
		Progress: os.Stderr,
	})
	if err != nil {
		return nil, fmt.Errorf("git clone: %w", err)
	}

	module := Module{
		Title:         mod.Title,
		Name:          mod.Name,
		Repo:          repo,
		VersionLookup: make(map[string]*ModuleVersion),
		APIs:          mod.APIs,
		Include:       mod.Include,
	}

	tagsRefs, err := repo.Tags()
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}

	err = tagsRefs.ForEach(func(tagRef *plumbing.Reference) error {
		name := tagRef.Name().Short()
		if !strings.HasPrefix(name, "v") {
			return nil
		}

		version, err := semver.NewVersion(name)
		if err != nil {
			return nil
		}

		commit, err := getCommitObjectForTag(repo, tagRef)
		if err != nil {
			return err
		}

		mv := ModuleVersion{
			Tag:          name,
			Commit:       commit,
			Version:      version,
			IsPrerelease: version.Prerelease() != "",
		}

		module.Versions = append(module.Versions, &mv)
		module.VersionLookup[mv.Tag] = &mv

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect version tags: %w", err)
	}

	slices.SortFunc(module.Versions, func(a, b *ModuleVersion) int {
		return a.Version.Compare(b.Version)
	})

	slices.Reverse(module.Versions)

	for _, v := range module.Versions {
		if v.Version.Prerelease() != "" {
			continue
		}

		module.LatestVersion = v

		break
	}

	return &module, nil
}

func getChangelog(module *Module, api string) ([]*ModuleVersion, error) {
	if len(module.Versions) == 0 {
		return nil, nil
	}

	versions := make([]*ModuleVersion, 0, len(module.Versions))

	// Semi-deep clone so that we don't pollute the shared Log slice.
	for i := range module.Versions {
		m := *module.Versions[i]

		tree, err := m.Commit.Tree()
		if err != nil {
			return nil, fmt.Errorf("get commit tree: %w", err)
		}

		_, err = tree.Tree(api)
		if errors.Is(err, object.ErrDirectoryNotFound) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("failed to list files: %w", err)
		}

		m.Log = nil

		versions = append(versions, &m)
	}

	if len(versions) == 0 {
		return nil, nil
	}

	log, err := module.Repo.Log(&git.LogOptions{
		From:  versions[0].Commit.Hash,
		Order: git.LogOrderCommitterTime,
	})
	if err != nil {
		return nil, fmt.Errorf("get git log: %w", err)
	}

	inScope := map[string]bool{}

	filtered := internal.NewCommitPathIterFromIter(
		func(c *object.Commit, names []string) bool {
			var ok bool

			for i := range names {
				ok = strings.HasPrefix(names[i], api+"/")
				if ok {
					inScope[c.Hash.String()] = true

					break
				}
			}

			tagCount := len(VersionsAtCommit(c.Hash, versions))

			return ok || tagCount > 0
		}, log)

	var accumulators []*ModuleVersion

	err = filtered.ForEach(func(commit *object.Commit) error {
		found := VersionsAtCommit(commit.Hash, versions)

		accumulators = slices.DeleteFunc(accumulators, func(e *ModuleVersion) bool {
			for _, f := range found {
				if !isPrerelease(f) || isPrerelease(e) {
					return true
				}
			}

			return false
		})

		accumulators = append(accumulators, found...)

		if inScope[commit.Hash.String()] {
			for _, acc := range accumulators {
				acc.Log = append(acc.Log, commit)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read git log: %w", err)
	}

	return versions, nil
}

func isPrerelease(v *ModuleVersion) bool {
	return v.Version.Prerelease() != ""
}

func getCommitObjectForTag(repo *git.Repository, tagRef *plumbing.Reference) (*object.Commit, error) {
	var commit *object.Commit

	t, err := repo.TagObject(tagRef.Hash())

	switch {
	case errors.Is(err, plumbing.ErrObjectNotFound):
		c, err := repo.CommitObject(tagRef.Hash())
		if err != nil {
			return nil, fmt.Errorf("get tag commit: %w", err)
		}

		commit = c
	case err != nil:
		return nil, fmt.Errorf("get tag object: %w", err)
	default:
		c, err := t.Commit()
		if err != nil {
			return nil, fmt.Errorf("get tag commit: %w", err)
		}

		commit = c
	}

	return commit, nil
}
