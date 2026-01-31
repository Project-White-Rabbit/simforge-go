package simforge

import "context"

// spanStackKey is the context key for the span stack.
// Using a private struct type ensures no collisions with other packages.
type spanStackKey struct{}

// spanEntry represents a single entry in the span stack.
type spanEntry struct {
	traceID string
	spanID  string
}

// currentSpan returns the top of the span stack from the context, or nil if empty.
func currentSpan(ctx context.Context) *spanEntry {
	stack, _ := ctx.Value(spanStackKey{}).([]spanEntry)
	if len(stack) == 0 {
		return nil
	}
	top := stack[len(stack)-1]
	return &top
}

// withSpanContext pushes a new span entry onto the context's span stack.
func withSpanContext(ctx context.Context, traceID, spanID string) context.Context {
	stack, _ := ctx.Value(spanStackKey{}).([]spanEntry)
	newStack := make([]spanEntry, len(stack)+1)
	copy(newStack, stack)
	newStack[len(stack)] = spanEntry{traceID: traceID, spanID: spanID}
	return context.WithValue(ctx, spanStackKey{}, newStack)
}
