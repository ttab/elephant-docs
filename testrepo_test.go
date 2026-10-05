package elephantdocs

import (
	"path"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
)

// testCommit is one commit in a synthetic repository. Files are added, never
// removed, which is all the discovery code needs to see.
type testCommit struct {
	Message string
	Tag     string
	Files   map[string]string
}

// buildRepo creates a repository in memory. The tests never touch the
// network: everything the generator reads comes out of a commit tree, so a
// handful of files and tags is a complete fixture.
func buildRepo(t *testing.T, commits ...testCommit) *git.Repository {
	t.Helper()

	fs := memfs.New()

	repo, err := git.Init(memory.NewStorage(), git.WithWorkTree(fs))
	if err != nil {
		t.Fatalf("init repository: %v", err)
	}

	// go-git resolves commit.gpgSign out to the system scope, so a
	// developer who signs their own commits would otherwise fail every
	// test that builds a fixture repository.
	cfg, err := repo.Config()
	if err != nil {
		t.Fatalf("read the repository config: %v", err)
	}

	cfg.Commit.GpgSign = config.OptBoolFalse

	err = repo.SetConfig(cfg)
	if err != nil {
		t.Fatalf("disable commit signing: %v", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("open worktree: %v", err)
	}

	when := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)

	for i, c := range commits {
		for name, contents := range c.Files {
			dir := path.Dir(name)
			if dir != "." {
				err := fs.MkdirAll(dir, 0o755)
				if err != nil {
					t.Fatalf("create %q: %v", dir, err)
				}
			}

			f, err := fs.Create(name)
			if err != nil {
				t.Fatalf("create %q: %v", name, err)
			}

			_, err = f.Write([]byte(contents))
			if err != nil {
				t.Fatalf("write %q: %v", name, err)
			}

			err = f.Close()
			if err != nil {
				t.Fatalf("close %q: %v", name, err)
			}

			_, err = wt.Add(name)
			if err != nil {
				t.Fatalf("add %q: %v", name, err)
			}
		}

		message := c.Message
		if message == "" {
			message = "commit"
		}

		hash, err := wt.Commit(message, &git.CommitOptions{
			Author: &object.Signature{
				Name:  "Test",
				Email: "test@example.com",
				When:  when.Add(time.Duration(i) * time.Hour),
			},
		})
		if err != nil {
			t.Fatalf("commit %q: %v", message, err)
		}

		if c.Tag != "" {
			_, err = repo.CreateTag(c.Tag, hash, nil)
			if err != nil {
				t.Fatalf("tag %q: %v", c.Tag, err)
			}
		}
	}

	return repo
}

func testModule(t *testing.T, conf ModuleConfig, repo *git.Repository) *Module {
	t.Helper()

	module, err := moduleFromRepo(conf, repo)
	if err != nil {
		t.Fatalf("collect module versions: %v", err)
	}

	return module
}

// serviceProto is a minimal but complete service declaration.
func serviceProto(pkg string, service string) string {
	return `syntax = "proto3";

package ` + pkg + `;

service ` + service + ` {
  rpc Get(GetRequest) returns (GetResponse);
}

message GetRequest {
  string uuid = 1;
}

message GetResponse {
  string uuid = 1;
}
`
}
