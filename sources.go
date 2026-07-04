package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/moby/buildkit/frontend/dockerfile/shell"
)

// argRefRegexp matches $NAME and ${NAME...} variable references.
var argRefRegexp = regexp.MustCompile(
	`\$(?:([A-Za-z_][A-Za-z0-9_]*)|\{([A-Za-z_][A-Za-z0-9_]*)[^}]*\})`)

// collectSourcesFromDockerfile collects and returns the sources from the
// Dockerfile. COPY and ADD sources may reference build arguments declared
// with ARG; such references are expanded using the values from buildArgs
// ("NAME=value" pairs) or the ARG defaults.
func collectSourcesFromDockerfile(f *os.File, buildArgs []string) (
	[]string, error) {

	res, err := parser.Parse(f)
	if err != nil {
		return nil, err
	}

	cliArgs := map[string]string{}
	for _, arg := range buildArgs {
		if name, value, found := strings.Cut(arg, "="); found {
			cliArgs[name] = value
		}
	}

	lex := shell.NewLex(res.EscapeToken)
	args := map[string]string{}

	// expandArgs expands ARG references in a COPY/ADD source token.
	// Unlike 'docker build', which silently expands undefined variables
	// to empty strings, an undefined or empty argument is an error here:
	// it would divert fingerprinting to a wrong path.
	expandArgs := func(token string) (string, error) {
		if !strings.ContainsRune(token, '$') {
			return token, nil
		}
		for _, ref := range argRefRegexp.FindAllStringSubmatch(
			token, -1) {

			name := ref[1]
			if name == "" {
				name = ref[2]
			}
			if args[name] == "" {
				return "", fmt.Errorf("cannot expand '%s': "+
					"build argument '%s' is not set",
					token, name)
			}
		}
		return lex.ProcessWordWithMap(token, args)
	}

	var sources []string
	alreadyAdded := map[string]bool{}

nextChild:
	for _, child := range res.AST.Children {
		if child.Value == "arg" {
			for arg := child.Next; arg != nil; arg = arg.Next {
				name, value, hasDefault :=
					strings.Cut(arg.Value, "=")
				if cliValue, found := cliArgs[name]; found {
					args[name] = cliValue
				} else if hasDefault {
					args[name] = value
				}
			}
			continue
		}

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
				source, err := expandArgs(src.Value)
				if err != nil {
					return nil, err
				}

				if !alreadyAdded[source] {
					sources = append(sources, source)
					alreadyAdded[source] = true
				}

				src = src.Next
			}
		}
	}

	return sources, nil
}
