package main

import (
	"crypto/sha1"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
)

// hex returns the hexadecimal representation of the hashsum.
func hex(h hash.Hash) string {
	return fmt.Sprintf("%x", h.Sum(nil))
}

// hashFiles hashes the files in the given pathname using SHA1 and returns
// the hashsum as a hexadecimal string.
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

// parseAndHashDockerfile parses the Dockerfile, extracts the sources from it,
// and returns the the sources and the hashsum of the Dockerfile using SHA1.
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

// computeFingerprint computes the fingerprint of the Dockerfile, all sources
// from it, and the build arguments using SHA1 and returns the fingerprint as
// a hexadecimal string.
func computeFingerprint(workingDir, dockerfile string, buildArgs []string,
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

	hashSource := func(source, pathname string) error {
		hash, err = getLastCommitHash(pathname)
		if err == nil {
			addSourceHash(source, "commit", hash)
		} else {
			fmt.Fprintf(os.Stderr, "Warning: unable to use git "+
				"commit hash for '%s': %v; falling back to "+
				"file content hashing\n", pathname, err)

			hash, err = hashFiles(pathname)
			if err != nil {
				return err
			}

			addSourceHash(source, "sha1", hash)
		}
		return nil
	}

	for _, source := range sources {
		source = filepath.Clean(source)
		pathname := filepath.Join(workingDir, source)

		if _, err := os.Stat(pathname); err != nil {
			if !os.IsNotExist(err) {
				return "", err
			}

			// Try interpreting the path as a glob pattern.
			matches, _ := filepath.Glob(pathname)
			// If nothing matched, return the original Stat() error.
			if len(matches) == 0 {
				return "", err
			}

			for _, pathname = range matches {
				// Ignore the impossible Rel() error.
				source, _ = filepath.Rel(workingDir, pathname)

				if err = hashSource(
					source, pathname); err != nil {
					return "", err
				}
			}
		} else if err = hashSource(source, pathname); err != nil {
			return "", err
		}

	}

	for _, buildArg := range buildArgs {
		if !quiet {
			fmt.Println("Arg:", buildArg)
		}
		h.Write([]byte(buildArg))
		h.Write([]byte("\n"))
	}

	return hex(h), nil
}
