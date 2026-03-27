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
		return session.FormatSnapshot(snapshot)
	}

	base := session.FormatSnapshot(snapshot)
	if snapshot.State.Exited || snapshot.State.Running || snapshot.Frame == nil {
		return maybeClear(state, clear) + base
	}

	source, err := sourceview.RenderFunction(snapshot.Frame.Location.File, snapshot.Frame.Location.Line)
	if err != nil {
		return maybeClear(state, clear) + base + fmt.Sprintf("sticky render: %v\n", err)
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
	return out.String()
}

func maybeClear(state *viewState, clear bool) string {
	if clear && state != nil && state.outputTTY {
		return clearScreenANSI
	}
	return ""
}
