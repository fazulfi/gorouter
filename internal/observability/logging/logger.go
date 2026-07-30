// Package logging provides a JSON-structured logger built on zerolog.
//
// The logger writes to stdout by default (never to files). Pretty mode enables
// a colorized console writer for local development.
//
// Context enrichment helpers (LogError, LogAndReturn) extract common fields
// such as request_id and actor_id from the Go context automatically.
package logging

import (
	"context"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

type ctxKey string

func (c ctxKey) String() string { return "gorouter.logging." + string(c) }

const (
	ctxRequestID ctxKey = "request_id"
	ctxActorID   ctxKey = "actor_id"
)

// NewLogger creates a new zerolog.Logger configured for JSON stdout.
//
//	level: one of trace, debug, info, warn, error, fatal (defaults to info)
//	pretty: enable console writer with color for development
//
// The logger always includes a timestamp (RFC3339) and caller (file:line).
// The caller frame skip is increased by 2 so that wrapper functions such as
// LogError and LogAndReturn resolve to their callers.
func NewLogger(level string, pretty bool) zerolog.Logger {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}

	var output io.Writer = os.Stdout
	if pretty {
		output = zerolog.NewConsoleWriter(func(w *zerolog.ConsoleWriter) {
			w.Out = os.Stdout
			w.TimeFormat = time.RFC3339
		})
	}

	return zerolog.New(output).
		Level(lvl).
		With().
		Timestamp().
		CallerWithSkipFrameCount(6).
		Logger()
}

// WithRequestID stores a request ID in the context for log enrichment.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, ctxRequestID, requestID)
}

// WithActorID stores an actor ID in the context for log enrichment.
func WithActorID(ctx context.Context, actorID string) context.Context {
	return context.WithValue(ctx, ctxActorID, actorID)
}

// GetRequestID extracts the request ID from context.
func GetRequestID(ctx context.Context) string {
	v, _ := ctx.Value(ctxRequestID).(string)
	return v
}

// GetActorID extracts the actor ID from context.
func GetActorID(ctx context.Context) string {
	v, _ := ctx.Value(ctxActorID).(string)
	return v
}

// LogAndReturn logs an error at the error level and returns it. This is
// intended for one-liner error handling:
//
//	return logging.LogAndReturn(ctx, logger, err)
func LogAndReturn(ctx context.Context, logger zerolog.Logger, err error) error {
	if err == nil {
		return nil
	}
	LogError(ctx, logger, err)
	return err
}

// LogError logs a structured error event. It enriches the log entry with
// request_id and actor_id from the context when present, and reads the error
// code and HTTP status when the error implements codedError.
func LogError(ctx context.Context, logger zerolog.Logger, err error) {
	if err == nil {
		return
	}

	_, file, line, ok := runtime.Caller(1)
	caller := ""
	if ok {
		caller = shortenCaller(file) + ":" + strconv.Itoa(line)
	}

	evt := logger.Error().Err(err)
	if caller != "" {
		evt.Str("caller", caller)
	}
	if requestID := GetRequestID(ctx); requestID != "" {
		evt.Str("request_id", requestID)
	}
	if actorID := GetActorID(ctx); actorID != "" {
		evt.Str("actor_id", actorID)
	}

	if ce, ok := err.(interface {
		Code() string
		HTTPStatusCode() int
	}); ok {
		evt.Str("error_code", ce.Code())
		evt.Int("http_status", ce.HTTPStatusCode())
	}

	evt.Msg("error")
}

func shortenCaller(file string) string {
	if idx := strings.LastIndex(file, "/gorouter/"); idx >= 0 {
		return file[idx+10:]
	}
	if idx := strings.LastIndex(file, "/"); idx >= 0 {
		return file[idx+1:]
	}
	return file
}
