package main

import (
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

	target, programArgs, sticky, verbose, err := parseLaunchArgs([]string{"./examples/hello"})
	if err != nil {
		t.Fatalf("parseLaunchArgs returned error: %v", err)
	}
	if target != "./examples/hello" {
		t.Fatalf("unexpected target: %q", target)
	}
	if len(programArgs) != 0 {
		t.Fatalf("expected no program args, got %#v", programArgs)
	}
	if !sticky {
		t.Fatal("expected sticky to be enabled by default")
	}
	if verbose {
		t.Fatal("expected verbose to be disabled by default")
	}
}

func TestParseLaunchArgsPlainDisablesSticky(t *testing.T) {
	t.Parallel()

	target, programArgs, sticky, verbose, err := parseLaunchArgs([]string{"--plain", "./examples/hello"})
	if err != nil {
		t.Fatalf("parseLaunchArgs returned error: %v", err)
	}
	if target != "./examples/hello" {
		t.Fatalf("unexpected target: %q", target)
	}
	if len(programArgs) != 0 {
		t.Fatalf("expected no program args, got %#v", programArgs)
	}
	if sticky {
		t.Fatal("expected plain mode to disable sticky output")
	}
	if verbose {
		t.Fatal("expected verbose to remain disabled")
	}
}

func TestParseLaunchArgsVerboseEnablesStartupLogs(t *testing.T) {
	t.Parallel()

	_, programArgs, sticky, verbose, err := parseLaunchArgs([]string{"--verbose", "./examples/hello"})
	if err != nil {
		t.Fatalf("parseLaunchArgs returned error: %v", err)
	}
	if len(programArgs) != 0 {
		t.Fatalf("expected no program args, got %#v", programArgs)
	}
	if !sticky {
		t.Fatal("expected sticky to remain enabled")
	}
	if !verbose {
		t.Fatal("expected verbose to be enabled")
	}
}

func TestParseLaunchArgsSupportsProgramArgsAfterSeparator(t *testing.T) {
	t.Parallel()

	target, programArgs, sticky, verbose, err := parseLaunchArgs([]string{"./examples/hello", "--", "--name", "alice"})
	if err != nil {
		t.Fatalf("parseLaunchArgs returned error: %v", err)
	}
	if target != "./examples/hello" {
		t.Fatalf("unexpected target: %q", target)
	}
	wantArgs := []string{"--name", "alice"}
	if strings.Join(programArgs, "|") != strings.Join(wantArgs, "|") {
		t.Fatalf("unexpected program args: got %#v want %#v", programArgs, wantArgs)
	}
	if !sticky {
		t.Fatal("expected sticky to remain enabled")
	}
	if verbose {
		t.Fatal("expected verbose to remain disabled")
	}
}

