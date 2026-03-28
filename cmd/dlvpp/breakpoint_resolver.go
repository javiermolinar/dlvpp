package main

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"strconv"
	"strings"

	"dlvpp/internal/session"
)

type breakpointResolveContext struct {
	Snapshot *session.Snapshot
}

func resolveBreakpointLocation(raw string, ctx breakpointResolveContext) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("break requires a location")
	}

	if line, ok := parseBreakpointLine(raw); ok {
		file := currentBreakpointFile(ctx)
		if file == "" {
			return "", errors.New("line breakpoints require a current file")
		}
		return fmt.Sprintf("%s:%d", file, line), nil
	}

	if isBareBreakpointSymbol(raw) {
		pkg, err := currentBreakpointPackage(ctx)
		if err != nil {
			return "", err
		}
		if pkg == "" {
			return raw, nil
		}
		return pkg + "." + raw, nil
	}

	return raw, nil
}

func parseBreakpointLine(raw string) (int, bool) {
	line, err := strconv.Atoi(raw)
	if err != nil || line <= 0 {
		return 0, false
	}
	return line, true
}

func isBareBreakpointSymbol(raw string) bool {
	return !strings.Contains(raw, ":") && !strings.Contains(raw, ".") && token.IsIdentifier(raw)
}

func currentBreakpointFile(ctx breakpointResolveContext) string {
	if ctx.Snapshot == nil || ctx.Snapshot.Frame == nil {
		return ""
	}
	return ctx.Snapshot.Frame.Location.File
}

func currentBreakpointPackage(ctx breakpointResolveContext) (string, error) {
	path := currentBreakpointFile(ctx)
	if path == "" {
		return "", nil
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
	if err != nil {
		return "", fmt.Errorf("infer package for breakpoint: %w", err)
	}
	if file.Name == nil {
		return "", nil
	}
	return file.Name.Name, nil
}
