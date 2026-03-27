package dap

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"dlvpp/internal/backend"
)

func TestLaunchClose(t *testing.T) {
	if _, err := exec.LookPath("dlv"); err != nil {
		t.Skip("dlv not found in PATH")
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve test file path")
	}

	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	target := filepath.Join(repoRoot, "examples", "hello")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := New()
	if err := client.Launch(ctx, backend.LaunchRequest{
		Mode:    backend.LaunchModeDebug,
		Target:  target,
		WorkDir: repoRoot,
	}); err != nil {
		t.Fatalf("launch failed: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Fatalf("close failed: %v", err)
		}
	}()
}
