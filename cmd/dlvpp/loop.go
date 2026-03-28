package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"dlvpp/internal/backend"
	"dlvpp/internal/session"
	"golang.org/x/term"
)

const (
	commandActionTimeout = 15 * time.Second
	ttyCtrlC             = 3
	ttyCtrlD             = 4
	ttyEscape            = 27
	ttyBackspace         = 8
	ttyDelete            = 127
	commandLoopHelp      = "Commands: n=next, s=step in, l=locals, o=output, :b <location>, q=quit"
	ansiReset            = "\x1b[0m"
	ansiDim              = "\x1b[2m"
	ansiCyan             = "\x1b[36m"
	ansiGreen            = "\x1b[32m"
	ansiMagenta          = "\x1b[35m"
	ansiYellow           = "\x1b[33m"
	ansiRed              = "\x1b[31m"
)

var (
	errQuitCommandLoop = errors.New("quit command loop")
	errProgramExited   = errors.New("program already exited")
)

type commandRunner interface {
	Do(ctx context.Context, action session.Action) (*session.Snapshot, error)
	CreateBreakpoint(ctx context.Context, spec backend.BreakpointSpec) (*backend.Breakpoint, error)
	Locals(ctx context.Context, frame backend.FrameRef) ([]backend.Variable, error)
	Output(ctx context.Context) ([]backend.OutputEntry, error)
}

func runCommandLoop(ctx context.Context, input io.Reader, output io.Writer, runner commandRunner, initialSnapshot *session.Snapshot, sticky bool) error {
	state := newViewState(sticky, output, initialSnapshot)
	if file, ok := input.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		return runTTYCommandLoop(ctx, file, output, runner, state)
	}
	return runLineCommandLoop(ctx, input, output, runner, state)
}

func runLineCommandLoop(ctx context.Context, input io.Reader, output io.Writer, runner commandRunner, state *viewState) error {
	if state != nil && state.sticky {
		_, _ = fmt.Fprintln(output, commandLoopHelp)
	}

	reader := bufio.NewReader(input)
	for {
		line, err := readCommand(ctx, reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		if done, err := processCommand(ctx, output, runner, state, strings.TrimSpace(line)); done || err != nil {
			return err
		}
	}
}

func runTTYCommandLoop(ctx context.Context, input *os.File, output io.Writer, runner commandRunner, state *viewState) error {
	oldState, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		return fmt.Errorf("enable raw mode: %w", err)
	}
	output = newlineWriter{Writer: output}
	defer func() {
		_ = term.Restore(int(input.Fd()), oldState)
		_, _ = fmt.Fprintln(output)
	}()

	var commandBuf []byte
	commandMode := false

	for {
		b, err := readByte(ctx, input)
		if err != nil {
			return err
		}

		switch b {
		case ttyCtrlC:
			return context.Canceled
		case ttyCtrlD:
			if !commandMode || len(commandBuf) == 0 {
				return nil
			}
		case 'q':
			if !commandMode {
				return nil
			}
		case 'n':
			if !commandMode {
				if done, err := processCommand(ctx, output, runner, state, "n"); done || err != nil {
					return err
				}
				continue
			}
		case 's':
			if !commandMode {
				if done, err := processCommand(ctx, output, runner, state, "s"); done || err != nil {
					return err
				}
				continue
			}
		case 'l':
			if !commandMode {
				if done, err := processCommand(ctx, output, runner, state, "l"); done || err != nil {
					return err
				}
				continue
			}
		case 'o':
			if !commandMode {
				if done, err := processCommand(ctx, output, runner, state, "o"); done || err != nil {
					return err
				}
				continue
			}
		case ':':
			if !commandMode {
				commandMode = true
				commandBuf = commandBuf[:0]
				_, _ = fmt.Fprint(output, ":")
				continue
			}
		case '\r', '\n':
			if !commandMode {
				continue
			}
			_, _ = fmt.Fprintln(output)
			commandMode = false
			if done, err := processCommand(ctx, output, runner, state, strings.TrimSpace(string(commandBuf))); done || err != nil {
				return err
			}
			commandBuf = commandBuf[:0]
			continue
		case ttyEscape:
			if commandMode {
				commandMode = false
				commandBuf = commandBuf[:0]
				_, _ = fmt.Fprintln(output)
				continue
			}
			if hasInspection(state) {
				clearInspection(state)
				_, _ = fmt.Fprint(output, formatSnapshotForView(state.currentSnapshot, state, true))
			}
			continue
		case ttyDelete, ttyBackspace:
			if !commandMode || len(commandBuf) == 0 {
				continue
			}
			commandBuf = commandBuf[:len(commandBuf)-1]
			_, _ = fmt.Fprint(output, "\b \b")
			continue
		}

		if !commandMode || !isPrintableByte(b) {
			continue
		}
		commandBuf = append(commandBuf, b)
		_, _ = fmt.Fprintf(output, "%c", b)
	}
}

