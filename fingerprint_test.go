package main

import (
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintString(t *testing.T) {
	tests := []struct {
		name     string
		fp       fingerprint
		expected string
	}{
		{
			name:     "SHA1 mode",
			fp:       fingerprint{modeSHA1, "abc123"},
			expected: "sha1:abc123",
		},
		{
			name:     "Commit mode",
			fp:       fingerprint{modeCommit, "def456"},
			expected: "commit:def456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fp.String(); got != tt.expected {
				t.Errorf("fingerprint.String() = %v, want %v",
					got, tt.expected)
			}
		})
	}
}

func TestFingerprintFromSHA1(t *testing.T) {
	h := sha1.New()
	h.Write([]byte("test content"))
	expected := fmt.Sprintf("%x", h.Sum(nil))

	fp := fingerprintFromSHA1(h)
	if fp.mode != modeSHA1 {
		t.Errorf("fingerprintFromSHA1() mode = %v, want %v",
			fp.mode, modeSHA1)
	}
	if fp.hash != expected {
		t.Errorf("fingerprintFromSHA1() hash = %v, want %v",
			fp.hash, expected)
	}
}

func TestHashFiles(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "docker-reuse-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test files
	testFiles := map[string]string{
		"file1.txt": "content1",
		"file2.txt": "content2",
	}

	for name, content := range testFiles {
		path := filepath.Join(tempDir, name)
		if err := os.WriteFile(path, []byte(content),
			0644); err != nil {
			t.Fatalf("Failed to write test file %s: %v", name, err)
		}
	}

	// Create a subdirectory
	subDir := filepath.Join(tempDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	// Create a file in the subdirectory
	subFile := filepath.Join(subDir, "subfile.txt")
	if err := os.WriteFile(
		subFile, []byte("subcontent"), 0644); err != nil {
		t.Fatalf("Failed to write subfile: %v", err)
	}

	// Create a symlink to the subdirectory
	symlinkPath := filepath.Join(tempDir, "symlink_to_dir")
	if err := os.Symlink("subdir", symlinkPath); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	// Test hashing - this should not fail with "is a directory" error
	fp, err := hashFiles(tempDir)
	if err != nil {
		t.Fatalf("hashFiles() error = %v", err)
	}

	if fp.mode != modeSHA1 {
		t.Errorf("hashFiles() mode = %v, want %v", fp.mode, modeSHA1)
	}
	if fp.hash == "" {
		t.Error("hashFiles() returned empty hash")
	}
}

func TestParseAndHashDockerfile(t *testing.T) {
	// Create a temporary Dockerfile
	tempDir, err := os.MkdirTemp("", "docker-reuse-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dockerfileContent := `FROM ubuntu:20.04
COPY file1.txt /app/
COPY file2.txt /app/
ARG BUILD_ARG=value
RUN echo "test"
`
	dockerfilePath := filepath.Join(tempDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfileContent),
		0644); err != nil {
		t.Fatalf("Failed to write Dockerfile: %v", err)
	}

	// Test parsing and hashing
	sources, fp, err := parseAndHashDockerfile(dockerfilePath)
	if err != nil {
		t.Fatalf("parseAndHashDockerfile() error = %v", err)
	}

	if fp.mode != modeSHA1 {
		t.Errorf("parseAndHashDockerfile() mode = %v, want %v",
			fp.mode, modeSHA1)
	}
	if fp.hash == "" {
		t.Error("parseAndHashDockerfile() returned empty hash")
	}

	// Check that sources were correctly extracted
	expectedSources := []string{"file1.txt", "file2.txt"}
	if len(sources) != len(expectedSources) {
		t.Errorf("parseAndHashDockerfile() sources length = %v, "+
			"want %v", len(sources), len(expectedSources))
	}
	for i, source := range sources {
		if source != expectedSources[i] {
			t.Errorf("parseAndHashDockerfile() sources[%d] = %v, "+
				"want %v", i, source, expectedSources[i])
		}
	}
}
