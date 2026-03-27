package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"dlvpp/internal/backend"
	dapbackend "dlvpp/internal/backend/dap"
	"dlvpp/internal/sourceview"
)

const (
	defaultBreakpoint  = "main.main"
	sourceContextLines = 5
	ansiReset          = "\x1b[0m"
	ansiCyan           = "\x1b[36m"
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := dapbackend.New()
	if err := client.Launch(ctx, backend.LaunchRequest{
		Mode:    backend.LaunchModeDebug,
		Target:  target,
		WorkDir: ".",
	}); err != nil {
		return fmt.Errorf("launch failed: %w", err)
	}
	defer closeBackend(client)

	fmt.Printf("launch OK for %s\n", target)
	bp, err := client.CreateBreakpoint(ctx, backend.BreakpointSpec{Location: defaultBreakpoint})
	if err != nil {
		return fmt.Errorf("set default breakpoint: %w", err)
	}
	fmt.Printf("default breakpoint: %s at %s:%d\n", bp.Location.Function, bp.Location.File, bp.Location.Line)
	if _, err := client.Continue(ctx); err != nil {
		return fmt.Errorf("continue: %w", err)
	}
	printCurrentLocation(ctx, client)
	waitForEnter()
	return nil
}

func runAttach(pid int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := dapbackend.New()
	if err := client.Attach(ctx, backend.AttachRequest{PID: pid}); err != nil {
		return fmt.Errorf("attach failed: %w", err)
	}
	defer closeBackend(client)

	fmt.Printf("attach OK for pid %d\n", pid)
	printCurrentLocation(ctx, client)
	waitForEnter()
	return nil
}

func printCurrentLocation(ctx context.Context, b backend.Backend) {
	state, err := b.State(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "state error: %v\n", err)
		return
	}

	frames, err := b.Stack(ctx, state.ThreadID, 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stack error: %v\n", err)
		return
	}
	if len(frames) == 0 {
		fmt.Println("stopped, but no stack frames available")
		return
	}

	frame := frames[0]
	fmt.Printf("stopped: %s at %s%s:%d%s\n", frame.Location.Function, ansiCyan, frame.Location.File, frame.Location.Line, ansiReset)
	printSourceWindow(frame.Location.File, frame.Location.Line, sourceContextLines)
}

func printSourceWindow(path string, line int, contextLines int) {
	rendered, err := sourceview.RenderWindow(path, line, contextLines)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return
	}
	fmt.Print(rendered)
}

func waitForEnter() {
	fmt.Println("Press Enter to disconnect...")
	reader := bufio.NewReader(os.Stdin)
	_, _ = reader.ReadString('\n')
}

func closeBackend(b backend.Backend) {
	if err := b.Close(); err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
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
