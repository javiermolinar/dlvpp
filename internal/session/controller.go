package session

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"dlvpp/internal/backend"
	"dlvpp/internal/sourceview"
)

const (
	defaultSourceContextLines = 5
	ansiReset                 = "\x1b[0m"
	ansiCyan                  = "\x1b[36m"
)

var ErrUnknownAction = errors.New("unknown action")

type Action string

const (
	ActionContinue Action = "continue"
	ActionNext     Action = "next"
	ActionStepIn   Action = "step-in"
	ActionStepOut  Action = "step-out"
	ActionPause    Action = "pause"
)

type Options struct {
	SourceContextLines int
}

type Controller struct {
	backend            backend.Backend
	sourceContextLines int
	lastSnapshot       *Snapshot
}

type Snapshot struct {
	State       backend.StopState
	Frame       *backend.Frame
	StackError  error
	Source      string
	SourceError error
}

type StartResult struct {
	Breakpoint *backend.Breakpoint
	Snapshot   *Snapshot
}

func New(b backend.Backend, opts Options) *Controller {
	sourceContextLines := opts.SourceContextLines
	if sourceContextLines <= 0 {
		sourceContextLines = defaultSourceContextLines
	}

	return &Controller{
		backend:            b,
		sourceContextLines: sourceContextLines,
	}
}

func (c *Controller) Launch(ctx context.Context, req backend.LaunchRequest) (*Snapshot, error) {
	if err := c.backend.Launch(ctx, req); err != nil {
		return nil, err
	}
	return c.refreshSnapshotOrClose(ctx)
}

func (c *Controller) Attach(ctx context.Context, req backend.AttachRequest) (*Snapshot, error) {
	if err := c.backend.Attach(ctx, req); err != nil {
		return nil, err
	}
	return c.refreshSnapshotOrClose(ctx)
}

func (c *Controller) CreateBreakpoint(ctx context.Context, spec backend.BreakpointSpec) (*backend.Breakpoint, error) {
	return c.backend.CreateBreakpoint(ctx, spec)
}

func (c *Controller) Breakpoints(ctx context.Context) ([]backend.Breakpoint, error) {
	return c.backend.Breakpoints(ctx)
}

func (c *Controller) Locals(ctx context.Context, frame backend.FrameRef) ([]backend.Variable, error) {
	return c.backend.Locals(ctx, frame)
}

func (c *Controller) Children(ctx context.Context, reference int) ([]backend.Variable, error) {
	return c.backend.Children(ctx, reference)
}

func (c *Controller) Eval(ctx context.Context, frame backend.FrameRef, expr string) (backend.Value, error) {
	return c.backend.Eval(ctx, frame, expr)
}

func (c *Controller) Output(ctx context.Context) ([]backend.OutputEntry, error) {
	return c.backend.Output(ctx)
}

func (c *Controller) StartLaunchSession(ctx context.Context, req backend.LaunchRequest, spec backend.BreakpointSpec) (*StartResult, error) {
	if _, err := c.Launch(ctx, req); err != nil {
		return nil, err
	}

	bp, err := c.CreateBreakpoint(ctx, spec)
	if err != nil {
		return nil, c.closeOnBootstrapError(err)
	}

	snapshot, err := c.Continue(ctx)
	if err != nil {
		return nil, c.closeOnBootstrapError(err)
	}

	return &StartResult{
		Breakpoint: bp,
		Snapshot:   snapshot,
	}, nil
}

func (c *Controller) StartAttachSession(ctx context.Context, req backend.AttachRequest) (*StartResult, error) {
	snapshot, err := c.Attach(ctx, req)
	if err != nil {
		return nil, err
	}

	return &StartResult{Snapshot: snapshot}, nil
}

func (c *Controller) Snapshot(ctx context.Context) (*Snapshot, error) {
	return c.refreshSnapshot(ctx)
}

func (c *Controller) LastSnapshot() *Snapshot {
	return c.lastSnapshot
}

