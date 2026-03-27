package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dlvpp/internal/backend"
)

func TestControllerSnapshotRendersTopFrame(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "main.go")
	source := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}

	frame := backend.Frame{
		Ref: backend.FrameRef{GoroutineID: 7, Index: 3},
		Location: backend.SourceLocation{
			File:     sourcePath,
			Line:     3,
			Function: "main.main",
		},
	}

	fake := &fakeBackend{
		state: backend.StopState{ThreadID: 7, GoroutineID: 7},
		stack: []backend.Frame{frame},
	}
	controller := New(fake, Options{SourceContextLines: 1})

	snapshot, err := controller.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.Frame == nil {
		t.Fatal("expected top frame")
	}
	if snapshot.State.Current.File != sourcePath || snapshot.State.Current.Line != 3 {
		t.Fatalf("unexpected current location: %+v", snapshot.State.Current)
	}
	if snapshot.SourceError != nil {
		t.Fatalf("unexpected source error: %v", snapshot.SourceError)
	}
	if !strings.Contains(snapshot.Source, "println") {
		t.Fatalf("expected rendered source to include source line, got %q", snapshot.Source)
	}

	formatted := FormatSnapshot(snapshot)
	if !strings.Contains(formatted, "stopped: main.main") {
		t.Fatalf("expected formatted snapshot to include stop line, got %q", formatted)
	}
}

func TestControllerStartLaunchSessionBootstrapsBreakpointAndContinue(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "main.go")
	source := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}

	fake := &fakeBackend{
		state: backend.StopState{ThreadID: 1, GoroutineID: 1},
		stack: []backend.Frame{{
			Ref: backend.FrameRef{GoroutineID: 1, Index: 1},
			Location: backend.SourceLocation{
				File:     sourcePath,
				Line:     3,
				Function: "main.main",
			},
		}},
		breakpoint: &backend.Breakpoint{
			ID: 1,
			Location: backend.SourceLocation{
				File:     sourcePath,
				Line:     3,
				Function: "main.main",
			},
			Enabled: true,
		},
	}
	controller := New(fake, Options{SourceContextLines: 1})

	result, err := controller.StartLaunchSession(context.Background(), backend.LaunchRequest{Target: "./examples/hello"}, backend.BreakpointSpec{Location: "main.main"})
	if err != nil {
		t.Fatalf("start launch session: %v", err)
	}
	if !fake.launchCalled {
		t.Fatal("expected Launch to be called")
	}
	if !fake.createBreakpointCalled {
		t.Fatal("expected CreateBreakpoint to be called")
	}
	if !fake.continueCalled {
		t.Fatal("expected Continue to be called")
	}
	if fake.closeCalls != 0 {
		t.Fatalf("expected backend to remain open on success, got %d close calls", fake.closeCalls)
	}
	if result == nil || result.Breakpoint == nil || result.Snapshot == nil || result.Snapshot.Frame == nil {
		t.Fatal("expected bootstrapped launch result")
	}
}

func TestControllerLaunchClosesBackendWhenStateRefreshFails(t *testing.T) {
	fake := &fakeBackend{
		stateErr: errors.New("state failed"),
	}
	controller := New(fake, Options{})

	_, err := controller.Launch(context.Background(), backend.LaunchRequest{Target: "./examples/hello"})
	if err == nil {
		t.Fatal("expected launch error")
	}
	if fake.closeCalls != 1 {
		t.Fatalf("expected backend to close once, got %d", fake.closeCalls)
	}
}

func TestControllerLaunchKeepsSessionOpenWhenStackRefreshFails(t *testing.T) {
	fake := &fakeBackend{
		state:    backend.StopState{ThreadID: 1, GoroutineID: 1},
		stackErr: errors.New("stack failed"),
	}
	controller := New(fake, Options{})

	snapshot, err := controller.Launch(context.Background(), backend.LaunchRequest{Target: "./examples/hello"})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if fake.closeCalls != 0 {
		t.Fatalf("expected backend to remain open, got %d close calls", fake.closeCalls)
	}
	if snapshot == nil || snapshot.StackError == nil {
		t.Fatal("expected snapshot stack error")
	}
	formatted := FormatSnapshot(snapshot)
	if !strings.Contains(formatted, "stack: stack failed") {
		t.Fatalf("expected formatted stack error, got %q", formatted)
	}
}

