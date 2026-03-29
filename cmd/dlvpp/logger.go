package main

import (
	"fmt"
	"io"
)

type logLevel int

const (
	logLevelSilent logLevel = iota
	logLevelInfo
	logLevelDebug
)

type logger interface {
	Infof(format string, args ...any)
	Debugf(format string, args ...any)
}

type writerLogger struct {
	level  logLevel
	writer io.Writer
}

func newLogger(verbose bool, writer io.Writer) logger {
	level := logLevelSilent
	if verbose {
		level = logLevelDebug
	}
	return writerLogger{level: level, writer: writer}
}

func (l writerLogger) Infof(format string, args ...any) {
	if l.level < logLevelInfo {
		return
	}
	_, _ = fmt.Fprintf(l.writer, format+"\n", args...)
}

func (l writerLogger) Debugf(format string, args ...any) {
	if l.level < logLevelDebug {
		return
	}
	_, _ = fmt.Fprintf(l.writer, format+"\n", args...)
}
