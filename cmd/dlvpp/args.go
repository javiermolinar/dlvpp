package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
)

func usage(w io.Writer) {
	_, _ = fmt.Fprintf(w, `dlvpp: opinionated Delve frontend

Usage:
  dlvpp version
  dlvpp launch [-p|--plain] <package-or-path>
  dlvpp test [-p|--plain] <package-or-path> <test-or-subtest>
  dlvpp attach [-p|--plain] <pid>

Modes:
  default         sticky, human-oriented view with re-rendered function context
  -p, --plain     compact, token-friendly view for agent/LLM-driven debugging

Interactive commands:
  %s
  Use h during a session to open the full help screen.

Examples:
  dlvpp version
  dlvpp launch ./examples/hello
  dlvpp launch -p ./path/to/your/package
  dlvpp test ./pkg/parser TestParse
  dlvpp test -p ./pkg/parser 'TestParse/case-1'
  dlvpp attach -p 12345
`, commandHelpSummary)
}

func parseLaunchArgs(args []string) (string, bool, error) {
	fs, plain := newPlainFlagSet("launch")
	if err := fs.Parse(args); err != nil {
		return "", false, err
	}
	if fs.NArg() == 0 {
		return "", false, errors.New("launch requires a package or path")
	}
	if fs.NArg() > 1 {
		return "", false, errors.New("launch accepts exactly one package or path")
	}
	return fs.Arg(0), !*plain, nil
}

func parseTestArgs(args []string) (string, string, bool, error) {
	fs, plain := newPlainFlagSet("test")
	if err := fs.Parse(args); err != nil {
		return "", "", false, err
	}
	if fs.NArg() == 0 {
		return "", "", false, errors.New("test requires a package or path")
	}
	if fs.NArg() == 1 {
		return "", "", false, errors.New("test requires a test or subtest name")
	}
	if fs.NArg() > 2 {
		return "", "", false, errors.New("test accepts exactly one package or path and one test or subtest name")
	}

	return fs.Arg(0), fs.Arg(1), !*plain, nil
}

func parseAttachArgs(args []string) (int, bool, error) {
	fs, plain := newPlainFlagSet("attach")
	if err := fs.Parse(args); err != nil {
		return 0, false, err
	}
	if fs.NArg() == 0 {
		return 0, false, errors.New("attach requires a pid")
	}
	if fs.NArg() > 1 {
		return 0, false, errors.New("attach accepts exactly one pid")
	}

	pid, err := strconv.Atoi(fs.Arg(0))
	if err != nil || pid <= 0 {
		return 0, false, fmt.Errorf("invalid pid: %q", fs.Arg(0))
	}
	return pid, !*plain, nil
}

func newPlainFlagSet(name string) (*flag.FlagSet, *bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	plain := new(bool)
	fs.BoolVar(plain, "plain", false, "disable sticky mode and use compact plain output")
	fs.BoolVar(plain, "p", false, "disable sticky mode and use compact plain output")
	return fs, plain
}
