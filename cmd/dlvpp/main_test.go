package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"dlvpp/internal/backend"
	"dlvpp/internal/session"
)

func TestRunCommandLoopReturnsOnEOF(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBuffer(nil), &output, &fakeCommandRunner{}); err != nil {
		t.Fatalf("runCommandLoop returned error on EOF: %v", err)
	}
}

func TestRunCommandLoopReturnsOnQuit(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString("q\n"), &output, &fakeCommandRunner{}); err != nil {
		t.Fatalf("runCommandLoop returned error on quit: %v", err)
	}
}

func TestRunCommandLoopRunsNext(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{
		snapshot: &session.Snapshot{State: backend.StopState{}},
	}
	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString("n\nq\n"), &output, runner); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	if len(runner.actions) == 0 || runner.actions[0] != session.ActionNext {
		t.Fatalf("expected next action, got %v", runner.actions)
	}
	if !strings.Contains(output.String(), "Commands: n=next, :=command, q=quit") {
		t.Fatalf("expected commands help, got %q", output.String())
	}
}

func TestRunCommandLoopCreatesBreakpointFromColonCommand(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{
		breakpoint: &backend.Breakpoint{
			ID:       3,
			Location: backend.SourceLocation{File: "main.go", Line: 12},
		},
	}
	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString(":b main.go:12\nq\n"), &output, runner); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	if len(runner.breakpointSpecs) == 0 || runner.breakpointSpecs[0].Location != "main.go:12" {
		t.Fatalf("expected breakpoint location, got %#v", runner.breakpointSpecs)
	}
	if !strings.Contains(output.String(), "breakpoint 3 at main.go:12") {
		t.Fatalf("expected breakpoint output, got %q", output.String())
	}
}

func TestRunCommandLoopStopsAfterExitSnapshot(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{
		snapshot: &session.Snapshot{State: backend.StopState{Exited: true, ExitStatus: 0}},
	}
	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString("n\nn\n"), &output, runner); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	if len(runner.actions) != 1 {
		t.Fatalf("expected command loop to stop after exit, got actions %v", runner.actions)
	}
	if !strings.Contains(output.String(), "exited with status 0") {
		t.Fatalf("expected exit output, got %q", output.String())
	}
}

func TestRunCommandLoopReturnsContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var output bytes.Buffer
	err := runCommandLoop(ctx, bytes.NewBuffer(nil), &output, &fakeCommandRunner{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestNewlineWriterConvertsLineFeedsForRawTTYOutput(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	writer := newlineWriter{Writer: &output}
	if _, err := writer.Write([]byte("a\nb\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := output.String(); got != "a\r\nb\r\n" {
		t.Fatalf("unexpected converted output: %q", got)
	}
}

type fakeCommandRunner struct {
	actions         []session.Action
	breakpointSpecs []backend.BreakpointSpec
	snapshot        *session.Snapshot
	breakpoint      *backend.Breakpoint
	err             error
}

func (f *fakeCommandRunner) Do(_ context.Context, action session.Action) (*session.Snapshot, error) {
	f.actions = append(f.actions, action)
	if f.err != nil {
		return nil, f.err
	}
	return f.snapshot, nil
}

func (f *fakeCommandRunner) CreateBreakpoint(_ context.Context, spec backend.BreakpointSpec) (*backend.Breakpoint, error) {
	f.breakpointSpecs = append(f.breakpointSpecs, spec)
	if f.err != nil {
		return nil, f.err
	}
	return f.breakpoint, nil
}
