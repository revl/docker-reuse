package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectSourcesFromDockerfile(t *testing.T) {
	tests := []struct {
		name            string
		dockerfile      string
		expectedSources []string
	}{
		{
			name: "Basic COPY commands",
			dockerfile: `FROM ubuntu:20.04
COPY file1.txt /app/
COPY file2.txt /app/
COPY file3.txt /app/`,
			expectedSources: []string{
				"file1.txt", "file2.txt", "file3.txt"},
		},
		{
			name: "ADD commands",
			dockerfile: `FROM ubuntu:20.04
ADD file1.txt /app/
ADD file2.txt /app/`,
			expectedSources: []string{
				"file1.txt", "file2.txt"},
		},
		{
			name: "Mixed COPY and ADD commands",
			dockerfile: `FROM ubuntu:20.04
COPY file1.txt /app/
ADD file2.txt /app/
COPY file3.txt /app/`,
			expectedSources: []string{
				"file1.txt", "file2.txt", "file3.txt"},
		},
		{
			name: "Multi-stage build with --from",
			dockerfile: `FROM ubuntu:20.04 AS builder
COPY file1.txt /app/
FROM ubuntu:20.04
COPY --from=builder /app/file1.txt /app/
COPY file2.txt /app/`,
			expectedSources: []string{"file1.txt", "file2.txt"},
		},
		{
			name: "No COPY or ADD commands",
			dockerfile: `FROM ubuntu:20.04
RUN echo "test"
ENV TEST=value`,
			expectedSources: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory
			tempDir, err := os.MkdirTemp("", "docker-reuse-test-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tempDir)

			// Create Dockerfile
			dockerfilePath := filepath.Join(tempDir, "Dockerfile")
			if err := os.WriteFile(dockerfilePath,
				[]byte(tt.dockerfile), 0644); err != nil {
				t.Fatalf("Failed to write Dockerfile: %v", err)
			}

			// Open Dockerfile
			f, err := os.Open(dockerfilePath)
			if err != nil {
				t.Fatalf("Failed to open Dockerfile: %v", err)
			}
			defer f.Close()

			// Test source collection
			sources, err := collectSourcesFromDockerfile(f)
			if err != nil {
				t.Fatalf("collectSourcesFromDockerfile() "+
					"error = %v", err)
			}

			// Check number of sources
			if len(sources) != len(tt.expectedSources) {
				t.Errorf("collectSourcesFromDockerfile() "+
					"returned %d sources, want %d",
					len(sources), len(tt.expectedSources))
			}

			// Check each source
			for i, source := range sources {
				if source != tt.expectedSources[i] {
					t.Errorf("collectSourcesFromDockerfile() "+
						"sources[%d] = %v, want %v",
						i, source,
						tt.expectedSources[i])
				}
			}
		})
	}
}