func (c *Controller) Do(ctx context.Context, action Action) (*Snapshot, error) {
	switch action {
	case ActionContinue:
		return c.Continue(ctx)
	case ActionNext:
		return c.Next(ctx)
	case ActionStepIn:
		return c.StepIn(ctx)
	case ActionStepOut:
		return c.StepOut(ctx)
	case ActionPause:
		return c.Pause(ctx)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownAction, action)
	}
}

func (c *Controller) Continue(ctx context.Context) (*Snapshot, error) {
	if _, err := c.backend.Continue(ctx); err != nil {
		return nil, err
	}
	return c.refreshSnapshot(ctx)
}

func (c *Controller) Next(ctx context.Context) (*Snapshot, error) {
	if _, err := c.backend.Next(ctx); err != nil {
		return nil, err
	}
	return c.refreshSnapshot(ctx)
}

func (c *Controller) StepIn(ctx context.Context) (*Snapshot, error) {
	if _, err := c.backend.StepIn(ctx); err != nil {
		return nil, err
	}
	return c.refreshSnapshot(ctx)
}

func (c *Controller) StepOut(ctx context.Context) (*Snapshot, error) {
	if _, err := c.backend.StepOut(ctx); err != nil {
		return nil, err
	}
	return c.refreshSnapshot(ctx)
}

func (c *Controller) Pause(ctx context.Context) (*Snapshot, error) {
	if _, err := c.backend.Pause(ctx); err != nil {
		return nil, err
	}
	return c.refreshSnapshot(ctx)
}

func (c *Controller) Close() error {
	return c.backend.Close()
}

func (c *Controller) closeOnBootstrapError(err error) error {
	if closeErr := c.backend.Close(); closeErr != nil {
		return errors.Join(err, closeErr)
	}
	return err
}

func (c *Controller) refreshSnapshotOrClose(ctx context.Context) (*Snapshot, error) {
	snapshot, err := c.refreshSnapshot(ctx)
	if err != nil {
		return nil, c.closeOnBootstrapError(err)
	}
	return snapshot, nil
}

func (c *Controller) refreshSnapshot(ctx context.Context) (*Snapshot, error) {
	state, err := c.backend.State(ctx)
	if err != nil {
		return nil, err
	}

	snapshot := &Snapshot{State: *state}
	if snapshot.State.Running || snapshot.State.Exited {
		c.lastSnapshot = snapshot
		return snapshot, nil
	}

	frames, err := c.backend.Stack(ctx, snapshot.State.ThreadID, 1)
	if err != nil {
		snapshot.StackError = fmt.Errorf("stack: %w", err)
		c.lastSnapshot = snapshot
		return snapshot, nil
	}
	if len(frames) == 0 {
		c.lastSnapshot = snapshot
		return snapshot, nil
	}

	frame := frames[0]
	snapshot.Frame = &frame
	snapshot.State.Current = frame.Location
	snapshot.State.CurrentFrame = frame.Ref

	rendered, err := sourceview.RenderWindow(frame.Location.File, frame.Location.Line, c.sourceContextLines)
	if err != nil {
		snapshot.SourceError = err
	} else {
		snapshot.Source = rendered
	}

	c.lastSnapshot = snapshot
	return snapshot, nil
}

func FormatSnapshot(snapshot *Snapshot) string {
	if snapshot == nil {
		return ""
	}

	var out strings.Builder
	switch {
	case snapshot.State.Exited:
		fmt.Fprintf(&out, "exited with status %d\n", snapshot.State.ExitStatus)
	case snapshot.State.Running:
		out.WriteString("running\n")
	case snapshot.Frame == nil:
		if snapshot.StackError != nil {
			fmt.Fprintf(&out, "%v\n", snapshot.StackError)
		} else {
			out.WriteString("stopped, but no stack frames available\n")
		}
	default:
		fmt.Fprintf(
			&out,
			"stopped: %s at %s%s:%d%s\n",
			snapshot.Frame.Location.Function,
			ansiCyan,
			snapshot.Frame.Location.File,
			snapshot.Frame.Location.Line,
			ansiReset,
		)
		if snapshot.Source != "" {
			out.WriteString(snapshot.Source)
		}
		if snapshot.SourceError != nil {
			fmt.Fprintf(&out, "%v\n", snapshot.SourceError)
		}
	}

	return out.String()
}
