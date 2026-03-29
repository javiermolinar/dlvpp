package dap

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dlvpp/internal/backend"
)

func TestCreateBreakpointKeepsExistingFileBreakpointsInDAPRequest(t *testing.T) {
	t.Parallel()

	sourcePath := filepath.Join(t.TempDir(), "main.go")
	conn := newFakeDAPConn(
		mustDAPResponse(t, 1, setBreakpointsBody{Breakpoints: []dapBreakpoint{{ID: 1, Verified: true, Line: 10, Source: stackSource{Path: sourcePath}}}}),
		mustDAPResponse(t, 2, setBreakpointsBody{Breakpoints: []dapBreakpoint{{ID: 5, Verified: true, Line: 10, Source: stackSource{Path: sourcePath}}, {ID: 6, Verified: true, Line: 20, Source: stackSource{Path: sourcePath}}}}),
	)

	client := New()
	client.conn = conn
	client.reader = bufio.NewReader(conn)

	if _, err := client.CreateBreakpoint(context.TODO(), backend.BreakpointSpec{Location: sourcePath + ":10"}); err != nil {
		t.Fatalf("first CreateBreakpoint returned error: %v", err)
	}
	bp, err := client.CreateBreakpoint(context.TODO(), backend.BreakpointSpec{Location: sourcePath + ":20"})
	if err != nil {
		t.Fatalf("second CreateBreakpoint returned error: %v", err)
	}
	if bp == nil || bp.ID != 6 || bp.Location.Line != 20 {
		t.Fatalf("unexpected returned breakpoint: %#v", bp)
	}

	all, err := client.Breakpoints(context.TODO())
	if err != nil {
		t.Fatalf("Breakpoints returned error: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 breakpoints, got %#v", all)
	}
	if all[0].ID != 5 || all[0].Location.Line != 10 || all[1].ID != 6 || all[1].Location.Line != 20 {
		t.Fatalf("expected refreshed breakpoint ids and lines, got %#v", all)
	}

	requests := decodeCapturedRequests(t, conn.writes.Bytes())
	if len(requests) != 2 {
		t.Fatalf("expected 2 requests, got %#v", requests)
	}
	if got := requests[1].Command; got != "setBreakpoints" {
		t.Fatalf("expected second command to be setBreakpoints, got %q", got)
	}
	args, ok := requests[1].Arguments.(map[string]any)
	if !ok {
		t.Fatalf("expected second request arguments map, got %#v", requests[1].Arguments)
	}
	entries, ok := args["breakpoints"].([]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("expected second request to contain 2 breakpoints, got %#v", args["breakpoints"])
	}
	assertSourceBreakpointLine(t, entries[0], 10)
	assertSourceBreakpointLine(t, entries[1], 20)
}

func TestCreateBreakpointKeepsExistingFunctionBreakpointsInDAPRequest(t *testing.T) {
	t.Parallel()

	conn := newFakeDAPConn(
		mustDAPResponse(t, 1, setBreakpointsBody{Breakpoints: []dapBreakpoint{{ID: 1, Verified: true}}}),
		mustDAPResponse(t, 2, setBreakpointsBody{Breakpoints: []dapBreakpoint{{ID: 7, Verified: true}, {ID: 8, Verified: true}}}),
	)

	client := New()
	client.conn = conn
	client.reader = bufio.NewReader(conn)

	if _, err := client.CreateBreakpoint(context.TODO(), backend.BreakpointSpec{Location: "main.main"}); err != nil {
		t.Fatalf("first CreateBreakpoint returned error: %v", err)
	}
	bp, err := client.CreateBreakpoint(context.TODO(), backend.BreakpointSpec{Location: "main.add"})
	if err != nil {
		t.Fatalf("second CreateBreakpoint returned error: %v", err)
	}
	if bp == nil || bp.ID != 8 || bp.Location.Function != "main.add" {
		t.Fatalf("unexpected returned breakpoint: %#v", bp)
	}

	all, err := client.Breakpoints(context.TODO())
	if err != nil {
		t.Fatalf("Breakpoints returned error: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 breakpoints, got %#v", all)
	}
	if all[0].Location.Function != "main.add" || all[1].Location.Function != "main.main" {
		t.Fatalf("expected tracked function breakpoints, got %#v", all)
	}

	requests := decodeCapturedRequests(t, conn.writes.Bytes())
	if len(requests) != 2 {
		t.Fatalf("expected 2 requests, got %#v", requests)
	}
	args, ok := requests[1].Arguments.(map[string]any)
	if !ok {
		t.Fatalf("expected second request arguments map, got %#v", requests[1].Arguments)
	}
	entries, ok := args["breakpoints"].([]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("expected second request to contain 2 function breakpoints, got %#v", args["breakpoints"])
	}
	assertFunctionBreakpointName(t, entries[0], "main.main")
	assertFunctionBreakpointName(t, entries[1], "main.add")
}

type fakeDAPConn struct {
	reads  *bytes.Reader
	writes bytes.Buffer
}

func newFakeDAPConn(frames ...[]byte) *fakeDAPConn {
	return &fakeDAPConn{reads: bytes.NewReader(bytes.Join(frames, nil))}
}

func (c *fakeDAPConn) Read(p []byte) (int, error)       { return c.reads.Read(p) }
func (c *fakeDAPConn) Write(p []byte) (int, error)      { return c.writes.Write(p) }
func (c *fakeDAPConn) Close() error                     { return nil }
func (c *fakeDAPConn) LocalAddr() net.Addr              { return fakeAddr("local") }
func (c *fakeDAPConn) RemoteAddr() net.Addr             { return fakeAddr("remote") }
func (c *fakeDAPConn) SetDeadline(time.Time) error      { return nil }
func (c *fakeDAPConn) SetReadDeadline(time.Time) error  { return nil }
func (c *fakeDAPConn) SetWriteDeadline(time.Time) error { return nil }

type fakeAddr string

func (a fakeAddr) Network() string { return "tcp" }
func (a fakeAddr) String() string  { return string(a) }

func mustDAPResponse(t *testing.T, requestSeq int, body any) []byte {
	t.Helper()

	payload, err := json.Marshal(response{Type: "response", RequestSeq: requestSeq, Success: true, Body: mustJSONRaw(t, body)})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	return []byte(fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(payload), payload))
}

func mustJSONRaw(t *testing.T, body any) json.RawMessage {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return payload
}

func decodeCapturedRequests(t *testing.T, data []byte) []request {
	t.Helper()

	reader := bufio.NewReader(bytes.NewReader(data))
	var requests []request
	for {
		length, err := readContentLength(reader)
		if err != nil {
			if err == io.EOF || strings.Contains(err.Error(), "EOF") {
				return requests
			}
			t.Fatalf("readContentLength returned error: %v", err)
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(reader, payload); err != nil {
			t.Fatalf("ReadFull returned error: %v", err)
		}
		var req request
		if err := json.Unmarshal(payload, &req); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		requests = append(requests, req)
	}
}

func assertSourceBreakpointLine(t *testing.T, value any, want int) {
	t.Helper()
	entry, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected breakpoint entry map, got %#v", value)
	}
	line, ok := entry["line"].(float64)
	if !ok || int(line) != want {
		t.Fatalf("expected breakpoint line %d, got %#v", want, entry["line"])
	}
}

func assertFunctionBreakpointName(t *testing.T, value any, want string) {
	t.Helper()
	entry, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected breakpoint entry map, got %#v", value)
	}
	name, ok := entry["name"].(string)
	if !ok || name != want {
		t.Fatalf("expected breakpoint name %q, got %#v", want, entry["name"])
	}
}
