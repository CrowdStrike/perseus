package main

import (
	"log/slog"

	"github.com/CrowdStrike/perseus/internal/log"
)

var (
	// stores the process-level verbosity
	logLevel logLevelVar
	// the logger
	logger = log.New(&logLevel)
)

// logLevelVar wraps a boolean value that controls logging verbosity and satisfies the [slog.Leveler]
// interface to translate that boolean to the equivalent [slog.Level], either [slog.LevelDebug] or [slog.LevelInfo].
type logLevelVar struct {
	debugMode bool
}

// Level satisfies the [slog.Leveler] interface and returns either [slog.LevelDebug] or [slog.LevelInfo]
// depending on whether or not debug verbosity was enabled.
func (v *logLevelVar) Level() slog.Level {
	if v.debugMode {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}
