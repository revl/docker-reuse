package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupGitRepo(t *testing.T) (string, func()) {
	// Create a temporary directory for the git repository
	tempDir, err := os.MkdirTemp("", "docker-reuse-git-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Initialize git repository
	cmd := exec.Command("git", "init")
	cmd.Dir = tempDir
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to initialize git repository: %v", err)
	}

	// Configure git user
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tempDir
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to configure git user: %v", err)
	}

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tempDir
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to configure git user: %v", err)
	}

	// Configure git to allow commits without a user
	cmd = exec.Command("git", "config", "commit.gpgsign", "false")
	cmd.Dir = tempDir
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to configure git: %v", err)
	}

	// Create cleanup function
	cleanup := func() {
		os.RemoveAll(tempDir)
	}

	return tempDir, cleanup
}

func TestGetLastCommitHash(t *testing.T) {
	repoDir, cleanup := setupGitRepo(t)
	defer cleanup()

	// Create a test file
	testFile := filepath.Join(repoDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"),
		0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Add and commit the file
	cmd := exec.Command("git", "add", "test.txt")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to add file: %v", err)
	}

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

	// Test getting commit hash
	fp, err := getLastCommitHash(repoDir)
	if err != nil {
		t.Fatalf("getLastCommitHash() error = %v", err)
	}

	if fp.mode != modeCommit {
		t.Errorf("getLastCommitHash() mode = %v, want %v",
			fp.mode, modeCommit)
	}
	if fp.hash == "" {
		t.Error("getLastCommitHash() returned empty hash")
	}

	// Test with non-git directory
	nonGitDir, err := os.MkdirTemp("", "docker-reuse-non-git-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(nonGitDir)

	_, err = getLastCommitHash(nonGitDir)
	if err == nil {
		t.Error("getLastCommitHash() expected error for non-git " +
			"directory")
	}

	// Test with modified files
	if err := os.WriteFile(testFile, []byte("modified content"),
		0644); err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	_, err = getLastCommitHash(repoDir)
	if err == nil {
		t.Error("getLastCommitHash() expected error for modified files")
	}
}
