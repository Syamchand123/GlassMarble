package logger

import (
	"log/slog"
	"os"
)

type Logger struct {
	slogLogger *slog.Logger
	debug      bool
}

func New(debug bool) *Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}

	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})

	return &Logger{
		slogLogger: slog.New(handler),
		debug:      debug,
	}
}

func (l *Logger) Info(msg string, args ...any) {
	l.slogLogger.Info(msg, args...)
}

func (l *Logger) Debug(msg string, args ...any) {
	l.slogLogger.Debug(msg, args...)
}

func (l *Logger) Error(msg string, args ...any) {
	l.slogLogger.Error(msg, args...)
}

func (l *Logger) Warn(msg string, args ...any) {
	l.slogLogger.Warn(msg, args...)
}