func processCommand(ctx context.Context, output io.Writer, runner commandRunner, state *viewState, text string) (bool, error) {
	err := executeCommandText(ctx, text, output, runner, state)
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, errQuitCommandLoop):
		return true, nil
	case errors.Is(err, context.Canceled):
		return false, err
	default:
		_, _ = fmt.Fprintf(output, "%v\n", err)
		return false, nil
	}
}

func executeCommandText(ctx context.Context, text string, output io.Writer, runner commandRunner, state *viewState) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if text == "" {
		return nil
	}
	if strings.HasPrefix(text, ":") {
		text = strings.TrimSpace(strings.TrimPrefix(text, ":"))
	}
	if text == "" {
		return nil
	}

	parts := strings.Fields(text)
	command := parts[0]
	args := parts[1:]

	if sessionExited(state) && command != "q" && command != "o" {
		return errProgramExited
	}

	switch command {
	case "q":
		return errQuitCommandLoop
	case "n":
		return runDebuggerAction(ctx, output, runner, state, session.ActionNext, "next")
	case "s":
		return runDebuggerAction(ctx, output, runner, state, session.ActionStepIn, "step in")
	case "l":
		return showLocals(ctx, output, runner, state)
	case "o":
		return showOutput(ctx, output, runner, state)
	case "b":
		if len(args) == 0 {
			return errors.New("break requires a location")
		}
		actionCtx, cancel := context.WithTimeout(ctx, commandActionTimeout)
		defer cancel()

		bp, err := runner.CreateBreakpoint(actionCtx, backend.BreakpointSpec{Location: strings.Join(args, " ")})
		if err != nil {
			return fmt.Errorf("break: %w", err)
		}
		_, _ = fmt.Fprintln(output, formatBreakpoint(bp, state))
		return nil
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}

func runDebuggerAction(ctx context.Context, output io.Writer, runner commandRunner, state *viewState, action session.Action, label string) error {
	actionCtx, cancel := context.WithTimeout(ctx, commandActionTimeout)
	defer cancel()

	snapshot, err := runner.Do(actionCtx, action)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	clearInspection(state)
	state.currentSnapshot = snapshot
	_, _ = fmt.Fprint(output, formatSnapshotForView(snapshot, state, true))
	if snapshot != nil && snapshot.State.Exited && state != nil && !state.sticky {
		entries, err := runner.Output(actionCtx)
		if err == nil {
			if block := formatPlainExitOutput(entries); block != "" {
				_, _ = fmt.Fprint(output, block)
			}
		}
	}
	return nil
}

func sessionExited(state *viewState) bool {
	return state != nil && state.currentSnapshot != nil && state.currentSnapshot.State.Exited
}

func showLocals(ctx context.Context, output io.Writer, runner commandRunner, state *viewState) error {
	if state == nil || state.currentSnapshot == nil || state.currentSnapshot.Frame == nil {
		return errors.New("locals: no current frame")
	}

	actionCtx, cancel := context.WithTimeout(ctx, commandActionTimeout)
	defer cancel()

	locals, err := runner.Locals(actionCtx, state.currentSnapshot.Frame.Ref)
	if err != nil {
		return fmt.Errorf("locals: %w", err)
	}
	body := formatLocalsForView(locals, inspectionColorsEnabled(state))
	setInspection(state, "locals", body)
	_, _ = fmt.Fprint(output, formatInspectionForView(state.currentSnapshot, state, "locals", body, true))
	return nil
}

