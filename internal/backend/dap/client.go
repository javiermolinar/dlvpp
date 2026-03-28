package dap

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"dlvpp/internal/backend"
)

var _ backend.Backend = (*Client)(nil)

// Client wraps a single DAP connection to Delve.
//
// This implementation is intentionally minimal: requests, responses, and events
// all share one stream and are handled synchronously. The mutex serializes access
// to that stream and to the client's mutable session state.
type Client struct {
	mu sync.Mutex

	cmd    *exec.Cmd
	cancel context.CancelFunc
	conn   net.Conn
	reader *bufio.Reader

	seq int

	serverAddr        string
	terminateDebuggee bool
	stopState         *backend.StopState
	stderr            bytes.Buffer
	stdout            bytes.Buffer
	debugOutput       bytes.Buffer
}

func New() *Client {
	return &Client{}
}

func (c *Client) Launch(ctx context.Context, req backend.LaunchRequest) error {
	if req.Target == "" {
		return errors.New("launch target is required")
	}

	mode := string(req.Mode)
	if mode == "" {
		mode = string(backend.LaunchModeDebug)
	}

	args := map[string]any{
		"request":     "launch",
		"mode":        mode,
		"program":     req.Target,
		"stopOnEntry": true,
	}
	if len(req.Args) > 0 {
		args["args"] = req.Args
	}
	if len(req.BuildFlags) > 0 {
		args["buildFlags"] = req.BuildFlags
	}
	if req.WorkDir != "" {
		args["cwd"] = req.WorkDir
		args["dlvCwd"] = req.WorkDir
	}

	return c.openSession(ctx, req.WorkDir, true, "launch", args)
}

func (c *Client) Attach(ctx context.Context, req backend.AttachRequest) error {
	if req.PID <= 0 {
		return errors.New("attach pid must be > 0")
	}

	args := map[string]any{
		"request":     "attach",
		"mode":        "local",
		"processId":   req.PID,
		"stopOnEntry": true,
	}

	return c.openSession(ctx, "", false, "attach", args)
}

func (c *Client) openSession(ctx context.Context, workDir string, terminateDebuggee bool, command string, arguments map[string]any) error {
	if err := c.start(ctx, workDir); err != nil {
		return err
	}
	c.terminateDebuggee = terminateDebuggee

	if err := c.initialize(ctx); err != nil {
		_ = c.Close()
		return err
	}
	if _, err := c.request(ctx, command, arguments); err != nil {
		_ = c.Close()
		return err
	}
	if _, err := c.request(ctx, "configurationDone", nil); err != nil {
		_ = c.Close()
		return err
	}
	if err := c.waitForStop(ctx); err != nil {
		_ = c.Close()
		return err
	}
	return nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var errs []error

	if c.conn != nil {
		disconnectCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := c.requestLocked(disconnectCtx, "disconnect", map[string]any{
			"terminateDebuggee": c.terminateDebuggee,
		})
		cancel()
		if err != nil && !isConnectionClose(err) {
			errs = append(errs, err)
		}
		if err := c.conn.Close(); err != nil && !isConnectionClose(err) {
			errs = append(errs, err)
		}
		c.conn = nil
		c.reader = nil
	}

	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}

	if c.cmd != nil {
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		if err := c.cmd.Wait(); err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				errs = append(errs, err)
			}
		}
		c.cmd = nil
	}

	c.seq = 0
	c.serverAddr = ""
	c.stopState = nil
	c.stderr.Reset()
	c.stdout.Reset()
	c.debugOutput.Reset()

	return errors.Join(errs...)
}

func (c *Client) runThreadAction(ctx context.Context, command string) (*backend.StopState, error) {
	threadID, err := func() (int, error) {
		c.mu.Lock()
		defer c.mu.Unlock()

		if c.conn == nil {
			return 0, errors.New("dap client is not connected")
		}

		threadID := 1
		if c.stopState != nil && c.stopState.ThreadID > 0 {
			threadID = c.stopState.ThreadID
		}
		c.stopState = &backend.StopState{Running: true, ThreadID: threadID, GoroutineID: threadID}
		return threadID, nil
	}()
	if err != nil {
		return nil, err
	}

	if _, err := c.request(ctx, command, map[string]any{"threadId": threadID}); err != nil {
		return nil, err
	}
	if err := c.waitForStop(ctx); err != nil {
		return nil, err
	}
	return c.State(ctx)
}

