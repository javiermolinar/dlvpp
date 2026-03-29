package main

import (
	"bytes"
	"context"
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"dlvpp/internal/backend"
)

func discoverCommentBreakpointsInPackage(ctx context.Context, target string, includeTests bool) ([]backend.BreakpointSpec, error) {
	entry, err := loadPackageEntry(ctx, target, includeTests)
	if err != nil {
		return nil, err
	}
	return discoverCommentBreakpointsInFiles(entry.Files)
}

func discoverCommentBreakpointsAcrossModule(ctx context.Context, target string, includeTests bool) ([]backend.BreakpointSpec, error) {
	entry, err := loadPackageEntry(ctx, target, includeTests)
	if err != nil {
		return nil, err
	}
	root, err := findModuleRoot(entry.Dir)
	if err != nil {
		return nil, err
	}
	files, err := walkModuleGoFiles(root, includeTests)
	if err != nil {
		return nil, err
	}
	return discoverCommentBreakpointsInFiles(files)
}

func discoverCommentBreakpointsInFiles(files []string) ([]backend.BreakpointSpec, error) {
	specs := make([]backend.BreakpointSpec, 0, len(files))
	for _, path := range files {
		fileSpecs, err := scanCommentBreakpointsInFile(path)
		if err != nil {
			return nil, err
		}
		specs = append(specs, fileSpecs...)
	}
	return dedupeBreakpointSpecs(specs), nil
}

func scanCommentBreakpointsInFile(path string) ([]backend.BreakpointSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read source file %s: %w", path, err)
	}
	if !bytes.Contains(data, []byte("breakpoint")) {
		return nil, nil
	}

	lines := strings.Split(string(data), "\n")
	fset := token.NewFileSet()
	file := fset.AddFile(filepath.Base(path), -1, len(data))

	var s scanner.Scanner
	s.Init(file, data, nil, scanner.ScanComments)

	specs := make([]backend.BreakpointSpec, 0, 1)
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			return specs, nil
		}
		if tok != token.COMMENT || !isBreakpointComment(lit) {
			continue
		}

		startOffset := file.Offset(pos)
		endOffset := startOffset + len(lit)
		if endOffset > len(data) {
			endOffset = len(data)
		}
		endLine := file.Position(file.Pos(endOffset)).Line
		targetLine := resolveBreakpointTargetLine(lines, endLine)
		if targetLine <= 0 {
			continue
		}
		specs = append(specs, backend.BreakpointSpec{Location: fmt.Sprintf("%s:%d", path, targetLine)})
	}
}
