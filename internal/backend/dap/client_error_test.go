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
}
