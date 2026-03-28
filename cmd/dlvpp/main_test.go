package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dlvpp/internal/backend"
	"dlvpp/internal/session"
)

func TestParseLaunchArgsDefaultsToSticky(t *testing.T) {
	t.Parallel()

	target, sticky, err := parseLaunchArgs([]string{"./examples/hello"})
	if err != nil {
		t.Fatalf("parseLaunchArgs returned error: %v", err)
	}
	if target != "./examples/hello" {
		t.Fatalf("unexpected target: %q", target)
	}
	if !sticky {
		t.Fatal("expected sticky to be enabled by default")
	}
}

func TestParseLaunchArgsPlainDisablesSticky(t *testing.T) {
	t.Parallel()

	target, sticky, err := parseLaunchArgs([]string{"--plain", "./examples/hello"})
	if err != nil {
		t.Fatalf("parseLaunchArgs returned error: %v", err)
	}
	if target != "./examples/hello" {
		t.Fatalf("unexpected target: %q", target)
	}
	if sticky {
		t.Fatal("expected plain mode to disable sticky output")
	}
}

func TestParseAttachArgsPlainDisablesSticky(t *testing.T) {
	t.Parallel()

	pid, sticky, err := parseAttachArgs([]string{"-p", "123"})
	if err != nil {
		t.Fatalf("parseAttachArgs returned error: %v", err)
	}
	if pid != 123 {
		t.Fatalf("unexpected pid: %d", pid)
	}
	if sticky {
		t.Fatal("expected plain mode to disable sticky output")
	}
}

func TestUsageDescribesModesAndCommands(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	usage(&output)
	text := output.String()
	if !strings.Contains(text, "Modes:") || !strings.Contains(text, "-p, --plain") {
		t.Fatalf("expected mode help, got %q", text)
	}
	if !strings.Contains(text, "Interactive commands:") || !strings.Contains(text, commandLoopHelp) {
		t.Fatalf("expected interactive command legend, got %q", text)
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
	if strings.Contains(output.String(), commandLoopHelp) {
		t.Fatalf("expected plain mode without commands help, got %q", output.String())
	}
}

func TestRunCommandLoopRunsStepIn(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{
		snapshots: []*session.Snapshot{{State: backend.StopState{}}},
	}
	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString("s\nq\n"), &output, runner, runner.currentSnapshot(), false); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	if len(runner.actions) == 0 || runner.actions[0] != session.ActionStepIn {
		t.Fatalf("expected step-in action, got %v", runner.actions)
	}
}

func TestRunCommandLoopShowsLocals(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{
		locals: []backend.Variable{{Name: "total", Type: "int", Value: "42"}},
		snapshots: []*session.Snapshot{{
			State: backend.StopState{},
			Frame: &backend.Frame{Ref: backend.FrameRef{GoroutineID: 1, Index: 2}, Location: backend.SourceLocation{File: "main.go", Line: 12, Function: "main.main"}},
		}},
	}
	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString("l\nq\n"), &output, runner, runner.currentSnapshot(), false); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	if !strings.Contains(output.String(), "locals main.main main.go:12") || !strings.Contains(output.String(), "total (int) = 42") {
		t.Fatalf("expected locals output, got %q", output.String())
	}
}

func TestRunCommandLoopShowsOutputInspection(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{
		output: []backend.OutputEntry{
			{Category: backend.OutputCategoryStdout, Text: "hello\n"},
			{Category: backend.OutputCategoryStderr, Text: "boom\n"},
		},
		snapshots: []*session.Snapshot{{
			State: backend.StopState{},
			Frame: &backend.Frame{Ref: backend.FrameRef{GoroutineID: 1, Index: 2}, Location: backend.SourceLocation{File: "main.go", Line: 12, Function: "main.main"}},
		}},
	}
	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString("o\nq\n"), &output, runner, runner.currentSnapshot(), false); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	text := output.String()
	if !strings.Contains(text, "output main.main main.go:12") || !strings.Contains(text, "stdout | hello") || !strings.Contains(text, "stderr | boom") {
		t.Fatalf("expected output inspection, got %q", text)
	}
}

