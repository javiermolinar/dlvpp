package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
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
	commandHelpSummary   = "c=continue, n=next, s=step in, l=locals, o=output, b=breakpoints, h=help, :e <local>, :b <location>, q=quit"
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
)

type commandRunner interface {
	Do(ctx context.Context, action session.Action) (*session.Snapshot, error)
	CreateBreakpoint(ctx context.Context, spec backend.BreakpointSpec) (*backend.Breakpoint, error)
	Breakpoints(ctx context.Context) ([]backend.Breakpoint, error)
	Locals(ctx context.Context, frame backend.FrameRef) ([]backend.Variable, error)
	Children(ctx context.Context, reference int) ([]backend.Variable, error)
	Output(ctx context.Context) ([]backend.OutputEntry, error)
}

func runCommandLoop(ctx context.Context, input io.Reader, output io.Writer, runner commandRunner, initialSnapshot *session.Snapshot, sticky bool) error {
	return runCommandLoopWithBreakpoints(ctx, input, output, runner, initialSnapshot, sticky, nil)
}

func runCommandLoopWithBreakpoints(ctx context.Context, input io.Reader, output io.Writer, runner commandRunner, initialSnapshot *session.Snapshot, sticky bool, initialBreakpoints []breakpointRecord) error {
	state := newViewState(sticky, output, initialSnapshot, initialBreakpoints)
	if file, ok := input.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		return runTTYCommandLoop(ctx, file, output, runner, state)
	}
	return runLineCommandLoop(ctx, input, output, runner, state)
}

