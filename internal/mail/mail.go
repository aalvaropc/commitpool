// Package mail sends CommitPool's plain-text notifications. A Mailer delivers
// one message; ErrRejected marks a refusal that sending again will not fix,
// such as an unverified recipient while Amazon SES is in its sandbox.
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

// Message is one plain-text email.
type Message struct {
	To, Subject, Text string
}

// Mailer delivers a message and returns the provider's id for it.
type Mailer interface {
	Send(ctx context.Context, m Message) (id string, err error)
}

// ErrRejected is a final refusal: the message is not sent again.
var ErrRejected = errors.New("mail: rejected by the provider")

// Log is the mailer for local runs and demos without email: it logs the
// masked recipient and the subject, never the body.
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

// Memory keeps every message it is given, for tests; FailNext makes the
// next sends fail with the given errors, in order.
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

// FailNext queues an error for the next send.
func (m *Memory) FailNext(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fails = append(m.fails, err)
}

// Sent returns the messages sent so far.
func (m *Memory) Sent() []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Message(nil), m.sent...)
}

// Mask keeps an address recognizable in logs without spelling it out:
// "ana@example.com" becomes "a***@example.com".
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