func (c *Client) Continue(ctx context.Context) (*backend.StopState, error) {
	return c.runThreadAction(ctx, "continue")
}

func (c *Client) Next(ctx context.Context) (*backend.StopState, error) {
	return c.runThreadAction(ctx, "next")
}

func (c *Client) StepIn(ctx context.Context) (*backend.StopState, error) {
	return c.runThreadAction(ctx, "stepIn")
}

func (c *Client) StepOut(ctx context.Context) (*backend.StopState, error) {
	return nil, backend.ErrUnsupported
}

func (c *Client) Pause(ctx context.Context) (*backend.StopState, error) {
	return nil, backend.ErrUnsupported
}

func (c *Client) State(ctx context.Context) (*backend.StopState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, errors.New("dap client is not connected")
	}
	if c.stopState == nil {
		return nil, errors.New("debuggee is not stopped")
	}

	state := *c.stopState
	return &state, nil
}

func (c *Client) Stack(ctx context.Context, goroutineID int, depth int) ([]backend.Frame, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, errors.New("dap client is not connected")
	}

	threadID := goroutineID
	if threadID <= 0 && c.stopState != nil {
		threadID = c.stopState.ThreadID
	}
	if threadID <= 0 {
		return nil, errors.New("no current thread id available")
	}

	args := map[string]any{"threadId": threadID}
	if depth > 0 {
		args["levels"] = depth
	}

	resp, err := c.requestLocked(ctx, "stackTrace", args)
	if err != nil {
		return nil, err
	}

	var body stackTraceBody
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		return nil, fmt.Errorf("decode stackTrace response: %w", err)
	}

	frames := make([]backend.Frame, 0, len(body.StackFrames))
	for _, frame := range body.StackFrames {
		frames = append(frames, backend.Frame{
			Ref: backend.FrameRef{
				GoroutineID: threadID,
				Index:       frame.ID,
			},
			Location: backend.SourceLocation{
				File:     frame.Source.Path,
				Line:     frame.Line,
				Function: frame.Name,
			},
		})
	}
	return frames, nil
}

func (c *Client) Locals(ctx context.Context, frame backend.FrameRef) ([]backend.Variable, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, errors.New("dap client is not connected")
	}
	if frame.Index <= 0 {
		return nil, errors.New("frame id is required")
	}

	resp, err := c.requestLocked(ctx, "scopes", map[string]any{"frameId": frame.Index})
	if err != nil {
		return nil, err
	}

	var scopes scopesBody
	if err := json.Unmarshal(resp.Body, &scopes); err != nil {
		return nil, fmt.Errorf("decode scopes response: %w", err)
	}

	selected := selectLocalScopes(scopes.Scopes)
	if len(selected) == 0 {
		return nil, nil
	}

	var locals []backend.Variable
	for _, scope := range selected {
		resp, err := c.requestLocked(ctx, "variables", map[string]any{"variablesReference": scope.VariablesReference})
		if err != nil {
			return nil, err
		}

		var body variablesBody
		if err := json.Unmarshal(resp.Body, &body); err != nil {
			return nil, fmt.Errorf("decode variables response: %w", err)
		}

		for _, variable := range body.Variables {
			locals = append(locals, backend.Variable{
				Name:        variable.Name,
				Type:        variable.Type,
				Value:       variable.Value,
				HasChildren: variable.VariablesReference > 0,
			})
		}
	}

	return locals, nil
}

func (c *Client) Goroutines(ctx context.Context) ([]backend.Goroutine, error) {
	return nil, backend.ErrUnsupported
}

func selectLocalScopes(scopes []scope) []scope {
	var selected []scope
	for _, scope := range scopes {
		switch strings.ToLower(scope.Name) {
		case "arguments", "locals":
			selected = append(selected, scope)
		}
	}
	if len(selected) > 0 {
		return selected
	}
	for _, scope := range scopes {
		if !scope.Expensive {
			selected = append(selected, scope)
		}
	}
	return selected
}

func (c *Client) Eval(ctx context.Context, frame backend.FrameRef, expr string) (backend.Value, error) {
	return backend.Value{}, backend.ErrUnsupported
}

