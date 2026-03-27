package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
)

func usage(w io.Writer) {
	_, _ = fmt.Fprint(w, `dlvpp: opinionated Delve frontend

Usage:
  dlvpp version
  dlvpp launch [-s|--sticky] <package-or-path>
  dlvpp attach [-s|--sticky] <pid>

Examples:
  dlvpp version
  dlvpp launch ./examples/hello
  dlvpp launch -s ./path/to/your/package
  dlvpp attach -s 12345
`)
}

func parseLaunchArgs(args []string) (string, bool, error) {
	fs, sticky := newStickyFlagSet("launch")
	if err := fs.Parse(args); err != nil {
		return "", false, err
	}
	if fs.NArg() == 0 {
		return "", false, errors.New("launch requires a package or path")
	}
	if fs.NArg() > 1 {
		return "", false, errors.New("launch accepts exactly one package or path")
	}
	return fs.Arg(0), *sticky, nil
}

func parseAttachArgs(args []string) (int, bool, error) {
	fs, sticky := newStickyFlagSet("attach")
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
	return pid, *sticky, nil
}

func newStickyFlagSet(name string) (*flag.FlagSet, *bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	sticky := new(bool)
	fs.BoolVar(sticky, "sticky", false, "show the current function after each stop")
	fs.BoolVar(sticky, "s", false, "show the current function after each stop")
	return fs, sticky
}
