package worker

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestLoop_RecoversAPanicAndKeepsBeating(t *testing.T) {
	logs := &syncBuffer{}
	r := &Runner{log: slog.New(slog.NewTextHandler(logs, nil))}
	var calls atomic.Int32
	var beat atomic.Int64
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.loop(ctx, "periodic", time.Millisecond, &beat, func(context.Context) {
			if calls.Add(1) == 1 {
				panic("boom")
			}
		})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done

	if calls.Load() < 3 {
		t.Fatalf("loop ran %d times after a panic, want it to keep going", calls.Load())
	}
	if beat.Load() == 0 {
		t.Error("loop never recorded a heartbeat")
	}
	if !strings.Contains(logs.String(), "boom") || !strings.Contains(logs.String(), "level=ERROR") {
		t.Errorf("panic not logged as an error:\n%s", logs.String())
	}
}
