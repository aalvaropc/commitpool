package store

import (
	"context"
	"encoding/json"
	"time"
)

// InsertProviderEvent records a webhook event once. It reports whether the
// event was new: a repeated delivery of the same event ID changes nothing and
// returns false. Empty resource fields are stored as NULL.
func (s *Store) InsertProviderEvent(ctx context.Context, id, eventType, resourceType, resourceID string, verified bool, payload json.RawMessage) (inserted bool, err error) {
	tag, err := s.Pool.Exec(ctx,
		`insert into provider_events (id, event_type, resource_type, resource_id, verified, payload)
		 values ($1, $2, nullif($3, ''), nullif($4, ''), $5, $6)
		 on conflict (id) do nothing`,
		id, eventType, resourceType, resourceID, verified, payload)
	if err != nil {
		return false, wrap("insert provider event", err)
	}
	return tag.RowsAffected() == 1, nil
}

// MarkEventProcessed stamps the event as handled. Marking it again keeps the
// first timestamp.
func (s *Store) MarkEventProcessed(ctx context.Context, id string) error {
	tag, err := s.Pool.Exec(ctx,
		`update provider_events set processed_at = coalesce(processed_at, now()) where id = $1`, id)
	if err != nil {
		return wrap("mark event processed", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PurgeProviderEvents deletes the webhook deliveries received more than
// olderThan ago, by the database clock that stamped them, and returns how
// many it deleted: their payloads may carry payer details and are only
// needed while a delivery can still be repeated.
func (s *Store) PurgeProviderEvents(ctx context.Context, olderThan time.Duration) (int, error) {
	tag, err := s.Pool.Exec(ctx, `delete from provider_events where received_at < now() - make_interval(secs => $1)`, olderThan.Seconds())
	if err != nil {
		return 0, wrap("purge provider events", err)
	}
	return int(tag.RowsAffected()), nil
}
