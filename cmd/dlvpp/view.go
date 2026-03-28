package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"dlvpp/internal/session"
	"dlvpp/internal/sourceview"
	"golang.org/x/term"
)

const (
	clearScreenANSI       = "\x1b[2J\x1b[H"
	plainContextLines     = 1
	stickyReservedRowsTTY = 5
)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

type viewState struct {
	sticky          bool
	outputTTY       bool
	outputHeight    int
	currentSnapshot *session.Snapshot
	inspectionTitle string
	inspectionBody  string
}

func newViewState(sticky bool, output io.Writer, initialSnapshot *session.Snapshot) *viewState {
	outputTTY := false
	outputHeight := 0
	if file, ok := output.(*os.File); ok {
		outputTTY = term.IsTerminal(int(file.Fd()))
		if outputTTY {
			_, height, err := term.GetSize(int(file.Fd()))
			if err == nil {
				outputHeight = height
			}
		}
	}
	return &viewState{
		sticky:          sticky,
		outputTTY:       outputTTY,
		outputHeight:    outputHeight,
		currentSnapshot: initialSnapshot,
	}
}

func formatSnapshotForView(snapshot *session.Snapshot, state *viewState, clear bool) string {
	if state == nil || state.sticky {
		return formatStickySnapshotForView(snapshot, state, clear)
	}
	return appendPrompt(appendExitHint(formatPlainSnapshot(snapshot), snapshot), state)
}

func formatStickySnapshotForView(snapshot *session.Snapshot, state *viewState, clear bool) string {
	if snapshot == nil || state == nil || !state.sticky {
		return appendPrompt(stripANSIForNonTTY(session.FormatSnapshot(snapshot), state), state)
	}

	base := stripANSIForNonTTY(appendExitHint(session.FormatSnapshot(snapshot), snapshot), state)
	if snapshot.State.Exited || snapshot.State.Running || snapshot.Frame == nil {
		return appendPrompt(maybeClear(state, clear)+base, state)
	}

	source, err := renderStickySource(snapshot, state)
	if err != nil {
		return appendPrompt(maybeClear(state, clear)+base+fmt.Sprintf("sticky render: %v\n", err), state)
	}

	path := snapshot.Frame.Location.File
	if state.outputTTY {
		path = "\x1b[36m" + path + "\x1b[0m"
	}

	var out strings.Builder
	out.WriteString(maybeClear(state, clear))
	fmt.Fprintf(
		&out,
		"stopped: %s at %s:%d\n",
		snapshot.Frame.Location.Function,
		path,
		snapshot.Frame.Location.Line,
	)
	out.WriteString(stripANSIForNonTTY(source, state))
	if snapshot.SourceError != nil {
		fmt.Fprintf(&out, "%v\n", snapshot.SourceError)
	}
	return appendPrompt(out.String(), state)
}

func renderStickySource(snapshot *session.Snapshot, state *viewState) (string, error) {
	if snapshot == nil || snapshot.Frame == nil {
		return "", nil
	}
	if state != nil && state.outputTTY && state.outputHeight > stickyReservedRowsTTY {
		visibleLines := max(1, state.outputHeight-stickyReservedRowsTTY)
		before := (visibleLines - 1) / 2
		after := visibleLines - before - 1
		start := max(1, snapshot.Frame.Location.Line-before)
		end := snapshot.Frame.Location.Line + after
		return sourceview.RenderRange(snapshot.Frame.Location.File, snapshot.Frame.Location.Line, start, end)
	}
	if snapshot.Source != "" {
		return snapshot.Source, nil
	}
	return sourceview.RenderWindow(snapshot.Frame.Location.File, snapshot.Frame.Location.Line, sourceContextLines)
}

func formatPlainSnapshot(snapshot *session.Snapshot) string {
	if snapshot == nil {
		return ""
	}

	var out strings.Builder
	switch {
	case snapshot.State.Exited:
		fmt.Fprintf(&out, "exit %d\n", snapshot.State.ExitStatus)
	case snapshot.State.Running:
		out.WriteString("running\n")
	case snapshot.Frame == nil:
		if snapshot.StackError != nil {
			fmt.Fprintf(&out, "err stack: %v\n", snapshot.StackError)
		} else {
			out.WriteString("stop no-frame\n")
		}
	default:
		fmt.Fprintf(
			&out,
			"stop %s %s:%d\n",
			fallbackText(snapshot.Frame.Location.Function, "<unknown>"),
			displayPath(snapshot.Frame.Location.File),
			snapshot.Frame.Location.Line,
		)
		source, err := renderPlainWindow(snapshot.Frame.Location.File, snapshot.Frame.Location.Line, plainContextLines)
		if err != nil {
			fmt.Fprintf(&out, "err source: %v\n", err)
			break
		}
		out.WriteString(source)
		if snapshot.SourceError != nil {
			fmt.Fprintf(&out, "err source: %v\n", snapshot.SourceError)
		}
	}

	return out.String()
}

