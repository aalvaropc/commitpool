package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const KindNotify = "notify"

type Notification struct {
	WorkshopID, ParticipantID, Kind, Key string
	To, Subject, Body, HTML              string
}

type NotificationRow struct {
	ID, WorkshopID, ParticipantID, Kind, Key string
	To, Subject, Body, Status                string
	ProviderMessageID                        *string
	SentAt                                   *time.Time
	HTML                                     string
}

const notificationCols = `id, workshop_id, coalesce(participant_id::text, ''), kind, idempotency_key, to_email, subject, body, status, provider_message_id, sent_at, html`

func (s *Store) SetParticipantEmail(ctx context.Context, participantID, email string) error {
	if _, err := s.Pool.Exec(ctx, `update participants set email = $2 where id = $1`, participantID, email); err != nil {
		return wrap("set participant email", err)
	}
	return nil
}

func (s *Store) ParticipantEmail(ctx context.Context, participantID string) (name, email string, err error) {
	var addr *string
	err = s.Pool.QueryRow(ctx, `select display_name, email from participants where id = $1`, participantID).Scan(&name, &addr)
	if err != nil {
		return "", "", lookupErr("participant email", err)
	}
	if addr != nil {
		email = *addr
	}
	return name, email, nil
}

type Contact struct{ ParticipantID, Name, Email string }

func (s *Store) ContactsOfWorkshop(ctx context.Context, workshopID string) ([]Contact, error) {
	return many[Contact](ctx, s.Pool, "workshop contacts",
		`select id, display_name, email from participants where workshop_id = $1 and email is not null order by created_at, id`, workshopID)
}

func (s *Store) EnqueueNotification(ctx context.Context, n Notification, now time.Time) (created bool, err error) {
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx,
			`insert into notifications (workshop_id, participant_id, kind, idempotency_key, to_email, subject, body, html)
			 values ($1, nullif($2, '')::uuid, $3, $4, $5, $6, $7, $8) on conflict (idempotency_key) do nothing returning id`,
			n.WorkshopID, n.ParticipantID, n.Kind, n.Key, n.To, n.Subject, n.Body, n.HTML).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return wrap("insert notification", err)
		}
		if _, err := tx.Exec(ctx,
			`insert into jobs (kind, idempotency_key, workshop_id, payload, run_at) values ($1, $2, $3, jsonb_build_object('notification_id', $4::text), $5)
			 on conflict (idempotency_key) do nothing`, KindNotify, n.Key, n.WorkshopID, id, now); err != nil {
			return wrap("enqueue notify", err)
		}
		created = true
		return nil
	})
	return created, err
}

func (s *Store) NotificationByKey(ctx context.Context, key string) (*NotificationRow, error) {
	return oneOrNil[NotificationRow](ctx, s.Pool, "notification by key",
		`select `+notificationCols+` from notifications where idempotency_key = $1`, key)
}

func (s *Store) MarkNotificationSent(ctx context.Context, key, providerID string, now time.Time, clearBody bool) error {
	if _, err := s.Pool.Exec(ctx,
		`update notifications set status = 'sent', provider_message_id = $2, sent_at = $3,
			body = case when $4 then '' else body end, html = case when $4 then '' else html end, updated_at = now()
		 where idempotency_key = $1`, key, providerID, now, clearBody); err != nil {
		return wrap("mark notification sent", err)
	}
	return nil
}

func (s *Store) MarkNotificationSkipped(ctx context.Context, key string) error {
	if _, err := s.Pool.Exec(ctx,
		`update notifications set status = 'skipped', body = '', html = '', updated_at = now() where idempotency_key = $1`, key); err != nil {
		return wrap("mark notification skipped", err)
	}
	return nil
}

func (s *Store) MarkNotificationFailed(ctx context.Context, key string) error {
	if _, err := s.Pool.Exec(ctx,
		`update notifications set status = 'failed', body = '', html = '', updated_at = now() where idempotency_key = $1`, key); err != nil {
		return wrap("mark notification failed", err)
	}
	return nil
}
