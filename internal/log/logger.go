package log

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// New initializes and returns a new [Logger] using [level] to dynamically determine the active
// verbosity level.
func New(level slog.Leveler) *Logger {
	opts := slog.HandlerOptions{
		AddSource:   true,
		Level:       level,
		ReplaceAttr: replaceRecordAttributes,
	}

	var logger *slog.Logger
	if inK8S() {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &opts))
	} else {
		logger = slog.New(slog.NewTextHandler(os.Stdout, &opts))
	}
	return &Logger{
		logger: logger,
	}
}

// Logger wraps a [slog.Logger] to provide a streamlined API and consistent behavior for the Perseus
// application.
type Logger struct {
	logger *slog.Logger
}

// Info logs a message at INFO level with the specified message and attributes.
func (l *Logger) Info(msg string, kvs ...any) {
	ctx, h := context.Background(), l.logger.Handler()
	if h.Enabled(ctx, slog.LevelInfo) {
		rec := getLogRecord(slog.LevelInfo, msg, kvs...)
		if err := h.Handle(ctx, rec); err != nil {
			dumpLogHandlerError(err, rec)
		}
	}
}

// Debug logs a message at DEBUG level with the specified message and attributes.
func (l *Logger) Debug(msg string, kvs ...any) {
	ctx, h := context.Background(), l.logger.Handler()
	if h.Enabled(ctx, slog.LevelDebug) {
		rec := getLogRecord(slog.LevelDebug, msg, kvs...)
		if err := h.Handle(ctx, rec); err != nil {
			dumpLogHandlerError(err, rec)
		}
	}
}

// Error logs a message at ERROR level with the specified message and attributes.  If [err] is not nil,
// it will be logged in an additional attribute called "err".
func (l *Logger) Error(err error, msg string, kvs ...any) {
	ctx, h := context.Background(), l.logger.Handler()
	if h.Enabled(ctx, slog.LevelError) {
		rec := getLogRecord(slog.LevelError, msg, kvs...)
		if err != nil {
			rec.Add(slog.String("err", err.Error()))
		}
		if err := h.Handle(ctx, rec); err != nil {
			dumpLogHandlerError(err, rec)
		}
	}
}

// getLogRecord builds a [slog.Record] with the provided level and message.  [kvs], if provided, must
// be a list of tuples where the first item is the string key for the attribute and the second is the
// value.  These are used to construct [slog.Any] attributes.
func getLogRecord(lvl slog.Level, msg string, kvs ...any) slog.Record {
	// skip runtime.Callers(), getLogRecord(), and Info()/Debug()/Error()
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])

	rec := slog.NewRecord(time.Now().UTC(), lvl, msg, pcs[0])
	if len(kvs) > 0 {
		attrs := make([]slog.Attr, 0, len(kvs)/2)
		for i := 0; i < len(kvs); i += 2 {
			k, ok := kvs[i].(string)
			if !ok {
				k = fmt.Sprintf("%v", kvs[i])
			}
			attrs = append(attrs, slog.Any(k, kvs[i+1]))
		}
		rec.AddAttrs(attrs...)
	}
	return rec
}

// replaceRecordAttributes massages the log record attributes before output
func replaceRecordAttributes(_ []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.SourceKey:
		// trim "source" down to relative path within this module
		val := a.Value.Any().(*slog.Source)
		if idx := strings.Index(val.File, "github.com/CrowdStrike/perseus/"); idx != -1 {
			val.File = val.File[idx+31:]
		}
	default:
	}
	return a
}

var (
	loadRunningInK8sOnce sync.Once
	runningInK8s         bool
)

// inK8S returns a boolean value that indicates whether or not the current process is running inside
// of Kubernetes.  This is used configure the log output to be either logfmt or JSON.
func inK8S() bool {
	loadRunningInK8sOnce.Do(func() {
		_, runningInK8s = os.LookupEnv("KUBERNETES_SERVICE_HOST")
	})
	return runningInK8s
}

// dumpLogHandlerError is called when invoking the underlying [slog.Handler] returns an error to write
// the error and additional details to [os.Stderr] in the appropriate format (JSON vs key/value) based
// on the environment.
func dumpLogHandlerError(err error, rec slog.Record) {
	var source string
	rec.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case slog.SourceKey:
			src := a.Value.Any().(*slog.Source)
			source = fmt.Sprintf("%s:%d", src.File, src.Line)
		default:
		}
		return true
	})
	// generate a JSON "log" if we're running in k8s, logfmt style k/v pairs otherwise
	logData := map[string]string{
		"time":        rec.Time.Format(time.RFC3339),
		"level":       "ERROR",
		"source":      source,
		"msg":         "failure invoking slog.Handler.Handle()",
		"originalMsg": rec.Message,
		"error":       err.Error(),
	}
	if inK8S() {
		output, _ := json.Marshal(logData)
		_, _ = os.Stderr.Write(output)
	} else {
		// output keys in an explicit order for consistency
		var (
			sb   strings.Builder
			keys = []string{"time", "level", "source", "msg", "orginalMsg", "error"}
		)
		for i, k := range keys {
			if i > 0 {
				sb.WriteRune(' ')
			}
			v := logData[k]
			// use snake-case instead of Pascal case for logfmt output
			if k == "originalMsg" {
				k = "original_msg"
			}
			sb.WriteString(k + "=")
			switch k {
			case "time", "level", "source":
				// these keys won't have embedded quotes or backslashes
				sb.WriteString(v)
			default:
				sb.WriteString("\"" + strings.ReplaceAll(v, "\"", "\\\"") + "\"")
			}
		}
		_, _ = os.Stderr.WriteString(sb.String())
	}
	_, _ = os.Stderr.WriteString("\n")
}