func appendExitHint(text string, snapshot *session.Snapshot) string {
	if snapshot == nil || !snapshot.State.Exited {
		return text
	}
	if text == "" || text[len(text)-1] != '\n' {
		text += "\n"
	}
	return text + "program exited; press o to inspect captured output, q to quit\n"
}

func setInspection(state *viewState, title string, body string) {
	if state == nil {
		return
	}
	state.inspectionTitle = title
	state.inspectionBody = body
}

func clearInspection(state *viewState) {
	if state == nil {
		return
	}
	state.inspectionTitle = ""
	state.inspectionBody = ""
}

func hasInspection(state *viewState) bool {
	return state != nil && state.inspectionBody != ""
}

func formatInspectionForView(snapshot *session.Snapshot, state *viewState, title string, body string, clear bool) string {
	if state == nil || !state.sticky {
		return appendPrompt(formatPlainInspection(snapshot, title, body), state)
	}
	if !state.outputTTY {
		return appendPrompt(title+":\n"+body, state)
	}

	var out strings.Builder
	out.WriteString(maybeClear(state, clear))
	if snapshot != nil && snapshot.Frame != nil {
		fmt.Fprintf(
			&out,
			"%s at %s%s:%d%s (%s)\n\n",
			title,
			"\x1b[36m",
			snapshot.Frame.Location.File,
			snapshot.Frame.Location.Line,
			"\x1b[0m",
			snapshot.Frame.Location.Function,
		)
		out.WriteString(body)
		out.WriteString("\n[Esc to return]\n")
		return appendPrompt(out.String(), state)
	}

	fmt.Fprintf(&out, "%s\n\n%s", title, body)
	return appendPrompt(out.String(), state)
}

func formatPlainInspection(snapshot *session.Snapshot, title string, body string) string {
	trimmedBody := strings.TrimRight(body, "\n")
	if snapshot != nil && snapshot.Frame != nil {
		return fmt.Sprintf(
			"%s %s %s:%d\n%s\n",
			title,
			fallbackText(snapshot.Frame.Location.Function, "<unknown>"),
			displayPath(snapshot.Frame.Location.File),
			snapshot.Frame.Location.Line,
			trimmedBody,
		)
	}
	if trimmedBody == "" {
		return title + "\n"
	}
	return title + "\n" + trimmedBody + "\n"
}

func appendPrompt(text string, state *viewState) string {
	if state == nil || !state.outputTTY {
		return text
	}
	trimmed := strings.TrimSuffix(text, commandLoopHelp+"\n>")
	trimmed = strings.TrimSuffix(trimmed, ">")
	if trimmed == "" || trimmed[len(trimmed)-1] != '\n' {
		trimmed += "\n"
	}
	if state.sticky {
		return trimmed + commandLoopHelp + "\n>"
	}
	return trimmed + ">"
}

func maybeClear(state *viewState, clear bool) string {
	if clear && state != nil && state.outputTTY {
		return clearScreenANSI
	}
	return ""
}

func stripANSIForNonTTY(text string, state *viewState) string {
	if state != nil && state.outputTTY {
		return text
	}
	return ansiEscapePattern.ReplaceAllString(text, "")
}

func displayPath(path string) string {
	if path == "" {
		return "<unknown>"
	}
	wd, err := os.Getwd()
	if err != nil {
		return filepath.Clean(path)
	}
	rel, err := filepath.Rel(wd, path)
	if err != nil {
		return filepath.Clean(path)
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return filepath.Clean(path)
	}
	return filepath.Clean(rel)
}

func renderPlainWindow(path string, line int, contextLines int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read source: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	if line <= 0 || line > len(lines) {
		return "", fmt.Errorf("source line out of range: %d", line)
	}

	start := max(1, line-contextLines)
	end := min(len(lines), line+contextLines)
	width := len(strconv.Itoa(end))

	var out strings.Builder
	for i := start; i <= end; i++ {
		marker := " "
		if i == line {
			marker = ">"
		}
		fmt.Fprintf(&out, "%s %*d | %s\n", marker, width, i, lines[i-1])
	}
	return out.String(), nil
}

func fallbackText(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
