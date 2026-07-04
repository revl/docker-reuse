package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectSourcesFromDockerfile(t *testing.T) {
	tests := []struct {
		name            string
		dockerfile      string
		buildArgs       []string
		expectedSources []string
		expectedError   string
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
		{
			name: "ARG expansion from build argument",
			dockerfile: `FROM ubuntu:20.04
ARG APP
COPY apps/${APP} /workdir/apps/${APP}`,
			buildArgs:       []string{"APP=web1"},
			expectedSources: []string{"apps/web1"},
		},
		{
			name: "ARG expansion without braces",
			dockerfile: `FROM ubuntu:20.04
ARG APP
COPY apps/$APP /workdir/`,
			buildArgs:       []string{"APP=web1"},
			expectedSources: []string{"apps/web1"},
		},
		{
			name: "ARG default value",
			dockerfile: `FROM ubuntu:20.04
ARG APP=web2
COPY apps/${APP} /workdir/`,
			expectedSources: []string{"apps/web2"},
		},
		{
			name: "Build argument overrides ARG default",
			dockerfile: `FROM ubuntu:20.04
ARG APP=web2
COPY apps/${APP} /workdir/`,
			buildArgs:       []string{"APP=web3"},
			expectedSources: []string{"apps/web3"},
		},
		{
			name: "Multiple ARG references in one source",
			dockerfile: `FROM ubuntu:20.04
ARG DIR=apps
ARG APP
COPY ${DIR}/${APP}/package.json /workdir/`,
			buildArgs:       []string{"APP=web1"},
			expectedSources: []string{"apps/web1/package.json"},
		},
		{
			name: "Undefined ARG in COPY source",
			dockerfile: `FROM ubuntu:20.04
COPY apps/${APP} /workdir/`,
			expectedError: "build argument 'APP' is not set",
		},
		{
			name: "Declared ARG without a value",
			dockerfile: `FROM ubuntu:20.04
ARG APP
COPY apps/${APP} /workdir/`,
			expectedError: "build argument 'APP' is not set",
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
			sources, err := collectSourcesFromDockerfile(
				f, tt.buildArgs)
			if tt.expectedError != "" {
				if err == nil || !strings.Contains(
					err.Error(), tt.expectedError) {
					t.Fatalf("collectSourcesFromDockerfile() "+
						"error = %v, want %q",
						err, tt.expectedError)
				}
				return
			}
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
