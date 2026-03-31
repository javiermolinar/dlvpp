package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		var exitErr exitCodeError
		if errors.As(err, &exitErr) {
			if exitErr.err != nil {
				fmt.Fprintln(os.Stderr, exitErr.err)
			}
			os.Exit(exitErr.code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage(os.Stderr)
		return exitCodeError{code: 2}
	}

	switch args[0] {
	case "version":
		return runDlvVersion()
	case "launch":
		target, programArgs, sticky, verbose, err := parseLaunchArgs(args[1:])
		if err != nil {
			if errors.Is(err, flag.ErrHelp) {
				usage(os.Stdout)
				return nil
			}
			usage(os.Stderr)
			return exitCodeError{code: 2, err: err}
		}
		return runLaunch(target, programArgs, sticky, newCommandLogger(verbose, os.Stdout))
	case "test":
		target, selector, programArgs, sticky, verbose, err := parseTestArgs(args[1:])
		if err != nil {
			if errors.Is(err, flag.ErrHelp) {
				usage(os.Stdout)
				return nil
			}
			usage(os.Stderr)
			return exitCodeError{code: 2, err: err}
		}
		return runTest(target, selector, programArgs, sticky, newCommandLogger(verbose, os.Stdout))
	case "attach":
		pid, sticky, verbose, err := parseAttachArgs(args[1:])
		if err != nil {
			if errors.Is(err, flag.ErrHelp) {
				usage(os.Stdout)
				return nil
			}
			usage(os.Stderr)
			return exitCodeError{code: 2, err: err}
		}
		return runAttach(pid, sticky, newCommandLogger(verbose, os.Stdout))
	case "help", "-h", "--help":
		usage(os.Stdout)
		return nil
	default:
		usage(os.Stderr)
		return exitCodeError{code: 2, err: fmt.Errorf("unknown command: %s", args[0])}
	}
}

type exitCodeError struct {
	code int
	err  error
}

func (e exitCodeError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}
