package main

import (
	"crypto/sha1"
	"fmt"
	"log"
	"path/filepath"
)

func computeFingerprint(workingDir, dockerfile string,
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
