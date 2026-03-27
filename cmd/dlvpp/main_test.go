package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"dlvpp/internal/backend"
	"dlvpp/internal/session"
)

func TestParseLaunchArgs(t *testing.T) {
	t.Parallel()

	target, sticky, err := parseLaunchArgs([]string{"-s", "./examples/hello"})
	if err != nil {
		t.Fatalf("parseLaunchArgs returned error: %v", err)
	}
	if target != "./examples/hello" {
		t.Fatalf("unexpected target: %q", target)
	}
	if !sticky {
		t.Fatal("expected sticky to be enabled")
	}
}

func TestParseAttachArgs(t *testing.T) {
	t.Parallel()

	pid, sticky, err := parseAttachArgs([]string{"--sticky", "123"})
	if err != nil {
		t.Fatalf("parseAttachArgs returned error: %v", err)
	}
	if pid != 123 {
		t.Fatalf("unexpected pid: %d", pid)
	}
	if !sticky {
		t.Fatal("expected sticky to be enabled")
	}
}

func TestRunCommandLoopReturnsOnEOF(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBuffer(nil), &output, &fakeCommandRunner{}, nil, false); err != nil {
		t.Fatalf("runCommandLoop returned error on EOF: %v", err)
	}
}

func TestRunCommandLoopReturnsOnQuit(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString("q\n"), &output, &fakeCommandRunner{}, nil, false); err != nil {
		t.Fatalf("runCommandLoop returned error on quit: %v", err)
	}
}

func TestRunCommandLoopRunsNext(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{
		snapshots: []*session.Snapshot{{State: backend.StopState{}}},
	}
	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString("n\nq\n"), &output, runner, runner.currentSnapshot(), false); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	if len(runner.actions) == 0 || runner.actions[0] != session.ActionNext {
		t.Fatalf("expected next action, got %v", runner.actions)
	}
	if !strings.Contains(output.String(), commandLoopHelp) {
		t.Fatalf("expected commands help, got %q", output.String())
	}
}

func TestRunCommandLoopRejectsLongFormCommands(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString("next\nq\n"), &output, &fakeCommandRunner{}, nil, false); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	if !strings.Contains(output.String(), "unknown command: next") {
		t.Fatalf("expected unknown command output, got %q", output.String())
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
	if err := runCommandLoop(context.Background(), bytes.NewBufferString(":b main.go:12\nq\n"), &output, runner, runner.currentSnapshot(), false); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	if len(runner.breakpointSpecs) == 0 || runner.breakpointSpecs[0].Location != "main.go:12" {
		t.Fatalf("expected breakpoint location, got %#v", runner.breakpointSpecs)
	}
	if !strings.Contains(output.String(), "breakpoint 3 at main.go:12") {
		t.Fatalf("expected breakpoint output, got %q", output.String())
	}
}

func TestRunCommandLoopStickyRendersCurrentFunctionWhenEnabled(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	sourcePath := tempDir + "/main.go"
	source := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}

	runner := &fakeCommandRunner{
		snapshots: []*session.Snapshot{{
			State: backend.StopState{},
			Frame: &backend.Frame{Location: backend.SourceLocation{File: sourcePath, Line: 3, Function: "main.main"}},
		}},
	}
	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString("n\nq\n"), &output, runner, runner.currentSnapshot(), true); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	if !strings.Contains(output.String(), "println(") {
		t.Fatalf("expected sticky output, got %q", output.String())
	}
}

func TestRunCommandLoopStickyPersistsAcrossStepSnapshots(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	sourcePath := tempDir + "/main.go"
	source := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n\tprintln(\"bye\")\n}\n"
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}

	runner := &fakeCommandRunner{
		snapshots: []*session.Snapshot{
			{State: backend.StopState{}, Frame: &backend.Frame{Location: backend.SourceLocation{File: sourcePath, Line: 3, Function: "main.main"}}},
			{State: backend.StopState{}, Frame: &backend.Frame{Location: backend.SourceLocation{File: sourcePath, Line: 4, Function: "main.main"}}},
		},
	}
	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString("n\nq\n"), &output, runner, runner.currentSnapshot(), true); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	if !strings.Contains(output.String(), ">   4") {
		t.Fatalf("expected stepped sticky rerender to use latest snapshot, got %q", output.String())
	}
	if strings.Contains(output.String(), clearScreenANSI) {
		t.Fatalf("expected no clear-screen escape in line mode, got %q", output.String())
	}
}

func TestRunCommandLoopStopsAfterExitSnapshot(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{
		snapshots: []*session.Snapshot{{State: backend.StopState{Exited: true, ExitStatus: 0}}},
	}
	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString("n\nn\n"), &output, runner, runner.currentSnapshot(), false); err != nil {
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
	err := runCommandLoop(ctx, bytes.NewBuffer(nil), &output, &fakeCommandRunner{}, nil, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestReadCommandReturnsOnContextCancelWhileBlocked(t *testing.T) {
	t.Parallel()

	reader, writer := io.Pipe()
	defer reader.Close()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := readCommand(ctx, bufio.NewReader(reader))
		result <- err
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		writer.Close()
		t.Fatal("readCommand did not return after context cancel")
	}

	writer.Close()
}

func TestReadByteReturnsOnContextCancelWhileBlocked(t *testing.T) {
	t.Parallel()

	reader, writer := io.Pipe()
	defer reader.Close()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := readByte(ctx, reader)
		result <- err
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		writer.Close()
		t.Fatal("readByte did not return after context cancel")
	}

	writer.Close()
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
	snapshots       []*session.Snapshot
	snapshotIndex   int
	breakpoint      *backend.Breakpoint
	err             error
}

func (f *fakeCommandRunner) Do(_ context.Context, action session.Action) (*session.Snapshot, error) {
	f.actions = append(f.actions, action)
	if f.err != nil {
		return nil, f.err
	}
	if len(f.snapshots) == 0 {
		return nil, nil
	}
	if f.snapshotIndex < len(f.snapshots)-1 {
		f.snapshotIndex++
	}
	return f.snapshots[f.snapshotIndex], nil
}

func (f *fakeCommandRunner) currentSnapshot() *session.Snapshot {
	if len(f.snapshots) == 0 {
		return nil
	}
	if f.snapshotIndex >= len(f.snapshots) {
		return f.snapshots[len(f.snapshots)-1]
	}
	return f.snapshots[f.snapshotIndex]
}

func (f *fakeCommandRunner) CreateBreakpoint(_ context.Context, spec backend.BreakpointSpec) (*backend.Breakpoint, error) {
	f.breakpointSpecs = append(f.breakpointSpecs, spec)
	if f.err != nil {
		return nil, f.err
	}
	return f.breakpoint, nil
}
