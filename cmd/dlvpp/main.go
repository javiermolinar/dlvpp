package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"dlvpp/internal/backend"
	dapbackend "dlvpp/internal/backend/dap"
	"dlvpp/internal/session"
)

const (
	defaultBreakpoint  = "main.main"
	sourceContextLines = 5
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
		if len(args) < 2 {
			usage(os.Stderr)
			return exitCodeError{code: 2, err: errors.New("launch requires a package or path")}
		}
		return runLaunch(args[1])
	case "attach":
		if len(args) < 2 {
			usage(os.Stderr)
			return exitCodeError{code: 2, err: errors.New("attach requires a pid")}
		}
		pid, err := strconv.Atoi(args[1])
		if err != nil || pid <= 0 {
			return exitCodeError{code: 2, err: fmt.Errorf("invalid pid: %q", args[1])}
		}
		return runAttach(pid)
	case "help", "-h", "--help":
		usage(os.Stdout)
		return nil
	default:
		usage(os.Stderr)
		return exitCodeError{code: 2, err: fmt.Errorf("unknown command: %s", args[0])}
	}
}

func usage(w *os.File) {
	_, _ = fmt.Fprint(w, `dlvpp: opinionated Delve frontend

Usage:
  dlvpp version
  dlvpp launch <package-or-path>
  dlvpp attach <pid>

Examples:
  dlvpp version
  dlvpp launch ./examples/hello
  dlvpp launch ./path/to/your/package
  dlvpp attach 12345
`)
}

func runDlvVersion() error {
	dlvPath, err := exec.LookPath("dlv")
	if err != nil {
		return errors.New("dlv not found in PATH")
	}

	cmd := exec.Command(dlvPath, "version")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitCodeError{code: exitErr.ExitCode()}
		}
		return fmt.Errorf("running dlv version: %w", err)
	}
	return nil
}

func runLaunch(target string) error {
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctx, cancel := context.WithTimeout(signalCtx, 15*time.Second)
	defer cancel()

	controller := session.New(dapbackend.New(), session.Options{SourceContextLines: sourceContextLines})
	defer closeSession(controller)

	result, err := controller.StartLaunchSession(ctx, backend.LaunchRequest{
		Mode:    backend.LaunchModeDebug,
		Target:  target,
		WorkDir: ".",
	}, backend.BreakpointSpec{Location: defaultBreakpoint})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return exitCodeError{code: 130}
		}
		return fmt.Errorf("launch failed: %w", err)
	}

	fmt.Print(session.FormatSnapshot(result.Snapshot))

	if err := waitForDisconnect(signalCtx, os.Stdin); err != nil {
		if errors.Is(err, context.Canceled) {
			return exitCodeError{code: 130}
		}
		return fmt.Errorf("wait for disconnect: %w", err)
	}
	return nil
}

func runAttach(pid int) error {
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctx, cancel := context.WithTimeout(signalCtx, 15*time.Second)
	defer cancel()

	controller := session.New(dapbackend.New(), session.Options{SourceContextLines: sourceContextLines})
	defer closeSession(controller)

	result, err := controller.StartAttachSession(ctx, backend.AttachRequest{PID: pid})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return exitCodeError{code: 130}
		}
		return fmt.Errorf("attach failed: %w", err)
	}

	fmt.Printf("attach OK for pid %d\n", pid)
	fmt.Print(session.FormatSnapshot(result.Snapshot))

	if err := waitForDisconnect(signalCtx, os.Stdin); err != nil {
		if errors.Is(err, context.Canceled) {
			return exitCodeError{code: 130}
		}
		return fmt.Errorf("wait for disconnect: %w", err)
	}
	return nil
}

func waitForDisconnect(ctx context.Context, input io.Reader) error {
	fmt.Println("Press Enter to disconnect...")

	if err := ctx.Err(); err != nil {
		return err
	}

	result := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(input)
		_, err := reader.ReadString('\n')
		if errors.Is(err, io.EOF) {
			result <- nil
			return
		}
		result <- err
	}()

	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func closeSession(s interface{ Close() error }) {
	if err := s.Close(); err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
		fmt.Fprintf(os.Stderr, "close backend: %v\n", err)
		os.Exit(1)
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
