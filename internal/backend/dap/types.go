package dap

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"dlvpp/internal/backend"
)

type request struct {
	Seq       int    `json:"seq"`
	Type      string `json:"type"`
	Command   string `json:"command"`
	Arguments any    `json:"arguments,omitempty"`
}

type response struct {
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	RequestSeq int             `json:"request_seq,omitempty"`
	Success    bool            `json:"success,omitempty"`
	Command    string          `json:"command,omitempty"`
	Message    string          `json:"message,omitempty"`
	Event      string          `json:"event,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
}

type stoppedEventBody struct {
	Reason   string `json:"reason"`
	ThreadID int    `json:"threadId"`
}

type outputEventBody struct {
	Category string `json:"category"`
	Output   string `json:"output"`
}

type errorResponseBody struct {
	Error dapError `json:"error"`
}

type dapError struct {
	ID       int    `json:"id"`
	Format   string `json:"format"`
	ShowUser bool   `json:"showUser"`
}

type setBreakpointsBody struct {
	Breakpoints []dapBreakpoint `json:"breakpoints"`
}

type dapBreakpoint struct {
	ID       int         `json:"id"`
	Verified bool        `json:"verified"`
	Line     int         `json:"line"`
	Source   stackSource `json:"source"`
	Message  string      `json:"message"`
}

type stackTraceBody struct {
	StackFrames []stackFrame `json:"stackFrames"`
}

type stackFrame struct {
	ID     int         `json:"id"`
	Name   string      `json:"name"`
	Line   int         `json:"line"`
	Source stackSource `json:"source"`
}

type stackSource struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type scopesBody struct {
	Scopes []scope `json:"scopes"`
}

type scope struct {
	Name               string `json:"name"`
	VariablesReference int    `json:"variablesReference"`
	Expensive          bool   `json:"expensive"`
}

type variablesBody struct {
	Variables []dapVariable `json:"variables"`
}

type dapVariable struct {
	Name               string `json:"name"`
	Value              string `json:"value"`
	Type               string `json:"type"`
	VariablesReference int    `json:"variablesReference"`
}

func mapStopReason(reason string) backend.StopReason {
	switch reason {
	case "entry":
		return backend.StopReasonEntry
	case "breakpoint":
		return backend.StopReasonBreakpoint
	case "step":
		return backend.StopReasonStep
	case "pause":
		return backend.StopReasonPause
	case "exit":
		return backend.StopReasonExit
	default:
		return backend.StopReasonUnknown
	}
}

func parseFileLineLocation(location string) (string, int, bool) {
	i := strings.LastIndex(location, ":")
	if i <= 0 || i == len(location)-1 {
		return "", 0, false
	}
	line, err := strconv.Atoi(location[i+1:])
	if err != nil || line <= 0 {
		return "", 0, false
	}
	return location[:i], line, true
}

func mapBreakpoint(spec backend.BreakpointSpec, wire dapBreakpoint) *backend.Breakpoint {
	bp := &backend.Breakpoint{
		ID: wire.ID,
		Location: backend.SourceLocation{
			File: wire.Source.Path,
			Line: wire.Line,
		},
		Condition: spec.Condition,
		Enabled:   wire.Verified,
	}
	if !strings.Contains(spec.Location, ":") {
		bp.Location.Function = spec.Location
	}
	return bp
}

func filepathBase(path string) string {
	return filepath.Base(path)
}

func decodeSingleBreakpoint(body json.RawMessage, command string) (dapBreakpoint, error) {
	var payload setBreakpointsBody
	if err := json.Unmarshal(body, &payload); err != nil {
		return dapBreakpoint{}, fmt.Errorf("decode %s response: %w", command, err)
	}
	if len(payload.Breakpoints) == 0 {
		return dapBreakpoint{}, errors.New("no breakpoint returned")
	}
	bp := payload.Breakpoints[0]
	if !bp.Verified {
		return dapBreakpoint{}, fmt.Errorf("breakpoint not verified: %s", bp.Message)
	}
	return bp, nil
}
