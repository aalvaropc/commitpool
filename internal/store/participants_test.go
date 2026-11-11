package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aalvaropc/commitpool/internal/store"
	"github.com/aalvaropc/commitpool/internal/store/storetest"
)

func TestCreateParticipantFrom_StoresSourceAndMessage(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	w, _, _ := newWorkshop(t, s, 1, 3)

	ana := newParticipant(t, s, w, "Ana")
	if ana.Source != store.SourceLink || ana.ChatMessage != nil || ana.ChatLinkToken != nil {
		t.Errorf("a participant who joined by link = %+v, want source link and no chat fields", ana)
	}

	said := "El sábado no puedo,\npero el domingo me viene perfecto."
	lucia, token, err := s.CreateParticipantFrom(ctx, w.ID, "Lucía Torres", store.SourceChat, said)
	if err != nil {
		t.Fatalf("CreateParticipantFrom: %v", err)
	}
	if lucia.Source != store.SourceChat || lucia.ChatMessage == nil || *lucia.ChatMessage != said {
		t.Errorf("chat participant = %+v, want source chat and the message", lucia)
	}
	if lucia.ChatLinkToken == nil || *lucia.ChatLinkToken != token {
		t.Errorf("chat participant link = %v, want the token %s kept for the organizer", lucia.ChatLinkToken, token)
	}
	byToken, err := s.ParticipantByToken(ctx, token)
	if err != nil || byToken.ID != lucia.ID || byToken.Source != store.SourceChat || *byToken.ChatMessage != said {
		t.Errorf("ParticipantByToken = %+v, %v", byToken, err)
	}
	all, err := s.ParticipantsByWorkshop(ctx, w.ID)
	if err != nil || len(all) != 2 || all[0].Source != store.SourceLink || all[1].Source != store.SourceChat {
		t.Errorf("ParticipantsByWorkshop = %+v, %v", all, err)
	}

	if _, _, err := s.CreateParticipantFrom(ctx, w.ID, "Ben", "email", ""); err == nil {
		t.Error("an unknown source was stored")
	}
	if _, _, err := s.CreateParticipantFrom(ctx, w.ID, "Ben", store.SourceChat, strings.Repeat("é", 2001)); err == nil {
		t.Error("a chat message over 2000 characters was stored")
	}
	if _, _, err := s.CreateParticipantFrom(ctx, w.ID, "Ben", store.SourceChat, strings.Repeat("é", 2000)); err != nil {
		t.Errorf("a chat message of 2000 characters was refused: %v", err)
	}
}

func TestRemoveChatParticipant_OnlyChatWhileCollectingWithoutEnrollment(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	w, _, _ := newWorkshop(t, s, 1, 3)
	ana := newParticipant(t, s, w, "Ana")
	ben, _, err := s.CreateParticipantFrom(ctx, w.ID, "Ben", store.SourceChat, "Saturday works")
	if err != nil {
		t.Fatal(err)
	}
	cleo, _, err := s.CreateParticipantFrom(ctx, w.ID, "Cleo", store.SourceChat, "Sunday works")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveAvailability(ctx, store.AvailabilityRow{ParticipantID: ben.ID, RawMessage: "Saturday works", Interpretation: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	other, _, _ := newWorkshop(t, s, 1, 3)

	refused := []struct {
		name               string
		workshop, particip string
	}{
		{"a link participant", w.ID, ana.ID},
		{"someone from another workshop", other.ID, ben.ID},
		{"an unknown id", w.ID, "00000000-0000-0000-0000-000000000000"},
		{"an id that is not a uuid", w.ID, "not-a-uuid"},
	}
	for _, tc := range refused {
		if err := s.RemoveChatParticipant(ctx, tc.workshop, tc.particip); !errors.Is(err, store.ErrConflict) {
			t.Errorf("removing %s = %v, want ErrConflict", tc.name, err)
		}
	}

	if err := s.RemoveChatParticipant(ctx, w.ID, ben.ID); err != nil {
		t.Fatalf("removing a chat participant with a reading: %v", err)
	}
	if left, err := s.ParticipantsByWorkshop(ctx, w.ID); err != nil || len(left) != 2 {
		t.Errorf("after the removal: %+v, %v", left, err)
	}

	p := openFunding(t, s, w, baseTime)
	if err := s.RemoveChatParticipant(ctx, w.ID, cleo.ID); !errors.Is(err, store.ErrConflict) {
		t.Errorf("removing while reservations are open = %v, want ErrConflict", err)
	}
	reserve(t, s, w, p, cleo.ID, baseTime)
	if _, err := s.Pool.Exec(ctx, `update workshops set status = 'COLLECTING' where id = $1`, w.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveChatParticipant(ctx, w.ID, cleo.ID); !errors.Is(err, store.ErrConflict) {
		t.Errorf("removing someone with an enrollment = %v, want ErrConflict", err)
	}
}