func TestFormatTTYLocalsAddsColorByRole(t *testing.T) {
	t.Parallel()

	out := formatTTYLocals([]backend.Variable{
		{Name: "message", Type: "string", Value: "\"hello\""},
		{Name: "total", Type: "int", Value: "42"},
		{Name: "ok", Type: "bool", Value: "true"},
		{Name: "err", Type: "error", Value: "nil"},
	})
	for _, want := range []string{ansiCyan + "message" + ansiReset, ansiDim + "(string)" + ansiReset, ansiGreen + "\"hello\"" + ansiReset, ansiMagenta + "42" + ansiReset, ansiYellow + "true" + ansiReset, ansiRed + "nil" + ansiReset} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected colored locals output to contain %q, got %q", want, out)
		}
	}
}

func TestFormatTTYOutputDifferentiatesStdoutAndStderr(t *testing.T) {
	t.Parallel()

	out := formatTTYOutput([]backend.OutputEntry{{Category: backend.OutputCategoryStdout, Text: "hello\n"}, {Category: backend.OutputCategoryStderr, Text: "boom\n"}})
	if !strings.Contains(out, ansiCyan+"stdout"+ansiReset+" | hello") {
		t.Fatalf("expected colored stdout output, got %q", out)
	}
	if !strings.Contains(out, ansiRed+"stderr"+ansiReset+" | boom") {
		t.Fatalf("expected colored stderr output, got %q", out)
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
	if !strings.Contains(output.String(), "bp 3 main.go:12") {
		t.Fatalf("expected breakpoint output, got %q", output.String())
	}
}

func TestFormatSnapshotForViewPlainUsesCompactTokenFriendlyOutput(t *testing.T) {
	t.Parallel()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	sourcePath := filepath.Clean(filepath.Join(wd, "..", "..", "examples", "hello", "main.go"))
	snapshot := &session.Snapshot{
		State: backend.StopState{},
		Frame: &backend.Frame{Location: backend.SourceLocation{File: sourcePath, Line: 4, Function: "main.main"}},
	}

	out := formatSnapshotForView(snapshot, &viewState{sticky: false}, false)
	expectedHeader := "stop main.main " + displayPath(sourcePath) + ":4"
	if !strings.Contains(out, expectedHeader) {
		t.Fatalf("expected compact stop header %q, got %q", expectedHeader, out)
	}
	if !strings.Contains(out, "> 4 |") {
		t.Fatalf("expected compact source window, got %q", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("expected plain output without ANSI escapes, got %q", out)
	}
	if strings.Contains(out, commandLoopHelp) {
		t.Fatalf("expected plain snapshot without repeated help, got %q", out)
	}
}

func TestRunCommandLoopPlainOmitsHelpLegend(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{
		snapshots: []*session.Snapshot{{State: backend.StopState{}}},
	}
	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString("n\nn\nq\n"), &output, runner, runner.currentSnapshot(), false); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	if strings.Contains(output.String(), commandLoopHelp) {
		t.Fatalf("expected plain mode without help legend, got %q", output.String())
	}
}

func TestRunCommandLoopStickyShowsHelpLegend(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{
		snapshots: []*session.Snapshot{{State: backend.StopState{}}},
	}
	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString("q\n"), &output, runner, runner.currentSnapshot(), true); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	if !strings.Contains(output.String(), commandLoopHelp) {
		t.Fatalf("expected sticky mode help legend, got %q", output.String())
	}
}