func (c *Client) CreateBreakpoint(ctx context.Context, spec backend.BreakpointSpec) (*backend.Breakpoint, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, errors.New("dap client is not connected")
	}
	if spec.Location == "" {
		return nil, errors.New("breakpoint location is required")
	}

	if file, line, ok := parseFileLineLocation(spec.Location); ok {
		if !filepath.IsAbs(file) {
			absFile, err := filepath.Abs(file)
			if err != nil {
				return nil, fmt.Errorf("resolve breakpoint path %q: %w", file, err)
			}
			file = absFile
		}
		return c.createBreakpointLocked(ctx, spec, "setBreakpoints", map[string]any{
			"source": map[string]any{
				"name": filepathBase(file),
				"path": file,
			},
			"breakpoints": []map[string]any{{
				"line": line,
			}},
		})
	}

	return c.createBreakpointLocked(ctx, spec, "setFunctionBreakpoints", map[string]any{
		"breakpoints": []map[string]any{{
			"name": spec.Location,
		}},
	})
}

func (c *Client) createBreakpointLocked(ctx context.Context, spec backend.BreakpointSpec, command string, arguments map[string]any) (*backend.Breakpoint, error) {
	resp, err := c.requestLocked(ctx, command, arguments)
	if err != nil {
		return nil, err
	}

	wire, err := decodeSingleBreakpoint(resp.Body, command)
	if err != nil {
		return nil, err
	}
	return mapBreakpoint(spec, wire), nil
}

func (c *Client) Breakpoints(ctx context.Context) ([]backend.Breakpoint, error) {
	return nil, backend.ErrUnsupported
}

func (c *Client) ClearBreakpoint(ctx context.Context, id int) error {
	return backend.ErrUnsupported
}

func (c *Client) start(ctx context.Context, workDir string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil || c.cmd != nil {
		return errors.New("dap client already started")
	}

	dlvPath, err := exec.LookPath("dlv")
	if err != nil {
		return fmt.Errorf("dlv not found in PATH: %w", err)
	}

	addr, err := reserveTCPAddr()
	if err != nil {
		return err
	}

	serverCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(serverCtx, dlvPath, "dap", "--listen", addr)
	if workDir != "" {
		cmd.Dir = workDir
	}
	cmd.Stdout = &c.stdout
	cmd.Stderr = &c.stderr

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start dlv dap: %w", err)
	}

	conn, err := dialRetry(ctx, addr)
	if err != nil {
		cancel()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("connect to dlv dap at %s: %w\n%s", addr, err, c.stderr.String())
	}

	c.cmd = cmd
	c.cancel = cancel
	c.conn = conn
	c.reader = bufio.NewReader(conn)
	c.serverAddr = addr
	return nil
}

func (c *Client) initialize(ctx context.Context) error {
	_, err := c.request(ctx, "initialize", map[string]any{
		"adapterID":                    "go",
		"pathFormat":                   "path",
		"linesStartAt1":                true,
		"columnsStartAt1":              true,
		"supportsVariableType":         true,
		"supportsVariablePaging":       true,
		"supportsRunInTerminalRequest": false,
		"supportsMemoryReferences":     true,
		"locale":                       "en-us",
	})
	return err
}

func (c *Client) request(ctx context.Context, command string, arguments any) (*response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requestLocked(ctx, command, arguments)
}

func (c *Client) requestLocked(ctx context.Context, command string, arguments any) (*response, error) {
	if c.conn == nil {
		return nil, errors.New("dap client is not connected")
	}

	debugOutputStart := c.debugOutput.Len()
	seq := c.nextSeqLocked()
	if err := c.writeRequestLocked(seq, command, arguments); err != nil {
		return nil, err
	}

	for {
		msg, err := c.readMessageLocked(ctx)
		if err != nil {
			return nil, err
		}
		if msg.Type == "event" {
			if err := c.handleEventLocked(msg); err != nil {
				return nil, err
			}
			continue
		}
		if msg.Type != "response" || msg.RequestSeq != seq {
			continue
		}
		if !msg.Success {
			return nil, c.requestErrorLocked(command, msg, debugOutputStart)
		}
		return msg, nil
	}
}

