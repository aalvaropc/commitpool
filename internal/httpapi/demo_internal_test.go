package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDemoQuota_ThirtyAnHourOverASlidingWindow(t *testing.T) {
	q := newDemoQuota(false)
	start := time.Date(2026, 11, 6, 9, 30, 0, 0, time.UTC)
	for i := range demoHourlyCap {
		ticket, refused := q.start(fmt.Sprintf("10.0.0.%d", i), start.Add(time.Duration(i)*time.Minute))
		if refused != nil {
			t.Fatalf("demo %d refused: %s", i+1, refused.err.message)
		}
		ticket.done()
	}
	_, refused := q.start("10.0.1.1", start.Add(40*time.Minute))
	if refused == nil || refused.err.code != "demo_busy" || refused.wait != 20*time.Minute {
		t.Fatalf("31st demo = %+v, want demo_busy until the first one is an hour old", refused)
	}
	if _, refused := q.start("10.0.1.1", start.Add(time.Hour)); refused != nil {
		t.Errorf("an hour after the first demo, the next one was refused: %s", refused.err.message)
	}
}

func TestDemoQuota_CapsDemosFillingInAtOnce(t *testing.T) {
	q := newDemoQuota(false)
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	var tickets []*demoTicket
	for i := range demoRunningCap {
		ticket, refused := q.start(fmt.Sprintf("10.0.0.%d", i), now)
		if refused != nil {
			t.Fatalf("demo %d refused", i+1)
		}
		tickets = append(tickets, ticket)
	}
	if _, refused := q.start("10.0.1.1", now); refused == nil || refused.err.code != "demo_busy" {
		t.Fatal("a seventh demo started while six were filling in")
	}
	tickets[0].done()
	tickets[0].done()
	if _, refused := q.start("10.0.1.1", now); refused != nil {
		t.Errorf("a finished demo did not free its place: %s", refused.err.message)
	}
	if _, refused := q.start("10.0.1.2", now); refused == nil {
		t.Error("calling done twice freed two places")
	}
}

func TestDemoQuota_AFailedStartDoesNotCount(t *testing.T) {
	q := newDemoQuota(true)
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	ticket, _ := q.start("10.0.0.1", now)
	ticket.refund()
	if len(q.started.times) != 0 || q.running != 0 {
		t.Errorf("after a refund: %d started, %d running", len(q.started.times), q.running)
	}
}

func TestClientAddr_GroupsAnIPv6NetworkAndKeepsIPv4(t *testing.T) {
	addr := func(remote, forwarded string) string {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = remote
		if forwarded != "" {
			r.Header.Set("X-Forwarded-For", forwarded)
		}
		return clientAddr(r)
	}
	if a, b := addr("[2001:db8:1:2::1]:443", ""), addr("[2001:db8:1:2:ffff::9]:443", ""); a != "2001:db8:1:2::/64" || a != b {
		t.Errorf("one IPv6 network = %q and %q, want the same /64", a, b)
	}
	if got := addr("172.18.0.2:5000", "203.0.113.9, 2001:db8:1:2::7"); got != "2001:db8:1:2::/64" {
		t.Errorf("forwarded IPv6 = %q", got)
	}
	if got := addr("203.0.113.9:1234", ""); got != "203.0.113.9" {
		t.Errorf("IPv4 = %q", got)
	}
}

func TestDraftQuota_TwoAtOnceThenAPauseAndAnHourlyCap(t *testing.T) {
	q := newDraftQuota(true)
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	for i := range 2 {
		if _, refused := q.take("10.0.0.1", now); refused != nil {
			t.Fatalf("draft %d refused", i+1)
		}
	}
	wait, refused := q.take("10.0.0.1", now)
	if refused == nil || refused.code != "draft_rate_limited" || wait != draftVisitorEvery {
		t.Fatalf("third draft = %v %+v, want draft_rate_limited for 15 s", wait, refused)
	}
	busy := newDraftQuota(false)
	for i := range draftHourlyCap {
		if _, refused := busy.take(fmt.Sprintf("10.0.%d.%d", i/250, i%250), now.Add(time.Duration(i)*time.Second)); refused != nil {
			t.Fatalf("draft %d refused", i+1)
		}
	}
	if _, refused := busy.take("10.9.9.9", now.Add(time.Hour-time.Second)); refused == nil || refused.code != "draft_busy" {
		t.Errorf("draft over the hourly cap = %+v", refused)
	}
}
