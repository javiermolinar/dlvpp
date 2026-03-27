package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestWaitForDisconnectReturnsOnEOF(t *testing.T) {
	t.Parallel()

	if err := waitForDisconnect(context.Background(), bytes.NewBuffer(nil)); err != nil {
		t.Fatalf("waitForDisconnect returned error on EOF: %v", err)
	}
}

func TestWaitForDisconnectReturnsOnNewline(t *testing.T) {
	t.Parallel()

	if err := waitForDisconnect(context.Background(), bytes.NewBufferString("\n")); err != nil {
		t.Fatalf("waitForDisconnect returned error on newline: %v", err)
	}
}

func TestWaitForDisconnectReturnsContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := waitForDisconnect(ctx, bytes.NewBuffer(nil))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}
