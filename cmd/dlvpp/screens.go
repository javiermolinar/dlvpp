package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"dlvpp/internal/backend"
	"dlvpp/internal/session"
)

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

func showHelp(output io.Writer, state *viewState) error {
	body := formatHelpBody()
	setInspection(state, "help", body)
	_, _ = fmt.Fprint(output, formatInspectionForView(currentSnapshot(state), state, "help", body, true))
	return nil
}

func showBreakpoints(output io.Writer, state *viewState) error {
	body := formatBreakpointsForView(state)
	setInspection(state, "breakpoints", body)
	_, _ = fmt.Fprint(output, formatInspectionForView(currentSnapshot(state), state, "breakpoints", body, true))
	return nil
}

func currentSnapshot(state *viewState) *session.Snapshot {
	if state == nil {
		return nil
	}
	return state.currentSnapshot
}

func inspectionColorsEnabled(state *viewState) bool {
	return state != nil && state.sticky && state.outputTTY
}

func formatHelpBody() string {
	return strings.Join([]string{
		"Navigation",
		"  c   continue",
		"  n   next",
		"  s   step in",
		"  q   quit",
		"",
		"Inspection",
		"  l   locals",
		"  o   output",
		"  b   breakpoints",
		"  h   help",
		"",
		"Breakpoints",
		"  b   list breakpoints",
		"  :b 14",
		"  :b add",
		"  :b file.go:23",
	}, "\n") + "\n"
}

func formatBreakpointsForView(state *viewState) string {
	if state == nil || len(state.breakpoints) == 0 {
		return "(no breakpoints)\n"
	}

	var out strings.Builder
	for _, bp := range state.breakpoints {
		marker := "o"
		if state.outputTTY {
			marker = ansiRed + "●" + ansiReset
		}
		location := displayPath(bp.File)
		if bp.Line > 0 {
			location = fmt.Sprintf("%s:%d", location, bp.Line)
		}
		function := bp.Function
		if function == "" {
			function = "-"
		}
		if bp.ID > 0 {
			fmt.Fprintf(&out, "%s #%d  %s  %s\n", marker, bp.ID, location, function)
			continue
		}
		fmt.Fprintf(&out, "%s %s  %s\n", marker, location, function)
	}
	return out.String()
}

func formatLocalsForView(locals []backend.Variable, color bool) string {
	if !color {
		return formatLocals(locals)
	}
	return formatTTYLocals(locals)
}

func formatOutputForView(entries []backend.OutputEntry, color bool) string {
	if !color {
		return formatOutput(entries)
	}
	return formatTTYOutput(entries)
}
