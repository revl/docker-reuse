// +build !windows

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

var appName = "docker-reuse"

func gitLog() error {
	r, err := git.PlainOpen(".")

	if err != nil {
		return err
	}

	// Gets the HEAD history from HEAD, just like this command:
	log.Println("git log")

	// ... retrieves the branch pointed by HEAD
	ref, err := r.Head()
	if err != nil {
		return err
	}

	// ... retrieves the commit history
	cIter, err := r.Log(&git.LogOptions{From: ref.Hash()})
	if err != nil {
		return err
	}

	// ... just iterates over the commits, printing it
	err = cIter.ForEach(func(c *object.Commit) error {
		fmt.Println(c.Hash)
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func collectSourcesFromDockerfile(pathname string) ([]string, error) {
	file, err := os.Open(pathname)
	if err != nil {
		return nil, err
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

	log.Println(args, *dockerfilePathname)

	sources, err := collectSourcesFromDockerfile(*dockerfilePathname)
	if err != nil {
		log.Fatalln(err)
	}
	for _, source := range sources {
		log.Println(source)
	}
	err = gitLog()
	if err != nil {
		log.Fatalln(err)
	}
}
