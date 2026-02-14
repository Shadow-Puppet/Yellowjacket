// Package logging provides a slog-based logger adapter for Wails.
package logging

import (
	"fmt"
	"log/slog"
	"strings"
)

// Logger wraps slog to implement the Wails logger interface.
type Logger struct {
	slogger       *slog.Logger
	moduleFilters []string
}

// NewLogger creates a logger with optional message filters.
func NewLogger(slogger *slog.Logger, filters []string) *Logger {
	return &Logger{
		slogger:       slogger,
		moduleFilters: filters,
	}
}

// Print outputs a message if not filtered.
func (l *Logger) Print(message string) {
	if l.isFilteredOut(message) {
		return
	}

	fmt.Printf("[Print] %s\n", message)
}

// Trace logs a trace-level message if not filtered.
func (l *Logger) Trace(message string) {
	if l.isFilteredOut(message) {
		return
	}

	l.slogger.Debug("[Trace] " + message)
}

// Debug logs a debug-level message if not filtered.
func (l *Logger) Debug(message string) {
	if l.isFilteredOut(message) {
		return
	}

	l.slogger.Debug(message)
}

// Info logs an info-level message if not filtered.
func (l *Logger) Info(message string) {
	if l.isFilteredOut(message) {
		return
	}

	l.slogger.Info(message)
}

// Warning logs a warning-level message if not filtered.
func (l *Logger) Warning(message string) {
	if l.isFilteredOut(message) {
		return
	}

	l.slogger.Warn(message)
}

func (l *Logger) Error(message string) {
	if l.isFilteredOut(message) {
		return
	}

	l.slogger.Error(message)
}

// Fatal logs a fatal-level message if not filtered.
func (l *Logger) Fatal(message string) {
	if l.isFilteredOut(message) {
		return
	}

	l.slogger.Error("[Trace] " + message)
}

func (l *Logger) isFilteredOut(message string) bool {
	for _, f := range l.moduleFilters {
		if strings.HasPrefix(message, fmt.Sprintf("[%s]", f)) {
			return true
		}
	}

	return false
}
