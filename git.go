package main

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
)

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
