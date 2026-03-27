package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"dlvpp/internal/backend"
	"dlvpp/internal/session"
	"golang.org/x/term"
)

const (
	commandActionTimeout = 15 * time.Second
	ttyCtrlC             = 3
	ttyCtrlD             = 4
	ttyEscape            = 27
	ttyBackspace         = 8
	ttyDelete            = 127
	commandLoopHelp      = "Commands: n=next, s=step in, :b <location>, q=quit"
)

var errQuitCommandLoop = errors.New("quit command loop")

type commandRunner interface {
	Do(ctx context.Context, action session.Action) (*session.Snapshot, error)
	CreateBreakpoint(ctx context.Context, spec backend.BreakpointSpec) (*backend.Breakpoint, error)
}

func runCommandLoop(ctx context.Context, input io.Reader, output io.Writer, runner commandRunner, initialSnapshot *session.Snapshot, sticky bool) error {
	state := newViewState(sticky, output, initialSnapshot)
	if file, ok := input.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		return runTTYCommandLoop(ctx, file, output, runner, state)
	}
	return runLineCommandLoop(ctx, input, output, runner, state)
}

func runLineCommandLoop(ctx context.Context, input io.Reader, output io.Writer, runner commandRunner, state *viewState) error {
	_, _ = fmt.Fprintln(output, commandLoopHelp)

	reader := bufio.NewReader(input)
	for {
		line, err := readCommand(ctx, reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		if done, err := processCommand(ctx, output, runner, state, strings.TrimSpace(line)); done || err != nil {
			return err
		}
	}
}

func runTTYCommandLoop(ctx context.Context, input *os.File, output io.Writer, runner commandRunner, state *viewState) error {
	_, _ = fmt.Fprintln(output, commandLoopHelp)

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

		switch b {
		case ttyCtrlC:
			return context.Canceled
		case ttyCtrlD:
			if !commandMode || len(commandBuf) == 0 {
				return nil
			}
		case 'q':
			if !commandMode {
				return nil
			}
		case 'n':
			if !commandMode {
				if done, err := processCommand(ctx, output, runner, state, "n"); done || err != nil {
					return err
				}
				continue
			}
		case 's':
			if !commandMode {
				if done, err := processCommand(ctx, output, runner, state, "s"); done || err != nil {
					return err
				}
				continue
			}
		case ':':
			if !commandMode {
				commandMode = true
				commandBuf = commandBuf[:0]
				_, _ = fmt.Fprint(output, ":")
				continue
			}
		case '\r', '\n':
			if !commandMode {
				continue
			}
			_, _ = fmt.Fprintln(output)
			commandMode = false
			if done, err := processCommand(ctx, output, runner, state, strings.TrimSpace(string(commandBuf))); done || err != nil {
				return err
			}
			commandBuf = commandBuf[:0]
			continue
		case ttyEscape:
			if !commandMode {
				continue
			}
			commandMode = false
			commandBuf = commandBuf[:0]
			_, _ = fmt.Fprintln(output)
			continue
		case ttyDelete, ttyBackspace:
			if !commandMode || len(commandBuf) == 0 {
				continue
			}
			commandBuf = commandBuf[:len(commandBuf)-1]
			_, _ = fmt.Fprint(output, "\b \b")
			continue
		}

		if !commandMode || !isPrintableByte(b) {
			continue
		}
		commandBuf = append(commandBuf, b)
		_, _ = fmt.Fprintf(output, "%c", b)
	}
}

func processCommand(ctx context.Context, output io.Writer, runner commandRunner, state *viewState, text string) (bool, error) {
	err := executeCommandText(ctx, text, output, runner, state)
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, errQuitCommandLoop):
		return true, nil
	case errors.Is(err, context.Canceled):
		return false, err
	default:
		_, _ = fmt.Fprintf(output, "%v\n", err)
		return false, nil
	}
}

func executeCommandText(ctx context.Context, text string, output io.Writer, runner commandRunner, state *viewState) error {
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
	case "q":
		return errQuitCommandLoop
	case "n":
		return runDebuggerAction(ctx, output, runner, state, session.ActionNext, "next")
	case "s":
		return runDebuggerAction(ctx, output, runner, state, session.ActionStepIn, "step in")
	case "b":
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
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}

func runDebuggerAction(ctx context.Context, output io.Writer, runner commandRunner, state *viewState, action session.Action, label string) error {
	actionCtx, cancel := context.WithTimeout(ctx, commandActionTimeout)
	defer cancel()

	snapshot, err := runner.Do(actionCtx, action)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	state.currentSnapshot = snapshot
	_, _ = fmt.Fprint(output, formatSnapshotForView(snapshot, state, true))
	if snapshot != nil && snapshot.State.Exited {
		return errQuitCommandLoop
	}
	return nil
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
