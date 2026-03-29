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
	bootstrapTimeout   = 60 * time.Second
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

func runLaunch(target string, sticky bool, verbose bool) error {
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log := newLogger(verbose, os.Stdout)
	return withController(signalCtx, func(startCtx context.Context, controller *session.Controller) error {
		launchReq, err := newLaunchRequest(target)
		if err != nil {
			return fmt.Errorf("resolve launch target: %w", err)
		}

		snapshot, initialBreakpoints, report, err := startLaunchWithAutoBreakpoints(startCtx, log, controller, launchReq, target, false, []backend.BreakpointSpec{{Location: defaultBreakpoint}})
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return exitCodeError{code: 130}
			}
			return fmt.Errorf("launch failed: %w", err)
		}

		logAutoBreakpointsSummary(log, report)
		printSnapshot(os.Stdout, snapshot, sticky, initialBreakpoints)
		return runInteractiveSession(signalCtx, controller, snapshot, sticky, initialBreakpoints)
	})
}

func runTest(target string, selector string, sticky bool, verbose bool) error {
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log := newLogger(verbose, os.Stdout)
	return withController(signalCtx, func(startCtx context.Context, controller *session.Controller) error {
		launchReq, err := newTestLaunchRequest(target, selector)
		if err != nil {
			return fmt.Errorf("resolve test target: %w", err)
		}

		breakpoint := topLevelTestName(selector)
		snapshot, initialBreakpoints, report, err := startLaunchWithAutoBreakpoints(startCtx, log, controller, launchReq, target, true, []backend.BreakpointSpec{{Location: breakpoint}})
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return exitCodeError{code: 130}
			}
			return fmt.Errorf("test launch failed: %w", err)
		}

		logAutoBreakpointsSummary(log, report)
		printSnapshot(os.Stdout, snapshot, sticky, initialBreakpoints)
		return runInteractiveSession(signalCtx, controller, snapshot, sticky, initialBreakpoints)
	})
}

type launchAutoBreakpointReport struct {
	scope             string
	discoveryDuration time.Duration
	launchDuration    time.Duration
	loaded            []breakpointRecord
	skipped           []string
}

func startLaunchWithAutoBreakpoints(ctx context.Context, log logger, controller *session.Controller, req backend.LaunchRequest, target string, includeTests bool, bootstrap []backend.BreakpointSpec) (*session.Snapshot, []breakpointRecord, launchAutoBreakpointReport, error) {
	type discoveryResult struct {
		scope    string
		specs    []backend.BreakpointSpec
		duration time.Duration
		err      error
	}

	discovery := make(chan discoveryResult, 1)
	if includeTests {
		log.Debugf("startup: scanning target package for auto breakpoints...")
	} else {
		log.Debugf("startup: scanning module for auto breakpoints...")
	}
	go func() {
		started := time.Now()
		index := newSourceIndex()
		var (
			scope string
			specs []backend.BreakpointSpec
			err   error
		)
		if includeTests {
			scope = "target package"
			specs, err = discoverCommentBreakpoints(ctx, index, target, true)
		} else {
			scope = "module"
			specs, err = discoverCommentBreakpointsInModule(ctx, index, target, false)
		}
		if err != nil {
			discovery <- discoveryResult{scope: scope, duration: time.Since(started), err: fmt.Errorf("discover comment breakpoints: %w", err)}
			return
		}
		discovery <- discoveryResult{scope: scope, specs: specs, duration: time.Since(started)}
	}()

	log.Debugf("startup: starting delve dap...")
	log.Debugf("startup: launching target...")
	launchStarted := time.Now()
	if _, err := controller.Launch(ctx, req); err != nil {
		return nil, nil, launchAutoBreakpointReport{}, err
	}
	launchDuration := time.Since(launchStarted)

	log.Debugf("startup: waiting for first stop...")
	var discovered discoveryResult
	select {
	case result := <-discovery:
		if result.err != nil {
			return nil, nil, launchAutoBreakpointReport{}, result.err
		}
		discovered = result
	case <-ctx.Done():
		return nil, nil, launchAutoBreakpointReport{}, ctx.Err()
	}

	report := launchAutoBreakpointReport{
		scope:             discovered.scope,
		discoveryDuration: discovered.duration,
		launchDuration:    launchDuration,
	}

	discoveredRecords := make([]breakpointRecord, 0, len(discovered.specs))
	discoveredLocations := make(map[string]struct{}, len(discovered.specs))
	for _, spec := range discovered.specs {
		discoveredLocations[spec.Location] = struct{}{}
		bp, err := controller.CreateBreakpoint(ctx, spec)
		if err != nil {
			report.skipped = append(report.skipped, fmt.Sprintf("%s (%v)", displayPathFromLocation(spec.Location), err))
			continue
		}
		if record, ok := breakpointRecordFromBackend(bp); ok {
			discoveredRecords = append(discoveredRecords, record)
		}
	}
	report.loaded = append([]breakpointRecord(nil), discoveredRecords...)

	bootstrapRecords := make([]breakpointRecord, 0, len(bootstrap))
	for _, spec := range dedupeBreakpointSpecs(bootstrap) {
		if _, ok := discoveredLocations[spec.Location]; ok {
			continue
		}
		bp, err := controller.CreateBreakpoint(ctx, spec)
		if err != nil {
			return nil, nil, launchAutoBreakpointReport{}, err
		}
		if record, ok := breakpointRecordFromBackend(bp); ok {
			bootstrapRecords = append(bootstrapRecords, record)
		}
	}

	snapshot, err := controller.Continue(ctx)
	if err != nil {
		return nil, nil, launchAutoBreakpointReport{}, err
	}
	allRecords := append(append([]breakpointRecord(nil), discoveredRecords...), bootstrapRecords...)
	return snapshot, allRecords, report, nil
}

