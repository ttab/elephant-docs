package elephantdocs

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
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
		Name:          mod.Name,
		Repo:          repo,
		VersionLookup: make(map[string]ModuleVersion),
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
			Tag:     name,
			Commit:  commit,
			Version: version,
		}

		module.Versions = append(module.Versions, mv)
		module.VersionLookup[mv.Tag] = mv

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect version tags: %w", err)
	}

	return &module, nil
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