func runLineCommandLoop(ctx context.Context, input io.Reader, output io.Writer, runner commandRunner, state *viewState) error {
	reader := newAsyncLineReader(input)
	defer reader.Close()

	for {
		line, err := reader.Next(ctx)
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

	reader := newAsyncByteReader(input)
	defer reader.Close()

	var commandBuf []byte
	commandMode := false

	for {
		b, err := reader.Next(ctx)
		if err != nil {
			return err
		}

		if sessionExited(state) && !commandMode {
			if b == 'o' && !hasInspection(state) {
				if done, err := processCommand(ctx, output, runner, state, "o"); done || err != nil {
					return err
				}
				continue
			}
			return nil
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
		case 'h':
			if !commandMode {
				if done, err := processCommand(ctx, output, runner, state, "h"); done || err != nil {
					return err
				}
				continue
			}
		case 'b':
			if !commandMode {
				if done, err := processCommand(ctx, output, runner, state, "b"); done || err != nil {
					return err
				}
				continue
			}
		case 'c':
			if !commandMode {
				if done, err := processCommand(ctx, output, runner, state, "c"); done || err != nil {
					return err
				}
				continue
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
			if done, err := processCommand(ctx, output, runner, state, ttyCommandText(commandBuf)); done || err != nil {
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

func ttyCommandText(commandBuf []byte) string {
	return ":" + strings.TrimSpace(string(commandBuf))
}

func refreshBreakpointState(ctx context.Context, state *viewState, runner commandRunner) error {
	if state == nil || runner == nil {
		return nil
	}
	bps, err := runner.Breakpoints(ctx)
	if err != nil {
		return err
	}
	state.breakpoints = breakpointRecordsFromBackend(bps)
	return nil
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
	colonCommand := strings.HasPrefix(text, ":")
	if colonCommand {
		text = strings.TrimSpace(strings.TrimPrefix(text, ":"))
	}
	if text == "" {
		return nil
	}

	parts := strings.Fields(text)
	command := parts[0]
	args := parts[1:]

	if sessionExited(state) {
		if command == "o" {
			return showOutput(ctx, output, runner, state)
		}
		return errQuitCommandLoop
	}

	switch command {
	case "q":
		return errQuitCommandLoop
	case "h":
		return showHelp(output, state)
	case "b":
		if !colonCommand {
			return showBreakpoints(output, state)
		}
		if len(args) == 0 {
			return errors.New("break requires a location")
		}
		location, err := resolveBreakpointLocation(strings.Join(args, " "), breakpointResolveContext{Snapshot: state.currentSnapshot})
		if err != nil {
			return fmt.Errorf("break: %w", err)
		}

		actionCtx, cancel := context.WithTimeout(ctx, commandActionTimeout)
		defer cancel()

		bp, err := runner.CreateBreakpoint(actionCtx, backend.BreakpointSpec{Location: location})
		if err != nil {
			return fmt.Errorf("break: %w", err)
		}
		if err := refreshBreakpointState(actionCtx, state, runner); err != nil {
			if !errors.Is(err, backend.ErrUnsupported) {
				return fmt.Errorf("break: %w", err)
			}
			rememberBreakpoint(state, bp)
		}
		if state != nil && state.sticky && state.currentSnapshot != nil {
			clearInspection(state)
			_, _ = fmt.Fprint(output, formatSnapshotForView(state.currentSnapshot, state, true))
			return nil
		}
		_, _ = fmt.Fprintln(output, formatBreakpoint(bp, state))
		return nil
	case "c":
		return runDebuggerAction(ctx, output, runner, state, session.ActionContinue, "continue")
	case "n":
		return runDebuggerAction(ctx, output, runner, state, session.ActionNext, "next")
	case "s":
		return runDebuggerAction(ctx, output, runner, state, session.ActionStepIn, "step in")
	case "e":
		if !colonCommand {
			return fmt.Errorf("unknown command: %s", command)
		}
		if len(args) == 0 {
			return errors.New("expand requires a local name")
		}
		return expandLocal(ctx, output, runner, state, strings.Join(args, " "))
	case "l":
		return showLocals(ctx, output, runner, state)
	case "o":
		return showOutput(ctx, output, runner, state)
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

func formatLocals(locals []backend.Variable) string {
	visible, hiddenCount := filterDisplayedLocals(locals)
	if len(visible) == 0 {
		if hiddenCount == 0 {
			return "(no locals)\n"
		}
		return "(no user locals)\n" + formatHiddenLocalsSummary(hiddenCount, false)
	}

	var out strings.Builder
	for _, local := range visible {
		value := formatLocalDisplayValue(local)
		switch {
		case local.Type != "":
			fmt.Fprintf(&out, "%s (%s) = %s\n", local.Name, local.Type, value)
		default:
			fmt.Fprintf(&out, "%s = %s\n", local.Name, value)
		}
	}
	out.WriteString(formatHiddenLocalsSummary(hiddenCount, false))
	return out.String()
}

func formatTTYLocals(locals []backend.Variable) string {
	visible, hiddenCount := filterDisplayedLocals(locals)
	if len(visible) == 0 {
		if hiddenCount == 0 {
			return ansiDim + "(no locals)" + ansiReset + "\n"
		}
		return ansiDim + "(no user locals)" + ansiReset + "\n" + formatHiddenLocalsSummary(hiddenCount, true)
	}

	var out strings.Builder
	for _, local := range visible {
		value := formatLocalDisplayValue(local)
		coloredName := ansiCyan + local.Name + ansiReset
		coloredValue := colorizeLocalValue(value)
		switch {
		case local.Type != "":
			fmt.Fprintf(&out, "%s %s(%s)%s = %s\n", coloredName, ansiDim, local.Type, ansiReset, coloredValue)
		default:
			fmt.Fprintf(&out, "%s = %s\n", coloredName, coloredValue)
		}
	}
	out.WriteString(formatHiddenLocalsSummary(hiddenCount, true))
	return out.String()
}

func formatExpandedLocals(locals []backend.Variable, expanded map[string][]backend.Variable, color bool) string {
	visible, hiddenCount := filterDisplayedLocals(locals)
	if len(visible) == 0 {
		if hiddenCount == 0 {
			if color {
				return ansiDim + "(no locals)" + ansiReset + "\n"
			}
			return "(no locals)\n"
		}
		if color {
			return ansiDim + "(no user locals)" + ansiReset + "\n" + formatHiddenLocalsSummary(hiddenCount, true)
		}
		return "(no user locals)\n" + formatHiddenLocalsSummary(hiddenCount, false)
	}

	var out strings.Builder
	for _, local := range visible {
		writeExpandedLocalLines(&out, local, local.Name, expanded, color, "")
	}
	out.WriteString(formatHiddenLocalsSummary(hiddenCount, color))
	return out.String()
}

func writeExpandedLocalLines(out *strings.Builder, local backend.Variable, path string, expanded map[string][]backend.Variable, color bool, indent string) {
	writeLocalLine(out, local, color, indent)
	children, ok := expanded[path]
	if !ok {
		return
	}
	visibleChildren, hiddenChildren := filterDisplayedLocals(children)
	childIndent := indent + "  "
	for _, child := range visibleChildren {
		writeExpandedLocalLines(out, child, joinLocalPath(path, child.Name), expanded, color, childIndent)
	}
	out.WriteString(formatHiddenLocalsSummaryWithIndent(hiddenChildren, color, childIndent))
}

func writeLocalLine(out *strings.Builder, local backend.Variable, color bool, indent string) {
	value := formatLocalDisplayValue(local)
	if !color {
		switch {
		case local.Type != "":
			fmt.Fprintf(out, "%s%s (%s) = %s\n", indent, local.Name, local.Type, value)
		default:
			fmt.Fprintf(out, "%s%s = %s\n", indent, local.Name, value)
		}
		return
	}

	coloredName := indent + ansiCyan + local.Name + ansiReset
	coloredValue := colorizeLocalValue(value)
	switch {
	case local.Type != "":
		fmt.Fprintf(out, "%s %s(%s)%s = %s\n", coloredName, ansiDim, local.Type, ansiReset, coloredValue)
	default:
		fmt.Fprintf(out, "%s = %s\n", coloredName, coloredValue)
	}
}

func filterDisplayedLocals(locals []backend.Variable) ([]backend.Variable, int) {
	visible := make([]backend.Variable, 0, len(locals))
	hiddenCount := 0
	for _, local := range locals {
		if isSyntheticLocal(local.Name) {
			hiddenCount++
			continue
		}
		visible = append(visible, local)
	}
	return visible, hiddenCount
}

func isSyntheticLocal(name string) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "~") {
		return true
	}
	return strings.HasPrefix(trimmed, "(") && strings.HasSuffix(trimmed, ")")
}

func formatLocalDisplayValue(local backend.Variable) string {
	value := strings.TrimSpace(local.Value)
	if value == "" {
		value = "<no value>"
	}
	if !local.HasChildren {
		return truncateText(value, 96)
	}
	return summarizeCompositeLocalValue(local.Type, value)
}

func summarizeCompositeLocalValue(typeName string, value string) string {
	if value == "nil" || value == "<nil>" {
		return value
	}

	firstBrace := strings.Index(value, "{")
	firstLen := topLevelLenIndex(value)
	if firstBrace >= 0 && (firstLen < 0 || firstBrace < firstLen) {
		return "{…}"
	}
	if summary := extractLenCapSummary(value, firstLen); summary != "" {
		return summary
	}

	baseType := strings.TrimLeft(strings.TrimSpace(typeName), "*")
	switch {
	case strings.HasPrefix(baseType, "map[") || strings.HasPrefix(value, "map[") || strings.Contains(value, " map["):
		return "map[…]"
	case strings.HasPrefix(baseType, "[]") || strings.HasPrefix(baseType, "[") || strings.HasPrefix(value, "["):
		return "[…]"
	default:
		return truncateText(value, 48)
	}
}

func topLevelLenIndex(value string) int {
	if strings.HasPrefix(value, "len:") {
		return 0
	}
	return strings.Index(value, " len:")
}

func extractLenCapSummary(value string, lenIndex int) string {
	if lenIndex < 0 || lenIndex >= len(value) {
		return ""
	}

	rest := strings.TrimSpace(value[lenIndex:])
	first, remaining := splitSummaryField(rest)
	first = strings.TrimSpace(first)
	if !strings.HasPrefix(first, "len:") {
		return ""
	}

	parts := []string{first}
	remaining = strings.TrimSpace(remaining)
	if strings.HasPrefix(remaining, "cap:") {
		second, _ := splitSummaryField(remaining)
		parts = append(parts, strings.TrimSpace(second))
	}
	return strings.Join(parts, ", ")
}

func splitSummaryField(value string) (string, string) {
	comma := strings.Index(value, ",")
	if comma < 0 {
		return value, ""
	}
	return value[:comma], value[comma+1:]
}

func formatHiddenLocalsSummary(hiddenCount int, color bool) string {
	return formatHiddenLocalsSummaryWithIndent(hiddenCount, color, "")
}

func formatHiddenLocalsSummaryWithIndent(hiddenCount int, color bool, indent string) string {
	if hiddenCount == 0 {
		return ""
	}

	noun := "locals"
	if hiddenCount == 1 {
		noun = "local"
	}
	text := fmt.Sprintf("(%d synthetic %s hidden)", hiddenCount, noun)
	if !color {
		return indent + text + "\n"
	}
	return indent + ansiDim + text + ansiReset + "\n"
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
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if _, err := strconv.ParseInt(trimmed, 0, 64); err == nil {
		return true
	}
	if _, err := strconv.ParseUint(trimmed, 0, 64); err == nil {
		return true
	}
	normalized := strings.ReplaceAll(trimmed, "_", "")
	if _, err := strconv.ParseFloat(normalized, 64); err == nil {
		return true
	}
	return false
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

type asyncLineReadResult struct {
	line string
	err  error
}

type asyncLineReader struct {
	requests  chan struct{}
	results   chan asyncLineReadResult
	closeOnce sync.Once
}

func newAsyncLineReader(input io.Reader) *asyncLineReader {
	reader := &asyncLineReader{
		requests: make(chan struct{}),
		results:  make(chan asyncLineReadResult, 1),
	}

	buffered := bufio.NewReader(input)
	go func() {
		defer close(reader.results)
		for range reader.requests {
			line, err := buffered.ReadString('\n')
			reader.results <- asyncLineReadResult{line: line, err: err}
			if err != nil {
				return
			}
		}
	}()

	return reader
}

func (r *asyncLineReader) Next(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	select {
	case r.requests <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}

	select {
	case result, ok := <-r.results:
		if !ok {
			return "", io.EOF
		}
		return result.line, result.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (r *asyncLineReader) Close() {
	r.closeOnce.Do(func() {
		close(r.requests)
	})
}

type asyncByteReadResult struct {
	b   byte
	err error
}

type asyncByteReader struct {
	requests  chan struct{}
	results   chan asyncByteReadResult
	closeOnce sync.Once
}

func newAsyncByteReader(input io.Reader) *asyncByteReader {
	reader := &asyncByteReader{
		requests: make(chan struct{}),
		results:  make(chan asyncByteReadResult, 1),
	}

	go func() {
		defer close(reader.results)
		for range reader.requests {
			var buf [1]byte
			_, err := input.Read(buf[:])
			reader.results <- asyncByteReadResult{b: buf[0], err: err}
			if err != nil {
				return
			}
		}
	}()

	return reader
}

func (r *asyncByteReader) Next(ctx context.Context) (byte, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	select {
	case r.requests <- struct{}{}:
	case <-ctx.Done():
		return 0, ctx.Err()
	}

	select {
	case result, ok := <-r.results:
		if !ok {
			return 0, io.EOF
		}
		return result.b, result.err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (r *asyncByteReader) Close() {
	r.closeOnce.Do(func() {
		close(r.requests)
	})
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
