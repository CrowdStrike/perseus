package git

import (
	"fmt"
	"strings"
	"sync"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"golang.org/x/mod/semver"
)

// Repo wraps a Git repository.
type Repo struct {
	repo *git.Repository
}

// Open opens a Git repository at the specified path.
func Open(dir string) (*Repo, error) {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return nil, fmt.Errorf("unable to open Git repository at %q: %w", dir, err)
	}
	return &Repo{
		repo: repo,
	}, nil
}

// VersionTags returns the SemVer tags associated with the current HEAD revision on the repo.
func (r *Repo) VersionTags() (tags []string, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("error inspecting Git repository: %w", err)
		}
	}()

	var head *plumbing.Reference
	if head, err = r.repo.Head(); err != nil {
		return nil, err
	}
	hh := head.Hash()

	// for efficiency, enumerate annotated and regular tags in parallel
	// . write any semver tags that reference the current HEAD to rc
	type tagResult struct {
		Tag string
		Err error
	}
	rc := make(chan tagResult)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		var it *object.TagIter
		if it, err = r.repo.TagObjects(); err != nil {
			rc <- tagResult{Err: err}
			return
		}
		_ = it.ForEach(func(t *object.Tag) error {
			if t.Target == hh {
				if isSemverTag := semver.IsValid(t.Name); isSemverTag {
					rc <- tagResult{Tag: t.Name}
				}
			}
			return nil
		})
	}()
	go func() {
		defer wg.Done()
		var it2 storer.ReferenceIter
		if it2, err = r.repo.Tags(); err != nil {
			rc <- tagResult{Err: err}
			return
		}
		_ = it2.ForEach(func(ref *plumbing.Reference) error {
			if ref.Hash() == hh {
				tag := strings.TrimPrefix(ref.Name().String(), "refs/tags/")
				if isSemverTag := semver.IsValid(tag); isSemverTag {
					rc <- tagResult{Tag: tag}
				}
			}
			return nil
		})
	}()
	go func() {
		wg.Wait()
		close(rc)
	}()

	for t := range rc {
		if t.Err != nil {
			return nil, t.Err
		}
		tags = append(tags, t.Tag)
	}
	return tags, nil
}
