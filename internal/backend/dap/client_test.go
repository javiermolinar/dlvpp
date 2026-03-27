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

func TestLaunchNext(t *testing.T) {
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

	if _, err := client.CreateBreakpoint(ctx, backend.BreakpointSpec{Location: "main.main"}); err != nil {
		t.Fatalf("create breakpoint: %v", err)
	}
	if _, err := client.Continue(ctx); err != nil {
		t.Fatalf("continue: %v", err)
	}
	state, err := client.Next(ctx)
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if state == nil {
		t.Fatal("expected stop state")
	}
	frames, err := client.Stack(ctx, state.ThreadID, 1)
	if err != nil {
		t.Fatalf("stack after next: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("expected stack frame after next")
	}
	if frames[0].Location.Line <= 10 {
		t.Fatalf("expected next to advance beyond line 10, got %d", frames[0].Location.Line)
	}
}
