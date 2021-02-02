// +build !windows

package main

import (
	"crypto/sha1"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

func computeImageHash(workingDir, dockerfile string,
	quiet bool) (string, error) {

	if dockerfile == "" {
		dockerfile = filepath.Join(workingDir, "Dockerfile")
	}

	buf := ""

	appendHash := func(source, commitHash string) {
		line := source + ":" + commitHash + "\n"
		if !quiet {
			log.Print(line)
		}
		buf += line
	}

	hash, err := getLastCommitHash(dockerfile)
	if err != nil {
		return "", err
	}
	appendHash("Dockerfile", hash)

	sources, err := collectSourcesFromDockerfile(dockerfile)
	if err != nil {
		return "", err
	}

	for _, source := range sources {
		hash, err = getLastCommitHash(filepath.Join(workingDir, source))
		if err != nil {
			return "", err
		}
		appendHash(source, hash)
	}

	return fmt.Sprintf("%x", sha1.Sum([]byte(buf))), nil
}

func main() {
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(),
			"Usage:  "+appName+" build [OPTIONS] PATH IMAGE FILE\n"+
				"  PATH\n"+
				"    \tDocker build context directory\n"+
				"  IMAGE\n"+
				"    \tName of the image to find or build\n"+
				"  FILE\n"+
				"    \tFile to update with the new image tag")
		flag.PrintDefaults()
	}

	dockerfile := flag.String("f", "",
		"Pathname of the `Dockerfile` (Default is 'PATH/Dockerfile')")
	quiet := flag.Bool("q", false, "Suppress build output")

	flag.Parse()

	args := flag.Args()

	if len(args) != 3 {
		fmt.Fprintln(flag.CommandLine.Output(),
			"invalid number of positional arguments")
		flag.Usage()
		os.Exit(2)
	}

	workingDir, imageName, templateFilename := args[0], args[1], args[2]

	templateContents, err := ioutil.ReadFile(templateFilename)
	if err != nil {
		log.Fatal(err)
	}

	// Image tag may contain lowercase and uppercase letters, digits,
	// underscores, periods and dashes.
	re := regexp.MustCompile(regexp.QuoteMeta(imageName) + "(?::[-.\\w]+)?")

	if len(re.Find(templateContents)) == 0 {
		log.Fatalf("'%s' does not contain references to '%s'",
			templateFilename, imageName)
	}

	imageHash, err := computeImageHash(workingDir, *dockerfile, *quiet)
	if err != nil {
		log.Fatal(err)
	}

	imageNameWithTag := imageName + ":" + imageHash
	if !*quiet {
		log.Println(imageNameWithTag)
	}

	// Check if the image already exists in the registry
	cmd := exec.Command("docker", "manifest", "inspect", imageNameWithTag)
	// cmd.Stderr = nil
	_, err = cmd.Output()

	if err == nil {
		if !*quiet {
			log.Print("Image already exists")
		}
	} else {
		if ee, ok := err.(*exec.ExitError); ok {
			for _, l := range strings.Split(
				string(ee.Stderr[:]), "\n") {

				if l != "" {
					log.Println(l)
				}
			}
		} else {
			log.Fatal(err)
		}
	}

	templateContents = re.ReplaceAll(
		templateContents, []byte(imageNameWithTag))

	if err = ioutil.WriteFile(
		templateFilename, templateContents, 0); err != nil {

		log.Fatal(err)
	}
}
