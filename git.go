package main

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
)

// getLastCommitHash returns the hash of the last commit in the subtree of the
// repository rooted at pathname. It returns an error if the repository cannot
// be opened or if there are local modifications.
func getLastCommitHash(pathname string) (string, error) {
	abs, err := filepath.Abs(pathname)
	if err != nil {
		return "", err
	}

	r, err := git.PlainOpenWithOptions(abs,
		&git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return "", err
	}

	wt, err := r.Worktree()
	if err != nil {
		return "", err
	}
	root := wt.Filesystem.Root()

	// Check if the repository subtree rooted at `pathname` is clean. The last
	// commit of `pathname` cannot be used as its fingerprint if there are
	// local modifications.
	status, err := wt.Status()
	if err != nil {
		return "", err
	}

	var clean bool

	logOptions := &git.LogOptions{}

	if root != abs {
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			// This will never happen because the worktree
			// root is derived from 'pathname'.
			panic(err)
		}

		logOptions.PathFilter = func(s string) bool {
			return strings.HasPrefix(s, rel)
		}

		clean = true
		for f, s := range status {
			if (s.Worktree != git.Unmodified ||
				s.Staging != git.Unmodified) &&
				strings.HasPrefix(f, rel) {
				clean = false
				break
			}
		}
	} else {
		clean = status.IsClean()
	}

	if !clean {
		return "", errors.New("local modifications detected")
	}

	// Get the last commit hash.
	commitIter, err := r.Log(logOptions)
	if err != nil {
		return "", err
	}
	defer commitIter.Close()

	lastCommit, err := commitIter.Next()
	if err != nil {
		return "", err
	}
	if lastCommit == nil {
		return "", errors.New("no commit history")
	}

	return lastCommit.Hash.String(), nil
}
