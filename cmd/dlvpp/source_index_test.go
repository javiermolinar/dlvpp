package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceIndexPackageInfoCollectsSymbols(t *testing.T) {
	t.Parallel()

	idx := newSourceIndex()
	infos, err := idx.PackageInfo(context.Background(), exampleTarget("parser"), true)
	if err != nil {
		t.Fatalf("PackageInfo returned error: %v", err)
	}

	var found bool
	for _, info := range infos {
		for _, symbol := range info.Symbols {
			if symbol.Name == "ParseInt" && symbol.Qualified == "parser.ParseInt" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("expected ParseInt symbol in package info")
	}
}

func TestDiscoverCommentBreakpointsForLaunchExample(t *testing.T) {
	t.Parallel()

	idx := newSourceIndex()
	specs, err := discoverCommentBreakpointsInModule(context.Background(), idx, exampleTarget("hello"), false)
	if err != nil {
		t.Fatalf("discoverCommentBreakpointsInModule returned error: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 breakpoint spec, got %#v", specs)
	}

	wd, err := filepath.Abs(filepath.Join("..", "..", "examples", "hello", "main.go"))
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	want := wd + ":13"
	if specs[0].Location != want {
		t.Fatalf("expected breakpoint location %q, got %q", want, specs[0].Location)
	}
}

func TestModuleFilesIncludeOtherGoPackagesInModule(t *testing.T) {
	t.Parallel()

	idx := newSourceIndex()
	files, err := idx.ModuleFiles(context.Background(), exampleTarget("hello"), false)
	if err != nil {
		t.Fatalf("ModuleFiles returned error: %v", err)
	}

	var foundHello bool
	var foundParser bool
	for _, file := range files {
		switch {
		case strings.HasSuffix(file, filepath.Join("examples", "hello", "main.go")):
			foundHello = true
		case strings.HasSuffix(file, filepath.Join("examples", "parser", "parser.go")):
			foundParser = true
		case strings.Contains(file, string(filepath.Separator)+"vendor"+string(filepath.Separator)):
			t.Fatalf("expected vendor paths to be skipped, got %q", file)
		}
	}
	if !foundHello || !foundParser {
		t.Fatalf("expected module scan to include hello and parser files, got %#v", files)
	}
}

func TestDiscoverCommentBreakpointsForTestExampleIncludesTestFiles(t *testing.T) {
	t.Parallel()

	idx := newSourceIndex()
	specs, err := discoverCommentBreakpoints(context.Background(), idx, exampleTarget("parser"), true)
	if err != nil {
		t.Fatalf("discoverCommentBreakpoints returned error: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 breakpoint spec, got %#v", specs)
	}
	if !strings.HasSuffix(specs[0].Location, filepath.Join("examples", "parser", "parser_test.go")+":18") {
		t.Fatalf("unexpected test breakpoint location: %q", specs[0].Location)
	}
}

func TestDiscoverCommentBreakpointsSkipsTestFilesWhenDisabled(t *testing.T) {
	t.Parallel()

	idx := newSourceIndex()
	specs, err := discoverCommentBreakpoints(context.Background(), idx, exampleTarget("parser"), false)
	if err != nil {
		t.Fatalf("discoverCommentBreakpoints returned error: %v", err)
	}
	if len(specs) != 0 {
		t.Fatalf("expected no breakpoint specs without test files, got %#v", specs)
	}
}

func exampleTarget(name string) string {
	return filepath.Join("..", "..", "examples", name)
}
