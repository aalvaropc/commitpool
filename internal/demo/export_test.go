package demo

import (
	"context"
	"testing"
	"time"
)

func SetSleep(t testing.TB, f func(context.Context, time.Duration) error) {
	old := sleep
	sleep = f
	t.Cleanup(func() { sleep = old })
}
