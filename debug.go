package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

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

	// golang.org/x/exp is still technically unstable and we'd rather eat a panic than crash
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "panic caught while writing debug log\n\t%v\n%s", r, string(debug.Stack()))
		}
	}()

	ctx := context.Background()
	if len(kvs) == 0 {
		logger.Log(ctx, slog.LevelDebug, msg)
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
	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])
	rec := slog.NewRecord(time.Now(), slog.LevelDebug, msg, pcs[0])
	rec.AddAttrs(attrs...)
	// TODO: what should we do if this call to logger.Handler().Handle() fails?
	_ = logger.Handler().Handle(ctx, rec)

	_ = attrs[len(kvs)]
}

func inK8S() bool {
	_, ok := os.LookupEnv("KUBERNETES_SERVICE_HOST")
	return ok
}
