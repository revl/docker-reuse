package main

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
)

// isPathClean checks if a specific path in the git repository is clean
// (has no modifications) using the native git command, which is much faster
// than using go-git's Status() method.
func isPathClean(repoRoot, path string) (bool, error) {
	// Use git status --porcelain to check for any changes (modified,
	// staged, or untracked) in the specified path.
	cmd := exec.Command("git", "status", "--porcelain",
		"--untracked-files=all", "--", path)
	cmd.Dir = repoRoot
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}

	// If there's any output, the path is not clean
	// (has modified, staged, or untracked files)
	return strings.TrimSpace(string(output)) == "", nil
}

// getLastCommitHash returns the hash of the last commit in the subtree of the
// repository rooted at pathname. It returns an error if the repository cannot
// be opened or if there are local modifications.
func getLastCommitHash(pathname string) (fingerprint, error) {
	abs, err := filepath.Abs(pathname)
	if err != nil {
		return fingerprint{}, err
	}

	r, err := git.PlainOpenWithOptions(abs,
		&git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return fingerprint{}, err
	}

	wt, err := r.Worktree()
	if err != nil {
		return fingerprint{}, err
	}
	root := wt.Filesystem.Root()

	// Check if the repository subtree rooted at `pathname` is clean. The
	// last commit of `pathname` cannot be used as its fingerprint if there
	// are local modifications.
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

		// Check status for only the specific path
		clean, err = isPathClean(root, rel)
		if err != nil {
			return fingerprint{}, err
		}
	} else {
		// Check status for the entire repository
		clean, err = isPathClean(root, ".")
		if err != nil {
			return fingerprint{}, err
		}
	}

	if !clean {
		return fingerprint{}, errors.New("local modifications detected")
	}

	// Get the last commit hash.
	commitIter, err := r.Log(logOptions)
	if err != nil {
		return fingerprint{}, err
	}
	defer commitIter.Close()

	lastCommit, err := commitIter.Next()
	if err != nil {
		return fingerprint{}, err
	}
	if lastCommit == nil {
		return fingerprint{}, errors.New("no commit history")
	}

	return fingerprint{modeCommit, lastCommit.Hash.String()}, nil
}