func TestRunCommandLoopStickyRendersSlidingWindowWhenEnabled(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	sourcePath := tempDir + "/main.go"
	var source strings.Builder
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&source, "line%02d\n", i)
	}
	if err := os.WriteFile(sourcePath, []byte(source.String()), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}

	runner := &fakeCommandRunner{
		snapshots: []*session.Snapshot{{
			State: backend.StopState{},
			Frame: &backend.Frame{Location: backend.SourceLocation{File: sourcePath, Line: 10, Function: "main.main"}},
		}},
	}
	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString("n\nq\n"), &output, runner, runner.currentSnapshot(), true); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	text := output.String()
	if !strings.Contains(text, "line05") || !strings.Contains(text, "line15") {
		t.Fatalf("expected sticky sliding window around current line, got %q", text)
	}
	if strings.Contains(text, "line01") || strings.Contains(text, "line20") {
		t.Fatalf("expected sticky output to avoid rendering the entire file/function, got %q", text)
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

func TestFormatSnapshotForViewShowsPromptAndHintsInStickyTTY(t *testing.T) {
	t.Parallel()

	state := &viewState{sticky: true, outputTTY: true}
	out := formatSnapshotForView(&session.Snapshot{State: backend.StopState{}}, state, false)
	if !strings.Contains(out, commandLoopHelp) {
		t.Fatalf("expected command hints, got %q", out)
	}
	if !strings.HasSuffix(out, ">") {
		t.Fatalf("expected prompt suffix, got %q", out)
	}
}

func TestFormatSnapshotForViewStickyTTYUsesTerminalHeight(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "main.txt")
	var source strings.Builder
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&source, "line%02d\n", i)
	}
	if err := os.WriteFile(sourcePath, []byte(source.String()), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}

	state := &viewState{sticky: true, outputTTY: true, outputHeight: 12}
	snapshot := &session.Snapshot{
		State: backend.StopState{},
		Frame: &backend.Frame{Location: backend.SourceLocation{File: sourcePath, Line: 10, Function: "main.main"}},
	}
	out := formatSnapshotForView(snapshot, state, false)
	for _, want := range []string{"line07", "line10", "line13"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected sticky tty output to contain %q, got %q", want, out)
		}
	}
	for _, unwanted := range []string{"line06", "line14"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("expected sticky tty output to respect terminal height and omit %q, got %q", unwanted, out)
		}
	}
}

func TestFormatSnapshotForViewPlainTTYSuppressesRepeatedHints(t *testing.T) {
	t.Parallel()

	state := &viewState{outputTTY: true}
	out := formatSnapshotForView(&session.Snapshot{State: backend.StopState{}}, state, false)
	if strings.Contains(out, commandLoopHelp) {
		t.Fatalf("expected plain tty output without repeated hints, got %q", out)
	}
	if !strings.HasSuffix(out, ">") {
		t.Fatalf("expected prompt suffix, got %q", out)
	}
}

func TestFormatSnapshotForViewAddsExitHint(t *testing.T) {
	t.Parallel()

	out := formatSnapshotForView(&session.Snapshot{State: backend.StopState{Exited: true, ExitStatus: 1}}, &viewState{}, false)
	if !strings.Contains(out, "program exited; press o to inspect captured output, q to quit") {
		t.Fatalf("expected exit hint, got %q", out)
	}
}

func TestFormatInspectionForViewUsesBlankCanvasInStickyTTY(t *testing.T) {
	t.Parallel()

	state := &viewState{sticky: true, outputTTY: true}
	snapshot := &session.Snapshot{
		Frame: &backend.Frame{Location: backend.SourceLocation{File: "/tmp/main.go", Line: 12, Function: "main.main"}},
	}
	out := formatInspectionForView(snapshot, state, "locals", "total (int) = 42\n", true)
	if !strings.HasPrefix(out, clearScreenANSI) {
		t.Fatalf("expected clear screen prefix, got %q", out)
	}
	if !strings.Contains(out, "locals at ") || !strings.Contains(out, "total (int) = 42") {
		t.Fatalf("expected inspection screen, got %q", out)
	}
	if !strings.Contains(out, "[Esc to return]") {
		t.Fatalf("expected escape hint, got %q", out)
	}
	if !strings.Contains(out, commandLoopHelp) {
		t.Fatalf("expected command hints, got %q", out)
	}
	if !strings.HasSuffix(out, ">") {
		t.Fatalf("expected prompt suffix, got %q", out)
	}
}

