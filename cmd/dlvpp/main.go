package main

import (
	"bufio"
	"bytes"
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
	"golang.org/x/term"
)

const (
	defaultBreakpoint    = "main.main"
	sourceContextLines   = 5
	commandActionTimeout = 15 * time.Second
)

var errQuitCommandLoop = errors.New("quit command loop")

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

	if err := runCommandLoop(signalCtx, os.Stdin, os.Stdout, controller); err != nil {
		if errors.Is(err, context.Canceled) {
			return exitCodeError{code: 130}
		}
		return err
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

	if err := runCommandLoop(signalCtx, os.Stdin, os.Stdout, controller); err != nil {
		if errors.Is(err, context.Canceled) {
			return exitCodeError{code: 130}
		}
		return err
	}
	return nil
}

type commandRunner interface {
	Do(ctx context.Context, action session.Action) (*session.Snapshot, error)
	CreateBreakpoint(ctx context.Context, spec backend.BreakpointSpec) (*backend.Breakpoint, error)
}

func runCommandLoop(ctx context.Context, input io.Reader, output io.Writer, runner commandRunner) error {
	if file, ok := input.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		return runTTYCommandLoop(ctx, file, output, runner)
	}
	return runLineCommandLoop(ctx, input, output, runner)
}

func runLineCommandLoop(ctx context.Context, input io.Reader, output io.Writer, runner commandRunner) error {
	_, _ = fmt.Fprintln(output, "Commands: n=next, :=command, q=quit")

	reader := bufio.NewReader(input)
	for {
		line, err := readCommand(ctx, reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		if err := executeCommandText(ctx, strings.TrimSpace(line), output, runner); err != nil {
			if errors.Is(err, errQuitCommandLoop) {
				return nil
			}
			if errors.Is(err, context.Canceled) {
				return err
			}
			_, _ = fmt.Fprintf(output, "%v\n", err)
		}
	}
}

func runTTYCommandLoop(ctx context.Context, input *os.File, output io.Writer, runner commandRunner) error {
	_, _ = fmt.Fprintln(output, "Commands: n=next, :=command, q=quit")

	oldState, err := term.MakeRaw(int(input.Fd()))
	if err != nil {
		return fmt.Errorf("enable raw mode: %w", err)
	}
	output = newlineWriter{Writer: output}
	defer func() {
		_ = term.Restore(int(input.Fd()), oldState)
		_, _ = fmt.Fprintln(output)
	}()

	var commandBuf []byte
	commandMode := false

	for {
		b, err := readByte(ctx, input)
		if err != nil {
			return err
		}

		if !commandMode {
			switch b {
			case 3:
				return context.Canceled
			case 4, 'q':
				return nil
			case 'n':
				if err := executeCommandText(ctx, "next", output, runner); err != nil {
					if errors.Is(err, errQuitCommandLoop) {
						return nil
					}
					if errors.Is(err, context.Canceled) {
						return err
					}
					_, _ = fmt.Fprintf(output, "%v\n", err)
				}
			case ':':
				commandMode = true
				commandBuf = commandBuf[:0]
				_, _ = fmt.Fprint(output, ":")
			case '\r', '\n':
				continue
			}
			continue
		}

		switch b {
		case 3:
			return context.Canceled
		case 27:
			commandMode = false
			commandBuf = commandBuf[:0]
			_, _ = fmt.Fprintln(output)
		case 127, 8:
			if len(commandBuf) == 0 {
				continue
			}
			commandBuf = commandBuf[:len(commandBuf)-1]
			_, _ = fmt.Fprint(output, "\b \b")
		case '\r', '\n':
			_, _ = fmt.Fprintln(output)
			commandMode = false
			if err := executeCommandText(ctx, strings.TrimSpace(string(commandBuf)), output, runner); err != nil {
				if errors.Is(err, errQuitCommandLoop) {
					return nil
				}
				if errors.Is(err, context.Canceled) {
					return err
				}
				_, _ = fmt.Fprintf(output, "%v\n", err)
			}
			commandBuf = commandBuf[:0]
		case 4:
			if len(commandBuf) == 0 {
				return nil
			}
		default:
			if !isPrintableByte(b) {
				continue
			}
			commandBuf = append(commandBuf, b)
			_, _ = fmt.Fprintf(output, "%c", b)
		}
	}
}

func executeCommandText(ctx context.Context, text string, output io.Writer, runner commandRunner) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if text == "" {
		return nil
	}
	if strings.HasPrefix(text, ":") {
		text = strings.TrimSpace(strings.TrimPrefix(text, ":"))
	}
	if text == "" {
		return nil
	}

	parts := strings.Fields(text)
	command := parts[0]
	args := parts[1:]

	switch command {
	case "q", "quit", "exit":
		return errQuitCommandLoop
	case "n", "next":
		actionCtx, cancel := context.WithTimeout(ctx, commandActionTimeout)
		defer cancel()

		snapshot, err := runner.Do(actionCtx, session.ActionNext)
		if err != nil {
			return fmt.Errorf("next: %w", err)
		}
		_, _ = fmt.Fprint(output, session.FormatSnapshot(snapshot))
		if snapshot != nil && snapshot.State.Exited {
			return errQuitCommandLoop
		}
		return nil
	case "b", "break":
		if len(args) == 0 {
			return errors.New("break requires a location")
		}
		actionCtx, cancel := context.WithTimeout(ctx, commandActionTimeout)
		defer cancel()

		bp, err := runner.CreateBreakpoint(actionCtx, backend.BreakpointSpec{Location: strings.Join(args, " ")})
		if err != nil {
			return fmt.Errorf("break: %w", err)
		}
		_, _ = fmt.Fprintln(output, formatBreakpoint(bp))
		return nil
	case "help", "h":
		_, _ = fmt.Fprintln(output, "Commands: n=next, :=command, q=quit")
		_, _ = fmt.Fprintln(output, "Command mode: :b <location>, :q")
		return nil
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}

func formatBreakpoint(bp *backend.Breakpoint) string {
	if bp == nil {
		return "breakpoint set"
	}
	if bp.Location.File != "" && bp.Location.Line > 0 {
		return fmt.Sprintf("breakpoint %d at %s:%d", bp.ID, bp.Location.File, bp.Location.Line)
	}
	if bp.Location.Function != "" {
		return fmt.Sprintf("breakpoint %d at %s", bp.ID, bp.Location.Function)
	}
	return fmt.Sprintf("breakpoint %d set", bp.ID)
}

func readCommand(ctx context.Context, reader *bufio.Reader) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	result := make(chan struct {
		line string
		err  error
	}, 1)
	go func() {
		line, err := reader.ReadString('\n')
		result <- struct {
			line string
			err  error
		}{line: line, err: err}
	}()

	select {
	case read := <-result:
		return read.line, read.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func readByte(ctx context.Context, input io.Reader) (byte, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	result := make(chan struct {
		b   byte
		err error
	}, 1)
	go func() {
		var buf [1]byte
		_, err := input.Read(buf[:])
		result <- struct {
			b   byte
			err error
		}{b: buf[0], err: err}
	}()

	select {
	case read := <-result:
		return read.b, read.err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func isPrintableByte(b byte) bool {
	return b >= 32 && b <= 126
}

type newlineWriter struct {
	io.Writer
}

func (w newlineWriter) Write(p []byte) (int, error) {
	converted := bytes.ReplaceAll(p, []byte("\n"), []byte("\r\n"))
	if _, err := w.Writer.Write(converted); err != nil {
		return 0, err
	}
	return len(p), nil
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
