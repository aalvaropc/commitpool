package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/store"
	"github.com/aalvaropc/commitpool/internal/store/storetest"
)

func TestProviderEvents_InsertIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)

	payload := json.RawMessage(`{"id":"WH-1","event_type":"PAYMENT.CAPTURE.COMPLETED"}`)
	inserted, err := s.InsertProviderEvent(ctx, "WH-1", "PAYMENT.CAPTURE.COMPLETED", "capture", "CAPTURE-1", true, payload)
	if err != nil || !inserted {
		t.Fatalf("first InsertProviderEvent = %v, %v; want true, nil", inserted, err)
	}

	inserted, err = s.InsertProviderEvent(ctx, "WH-1", "PAYMENT.CAPTURE.COMPLETED", "capture", "CAPTURE-1", false, json.RawMessage(`{"replayed":true}`))
	if err != nil || inserted {
		t.Fatalf("duplicate InsertProviderEvent = %v, %v; want false, nil", inserted, err)
	}

	if n := count(t, s, `select count(*) from provider_events`); n != 1 {
		t.Errorf("count(provider_events) = %d, want 1", n)
	}
	var (
		stored   json.RawMessage
		verified bool
		resource string
		kind     string
		received time.Time
	)
	err = s.Pool.QueryRow(ctx, `select payload, verified, resource_id, resource_type, received_at from provider_events where id = 'WH-1'`).
		Scan(&stored, &verified, &resource, &kind, &received)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, "stored payload", stored, payload)
	if !verified || resource != "CAPTURE-1" || kind != "capture" || received.IsZero() {
		t.Errorf("stored event: verified=%v resource=%q type=%q received=%v; the duplicate must not overwrite the first", verified, resource, kind, received)
	}

	if inserted, err := s.InsertProviderEvent(ctx, "WH-2", "PAYMENT.AUTHORIZATION.VOIDED", "", "", false, json.RawMessage(`{}`)); err != nil || !inserted {
		t.Fatalf("InsertProviderEvent with empty resource = %v, %v", inserted, err)
	}
	if n := count(t, s, `select count(*) from provider_events where id = 'WH-2' and resource_id is null and resource_type is null`); n != 1 {
		t.Errorf("empty resource fields should be stored as NULL")
	}
}

func TestProviderEvents_MarkProcessed(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)

	if _, err := s.InsertProviderEvent(ctx, "WH-1", "PAYMENT.CAPTURE.COMPLETED", "capture", "CAPTURE-1", true, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `select count(*) from provider_events where processed_at is not null`); n != 0 {
		t.Fatalf("a new event is already processed")
	}

	if err := s.MarkEventProcessed(ctx, "WH-1"); err != nil {
		t.Fatalf("MarkEventProcessed: %v", err)
	}
	var first time.Time
	if err := s.Pool.QueryRow(ctx, `select processed_at from provider_events where id = 'WH-1'`).Scan(&first); err != nil {
		t.Fatal(err)
	}

	if err := s.MarkEventProcessed(ctx, "WH-1"); err != nil {
		t.Fatalf("second MarkEventProcessed: %v", err)
	}
	var second time.Time
	if err := s.Pool.QueryRow(ctx, `select processed_at from provider_events where id = 'WH-1'`).Scan(&second); err != nil {
		t.Fatal(err)
	}
	if !second.Equal(first) {
		t.Errorf("processed_at moved from %v to %v on a repeat", first, second)
	}

	if err := s.MarkEventProcessed(ctx, "WH-404"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("MarkEventProcessed(unknown) error = %v, want ErrNotFound", err)
	}
}
