package main

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"golang.org/x/exp/slog"
)

var (
	debugMode   bool
	initLogOnce sync.Once
	logger      *slog.Logger
)

// debugLog writes the provided message and key/value pairs to stdout using structured logging
func debugLog(msg string, kvs ...any) {
	if !debugMode {
		return
	}
	initLogOnce.Do(func() {
		opts := slog.HandlerOptions{
			AddSource: true,
			Level:     slog.LevelDebug,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				switch a.Key {
				case slog.SourceKey:
					// trim "source" down to relative path within this module
					val := a.Value.String()
					if idx := strings.Index(val, "github.com/CrowdStrike/perseus/"); idx != -1 {
						a.Value = slog.StringValue(val[idx+31:])
					}
				case slog.LevelKey:
					// don't output "level" since we're only ever generating debug logs
					a = slog.Attr{}
				default:
				}
				return a
			},
		}
		if inK8S() {
			logger = slog.New(opts.NewJSONHandler(os.Stdout))
		} else {
			logger = slog.New(opts.NewTextHandler(os.Stdout))
		}
	})

	if len(kvs) == 0 {
		logger.LogDepth(1, slog.LevelDebug, msg)
		return
	}

	attrs := make([]slog.Attr, 0, len(kvs)/2)
	for i := 0; i < len(kvs); i += 2 {
		k, ok := kvs[i].(string)
		if !ok {
			k = fmt.Sprintf("%v", kvs[i])
		}
		attrs = append(attrs, slog.Any(k, kvs[i+1]))
	}
	logger.LogAttrsDepth(1, slog.LevelDebug, msg, attrs...)
}

func inK8S() bool {
	_, ok := os.LookupEnv("KUBERNETES_SERVICE_HOST")
	return ok
}
