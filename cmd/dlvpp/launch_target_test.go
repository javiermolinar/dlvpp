package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewLaunchRequestKeepsRawDirectoryTarget(t *testing.T) {
	t.Parallel()

	req, err := newLaunchRequest("./examples/hello")
	if err != nil {
		t.Fatalf("newLaunchRequest returned error: %v", err)
	}
	if req.Target != "./examples/hello" {
		t.Fatalf("unexpected target: %q", req.Target)
	}
}

func TestNewLaunchRequestRejectsExistingGoFile(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	targetFile := filepath.Join(tempDir, "cmd", "app", "main.go")
	mustWriteFile(t, targetFile, "package main\n")

	_, err := newLaunchRequest(targetFile)
	if err == nil {
		t.Fatal("expected newLaunchRequest to reject .go file target")
	}
	if !strings.Contains(err.Error(), targetFile) {
		t.Fatalf("expected error to mention file target, got %q", err)
	}
	if !strings.Contains(err.Error(), filepath.Dir(targetFile)) {
		t.Fatalf("expected error to suggest package dir, got %q", err)
	}
}

func TestNewLaunchRequestLeavesImportPathUnchanged(t *testing.T) {
	t.Parallel()

	req, err := newLaunchRequest("example.com/app/cmd/tool")
	if err != nil {
		t.Fatalf("newLaunchRequest returned error: %v", err)
	}
	if req.Target != "example.com/app/cmd/tool" {
		t.Fatalf("unexpected target: %q", req.Target)
	}
}

func TestNewLaunchRequestLeavesMissingGoFileUntouched(t *testing.T) {
	t.Parallel()

	target := filepath.Join(t.TempDir(), "missing.go")
	req, err := newLaunchRequest(target)
	if err != nil {
		t.Fatalf("newLaunchRequest returned error: %v", err)
	}
	if req.Target != target {
		t.Fatalf("expected target %q, got %q", target, req.Target)
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir parent for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
