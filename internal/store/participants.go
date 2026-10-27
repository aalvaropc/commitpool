package store

import (
	"context"
	"encoding/json"
	"time"
)

type ParticipantRow struct{ ID, WorkshopID, DisplayName string }

type AvailabilityRow struct {
	ID, ParticipantID, RawMessage string
	Interpretation, Corrected     json.RawMessage
	NeedsClarification            bool
	ModelMeta                     json.RawMessage
	CreatedAt                     time.Time
}

const (
	participantCols  = `id, workshop_id, display_name`
	availabilityCols = `id, participant_id, raw_message, interpretation, corrected, needs_clarification, model_meta, created_at`
)

// CreateParticipant joins a workshop and returns the participant's access
// token in plaintext; only its hash is stored.
func (s *Store) CreateParticipant(ctx context.Context, workshopID, displayName string) (ParticipantRow, string, error) {
	token, hash, err := NewToken()
	if err != nil {
		return ParticipantRow{}, "", err
	}
	p, err := one[ParticipantRow](ctx, s.Pool,
		`insert into participants (workshop_id, display_name, access_token_hash) values ($1, $2, $3) returning `+participantCols,
		workshopID, displayName, hash)
	if err != nil {
		return ParticipantRow{}, "", wrap("insert participant", err)
	}
	return p, token, nil
}

func (s *Store) ParticipantByToken(ctx context.Context, token string) (ParticipantRow, error) {
	p, err := one[ParticipantRow](ctx, s.Pool,
		`select `+participantCols+` from participants where access_token_hash = $1`, HashToken(token))
	if err != nil {
		return ParticipantRow{}, lookupErr("participant by token", err)
	}
	return p, nil
}

// ParticipantsByWorkshop returns the workshop's participants in join order.
func (s *Store) ParticipantsByWorkshop(ctx context.Context, workshopID string) ([]ParticipantRow, error) {
	return many[ParticipantRow](ctx, s.Pool, "participants",
		`select `+participantCols+` from participants where workshop_id = $1 order by created_at, id`, workshopID)
}

// SaveAvailability stores one availability message and its interpretation. An
// empty ModelMeta is stored as {}.
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

// CorrectAvailability stores the participant's own correction of an
// interpretation. It fails with ErrNotFound unless the availability belongs to
// that participant.
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

// LatestAvailabilityByParticipant returns the participant's most recent
// availability, or nil when they have not sent one.
func (s *Store) LatestAvailabilityByParticipant(ctx context.Context, participantID string) (*AvailabilityRow, error) {
	return oneOrNil[AvailabilityRow](ctx, s.Pool, "latest availability",
		`select `+availabilityCols+` from availabilities where participant_id = $1 order by created_at desc, id desc limit 1`,
		participantID)
}

// AvailabilitiesByWorkshop returns the most recent availability of each
// participant of the workshop, oldest first.
func (s *Store) AvailabilitiesByWorkshop(ctx context.Context, workshopID string) ([]AvailabilityRow, error) {
	return many[AvailabilityRow](ctx, s.Pool, "availabilities by workshop",
		`select `+availabilityCols+` from (
			select distinct on (a.participant_id) a.*
			from availabilities a join participants p on p.id = a.participant_id
			where p.workshop_id = $1
			order by a.participant_id, a.created_at desc, a.id desc
		) latest order by created_at, id`, workshopID)
}