func TestControllerStartLaunchSessionClosesBackendOnBreakpointError(t *testing.T) {
	fake := &fakeBackend{
		state:               backend.StopState{ThreadID: 1, GoroutineID: 1},
		stack:               []backend.Frame{},
		createBreakpointErr: errors.New("breakpoint failed"),
	}
	controller := New(fake, Options{})

	_, err := controller.StartLaunchSession(context.Background(), backend.LaunchRequest{Target: "./examples/hello"}, backend.BreakpointSpec{Location: "main.main"})
	if err == nil {
		t.Fatal("expected start launch session error")
	}
	if fake.closeCalls != 1 {
		t.Fatalf("expected backend to close once, got %d", fake.closeCalls)
	}
}

func TestControllerStartLaunchSessionClosesBackendOnContinueError(t *testing.T) {
	fake := &fakeBackend{
		state: backend.StopState{ThreadID: 1, GoroutineID: 1},
		stack: []backend.Frame{},
		breakpoint: &backend.Breakpoint{
			ID:      1,
			Enabled: true,
		},
		continueErr: errors.New("continue failed"),
	}
	controller := New(fake, Options{})

	_, err := controller.StartLaunchSession(context.Background(), backend.LaunchRequest{Target: "./examples/hello"}, backend.BreakpointSpec{Location: "main.main"})
	if err == nil {
		t.Fatal("expected start launch session error")
	}
	if fake.closeCalls != 1 {
		t.Fatalf("expected backend to close once, got %d", fake.closeCalls)
	}
}

func TestControllerStartAttachSessionClosesBackendWhenAttachRefreshFails(t *testing.T) {
	fake := &fakeBackend{
		stateErr: errors.New("state failed"),
	}
	controller := New(fake, Options{})

	_, err := controller.StartAttachSession(context.Background(), backend.AttachRequest{PID: 123})
	if err == nil {
		t.Fatal("expected start attach session error")
	}
	if fake.closeCalls != 1 {
		t.Fatalf("expected backend to close once, got %d", fake.closeCalls)
	}
}

type fakeBackend struct {
	state                  backend.StopState
	stack                  []backend.Frame
	breakpoint             *backend.Breakpoint
	launchErr              error
	attachErr              error
	continueErr            error
	stateErr               error
	stackErr               error
	createBreakpointErr    error
	launchCalled           bool
	attachCalled           bool
	continueCalled         bool
	createBreakpointCalled bool
	closeCalls             int
}

func (f *fakeBackend) Launch(context.Context, backend.LaunchRequest) error {
	f.launchCalled = true
	return f.launchErr
}

func (f *fakeBackend) Attach(context.Context, backend.AttachRequest) error {
	f.attachCalled = true
	return f.attachErr
}

func (f *fakeBackend) Close() error {
	f.closeCalls++
	return nil
}

func (f *fakeBackend) Continue(context.Context) (*backend.StopState, error) {
	f.continueCalled = true
	if f.continueErr != nil {
		return nil, f.continueErr
	}
	return &f.state, nil
}

func (f *fakeBackend) Next(context.Context) (*backend.StopState, error) {
	return &f.state, nil
}

func (f *fakeBackend) StepIn(context.Context) (*backend.StopState, error) {
	return &f.state, nil
}

func (f *fakeBackend) StepOut(context.Context) (*backend.StopState, error) {
	return &f.state, nil
}

func (f *fakeBackend) Pause(context.Context) (*backend.StopState, error) {
	return &f.state, nil
}

func (f *fakeBackend) State(context.Context) (*backend.StopState, error) {
	if f.stateErr != nil {
		return nil, f.stateErr
	}
	return &f.state, nil
}

func (f *fakeBackend) Stack(context.Context, int, int) ([]backend.Frame, error) {
	if f.stackErr != nil {
		return nil, f.stackErr
	}
	return f.stack, nil
}

func (f *fakeBackend) Locals(context.Context, backend.FrameRef) ([]backend.Variable, error) {
	return nil, backend.ErrUnsupported
}

func (f *fakeBackend) Goroutines(context.Context) ([]backend.Goroutine, error) {
	return nil, backend.ErrUnsupported
}

func (f *fakeBackend) Eval(context.Context, backend.FrameRef, string) (backend.Value, error) {
	return backend.Value{}, backend.ErrUnsupported
}

func (f *fakeBackend) CreateBreakpoint(context.Context, backend.BreakpointSpec) (*backend.Breakpoint, error) {
	f.createBreakpointCalled = true
	if f.createBreakpointErr != nil {
		return nil, f.createBreakpointErr
	}
	if f.breakpoint == nil {
		return nil, backend.ErrUnsupported
	}
	return f.breakpoint, nil
}

func (f *fakeBackend) Breakpoints(context.Context) ([]backend.Breakpoint, error) {
	return nil, backend.ErrUnsupported
}

func (f *fakeBackend) ClearBreakpoint(context.Context, int) error {
	return backend.ErrUnsupported
}
