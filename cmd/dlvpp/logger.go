package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

func newCommandLogger(verbose bool, writer io.Writer) *slog.Logger {
	level := slog.LevelError + 1
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(newPlainHandler(writer, level))
}

type plainHandler struct {
	writer io.Writer
	level  slog.Level
	attrs  []slog.Attr
	groups []string
}

func newPlainHandler(writer io.Writer, level slog.Level) slog.Handler {
	return &plainHandler{writer: writer, level: level}
}

func (h *plainHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *plainHandler) Handle(_ context.Context, record slog.Record) error {
	var out strings.Builder
	out.WriteString(record.Message)

	attrs := append([]slog.Attr(nil), h.attrs...)
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})
	for _, attr := range attrs {
		key := attr.Key
		if len(h.groups) > 0 {
			key = strings.Join(append(append([]string(nil), h.groups...), key), ".")
		}
		fmt.Fprintf(&out, " %s=%v", key, attr.Value.Any())
	}
	out.WriteByte('\n')
	_, err := io.WriteString(h.writer, out.String())
	return err
}

func (h *plainHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &clone
}

func (h *plainHandler) WithGroup(name string) slog.Handler {
	clone := *h
	clone.groups = append(append([]string(nil), h.groups...), name)
	return &clone
}
