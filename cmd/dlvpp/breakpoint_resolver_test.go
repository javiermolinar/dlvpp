package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"dlvpp/internal/backend"
	"dlvpp/internal/session"
)

func TestResolveBreakpointLocationLineUsesCurrentFile(t *testing.T) {
	t.Parallel()

	sourcePath := writeBreakpointResolverFixture(t, "package main\n\nfunc add(a, b int) int {\n\treturn a + b\n}\n")
	got, err := resolveBreakpointLocation("14", breakpointResolveContext{
		Snapshot: &session.Snapshot{Frame: &backend.Frame{Location: backend.SourceLocation{File: sourcePath, Line: 3, Function: "main.add"}}},
	})
	if err != nil {
		t.Fatalf("resolveBreakpointLocation returned error: %v", err)
	}
	want := sourcePath + ":14"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestResolveBreakpointLocationBareSymbolUsesCurrentPackage(t *testing.T) {
	t.Parallel()

	sourcePath := writeBreakpointResolverFixture(t, "package main\n\nfunc add(a, b int) int {\n\treturn a + b\n}\n")
	got, err := resolveBreakpointLocation("add", breakpointResolveContext{
		Snapshot: &session.Snapshot{Frame: &backend.Frame{Location: backend.SourceLocation{File: sourcePath, Line: 3, Function: "main.main"}}},
	})
	if err != nil {
		t.Fatalf("resolveBreakpointLocation returned error: %v", err)
	}
	if got != "main.add" {
		t.Fatalf("expected %q, got %q", "main.add", got)
	}
}

func TestRunCommandLoopResolvesLineBreakpointAgainstCurrentFile(t *testing.T) {
	t.Parallel()

	sourcePath := writeBreakpointResolverFixture(t, "package main\n\nfunc main() {}\n")
	runner := &fakeCommandRunner{
		breakpoint: &backend.Breakpoint{ID: 1, Location: backend.SourceLocation{File: sourcePath, Line: 14}},
		snapshots: []*session.Snapshot{{
			State: backend.StopState{},
			Frame: &backend.Frame{Location: backend.SourceLocation{File: sourcePath, Line: 3, Function: "main.main"}},
		}},
	}

	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString(":b 14\nq\n"), &output, runner, runner.currentSnapshot(), false); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	if len(runner.breakpointSpecs) != 1 {
		t.Fatalf("expected one breakpoint spec, got %#v", runner.breakpointSpecs)
	}
	if got, want := runner.breakpointSpecs[0].Location, sourcePath+":14"; got != want {
		t.Fatalf("expected breakpoint location %q, got %q", want, got)
	}
}

func TestRunCommandLoopResolvesBareSymbolBreakpointAgainstCurrentPackage(t *testing.T) {
	t.Parallel()

	sourcePath := writeBreakpointResolverFixture(t, "package main\n\nfunc add(a, b int) int {\n\treturn a + b\n}\n")
	runner := &fakeCommandRunner{
		breakpoint: &backend.Breakpoint{ID: 2, Location: backend.SourceLocation{Function: "main.add"}},
		snapshots: []*session.Snapshot{{
			State: backend.StopState{},
			Frame: &backend.Frame{Location: backend.SourceLocation{File: sourcePath, Line: 3, Function: "main.main"}},
		}},
	}

	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString(":b add\nq\n"), &output, runner, runner.currentSnapshot(), false); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	if len(runner.breakpointSpecs) != 1 {
		t.Fatalf("expected one breakpoint spec, got %#v", runner.breakpointSpecs)
	}
	if got, want := runner.breakpointSpecs[0].Location, "main.add"; got != want {
		t.Fatalf("expected breakpoint location %q, got %q", want, got)
	}
}

func writeBreakpointResolverFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}
