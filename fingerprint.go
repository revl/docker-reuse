package main

import (
	"crypto/sha1"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
)

func hex(h hash.Hash) string {
	return fmt.Sprintf("%x", h.Sum(nil))
}

func hashFiles(pathname string) (string, error) {
	h := sha1.New()

	err := filepath.Walk(pathname, func(p string,
		info os.FileInfo, err error) error {

		if err != nil {
			return err
		}

		if info.IsDir() {
			// Ignore hidden directories
			if p != "." && filepath.Base(p)[0] == '.' {
				return filepath.SkipDir
			}
			return nil
		}

		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()

		if _, err := io.Copy(h, f); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return "", err
	}

	return hex(h), nil
}

func parseAndHashDockerfile(dockerfile string) ([]string, string, error) {
	f, err := os.Open(dockerfile)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	sources, err := collectSourcesFromDockerfile(f)
	if err != nil {
		return nil, "", err
	}

	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return nil, "", err
	}

	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, "", err
	}

	return sources, hex(h), nil
}

func computeFingerprint(workingDir, dockerfile string,
	quiet bool) (string, error) {

	workingDir = filepath.Clean(workingDir)

	if dockerfile == "" {
		dockerfile = filepath.Join(workingDir, "Dockerfile")
	}

	sources, hash, err := parseAndHashDockerfile(dockerfile)

	h := sha1.New()

	addSourceHash := func(source, hashType, hash string) {
		if !quiet {
			fmt.Println("Source:", source, hashType, hash)
		}
		h.Write([]byte(source + "@" + hashType + ":" + hash + "\n"))
	}

	addSourceHash("Dockerfile", "sha1", hash)

	for _, source := range sources {
		source = filepath.Clean(source)
		pathname := filepath.Join(workingDir, source)

		hash, err = getLastCommitHash(pathname)
		if err == nil {
			addSourceHash(source, "commit", hash)
		} else {
			fmt.Fprintf(os.Stderr, "Warning: unable to use git "+
				"commit hash for '%s': %v; falling back to "+
				"file content hashing\n", pathname, err)

			hash, err = hashFiles(pathname)
			if err != nil {
				return "", err
			}

			addSourceHash(source, "sha1", hash)
		}
	}

	return hex(h), nil
}
