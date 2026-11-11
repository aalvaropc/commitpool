package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/aalvaropc/commitpool/internal/domain"
)

const (
	SourceLink = "link"
	SourceChat = "chat"
)

type ParticipantRow struct {
	ID, WorkshopID, DisplayName string
	Source                      string
	ChatMessage                 *string
	ChatLinkToken               *string
}

type AvailabilityRow struct {
	ID, ParticipantID, RawMessage string
	Interpretation, Corrected     json.RawMessage
	NeedsClarification            bool
	ModelMeta                     json.RawMessage
	CreatedAt                     time.Time
}

const (
	participantCols  = `id, workshop_id, display_name, source, chat_message, chat_link_token`
	availabilityCols = `id, participant_id, raw_message, interpretation, corrected, needs_clarification, model_meta, created_at`
)

func (s *Store) CreateParticipant(ctx context.Context, workshopID, displayName string) (ParticipantRow, string, error) {
	return s.CreateParticipantFrom(ctx, workshopID, displayName, SourceLink, "")
}

func (s *Store) CreateParticipantFrom(ctx context.Context, workshopID, displayName, source, chatMessage string) (ParticipantRow, string, error) {
	token, hash, err := NewToken()
	if err != nil {
		return ParticipantRow{}, "", err
	}
	var link *string
	if source == SourceChat {
		link = &token
	}
	p, err := one[ParticipantRow](ctx, s.Pool,
		`insert into participants (workshop_id, display_name, access_token_hash, source, chat_message, chat_link_token)
		values ($1, $2, $3, $4, nullif($5, ''), $6) returning `+participantCols,
		workshopID, displayName, hash, source, chatMessage, link)
	if err != nil {
		return ParticipantRow{}, "", wrap("insert participant", err)
	}
	return p, token, nil
}

func (s *Store) RemoveChatParticipant(ctx context.Context, workshopID, participantID string) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		status, err := lockWorkshop(ctx, tx, workshopID)
		if errors.Is(err, ErrNotFound) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		if status != domain.WorkshopCollecting {
			return ErrConflict
		}
		var id string
		err = tx.QueryRow(ctx,
			`select p.id from participants p
			where p.id = $1 and p.workshop_id = $2 and p.source = 'chat'
			and not exists (select 1 from enrollments e where e.participant_id = p.id)
			and not exists (select 1 from notifications n where n.participant_id = p.id)
			for update`, participantID, workshopID).Scan(&id)
		if isMissing(err) {
			return ErrConflict
		}
		if err != nil {
			return wrap("find chat participant", err)
		}
		if _, err := tx.Exec(ctx, `delete from availabilities where participant_id = $1`, id); err != nil {
			return wrap("delete availabilities", err)
		}
		if _, err := tx.Exec(ctx, `delete from participants where id = $1`, id); err != nil {
			return wrap("delete participant", err)
		}
		return nil
	})
}

func (s *Store) ParticipantByToken(ctx context.Context, token string) (ParticipantRow, error) {
	p, err := one[ParticipantRow](ctx, s.Pool,
		`select `+participantCols+` from participants where access_token_hash = $1`, HashToken(token))
	if err != nil {
		return ParticipantRow{}, lookupErr("participant by token", err)
	}
	return p, nil
}

func (s *Store) ParticipantsByWorkshop(ctx context.Context, workshopID string) ([]ParticipantRow, error) {
	return many[ParticipantRow](ctx, s.Pool, "participants",
		`select `+participantCols+` from participants where workshop_id = $1 order by created_at, id`, workshopID)
}

type DemoLinkRow struct{ DisplayName, Token string }

func (s *Store) KeepDemoLink(ctx context.Context, token string) (bool, error) {
	tag, err := s.Pool.Exec(ctx,
		`update participants p set demo_link_token = $1
		from workshops w
		where p.access_token_hash = $2 and w.id = p.workshop_id and w.is_demo`,
		token, HashToken(token))
	if err != nil {
		return false, wrap("keep demo link", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) DemoLinks(ctx context.Context, workshopID string) ([]DemoLinkRow, error) {
	return many[DemoLinkRow](ctx, s.Pool, "demo links",
		`select p.display_name, p.demo_link_token
		from participants p join workshops w on w.id = p.workshop_id
		where p.workshop_id = $1 and w.is_demo and p.demo_link_token is not null
		order by p.created_at, p.id`, workshopID)
}

func (s *Store) SaveAvailability(ctx context.Context, a AvailabilityRow) (AvailabilityRow, error) {
	saved, err := one[AvailabilityRow](ctx, s.Pool,
		`insert into availabilities (participant_id, raw_message, interpretation, corrected, needs_clarification, model_meta)
		values ($1, $2, $3, $4, $5, coalesce($6::jsonb, '{}'::jsonb))
		returning `+availabilityCols,
		a.ParticipantID, a.RawMessage, a.Interpretation, a.Corrected, a.NeedsClarification, a.ModelMeta)
	if err != nil {
		return AvailabilityRow{}, wrap("insert availability", err)
	}
	return saved, nil
}

func (s *Store) CorrectAvailability(ctx context.Context, id, participantID string, corrected json.RawMessage) error {
	tag, err := s.Pool.Exec(ctx,
		`update availabilities set corrected = $3 where id = $1 and participant_id = $2`,
		id, participantID, corrected)
	if err != nil {
		return lookupErr("correct availability", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) LatestAvailabilityByParticipant(ctx context.Context, participantID string) (*AvailabilityRow, error) {
	return oneOrNil[AvailabilityRow](ctx, s.Pool, "latest availability",
		`select `+availabilityCols+` from availabilities where participant_id = $1 order by created_at desc, id desc limit 1`,
		participantID)
}

func (s *Store) RecentAvailabilitiesByParticipant(ctx context.Context, participantID string, limit int) ([]AvailabilityRow, error) {
	return many[AvailabilityRow](ctx, s.Pool, "recent availabilities",
		`select `+availabilityCols+` from availabilities where participant_id = $1 order by created_at desc, id desc limit $2`,
		participantID, limit)
}

func (s *Store) AvailabilitiesByWorkshop(ctx context.Context, workshopID string) ([]AvailabilityRow, error) {
	return many[AvailabilityRow](ctx, s.Pool, "availabilities by workshop",
		`select `+availabilityCols+` from (
			select distinct on (a.participant_id) a.*
			from availabilities a join participants p on p.id = a.participant_id
			where p.workshop_id = $1
			order by a.participant_id, a.created_at desc, a.id desc
		) latest order by created_at, id`, workshopID)
}

func (s *Store) CountParticipants(ctx context.Context, workshopID string) (int, error) {
	var n int
	if err := s.Pool.QueryRow(ctx, `select count(*) from participants where workshop_id = $1`, workshopID).Scan(&n); err != nil {
		return 0, wrap("count participants", err)
	}
	return n, nil
}

func (s *Store) CountAvailabilities(ctx context.Context, participantID string) (int, error) {
	var n int
	if err := s.Pool.QueryRow(ctx, `select count(*) from availabilities where participant_id = $1`, participantID).Scan(&n); err != nil {
		return 0, wrap("count availabilities", err)
	}
	return n, nil
}
