package internal

import (
	"io"

	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// Adapted from github.com/go-git/go-git/v6/object commitPathIter to allow for
// filtering by all the paths in the diff and the commit.
//
// TODO: One limitation of the iterator pattern and the iteratorsthat are
// provided by go-git is that they not very composable. Or rather they only
// composes in one way, as successive filters: X && Y && Z. So we cannot express
// (X || Y) like I wanted to do by including commits if they matched a path
// prefix or if they were tagged. Out of scope, but would be interesting to
// think about ways to improve this.
type commitPathIter struct {
	pathFilter    func(*object.Commit, []string) bool
	sourceIter    object.CommitIter
	currentCommit *object.Commit
}

// NewCommitPathIterFromIter returns a commit iterator which performs diffTree between
// successive trees returned from the commit iterator from the argument. The purpose of this is
// to find the commits that explain how the files that match the path came to be.
// If checkParent is true then the function double checks if potential parent (next commit in a path)
// is one of the parents in the tree (it's used by `git log --all`).
// pathFilter is a function that takes path of file as argument and returns true if we want it
func NewCommitPathIterFromIter(
	pathFilter func(*object.Commit, []string) bool,
	commitIter object.CommitIter,
) object.CommitIter {
	iterator := new(commitPathIter)
	iterator.sourceIter = commitIter
	iterator.pathFilter = pathFilter
	return iterator
}

func (c *commitPathIter) Next() (*object.Commit, error) {
	if c.currentCommit == nil {
		var err error
		c.currentCommit, err = c.sourceIter.Next()
		if err != nil {
			return nil, err
		}
	}
	commit, commitErr := c.getNextFileCommit()

	// Setting current-commit to nil to prevent unwanted states when errors are raised
	if commitErr != nil {
		c.currentCommit = nil
	}
	return commit, commitErr
}

func (c *commitPathIter) getNextFileCommit() (*object.Commit, error) {
	var parentTree, currentTree *object.Tree

	for {
		// Parent-commit can be nil if the current-commit is the initial commit
		parentCommit, parentCommitErr := c.sourceIter.Next()
		if parentCommitErr != nil {
			// If the parent-commit is beyond the initial commit, keep it nil
			if parentCommitErr != io.EOF {
				return nil, parentCommitErr
			}
			parentCommit = nil
		}

		if parentTree == nil {
			var currTreeErr error
			currentTree, currTreeErr = c.currentCommit.Tree()
			if currTreeErr != nil {
				return nil, currTreeErr
			}
		} else {
			currentTree = parentTree
			parentTree = nil
		}

		if parentCommit != nil {
			var parentTreeErr error
			parentTree, parentTreeErr = parentCommit.Tree()
			if parentTreeErr != nil {
				return nil, parentTreeErr
			}
		}

		// Find diff between current and parent trees
		changes, diffErr := object.DiffTree(currentTree, parentTree)
		if diffErr != nil {
			return nil, diffErr
		}

		found := c.hasFileChange(changes)

		// Storing the current-commit in-case a change is found, and
		// Updating the current-commit for the next-iteration
		prevCommit := c.currentCommit
		c.currentCommit = parentCommit

		if found {
			return prevCommit, nil
		}

		// If not matches found and if parent-commit is beyond the initial commit, then return with EOF
		if parentCommit == nil {
			return nil, io.EOF
		}
	}
}

func (c *commitPathIter) hasFileChange(changes object.Changes) bool {
	var names []string

	for _, change := range changes {
		name := change.From.Name
		if name == "" {
			name = change.To.Name
		}

		names = append(names, name)
	}

	return c.pathFilter(c.currentCommit, names)
}

func (c *commitPathIter) ForEach(cb func(*object.Commit) error) error {
	for {
		commit, nextErr := c.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return nextErr
		}
		err := cb(commit)
		if err == storer.ErrStop {
			return nil
		} else if err != nil {
			return err
		}
	}
	return nil
}

func (c *commitPathIter) Close() {
	c.sourceIter.Close()
}
