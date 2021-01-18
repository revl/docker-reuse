// +build !windows

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

var appName = "docker-reuse"

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
		return "", fmt.Errorf("Error: %s has modifications", pathname)
	}

	commitIter, err := r.Log(logOptions)
	if err != nil {
		return "", err
	}
	defer commitIter.Close()

	lastCommit, err := commitIter.Next()
	if err != nil || lastCommit == nil {
		return "", fmt.Errorf(
			"No commit history found for %s", pathname)
	}

	return lastCommit.Hash.String(), nil
}

func collectSourcesFromDockerfile(pathname string) ([]string, error) {
	file, err := os.Open(pathname)
	if err != nil {
		log.Fatalf("Error parsing %s: %v", pathname, err)
		return nil, fmt.Errorf("Error parsing %s: %v", pathname, err)
	}
	defer file.Close()

	res, err := parser.Parse(file)
	if err != nil {
		return nil, err
	}

	var sources []string
	alreadyAdded := map[string]bool{}

nextChild:
	for _, child := range res.AST.Children {
		if child.Value != "add" && child.Value != "copy" {
			continue
		}

		for _, flag := range child.Flags {
			if strings.HasPrefix(flag, "--from") {
				continue nextChild
			}
		}

		if child.Next != nil {
			src := child.Next

			// Stop at the last token, which is <dest>.
			for src.Next != nil {
				if !alreadyAdded[src.Value] {
					sources = append(sources, src.Value)
					alreadyAdded[src.Value] = true
				}

				src = src.Next
			}
		}
	}

	return sources, nil
}

func main() {
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(),
			"Usage:  "+appName+" build [OPTIONS] PATH NAME\n"+
				"  PATH\n"+
				"    \tDocker build context directory\n"+
				"  NAME\n"+
				"    \tImage name, including GCR repository")
		flag.PrintDefaults()
	}

	dockerfilePathname := flag.String("f", "",
		"Pathname of the `Dockerfile` (Default is 'PATH/Dockerfile')")

	flag.Parse()

	args := flag.Args()

	if len(args) != 2 {
		fmt.Fprintln(flag.CommandLine.Output(),
			"invalid number of positional arguments")
		flag.Usage()
		os.Exit(2)
	}

	workingDir := args[0]
	// imageName := args[1]

	if *dockerfilePathname == "" {
		*dockerfilePathname = filepath.Join(workingDir, "Dockerfile")
	}

	hash, err := getLastCommitHash(*dockerfilePathname)
	if err != nil {
		log.Fatalln(err)
	}
	log.Println("Dockerfile:" + hash)

	sources, err := collectSourcesFromDockerfile(*dockerfilePathname)
	if err != nil {
		log.Fatalln(err)
	}
	for _, source := range sources {
		pathname := filepath.Join(workingDir, source)
		hash, err = getLastCommitHash(pathname)
		if err != nil {
			log.Fatalln(err)
		}
		log.Println(source + ":" + hash)
	}
}
