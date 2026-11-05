// Package inproc marks a request the service sends to its own API in process,
// such as an MCP tool call, so per-address limits that already counted the
// outer request do not count it again. Only code in this process can set the
// mark: it lives in the context, never in a header.
package inproc

import "context"

type key struct{}

// With returns ctx marked as an in-process call.
func With(ctx context.Context) context.Context { return context.WithValue(ctx, key{}, true) }

// Is reports whether ctx carries the in-process mark.
func Is(ctx context.Context) bool {
	marked, _ := ctx.Value(key{}).(bool)
	return marked
}
