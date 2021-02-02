package main

import (
	"os"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

func collectSourcesFromDockerfile(f *os.File) ([]string, error) {
	res, err := parser.Parse(f)
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
