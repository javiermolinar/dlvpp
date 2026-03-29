package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanCommentBreakpointsInFileFindsInlineCommentMarker(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	source := strings.Join([]string{
		"package main",
		"",
		"func main() {",
		"\tprintln(\"before\") // breakpoint",
		"",
		"\tprintln(\"after\")",
		"}",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}

	specs, err := scanCommentBreakpointsInFile(path)
	if err != nil {
		t.Fatalf("scanCommentBreakpointsInFile returned error: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 breakpoint spec, got %#v", specs)
	}
	if got, want := specs[0].Location, path+":6"; got != want {
		t.Fatalf("expected breakpoint location %q, got %q", want, got)
	}
}

func TestScanCommentBreakpointsInFileIgnoresBreakpointTextInsideStrings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	source := strings.Join([]string{
		"package main",
		"",
		"func main() {",
		"\tprintln(\"http://example/breakpoint\")",
		"}",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}

	specs, err := scanCommentBreakpointsInFile(path)
	if err != nil {
		t.Fatalf("scanCommentBreakpointsInFile returned error: %v", err)
	}
	if len(specs) != 0 {
		t.Fatalf("expected no breakpoint specs, got %#v", specs)
	}
}
