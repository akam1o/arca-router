// Package correlation carries request correlation IDs through call paths.
package correlation

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

const (
	// HeaderName is the canonical HTTP response/request header for request IDs.
	HeaderName = "X-Request-ID"

	// MetadataKey is the lowercase gRPC metadata key for request IDs.
	MetadataKey = "x-request-id"

	// AlternateMetadataKey accepts the common correlation-id spelling.
	AlternateMetadataKey = "x-correlation-id"
)

type contextKey struct{}

// ID returns the correlation ID stored on ctx, if one exists.
func ID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(contextKey{}).(string); ok {
		return id
	}
	return ""
}

// WithID returns a child context carrying id. Blank IDs leave ctx unchanged.
func WithID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	id = Normalize(id)
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, id)
}

// EnsureID returns a context carrying an ID and the effective ID value.
func EnsureID(ctx context.Context) (context.Context, string) {
	if id := ID(ctx); id != "" {
		return ctx, id
	}
	id := uuid.NewString()
	return WithID(ctx, id), id
}

// Normalize trims unsafe request ID input for log and audit usage.
func Normalize(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	id = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, id)
	if len(id) > 128 {
		return id[:128]
	}
	return id
}