func runAttach(pid int, sticky bool, verbose bool) error {
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log := newLogger(verbose, os.Stdout)
	return withController(signalCtx, func(startCtx context.Context, controller *session.Controller) error {
		result, err := controller.StartAttachSession(startCtx, backend.AttachRequest{PID: pid})
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return exitCodeError{code: 130}
			}
			return fmt.Errorf("attach failed: %w", err)
		}

		log.Infof("attach OK for pid %d", pid)
		printSnapshot(os.Stdout, result.Snapshot, sticky, nil)
		return runInteractiveSession(signalCtx, controller, result.Snapshot, sticky, nil)
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

func runInteractiveSession(ctx context.Context, runner commandRunner, snapshot *session.Snapshot, sticky bool, initialBreakpoints []breakpointRecord) error {
	if err := runCommandLoopWithBreakpoints(ctx, os.Stdin, os.Stdout, runner, snapshot, sticky, initialBreakpoints); err != nil {
		if errors.Is(err, context.Canceled) {
			return exitCodeError{code: 130}
		}
		return err
	}
	return nil
}

func logAutoBreakpointsSummary(log logger, report launchAutoBreakpointReport) {
	log.Infof("startup: dap=%s source-scan=%s", report.launchDuration.Round(time.Millisecond), report.discoveryDuration.Round(time.Millisecond))
	if len(report.loaded) == 0 {
		log.Infof("auto breakpoints (%s): none loaded", report.scope)
	} else {
		log.Infof("auto breakpoints (%s): %d loaded", report.scope, len(report.loaded))
		for _, record := range report.loaded {
			if record.Function != "" {
				log.Infof("- %s:%d (%s)", displayPath(record.File), record.Line, record.Function)
				continue
			}
			log.Infof("- %s:%d", displayPath(record.File), record.Line)
		}
	}
	if len(report.skipped) > 0 {
		log.Infof("auto breakpoints skipped: %d", len(report.skipped))
		for _, skipped := range report.skipped {
			log.Infof("- %s", skipped)
		}
	}
}

func displayPathFromLocation(location string) string {
	path, line, ok := strings.Cut(location, ":")
	if ok && line != "" {
		return displayPath(path) + ":" + line
	}
	return location
}

func printSnapshot(w *os.File, snapshot *session.Snapshot, sticky bool, initialBreakpoints []breakpointRecord) {
	state := newViewState(sticky, w, snapshot, initialBreakpoints)
	_, _ = fmt.Fprint(w, formatSnapshotForView(snapshot, state, false))
}

func closeSession(s interface{ Close() error }) error {
	if err := s.Close(); err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
		return fmt.Errorf("close backend: %w", err)
	}
	return nil
}