func showOutput(ctx context.Context, output io.Writer, runner commandRunner, state *viewState) error {
	if state == nil || state.currentSnapshot == nil {
		return errors.New("output: no current session")
	}

	actionCtx, cancel := context.WithTimeout(ctx, commandActionTimeout)
	defer cancel()

	entries, err := runner.Output(actionCtx)
	if err != nil {
		return fmt.Errorf("output: %w", err)
	}
	body := formatOutputForView(entries, inspectionColorsEnabled(state))
	setInspection(state, "output", body)
	_, _ = fmt.Fprint(output, formatInspectionForView(state.currentSnapshot, state, "output", body, true))
	return nil
}

func inspectionColorsEnabled(state *viewState) bool {
	return state != nil && state.sticky && state.outputTTY
}

func formatLocalsForView(locals []backend.Variable, color bool) string {
	if !color {
		return formatLocals(locals)
	}
	return formatTTYLocals(locals)
}

func formatLocals(locals []backend.Variable) string {
	if len(locals) == 0 {
		return "(no locals)\n"
	}

	var out strings.Builder
	for _, local := range locals {
		value := local.Value
		if value == "" {
			value = "<no value>"
		}
		value = truncateText(value, 96)
		switch {
		case local.Type != "":
			fmt.Fprintf(&out, "%s (%s) = %s\n", local.Name, local.Type, value)
		default:
			fmt.Fprintf(&out, "%s = %s\n", local.Name, value)
		}
	}
	return out.String()
}

func formatTTYLocals(locals []backend.Variable) string {
	if len(locals) == 0 {
		return ansiDim + "(no locals)" + ansiReset + "\n"
	}

	var out strings.Builder
	for _, local := range locals {
		value := local.Value
		if value == "" {
			value = "<no value>"
		}
		value = truncateText(value, 96)
		coloredName := ansiCyan + local.Name + ansiReset
		coloredValue := colorizeLocalValue(value)
		switch {
		case local.Type != "":
			fmt.Fprintf(&out, "%s %s(%s)%s = %s\n", coloredName, ansiDim, local.Type, ansiReset, coloredValue)
		default:
			fmt.Fprintf(&out, "%s = %s\n", coloredName, coloredValue)
		}
	}
	return out.String()
}

func formatOutputForView(entries []backend.OutputEntry, color bool) string {
	if !color {
		return formatOutput(entries)
	}
	return formatTTYOutput(entries)
}

func formatOutput(entries []backend.OutputEntry) string {
	if len(entries) == 0 {
		return "(no output)\n"
	}

	var out strings.Builder
	for _, entry := range entries {
		text := strings.TrimRight(entry.Text, "\n")
		if text == "" {
			continue
		}
		for _, line := range strings.Split(text, "\n") {
			switch entry.Category {
			case backend.OutputCategoryStderr:
				fmt.Fprintf(&out, "stderr | %s\n", line)
			case backend.OutputCategoryStdout:
				fmt.Fprintf(&out, "stdout | %s\n", line)
			default:
				fmt.Fprintf(&out, "%s\n", line)
			}
		}
	}
	if out.Len() == 0 {
		return "(no output)\n"
	}
	return out.String()
}

func formatTTYOutput(entries []backend.OutputEntry) string {
	if len(entries) == 0 {
		return ansiDim + "(no output)" + ansiReset + "\n"
	}

	var out strings.Builder
	for _, entry := range entries {
		text := strings.TrimRight(entry.Text, "\n")
		if text == "" {
			continue
		}
		for _, line := range strings.Split(text, "\n") {
			switch entry.Category {
			case backend.OutputCategoryStderr:
				fmt.Fprintf(&out, "%sstderr%s | %s\n", ansiRed, ansiReset, line)
			case backend.OutputCategoryStdout:
				fmt.Fprintf(&out, "%sstdout%s | %s\n", ansiCyan, ansiReset, line)
			default:
				fmt.Fprintf(&out, "%s\n", line)
			}
		}
	}
	if out.Len() == 0 {
		return ansiDim + "(no output)" + ansiReset + "\n"
	}
	return out.String()
}

