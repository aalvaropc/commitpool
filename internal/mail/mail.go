package mail

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"sync"
)

type Message struct {
	To, Subject, Text string
}

type Mailer interface {
	Send(ctx context.Context, m Message) (id string, err error)
}

var ErrRejected = errors.New("mail: rejected by the provider")

type Log struct{ Log *slog.Logger }

func (l Log) Send(_ context.Context, m Message) (string, error) {
	log := l.Log
	if log == nil {
		log = slog.Default()
	}
	id := "log-" + randomID()
	log.Info("mail: not sent, logged instead", "to", Mask(m.To), "subject", m.Subject, "id", id)
	return id, nil
}

type Memory struct {
	mu    sync.Mutex
	sent  []Message
	fails []error
}

func (m *Memory) Send(_ context.Context, msg Message) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.fails) > 0 {
		err := m.fails[0]
		m.fails = m.fails[1:]
		return "", err
	}
	m.sent = append(m.sent, msg)
	return "mem-" + randomID(), nil
}

func (m *Memory) FailNext(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fails = append(m.fails, err)
}

func (m *Memory) Sent() []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Message(nil), m.sent...)
}

func Mask(addr string) string {
	local, domain, ok := strings.Cut(addr, "@")
	if !ok || local == "" {
		return "***"
	}
	return local[:1] + "***@" + domain
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
