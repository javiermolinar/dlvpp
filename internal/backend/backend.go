package backend

import (
	"context"
	"errors"
)

var ErrUnsupported = errors.New("backend operation not supported")

type LaunchMode string

const (
	LaunchModeDebug LaunchMode = "debug"
	LaunchModeTest  LaunchMode = "test"
)

type Backend interface {
	Launch(ctx context.Context, req LaunchRequest) error
	Attach(ctx context.Context, req AttachRequest) error
	Close() error

	Continue(ctx context.Context) (*StopState, error)
	Next(ctx context.Context) (*StopState, error)
	StepIn(ctx context.Context) (*StopState, error)
	StepOut(ctx context.Context) (*StopState, error)
	Pause(ctx context.Context) (*StopState, error)
	State(ctx context.Context) (*StopState, error)

	Stack(ctx context.Context, goroutineID int, depth int) ([]Frame, error)
	Locals(ctx context.Context, frame FrameRef) ([]Variable, error)
	Goroutines(ctx context.Context) ([]Goroutine, error)
	Eval(ctx context.Context, frame FrameRef, expr string) (Value, error)

	CreateBreakpoint(ctx context.Context, spec BreakpointSpec) (*Breakpoint, error)
	Breakpoints(ctx context.Context) ([]Breakpoint, error)
	ClearBreakpoint(ctx context.Context, id int) error
}

type LaunchRequest struct {
	Mode       LaunchMode
	Target     string
	Args       []string
	BuildFlags []string
	WorkDir    string
}

type AttachRequest struct {
	PID int
}

type StopReason string

const (
	StopReasonUnknown    StopReason = "unknown"
	StopReasonEntry      StopReason = "entry"
	StopReasonBreakpoint StopReason = "breakpoint"
	StopReasonStep       StopReason = "step"
	StopReasonPause      StopReason = "pause"
	StopReasonExit       StopReason = "exit"
)

type StopState struct {
	Running       bool
	Exited        bool
	ExitStatus    int
	Reason        StopReason
	Current       SourceLocation
	CurrentFrame  FrameRef
	GoroutineID   int
	ThreadID      int
	BreakpointIDs []int
}

type SourceLocation struct {
	File     string
	Line     int
	Function string
}

type FrameRef struct {
	GoroutineID int
	Index       int
}

type Frame struct {
	Ref      FrameRef
	Location SourceLocation
}

type Variable struct {
	Name        string
	Type        string
	Value       string
	HasChildren bool
}

type Value struct {
	Type  string
	Value string
}

type Goroutine struct {
	ID       int
	State    string
	Location SourceLocation
	Current  bool
}

type BreakpointSpec struct {
	Location  string
	Condition string
}

type Breakpoint struct {
	ID        int
	Location  SourceLocation
	Condition string
	Enabled   bool
}