func colorizeLocalValue(value string) string {
	trimmed := strings.TrimSpace(value)
	switch {
	case trimmed == "nil" || trimmed == "<nil>":
		return ansiRed + value + ansiReset
	case trimmed == "true" || trimmed == "false":
		return ansiYellow + value + ansiReset
	case isQuotedValue(trimmed):
		return ansiGreen + value + ansiReset
	case isNumericValue(trimmed):
		return ansiMagenta + value + ansiReset
	default:
		return value
	}
}

func isQuotedValue(value string) bool {
	if len(value) < 2 {
		return false
	}
	return (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) ||
		(strings.HasPrefix(value, "`") && strings.HasSuffix(value, "`"))
}

func isNumericValue(value string) bool {
	if value == "" {
		return false
	}
	trimmed := strings.TrimLeft(value, "+-")
	if trimmed == "" {
		return false
	}
	hasDigit := false
	for _, r := range trimmed {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case r == '.' || r == '_' || r == 'x' || r == 'X' || r == 'o' || r == 'O' || r == 'b' || r == 'B' || r == 'e' || r == 'E':
		default:
			return false
		}
	}
	return hasDigit
}

func formatPlainExitOutput(entries []backend.OutputEntry) string {
	body := strings.TrimRight(formatOutput(entries), "\n")
	if body == "" || body == "(no output)" {
		return ""
	}
	return "OUTPUT-BEGIN\n" + body + "\nOUTPUT-END\n"
}

func truncateText(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	if limit <= 1 {
		return s[:limit]
	}
	return s[:limit-1] + "…"
}

func formatBreakpoint(bp *backend.Breakpoint, state *viewState) string {
	if state != nil && !state.sticky {
		if bp == nil {
			return "bp"
		}
		if bp.Location.File != "" && bp.Location.Line > 0 {
			return fmt.Sprintf("bp %d %s:%d", bp.ID, displayPath(bp.Location.File), bp.Location.Line)
		}
		if bp.Location.Function != "" {
			return fmt.Sprintf("bp %d %s", bp.ID, bp.Location.Function)
		}
		return fmt.Sprintf("bp %d", bp.ID)
	}

	if bp == nil {
		return "breakpoint set"
	}
	if bp.Location.File != "" && bp.Location.Line > 0 {
		return fmt.Sprintf("breakpoint %d at %s:%d", bp.ID, bp.Location.File, bp.Location.Line)
	}
	if bp.Location.Function != "" {
		return fmt.Sprintf("breakpoint %d at %s", bp.ID, bp.Location.Function)
	}
	return fmt.Sprintf("breakpoint %d set", bp.ID)
}

func readCommand(ctx context.Context, reader *bufio.Reader) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	result := make(chan struct {
		line string
		err  error
	}, 1)
	go func() {
		line, err := reader.ReadString('\n')
		result <- struct {
			line string
			err  error
		}{line: line, err: err}
	}()

	select {
	case read := <-result:
		return read.line, read.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func readByte(ctx context.Context, input io.Reader) (byte, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	result := make(chan struct {
		b   byte
		err error
	}, 1)
	go func() {
		var buf [1]byte
		_, err := input.Read(buf[:])
		result <- struct {
			b   byte
			err error
		}{b: buf[0], err: err}
	}()

	select {
	case read := <-result:
		return read.b, read.err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func isPrintableByte(b byte) bool {
	return b >= 32 && b <= 126
}

type newlineWriter struct {
	io.Writer
}

func (w newlineWriter) Write(p []byte) (int, error) {
	converted := bytes.ReplaceAll(p, []byte("\n"), []byte("\r\n"))
	if _, err := w.Writer.Write(converted); err != nil {
		return 0, err
	}
	return len(p), nil
}
