package logger

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

type contextKey string

const (
	TraceIDKey       contextKey = "trace_id"
	ActorUserIDKey   contextKey = "actor_user_id"
	PropertyIDKey    contextKey = "property_id"
	RoomTypeIDKey    contextKey = "room_type_id"
	ReservationIDKey contextKey = "reservation_id"
)

// Config configures the structured logger.
type Config struct {
	Level  slog.Level
	Output io.Writer
}

// New initializes a production structured JSON logger writing to the specified writer (or Stdout).
func New(cfg Config) *slog.Logger {
	out := cfg.Output
	if out == nil {
		out = os.Stdout
	}

	opts := &slog.HandlerOptions{
		Level: cfg.Level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Customize standard keys per blueprint
			if a.Key == slog.TimeKey {
				a.Key = "ts"
				a.Value = slog.StringValue(a.Value.Time().UTC().Format(time.RFC3339))
			}
			if a.Key == slog.MessageKey && a.Value.String() == "" {
				return slog.Attr{}
			}
			return a
		},
	}

	handler := slog.NewJSONHandler(out, opts)
	return slog.New(handler)
}

// Context helpers for request tracing and contextual enrichment

// WithTraceID injects a trace ID into the context.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, TraceIDKey, traceID)
}

// GetTraceID retrieves the trace ID from the context.
func GetTraceID(ctx context.Context) string {
	if val, ok := ctx.Value(TraceIDKey).(string); ok {
		return val
	}
	return ""
}

// WithActorUserID injects the actor user ID into the context.
func WithActorUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, ActorUserIDKey, userID)
}

// WithPropertyID injects the property ID into the context.
func WithPropertyID(ctx context.Context, propertyID string) context.Context {
	return context.WithValue(ctx, PropertyIDKey, propertyID)
}

// WithReservationID injects the reservation ID into the context.
func WithReservationID(ctx context.Context, resID string) context.Context {
	return context.WithValue(ctx, ReservationIDKey, resID)
}

// ContextAttributes extracts all request-scoped metadata from context into slog attributes.
func ContextAttributes(ctx context.Context) []slog.Attr {
	var attrs []slog.Attr

	if traceID := GetTraceID(ctx); traceID != "" {
		attrs = append(attrs, slog.String("trace_id", traceID))
	}
	if actorID, ok := ctx.Value(ActorUserIDKey).(string); ok && actorID != "" {
		attrs = append(attrs, slog.String("actor_user_id", actorID))
	}
	if propID, ok := ctx.Value(PropertyIDKey).(string); ok && propID != "" {
		attrs = append(attrs, slog.String("property_id", propID))
	}
	if roomTypeID, ok := ctx.Value(RoomTypeIDKey).(string); ok && roomTypeID != "" {
		attrs = append(attrs, slog.String("room_type_id", roomTypeID))
	}
	if resID, ok := ctx.Value(ReservationIDKey).(string); ok && resID != "" {
		attrs = append(attrs, slog.String("reservation_id", resID))
	}

	return attrs
}

// HTTPMiddleware returns a chi-compatible middleware that logs HTTP requests with structured metadata.
func HTTPMiddleware(log *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Extract or generate trace_id
			traceID := r.Header.Get("X-Request-ID")
			if traceID == "" {
				traceID = r.Header.Get("X-Trace-ID")
			}
			if traceID == "" {
				traceID = uuid.New().String()
			}

			// Attach trace ID to context and response headers
			ctx := WithTraceID(r.Context(), traceID)
			w.Header().Set("X-Trace-ID", traceID)

			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			defer func() {
				latencyMs := time.Since(start).Milliseconds()
				route := r.URL.Path
				status := ww.Status()
				if status == 0 {
					status = http.StatusOK
				}

				// Build log attributes
				attrs := []slog.Attr{
					slog.String("trace_id", traceID),
					slog.String("route", route),
					slog.String("method", r.Method),
					slog.Int64("latency_ms", latencyMs),
					slog.Int("status", status),
				}

				// Append contextual attributes (actor, property, reservation)
				attrs = append(attrs, ContextAttributes(ctx)...)

				level := slog.LevelInfo
				if status >= 500 {
					level = slog.LevelError
				} else if status >= 400 {
					level = slog.LevelWarn
				}

				log.LogAttrs(ctx, level, "http_request", attrs...)
			}()

			next.ServeHTTP(ww, r.WithContext(ctx))
		})
	}
}
