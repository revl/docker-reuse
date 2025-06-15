package main

import (
	"crypto/sha1"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
)

// fingerprintMode represents the fingerprinting mode for Dockerfile sources.
type fingerprintMode string

const (
	// modeCommit uses git commit hashes for fingerprinting
	modeCommit fingerprintMode = "commit"
	// modeSHA1 uses file content hashing for fingerprinting
	modeSHA1 fingerprintMode = "sha1"
	// modeAuto tries git commit hash first, falls back to content hashing
	modeAuto fingerprintMode = "auto"
)

// fingerprintModeOptions returns the string representation of the fingerprint
// mode options.
func fingerprintModeOptions() string {
	return fmt.Sprintf("\"%s\", \"%s\", or \"%s\"",
		modeCommit, modeSHA1, modeAuto)
}

// fingerprint represents a fingerprint of a Dockerfile source.
type fingerprint struct {
	mode fingerprintMode
	hash string
}

// String returns the string representation of the fingerprint.
func (fp fingerprint) String() string {
	return fmt.Sprintf("%s:%s", fp.mode, fp.hash)
}

// fingerprintFromSHA1 builds a fingerprint from the hexadecimal
// representation of the hashsum.
func fingerprintFromSHA1(h hash.Hash) fingerprint {
	return fingerprint{modeSHA1, fmt.Sprintf("%x", h.Sum(nil))}
}

// hashFiles hashes the files in the given pathname using SHA1 and returns
// the hashsum as a hexadecimal string.
func hashFiles(pathname string) (fingerprint, error) {
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
		return fingerprint{}, err
	}

	return fingerprintFromSHA1(h), nil
}

// parseAndHashDockerfile parses the Dockerfile, extracts the sources from it,
// and returns the the sources and the hashsum of the Dockerfile using SHA1.
func parseAndHashDockerfile(dockerfile string) ([]string, fingerprint, error) {
	f, err := os.Open(dockerfile)
	if err != nil {
		return nil, fingerprint{}, err
	}
	defer f.Close()

	sources, err := collectSourcesFromDockerfile(f)
	if err != nil {
		return nil, fingerprint{}, err
	}

	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return nil, fingerprint{}, err
	}

	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, fingerprint{}, err
	}

	return sources, fingerprintFromSHA1(h), nil
}

// fingerprintFunc defines the type of functions that compute Dockerfile source
// fingerprints.
type fingerprintFunc func(pathname string) (fingerprint, error)

// computeImageFingerprint computes the fingerprint of the Dockerfile, all
// sources from it, and the build arguments using SHA1 and returns the
// fingerprint as a hexadecimal string.
func computeImageFingerprint(workingDir, dockerfile string, buildArgs []string,
	computeFingerprint fingerprintFunc, quiet bool) (fingerprint, error) {

	workingDir = filepath.Clean(workingDir)

	if dockerfile == "" {
		dockerfile = filepath.Join(workingDir, "Dockerfile")
	}

	sources, dockerfileFingerprint, err := parseAndHashDockerfile(
		dockerfile)
	if err != nil {
		return fingerprint{}, err
	}

	h := sha1.New()

	addSourceFingerprint := func(source string, fp fingerprint) {
		if !quiet {
			fmt.Println(source, "fingerprint", fp)
		}
		h.Write([]byte(source + "@" + fp.String() + "\n"))
	}

	addSourceFingerprint("Dockerfile", dockerfileFingerprint)

	computeAndAddSourceFingerprint := func(source, pathname string) error {
		fp, err := computeFingerprint(pathname)
		if err != nil {
			return err
		}
		addSourceFingerprint(source, fp)
		return nil
	}

	for _, source := range sources {
		source = filepath.Clean(source)
		pathname := filepath.Join(workingDir, source)

		if _, err := os.Stat(pathname); err != nil {
			if !os.IsNotExist(err) {
				return fingerprint{}, err
			}

			// Try interpreting the path as a glob pattern.
			matches, _ := filepath.Glob(pathname)
			// If nothing matched, return the original Stat() error.
			if len(matches) == 0 {
				return fingerprint{}, err
			}

			for _, pathname = range matches {
				// Ignore the impossible Rel() error.
				source, _ = filepath.Rel(workingDir, pathname)

				if err = computeAndAddSourceFingerprint(
					source, pathname); err != nil {
					return fingerprint{}, err
				}
			}
		} else if err = computeAndAddSourceFingerprint(
			source, pathname); err != nil {
			return fingerprint{}, err
		}

	}

	for _, buildArg := range buildArgs {
		if !quiet {
			fmt.Println("Arg:", buildArg)
		}
		h.Write([]byte(buildArg))
		h.Write([]byte("\n"))
	}

	return fingerprintFromSHA1(h), nil
}
