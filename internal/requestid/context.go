// Package requestid carries request identifiers across application layers.
package requestid

import "context"

type contextKey struct{}

// WithContext returns a context containing the request identifier.
func WithContext(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// FromContext returns the identifier, or an empty string when none is present.
func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}