func TestParseLaunchArgsRequiresSeparatorBeforeProgramArgs(t *testing.T) {
	t.Parallel()

	_, _, _, _, err := parseLaunchArgs([]string{"./examples/hello", "alice"})
	if err == nil {
		t.Fatal("expected parseLaunchArgs to reject extra positional args without --")
	}
	if !strings.Contains(err.Error(), "use -- to pass program args") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseTestArgsDefaultsToSticky(t *testing.T) {
	t.Parallel()

	target, selector, programArgs, sticky, verbose, err := parseTestArgs([]string{"./pkg/parser", "TestParse"})
	if err != nil {
		t.Fatalf("parseTestArgs returned error: %v", err)
	}
	if target != "./pkg/parser" {
		t.Fatalf("unexpected target: %q", target)
	}
	if selector != "TestParse" {
		t.Fatalf("unexpected selector: %q", selector)
	}
	if len(programArgs) != 0 {
		t.Fatalf("expected no test binary args, got %#v", programArgs)
	}
	if !sticky {
		t.Fatal("expected sticky to be enabled by default")
	}
	if verbose {
		t.Fatal("expected verbose to be disabled by default")
	}
}

func TestParseTestArgsRequiresSelector(t *testing.T) {
	t.Parallel()

	_, _, _, _, _, err := parseTestArgs([]string{"./pkg/parser"})
	if err == nil {
		t.Fatal("expected parseTestArgs to require a selector")
	}
	if !strings.Contains(err.Error(), "test or subtest name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseTestArgsPlainDisablesSticky(t *testing.T) {
	t.Parallel()

	target, selector, programArgs, sticky, verbose, err := parseTestArgs([]string{"--plain", "./pkg/parser", "TestParse/case-1"})
	if err != nil {
		t.Fatalf("parseTestArgs returned error: %v", err)
	}
	if target != "./pkg/parser" {
		t.Fatalf("unexpected target: %q", target)
	}
	if selector != "TestParse/case-1" {
		t.Fatalf("unexpected selector: %q", selector)
	}
	if len(programArgs) != 0 {
		t.Fatalf("expected no test binary args, got %#v", programArgs)
	}
	if sticky {
		t.Fatal("expected plain mode to disable sticky output")
	}
	if verbose {
		t.Fatal("expected verbose to remain disabled")
	}
}

func TestParseTestArgsSupportsBinaryArgsAfterSeparator(t *testing.T) {
	t.Parallel()

	target, selector, programArgs, sticky, verbose, err := parseTestArgs([]string{"./pkg/parser", "TestParse/case-1", "--", "-test.v"})
	if err != nil {
		t.Fatalf("parseTestArgs returned error: %v", err)
	}
	if target != "./pkg/parser" {
		t.Fatalf("unexpected target: %q", target)
	}
	if selector != "TestParse/case-1" {
		t.Fatalf("unexpected selector: %q", selector)
	}
	wantArgs := []string{"-test.v"}
	if strings.Join(programArgs, "|") != strings.Join(wantArgs, "|") {
		t.Fatalf("unexpected test binary args: got %#v want %#v", programArgs, wantArgs)
	}
	if !sticky {
		t.Fatal("expected sticky to remain enabled")
	}
	if verbose {
		t.Fatal("expected verbose to remain disabled")
	}
}

func TestParseTestArgsRequiresSeparatorBeforeBinaryArgs(t *testing.T) {
	t.Parallel()

	_, _, _, _, _, err := parseTestArgs([]string{"./pkg/parser", "TestParse", "-test.v"})
	if err == nil {
		t.Fatal("expected parseTestArgs to reject extra positional args without --")
	}
	if !strings.Contains(err.Error(), "use -- to pass test binary args") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseAttachArgsPlainDisablesSticky(t *testing.T) {
	t.Parallel()

	pid, sticky, verbose, err := parseAttachArgs([]string{"-p", "123"})
	if err != nil {
		t.Fatalf("parseAttachArgs returned error: %v", err)
	}
	if pid != 123 {
		t.Fatalf("unexpected pid: %d", pid)
	}
	if sticky {
		t.Fatal("expected plain mode to disable sticky output")
	}
	if verbose {
		t.Fatal("expected verbose to remain disabled")
	}
}

func TestUsageDescribesModesAndCommands(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	usage(&output)
	text := output.String()
	if !strings.Contains(text, "Modes:") || !strings.Contains(text, "-p, --plain") || !strings.Contains(text, "-v, --verbose") {
		t.Fatalf("expected mode help, got %q", text)
	}
	if !strings.Contains(text, "launch [-p|--plain] [-v|--verbose] <package-or-path> [-- <program-args...>]") {
		t.Fatalf("expected launch usage with program args separator, got %q", text)
	}
	if !strings.Contains(text, "test [-p|--plain] [-v|--verbose] <package-or-path> <test-or-subtest> [-- <test-binary-args...>]") {
		t.Fatalf("expected test usage with binary args separator, got %q", text)
	}
	if !strings.Contains(text, "Interactive commands:") || !strings.Contains(text, commandHelpSummary) || !strings.Contains(text, "Use h during a session") {
		t.Fatalf("expected interactive command help, got %q", text)
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

func TestRunCommandLoopRunsContinue(t *testing.T) {
	t.Parallel()

	runner := &fakeCommandRunner{
		snapshots: []*session.Snapshot{{State: backend.StopState{}}},
	}
	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString("c\nq\n"), &output, runner, runner.currentSnapshot(), false); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	if len(runner.actions) == 0 || runner.actions[0] != session.ActionContinue {
		t.Fatalf("expected continue action, got %v", runner.actions)
	}
	if strings.Contains(output.String(), commandHelpSummary) {
		t.Fatalf("expected plain mode without commands help, got %q", output.String())
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
	if strings.Contains(output.String(), commandHelpSummary) {
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

func TestBreakpointRecordFromBackendInfersEnclosingFunctionForFileLineBreakpoint(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "main.go")
	source := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}

	record, ok := breakpointRecordFromBackend(&backend.Breakpoint{ID: 1, Location: backend.SourceLocation{File: sourcePath, Line: 4}})
	if !ok {
		t.Fatal("expected breakpoint record")
	}
	if record.Function != "main.main" {
		t.Fatalf("expected inferred function main.main, got %q", record.Function)
	}
}

func TestRunCommandLoopShowsBreakpointsInspection(t *testing.T) {
	t.Parallel()

	sourcePath := filepath.Join(t.TempDir(), "main.go")
	runner := &fakeCommandRunner{}
	initialBreakpoints := []breakpointRecord{{ID: 1, File: sourcePath, Line: 14, Function: "main.add"}}
	var output bytes.Buffer
	if err := runCommandLoopWithBreakpoints(context.Background(), bytes.NewBufferString("b\nq\n"), &output, runner, nil, false, initialBreakpoints); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	text := output.String()
	if !strings.Contains(text, "breakpoints") || !strings.Contains(text, "#1") || !strings.Contains(text, displayPath(sourcePath)+":14") || !strings.Contains(text, "main.add") {
		t.Fatalf("expected breakpoints inspection, got %q", text)
	}
}

func TestFormatTTYLocalsAddsColorByRole(t *testing.T) {
	t.Parallel()

	out := formatTTYLocals([]backend.Variable{
		{Name: "message", Type: "string", Value: "\"hello\""},
		{Name: "total", Type: "int", Value: "42"},
		{Name: "hex", Type: "uintptr", Value: "0x2a"},
		{Name: "grouped", Type: "int", Value: "1_000"},
		{Name: "ok", Type: "bool", Value: "true"},
		{Name: "err", Type: "error", Value: "nil"},
	})
	for _, want := range []string{ansiCyan + "message" + ansiReset, ansiDim + "(string)" + ansiReset, ansiGreen + "\"hello\"" + ansiReset, ansiMagenta + "42" + ansiReset, ansiMagenta + "0x2a" + ansiReset, ansiMagenta + "1_000" + ansiReset, ansiYellow + "true" + ansiReset, ansiRed + "nil" + ansiReset} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected colored locals output to contain %q, got %q", want, out)
		}
	}
}

func TestFormatLocalsHidesSyntheticLocalsByDefault(t *testing.T) {
	t.Parallel()

	out := formatLocals([]backend.Variable{
		{Name: "~r0", Type: "[]string", Value: "[]string len: 0, cap: 0, nil", HasChildren: true},
		{Name: "(err)", Type: "error", Value: "nil"},
		{Name: "value", Type: "string", Value: "\"ok\""},
	})
	if strings.Contains(out, "~r0") || strings.Contains(out, "(err)") {
		t.Fatalf("expected synthetic locals to be hidden, got %q", out)
	}
	if !strings.Contains(out, "value (string) = \"ok\"") {
		t.Fatalf("expected visible local to remain, got %q", out)
	}
	if !strings.Contains(out, "(2 synthetic locals hidden)") {
		t.Fatalf("expected hidden synthetic locals summary, got %q", out)
	}
}

func TestFormatLocalsCollapsesCompositeValues(t *testing.T) {
	t.Parallel()

	out := formatLocals([]backend.Variable{
		{Name: "p", Type: "main.Page", Value: "main.Page {Header: main.PageHeader {HType: 10, NumCells: 190}}", HasChildren: true},
		{Name: "dat", Type: "*os.File", Value: "*os.File {file: *os.file {name: \"companies.db\"}}", HasChildren: true},
		{Name: "resp", Type: "[]string", Value: "[]string len: 2, cap: 2, [\"\",\"\"]", HasChildren: true},
		{Name: "r", Type: "*bufio.Reader", Value: "*bufio.Reader {buf: []uint8 len: 4096, cap: 4096, [17,3]}", HasChildren: true},
	})
	for _, want := range []string{
		"p (main.Page) = {…}",
		"dat (*os.File) = {…}",
		"resp ([]string) = len: 2, cap: 2",
		"r (*bufio.Reader) = {…}",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected compact composite locals output to contain %q, got %q", want, out)
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

func TestTTYCommandTextPreservesColonCommandMode(t *testing.T) {
	t.Parallel()

	if got := ttyCommandText([]byte("b 7")); got != ":b 7" {
		t.Fatalf("expected tty command text %q, got %q", ":b 7", got)
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

func TestRunCommandLoopStickyReprintsSnapshotAfterBreakpointCreate(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "main.go")
	source := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}

	runner := &fakeCommandRunner{
		breakpoint: &backend.Breakpoint{
			ID:       2,
			Location: backend.SourceLocation{File: sourcePath, Line: 4, Function: "main.main"},
		},
		snapshots: []*session.Snapshot{{
			State: backend.StopState{},
			Frame: &backend.Frame{Location: backend.SourceLocation{File: sourcePath, Line: 3, Function: "main.main"}},
		}},
	}
	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString(":b 4\nq\n"), &output, runner, runner.currentSnapshot(), true); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	text := output.String()
	if strings.Contains(text, "breakpoint 2 at") {
		t.Fatalf("expected sticky mode to rerender instead of printing breakpoint summary, got %q", text)
	}
	if !strings.Contains(text, "4 ●") {
		t.Fatalf("expected sticky rerender with breakpoint marker, got %q", text)
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
	if !strings.Contains(out, "> 4   |") {
		t.Fatalf("expected compact source window, got %q", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("expected plain output without ANSI escapes, got %q", out)
	}
	if strings.Contains(out, commandHelpSummary) {
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
	if strings.Contains(output.String(), commandHelpSummary) {
		t.Fatalf("expected plain mode without help legend, got %q", output.String())
	}
}

func TestRunCommandLoopShowsHelpScreen(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	if err := runCommandLoop(context.Background(), bytes.NewBufferString("h\nq\n"), &output, &fakeCommandRunner{}, nil, false); err != nil {
		t.Fatalf("runCommandLoop returned error: %v", err)
	}
	text := output.String()
	if !strings.Contains(text, "help") || !strings.Contains(text, "Navigation") || !strings.Contains(text, "Breakpoints") {
		t.Fatalf("expected help screen output, got %q", text)
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

func TestFormatSnapshotForViewShowsPromptInStickyTTY(t *testing.T) {
	t.Parallel()

	state := &viewState{sticky: true, outputTTY: true}
	out := formatSnapshotForView(&session.Snapshot{State: backend.StopState{}}, state, false)
	if strings.Contains(out, commandHelpSummary) {
		t.Fatalf("expected no inline help legend, got %q", out)
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

func TestFormatSnapshotForViewStickyNonTTYDoesNotUseANSI(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "main.go")
	source := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}

	state := &viewState{sticky: true, breakpoints: []breakpointRecord{{File: sourcePath, Line: 4}}}
	snapshot := &session.Snapshot{
		State: backend.StopState{},
		Frame: &backend.Frame{Location: backend.SourceLocation{File: sourcePath, Line: 3, Function: "main.main"}},
	}
	out := formatSnapshotForView(snapshot, state, false)
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("expected sticky non-tty output without ANSI escapes, got %q", out)
	}
	if !strings.Contains(out, "stopped: main.main at "+sourcePath+":3") {
		t.Fatalf("expected sticky header, got %q", out)
	}
	if !strings.Contains(out, ">   3   func main() {") {
		t.Fatalf("expected rendered source without ANSI formatting, got %q", out)
	}
	if !strings.Contains(out, "    4 ● \tprintln(\"hello\")") {
		t.Fatalf("expected sticky breakpoint marker, got %q", out)
	}
}

func TestFormatSnapshotForViewPlainMarksBreakpointColumn(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "main.go")
	source := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}

	state := &viewState{breakpoints: []breakpointRecord{{File: sourcePath, Line: 4}}}
	snapshot := &session.Snapshot{
		State: backend.StopState{},
		Frame: &backend.Frame{Location: backend.SourceLocation{File: sourcePath, Line: 3, Function: "main.main"}},
	}
	out := formatSnapshotForView(snapshot, state, false)
	if !strings.Contains(out, " 4 o | \tprintln(\"hello\")") {
		t.Fatalf("expected plain breakpoint marker, got %q", out)
	}
}

func TestFormatSnapshotForViewPlainTTYSuppressesRepeatedHints(t *testing.T) {
	t.Parallel()

	state := &viewState{outputTTY: true}
	out := formatSnapshotForView(&session.Snapshot{State: backend.StopState{}}, state, false)
	if strings.Contains(out, commandHelpSummary) {
		t.Fatalf("expected plain tty output without repeated hints, got %q", out)
	}
	if !strings.HasSuffix(out, ">") {
		t.Fatalf("expected prompt suffix, got %q", out)
	}
}

func TestFormatSnapshotForViewAddsExitHint(t *testing.T) {
	t.Parallel()

	out := formatSnapshotForView(&session.Snapshot{State: backend.StopState{Exited: true, ExitStatus: 1}}, &viewState{}, false)
	if !strings.Contains(out, "program exited; press o to inspect captured output, any other key to quit") {
		t.Fatalf("expected exit hint, got %q", out)
	}
}

func TestFormatSnapshotForViewExitTTYOmitsPrompt(t *testing.T) {
	t.Parallel()

	snapshot := &session.Snapshot{State: backend.StopState{Exited: true, ExitStatus: 0}}
	out := formatSnapshotForView(snapshot, &viewState{outputTTY: true, currentSnapshot: snapshot}, false)
	if strings.HasSuffix(out, ">") {
		t.Fatalf("expected no prompt after exit, got %q", out)
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
	if strings.Contains(out, commandHelpSummary) {
		t.Fatalf("expected no inline command legend, got %q", out)
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
	if strings.Contains(out, commandHelpSummary) {
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
	if !strings.Contains(text, "program exited; press o to inspect captured output, any other key to quit") {
		t.Fatalf("expected exit hint, got %q", text)
	}
	if !strings.Contains(text, "OUTPUT-BEGIN\nstdout | done\nOUTPUT-END") {
		t.Fatalf("expected plain exit output block, got %q", text)
	}
	if !strings.Contains(text, "output\nstdout | done") {
		t.Fatalf("expected output inspection after exit, got %q", text)
	}
}

func TestRunCommandLoopQuitsOnNonOutputInputAfterExit(t *testing.T) {
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
	if output.String() != "" {
		t.Fatalf("expected clean quit after exit, got %q", output.String())
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

func TestAsyncLineReaderReturnsOnContextCancelWhileBlocked(t *testing.T) {
	t.Parallel()

	reader, writer := io.Pipe()
	defer func() { _ = reader.Close() }()

	asyncReader := newAsyncLineReader(reader)
	defer asyncReader.Close()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := asyncReader.Next(ctx)
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
		t.Fatal("async line reader did not return after context cancel")
	}

	_ = writer.Close()
}

func TestAsyncByteReaderReturnsOnContextCancelWhileBlocked(t *testing.T) {
	t.Parallel()

	reader, writer := io.Pipe()
	defer func() { _ = reader.Close() }()

	asyncReader := newAsyncByteReader(reader)
	defer asyncReader.Close()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := asyncReader.Next(ctx)
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
		t.Fatal("async byte reader did not return after context cancel")
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

func TestCloseSessionReturnsWrappedError(t *testing.T) {
	t.Parallel()

	err := closeSession(fakeCloser{err: errors.New("boom")})
	if err == nil {
		t.Fatal("expected close error")
	}
	if !strings.Contains(err.Error(), "close backend: boom") {
		t.Fatalf("expected wrapped close error, got %v", err)
	}
}

func TestCloseSessionIgnoresClosedNetworkConnection(t *testing.T) {
	t.Parallel()

	err := closeSession(fakeCloser{err: errors.New("use of closed network connection")})
	if err != nil {
		t.Fatalf("expected ignored close error, got %v", err)
	}
}

type fakeCloser struct {
	err error
}

func (f fakeCloser) Close() error {
	return f.err
}

type fakeCommandRunner struct {
	actions         []session.Action
	breakpointSpecs []backend.BreakpointSpec
	breakpoints     []backend.Breakpoint
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
	if f.breakpoint != nil {
		updated := false
		for i, existing := range f.breakpoints {
			if existing.Location.File == f.breakpoint.Location.File && existing.Location.Line == f.breakpoint.Location.Line && existing.Location.Function == f.breakpoint.Location.Function {
				f.breakpoints[i] = *f.breakpoint
				updated = true
				break
			}
		}
		if !updated {
			f.breakpoints = append(f.breakpoints, *f.breakpoint)
		}
	}
	return f.breakpoint, nil
}

func (f *fakeCommandRunner) Breakpoints(_ context.Context) ([]backend.Breakpoint, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(f.breakpoints) > 0 {
		return append([]backend.Breakpoint(nil), f.breakpoints...), nil
	}
	if f.breakpoint != nil {
		return []backend.Breakpoint{*f.breakpoint}, nil
	}
	return nil, nil
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
