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
  dlvpp launch [-p|--plain] [-v|--verbose] <package-or-path> [-- <program-args...>]
  dlvpp test [-p|--plain] [-v|--verbose] <package-or-path> <test-or-subtest> [-- <test-binary-args...>]
  dlvpp attach [-p|--plain] [-v|--verbose] <pid>

Modes:
  default         sticky, human-oriented view with re-rendered function context
  -p, --plain     compact, token-friendly view for agent/LLM-driven debugging
  -v, --verbose   print startup/debugger bootstrap logs

Interactive commands:
  %s
  Use h during a session to open the full help screen.

Examples:
  dlvpp version
  dlvpp launch ./examples/hello
  dlvpp launch -p ./path/to/your/package -- --name alice
  dlvpp test ./pkg/parser TestParse
  dlvpp test -p ./pkg/parser 'TestParse/case-1' -- -test.v
  dlvpp attach -p 12345
`, commandHelpSummary)
}

func parseLaunchArgs(args []string) (string, []string, bool, bool, error) {
	fs, plain, verbose := newCommandFlagSet("launch")
	if err := fs.Parse(args); err != nil {
		return "", nil, false, false, err
	}

	positionals, programArgs, hasSeparator := splitPassthroughArgs(fs.Args())
	if len(positionals) == 0 {
		return "", nil, false, false, errors.New("launch requires a package or path")
	}
	if len(positionals) > 1 {
		if !hasSeparator {
			return "", nil, false, false, errors.New("launch accepts exactly one package or path; use -- to pass program args")
		}
		return "", nil, false, false, errors.New("launch accepts exactly one package or path before --")
	}
	return positionals[0], programArgs, !*plain, *verbose, nil
}

func parseTestArgs(args []string) (string, string, []string, bool, bool, error) {
	fs, plain, verbose := newCommandFlagSet("test")
	if err := fs.Parse(args); err != nil {
		return "", "", nil, false, false, err
	}

	positionals, programArgs, hasSeparator := splitPassthroughArgs(fs.Args())
	if len(positionals) == 0 {
		return "", "", nil, false, false, errors.New("test requires a package or path")
	}
	if len(positionals) == 1 {
		return "", "", nil, false, false, errors.New("test requires a test or subtest name")
	}
	if len(positionals) > 2 {
		if !hasSeparator {
			return "", "", nil, false, false, errors.New("test accepts exactly one package or path and one test or subtest name; use -- to pass test binary args")
		}
		return "", "", nil, false, false, errors.New("test accepts exactly one package or path and one test or subtest name before --")
	}

	return positionals[0], positionals[1], programArgs, !*plain, *verbose, nil
}

func splitPassthroughArgs(args []string) ([]string, []string, bool) {
	for idx, arg := range args {
		if arg != "--" {
			continue
		}
		return args[:idx], args[idx+1:], true
	}
	return args, nil, false
}

func parseAttachArgs(args []string) (int, bool, bool, error) {
	fs, plain, verbose := newCommandFlagSet("attach")
	if err := fs.Parse(args); err != nil {
		return 0, false, false, err
	}
	if fs.NArg() == 0 {
		return 0, false, false, errors.New("attach requires a pid")
	}
	if fs.NArg() > 1 {
		return 0, false, false, errors.New("attach accepts exactly one pid")
	}

	pid, err := strconv.Atoi(fs.Arg(0))
	if err != nil || pid <= 0 {
		return 0, false, false, fmt.Errorf("invalid pid: %q", fs.Arg(0))
	}
	return pid, !*plain, *verbose, nil
}

func newCommandFlagSet(name string) (*flag.FlagSet, *bool, *bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	plain := new(bool)
	verbose := new(bool)
	fs.BoolVar(plain, "plain", false, "disable sticky mode and use compact plain output")
	fs.BoolVar(plain, "p", false, "disable sticky mode and use compact plain output")
	fs.BoolVar(verbose, "verbose", false, "print startup/debugger bootstrap logs")
	fs.BoolVar(verbose, "v", false, "print startup/debugger bootstrap logs")
	return fs, plain, verbose
}
