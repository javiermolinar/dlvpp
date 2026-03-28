package dap

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRequestErrorLockedIncludesFormattedErrorAndDebugOutput(t *testing.T) {
	t.Parallel()

	client := New()
	_, _ = client.debugOutput.WriteString("Build Error: go build ./cmd/app\nmissing symbol\n")

	body, err := json.Marshal(errorResponseBody{Error: dapError{Format: "Failed to launch: Build error: Check the debug console for details."}})
	if err != nil {
		t.Fatalf("marshal error response body: %v", err)
	}

	err = client.requestErrorLocked("launch", &response{
		Message: "Failed to launch",
		Body:    body,
	}, 0)
	if err == nil {
		t.Fatal("expected requestErrorLocked to return an error")
	}

	message := err.Error()
	for _, want := range []string{
		"dap launch failed: Failed to launch",
		"Failed to launch: Build error: Check the debug console for details.",
		"Build Error: go build ./cmd/app",
		"missing symbol",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected error to contain %q, got %q", want, message)
		}
	}
}

func TestHandleEventLockedCapturesOutputEvents(t *testing.T) {
	t.Parallel()

	client := New()
	body, err := json.Marshal(outputEventBody{Category: "stderr", Output: "boom\n"})
	if err != nil {
		t.Fatalf("marshal output event body: %v", err)
	}

	if err := client.handleEventLocked(&response{Event: "output", Body: body}); err != nil {
		t.Fatalf("handleEventLocked returned error: %v", err)
	}
	if got := client.debugOutput.String(); got != "boom\n" {
		t.Fatalf("expected captured output, got %q", got)
	}
	entries, err := client.Output(nil)
	if err != nil {
		t.Fatalf("Output returned error: %v", err)
	}
	if len(entries) != 1 || entries[0].Category != "stderr" || entries[0].Text != "boom\n" {
		t.Fatalf("unexpected output entries: %#v", entries)
	}
}

func TestOutputIncludesProcessStdoutAndFiltersDAPBanner(t *testing.T) {
	t.Parallel()

	client := New()
	_, _ = client.stdout.WriteString("DAP server listening at: 127.0.0.1:12345\nhello\nworld\n")

	entries, err := client.Output(nil)
	if err != nil {
		t.Fatalf("Output returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one stdout entry, got %#v", entries)
	}
	if entries[0].Category != "stdout" || entries[0].Text != "hello\nworld\n" {
		t.Fatalf("unexpected stdout entry: %#v", entries[0])
	}
}
