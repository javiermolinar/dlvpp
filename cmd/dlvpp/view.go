package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"dlvpp/internal/session"
	"dlvpp/internal/sourceview"
	"golang.org/x/term"
)

const clearScreenANSI = "\x1b[2J\x1b[H"

type viewState struct {
	sticky          bool
	outputTTY       bool
	currentSnapshot *session.Snapshot
	inspectionTitle string
	inspectionBody  string
}

func newViewState(sticky bool, output io.Writer, initialSnapshot *session.Snapshot) *viewState {
	outputTTY := false
	if file, ok := output.(*os.File); ok {
		outputTTY = term.IsTerminal(int(file.Fd()))
	}
	return &viewState{
		sticky:          sticky,
		outputTTY:       outputTTY,
		currentSnapshot: initialSnapshot,
	}
}

func formatSnapshotForView(snapshot *session.Snapshot, state *viewState, clear bool) string {
	if snapshot == nil || state == nil || !state.sticky {
		return appendPrompt(session.FormatSnapshot(snapshot), state)
	}

	base := session.FormatSnapshot(snapshot)
	if snapshot.State.Exited || snapshot.State.Running || snapshot.Frame == nil {
		return appendPrompt(maybeClear(state, clear)+base, state)
	}

	source, err := sourceview.RenderFunction(snapshot.Frame.Location.File, snapshot.Frame.Location.Line)
	if err != nil {
		return appendPrompt(maybeClear(state, clear)+base+fmt.Sprintf("sticky render: %v\n", err), state)
	}

	var out strings.Builder
	out.WriteString(maybeClear(state, clear))
	fmt.Fprintf(
		&out,
		"stopped: %s at %s%s:%d%s\n",
		snapshot.Frame.Location.Function,
		"\x1b[36m",
		snapshot.Frame.Location.File,
		snapshot.Frame.Location.Line,
		"\x1b[0m",
	)
	out.WriteString(source)
	if snapshot.SourceError != nil {
		fmt.Fprintf(&out, "%v\n", snapshot.SourceError)
	}
	return appendPrompt(out.String(), state)
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
	if state == nil || !state.sticky || !state.outputTTY {
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

func appendPrompt(text string, state *viewState) string {
	if state == nil || !state.outputTTY {
		return text
	}
	trimmed := strings.TrimPrefix(text, commandLoopHelp+"\n")
	if trimmed == "" || trimmed[len(trimmed)-1] != '\n' {
		trimmed += "\n"
	}
	return trimmed + commandLoopHelp + "\n>"
}

func maybeClear(state *viewState, clear bool) string {
	if clear && state != nil && state.outputTTY {
		return clearScreenANSI
	}
	return ""
}
