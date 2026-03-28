package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
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
	bootstrapTimeout   = 15 * time.Second
)

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

func runLaunch(target string, sticky bool) error {
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return withController(signalCtx, func(startCtx context.Context, controller *session.Controller) error {
		launchReq, err := newLaunchRequest(target)
		if err != nil {
			return fmt.Errorf("resolve launch target: %w", err)
		}

		result, err := controller.StartLaunchSession(startCtx, launchReq, backend.BreakpointSpec{Location: defaultBreakpoint})
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return exitCodeError{code: 130}
			}
			return fmt.Errorf("launch failed: %w", err)
		}

		printSnapshot(os.Stdout, result.Snapshot, sticky)
		return runInteractiveSession(signalCtx, controller, result.Snapshot, sticky)
	})
}

func runAttach(pid int, sticky bool) error {
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return withController(signalCtx, func(startCtx context.Context, controller *session.Controller) error {
		result, err := controller.StartAttachSession(startCtx, backend.AttachRequest{PID: pid})
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return exitCodeError{code: 130}
			}
			return fmt.Errorf("attach failed: %w", err)
		}

		fmt.Printf("attach OK for pid %d\n", pid)
		printSnapshot(os.Stdout, result.Snapshot, sticky)
		return runInteractiveSession(signalCtx, controller, result.Snapshot, sticky)
	})
}

func withController(signalCtx context.Context, fn func(context.Context, *session.Controller) error) (err error) {
	startCtx, cancel := context.WithTimeout(signalCtx, bootstrapTimeout)
	defer cancel()

	controller := session.New(dapbackend.New(), session.Options{SourceContextLines: sourceContextLines})
	defer func() {
		err = errors.Join(err, closeSession(controller))
	}()

	return fn(startCtx, controller)
}

func runInteractiveSession(ctx context.Context, runner commandRunner, snapshot *session.Snapshot, sticky bool) error {
	if err := runCommandLoop(ctx, os.Stdin, os.Stdout, runner, snapshot, sticky); err != nil {
		if errors.Is(err, context.Canceled) {
			return exitCodeError{code: 130}
		}
		return err
	}
	return nil
}

func printSnapshot(w *os.File, snapshot *session.Snapshot, sticky bool) {
	state := newViewState(sticky, w, snapshot)
	_, _ = fmt.Fprint(w, formatSnapshotForView(snapshot, state, false))
}

func closeSession(s interface{ Close() error }) error {
	if err := s.Close(); err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
		return fmt.Errorf("close backend: %w", err)
	}
	return nil
}
