package httpapi

import (
	"fmt"
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
	if len(q.started) != 0 || q.running != 0 {
		t.Errorf("after a refund: %d started, %d running", len(q.started), q.running)
	}
}
