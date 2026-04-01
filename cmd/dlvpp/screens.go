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
	locals, err := fetchLocals(ctx, runner, state)
	if err != nil {
		return err
	}
	return renderLocals(ctx, output, runner, state, locals)
}

func expandLocal(ctx context.Context, output io.Writer, runner commandRunner, state *viewState, name string) error {
	locals, err := fetchLocals(ctx, runner, state)
	if err != nil {
		return err
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("expand requires a local name")
	}

	actionCtx, cancel := context.WithTimeout(ctx, commandActionTimeout)
	defer cancel()

	path, local, err := resolveLocalPath(actionCtx, runner, state, locals, name)
	if err != nil {
		return err
	}
	if !local.HasChildren || local.Reference <= 0 {
		return fmt.Errorf("expand: %s has no children", name)
	}
	rememberExpandedLocalPath(state, path)
	return renderLocals(ctx, output, runner, state, locals)
}

func fetchLocals(ctx context.Context, runner commandRunner, state *viewState) ([]backend.Variable, error) {
	if state == nil || state.currentSnapshot == nil || state.currentSnapshot.Frame == nil {
		return nil, errors.New("locals: no current frame")
	}

	actionCtx, cancel := context.WithTimeout(ctx, commandActionTimeout)
	defer cancel()

	locals, err := runner.Locals(actionCtx, state.currentSnapshot.Frame.Ref)
	if err != nil {
		return nil, fmt.Errorf("locals: %w", err)
	}
	return locals, nil
}

func renderLocals(ctx context.Context, output io.Writer, runner commandRunner, state *viewState, locals []backend.Variable) error {
	expanded, err := expandedLocals(ctx, runner, state, locals)
	if err != nil {
		return err
	}
	body := formatLocalsForView(locals, expanded, inspectionColorsEnabled(state))
	setInspection(state, "locals", body)
	_, _ = fmt.Fprint(output, formatInspectionForView(state.currentSnapshot, state, "locals", body, true))
	return nil
}

func expandedLocals(ctx context.Context, runner commandRunner, state *viewState, locals []backend.Variable) (map[string][]backend.Variable, error) {
	if state == nil || len(state.expandedLocals) == 0 {
		return nil, nil
	}

	actionCtx, cancel := context.WithTimeout(ctx, commandActionTimeout)
	defer cancel()

	expanded := make(map[string][]backend.Variable, len(state.expandedLocals))
	visible, _ := filterDisplayedLocals(locals)
	for _, local := range visible {
		if err := collectExpandedLocals(actionCtx, runner, state.expandedLocals, expanded, local, local.Name); err != nil {
			return nil, err
		}
	}
	return expanded, nil
}

func resolveLocalPath(ctx context.Context, runner commandRunner, state *viewState, locals []backend.Variable, name string) (string, backend.Variable, error) {
	if strings.Contains(name, ".") {
		return resolveLocalPathBySegments(ctx, runner, locals, name)
	}

	expanded, err := expandedLocals(ctx, runner, state, locals)
	if err != nil {
		return "", backend.Variable{}, err
	}

	matches := findVisibleLocalMatches(locals, expanded, name)
	switch len(matches) {
	case 0:
		return "", backend.Variable{}, fmt.Errorf("expand: unknown local %q", name)
	case 1:
		return matches[0].Path, matches[0].Variable, nil
	default:
		paths := make([]string, 0, len(matches))
		for _, match := range matches {
			paths = append(paths, match.Path)
		}
		return "", backend.Variable{}, fmt.Errorf("expand: ambiguous local %q (%s)", name, strings.Join(paths, ", "))
	}
}

func resolveLocalPathBySegments(ctx context.Context, runner commandRunner, locals []backend.Variable, name string) (string, backend.Variable, error) {
	parts := strings.Split(name, ".")
	visible, _ := filterDisplayedLocals(locals)
	current, ok := findLocalByName(visible, parts[0])
	if !ok {
		return "", backend.Variable{}, fmt.Errorf("expand: unknown local %q", name)
	}

	path := current.Name
	for _, part := range parts[1:] {
		if !current.HasChildren || current.Reference <= 0 {
			return "", backend.Variable{}, fmt.Errorf("expand: unknown local %q", name)
		}
		children, err := runner.Children(ctx, current.Reference)
		if err != nil {
			return "", backend.Variable{}, fmt.Errorf("locals: expand %s: %w", path, err)
		}
		visibleChildren, _ := filterDisplayedLocals(children)
		next, ok := findLocalByName(visibleChildren, part)
		if !ok {
			return "", backend.Variable{}, fmt.Errorf("expand: unknown local %q", name)
		}
		path = joinLocalPath(path, next.Name)
		current = next
	}
	return path, current, nil
}

type localMatch struct {
	Path     string
	Variable backend.Variable
}

func findVisibleLocalMatches(locals []backend.Variable, expanded map[string][]backend.Variable, name string) []localMatch {
	visible, _ := filterDisplayedLocals(locals)
	matches := make([]localMatch, 0, 1)
	for _, local := range visible {
		collectVisibleLocalMatches(&matches, local, local.Name, expanded, name)
	}
	return matches
}

func collectVisibleLocalMatches(matches *[]localMatch, local backend.Variable, path string, expanded map[string][]backend.Variable, name string) {
	if local.Name == name {
		*matches = append(*matches, localMatch{Path: path, Variable: local})
	}
	children, ok := expanded[path]
	if !ok {
		return
	}
	visibleChildren, _ := filterDisplayedLocals(children)
	for _, child := range visibleChildren {
		collectVisibleLocalMatches(matches, child, joinLocalPath(path, child.Name), expanded, name)
	}
}

func findLocalByName(locals []backend.Variable, name string) (backend.Variable, bool) {
	for _, local := range locals {
		if local.Name == name {
			return local, true
		}
	}
	return backend.Variable{}, false
}

func collectExpandedLocals(ctx context.Context, runner commandRunner, wanted map[string]struct{}, expanded map[string][]backend.Variable, local backend.Variable, path string) error {
	if _, ok := wanted[path]; !ok {
		return nil
	}
	if local.Reference <= 0 {
		return nil
	}
	children, err := runner.Children(ctx, local.Reference)
	if err != nil {
		return fmt.Errorf("locals: expand %s: %w", path, err)
	}
	expanded[path] = children
	visibleChildren, _ := filterDisplayedLocals(children)
	for _, child := range visibleChildren {
		if err := collectExpandedLocals(ctx, runner, wanted, expanded, child, joinLocalPath(path, child.Name)); err != nil {
			return err
		}
	}
	return nil
}

func rememberExpandedLocalPath(state *viewState, path string) {
	if state == nil || path == "" {
		return
	}
	if state.expandedLocals == nil {
		state.expandedLocals = make(map[string]struct{})
	}
	parts := strings.Split(path, ".")
	for i := 1; i <= len(parts); i++ {
		state.expandedLocals[strings.Join(parts[:i], ".")] = struct{}{}
	}
}

func joinLocalPath(parent string, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
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
		"  :e result",
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

func formatLocalsForView(locals []backend.Variable, expanded map[string][]backend.Variable, color bool) string {
	if len(expanded) == 0 {
		if !color {
			return formatLocals(locals)
		}
		return formatTTYLocals(locals)
	}
	return formatExpandedLocals(locals, expanded, color)
}

func formatOutputForView(entries []backend.OutputEntry, color bool) string {
	if !color {
		return formatOutput(entries)
	}
	return formatTTYOutput(entries)
}