func (c *Client) requestErrorLocked(command string, msg *response, debugOutputStart int) error {
	details := make([]string, 0, 2)
	if message := strings.TrimSpace(msg.Message); message != "" {
		details = append(details, message)
	}

	var body errorResponseBody
	if len(msg.Body) > 0 && json.Unmarshal(msg.Body, &body) == nil {
		if formatted := strings.TrimSpace(body.Error.Format); formatted != "" && !containsString(details, formatted) {
			details = append(details, formatted)
		}
	}

	if output := strings.TrimSpace(debugOutputSince(&c.debugOutput, debugOutputStart)); output != "" {
		details = append(details, output)
	}

	if len(details) == 0 {
		return fmt.Errorf("dap %s failed", command)
	}
	return fmt.Errorf("dap %s failed: %s", command, strings.Join(details, "\n"))
}

func debugOutputSince(buf *bytes.Buffer, start int) string {
	if start < 0 {
		start = 0
	}
	bytes := buf.Bytes()
	if start >= len(bytes) {
		return ""
	}
	return string(bytes[start:])
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func (c *Client) waitForStop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stopState != nil && !c.stopState.Running {
		return nil
	}

	for {
		msg, err := c.readMessageLocked(ctx)
		if err != nil {
			return err
		}
		if msg.Type != "event" {
			continue
		}
		if err := c.handleEventLocked(msg); err != nil {
			return err
		}
		if c.stopState != nil && !c.stopState.Running {
			return nil
		}
	}
}

func (c *Client) handleEventLocked(msg *response) error {
	switch msg.Event {
	case "stopped":
		var body stoppedEventBody
		if err := json.Unmarshal(msg.Body, &body); err != nil {
			return fmt.Errorf("decode stopped event: %w", err)
		}
		c.stopState = &backend.StopState{
			Running:     false,
			Exited:      false,
			Reason:      mapStopReason(body.Reason),
			ThreadID:    body.ThreadID,
			GoroutineID: body.ThreadID,
		}
	case "continued":
		threadID := 0
		if c.stopState != nil {
			threadID = c.stopState.ThreadID
		}
		c.stopState = &backend.StopState{
			Running:     true,
			Exited:      false,
			Reason:      backend.StopReasonUnknown,
			ThreadID:    threadID,
			GoroutineID: threadID,
		}
	case "terminated":
		c.stopState = &backend.StopState{
			Running: false,
			Exited:  true,
			Reason:  backend.StopReasonExit,
		}
	case "output":
		var body outputEventBody
		if err := json.Unmarshal(msg.Body, &body); err != nil {
			return fmt.Errorf("decode output event: %w", err)
		}
		_, _ = c.debugOutput.WriteString(body.Output)
	}
	return nil
}

func (c *Client) nextSeqLocked() int {
	c.seq++
	return c.seq
}

func (c *Client) writeRequestLocked(seq int, command string, arguments any) error {
	msg := request{
		Seq:       seq,
		Type:      "request",
		Command:   command,
		Arguments: arguments,
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal dap request %q: %w", command, err)
	}

	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload))
	if _, err := io.WriteString(c.conn, frame); err != nil {
		return fmt.Errorf("write dap header: %w", err)
	}
	if _, err := c.conn.Write(payload); err != nil {
		return fmt.Errorf("write dap payload: %w", err)
	}
	return nil
}

func (c *Client) readMessageLocked(ctx context.Context) (*response, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetReadDeadline(deadline)
	} else {
		_ = c.conn.SetReadDeadline(time.Time{})
	}

	length, err := readContentLength(c.reader)
	if err != nil {
		return nil, err
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return nil, fmt.Errorf("read dap payload: %w", err)
	}

	var msg response
	if err := json.Unmarshal(payload, &msg); err != nil {
		return nil, fmt.Errorf("decode dap message: %w", err)
	}
	return &msg, nil
}

func readContentLength(r *bufio.Reader) (int, error) {
	contentLength := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return 0, fmt.Errorf("read dap header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(key), "Content-Length") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return 0, fmt.Errorf("invalid content length %q: %w", value, err)
		}
		contentLength = n
	}
	if contentLength < 0 {
		return 0, errors.New("missing Content-Length header")
	}
	return contentLength, nil
}

func reserveTCPAddr() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("reserve tcp address: %w", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		return "", fmt.Errorf("release reserved tcp address: %w", err)
	}
	return addr, nil
}

func dialRetry(ctx context.Context, addr string) (net.Conn, error) {
	var lastErr error
	for {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		if err == nil {
			return conn, nil
		}
		lastErr = err

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("dial timeout: %w", errors.Join(lastErr, ctx.Err()))
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func isConnectionClose(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF)
}
