package inproc

import "context"

type key struct{}

func With(ctx context.Context) context.Context { return context.WithValue(ctx, key{}, true) }

func Is(ctx context.Context) bool {
	marked, _ := ctx.Value(key{}).(bool)
	return marked
}
