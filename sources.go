package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

func collectSourcesFromDockerfile(pathname string) ([]string, error) {
	file, err := os.Open(pathname)
	if err != nil {
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