func TestFormatInspectionForViewPlainUsesCompactHeader(t *testing.T) {
	t.Parallel()

	state := &viewState{}
	snapshot := &session.Snapshot{
		Frame: &backend.Frame{Location: backend.SourceLocation{File: "main.go", Line: 12, Function: "main.main"}},
	}
	out := formatInspectionForView(snapshot, state, "locals", "total (int) = 42\n", false)
	if !strings.Contains(out, "locals main.main main.go:12") {
		t.Fatalf("expected compact inspection header, got %q", out)
	}
	if strings.Contains(out, commandLoopHelp) {
		t.Fatalf("expected plain inspection without repeated hints, got %q", out)
	}
	if !strings.Contains(out, "total (int) = 42") {
		t.Fatalf("expected locals body, got %q", out)
	}
}

func TestRunDebuggerActionClearsInspection(t *testing.T) {
	t.Parallel()

	state := &viewState{inspectionTitle: "locals", inspectionBody: "total (int) = 42\n"}
	runner := &fakeCommandRunner{snapshots: []*session.Snapshot{{State: backend.StopState{}}}}
	var output bytes.Buffer
	if err := runDebuggerAction(context.Background(), &output, runner, state, session.ActionNext, "next"); err != nil {
		t.Fatalf("runDebuggerAction returned error: %v", err)
	}
	if hasInspection(state) {
		t.Fatalf("expected inspection to be cleared, got %#v", state)
	}
}

func TestRunCommandLoopKeepsSessionOpenAfterExitForOutputInspection(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{
		output: []backend.OutputEntry{{Category: backend.OutputCategoryStdout, Text: "done\n"}},
		snapshots: []*session.Snapshot{
			{State: backend.StopState{}},
			{State: backend.StopState{Exited: true, ExitStatus: 0}},
		},
	}
	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString("n\no\nq\n"), &output, runner, runner.currentSnapshot(), false); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	text := output.String()
	if len(runner.actions) != 1 {
		t.Fatalf("expected exactly one debugger action, got %v", runner.actions)
	}
	if !strings.Contains(text, "exit 0") {
		t.Fatalf("expected exit output, got %q", text)
	}
	if !strings.Contains(text, "program exited; press o to inspect captured output, q to quit") {
		t.Fatalf("expected exit hint, got %q", text)
	}
	if !strings.Contains(text, "OUTPUT-BEGIN\nstdout | done\nOUTPUT-END") {
		t.Fatalf("expected plain exit output block, got %q", text)
	}
	if !strings.Contains(text, "output\nstdout | done") {
		t.Fatalf("expected output inspection after exit, got %q", text)
	}
}

func TestRunCommandLoopRejectsSteppingAfterExit(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{
		snapshots: []*session.Snapshot{{State: backend.StopState{Exited: true, ExitStatus: 0}}},
	}
	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString("n\nq\n"), &output, runner, runner.currentSnapshot(), false); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	if len(runner.actions) != 0 {
		t.Fatalf("expected no debugger actions after exit, got %v", runner.actions)
	}
	if !strings.Contains(output.String(), "program already exited") {
		t.Fatalf("expected exited error, got %q", output.String())
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
	defer func() { _ = reader.Close() }()

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
		_ = writer.Close()
		t.Fatal("readCommand did not return after context cancel")
	}

	_ = writer.Close()
}

func TestReadByteReturnsOnContextCancelWhileBlocked(t *testing.T) {
	t.Parallel()

	reader, writer := io.Pipe()
	defer func() { _ = reader.Close() }()

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
		_ = writer.Close()
		t.Fatal("readByte did not return after context cancel")
	}

	_ = writer.Close()
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
	locals          []backend.Variable
	output          []backend.OutputEntry
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

func (f *fakeCommandRunner) Locals(_ context.Context, _ backend.FrameRef) ([]backend.Variable, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.locals, nil
}

func (f *fakeCommandRunner) Output(_ context.Context) ([]backend.OutputEntry, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.output, nil
}
