package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

func TestWorkshop_CreateAndLookupByTokens(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)

	w, owner, share := newWorkshop(t, s, 3, 6)
	if w.ID == "" || owner == "" || share == "" || owner == share {
		t.Fatalf("CreateWorkshop returned id=%q owner=%q share=%q", w.ID, owner, share)
	}
	if w.Status != domain.WorkshopCollecting || w.Resolution != domain.ResolutionOpen {
		t.Errorf("new workshop is %s/%s, want COLLECTING/OPEN", w.Status, w.Resolution)
	}
	if w.Title != "Go for operators" || w.Timezone != "America/Lima" || w.Minimum != 3 || w.Capacity != 6 ||
		w.PriceMinor != 1000 || w.Currency != "USD" || w.PaymentWindowMin != 1440 || w.DurationMin != 180 {
		t.Errorf("CreateWorkshop row = %+v", w)
	}
	if w.CreatedAt.IsZero() {
		t.Errorf("CreatedAt is zero")
	}

	byOwner, err := s.WorkshopByOwnerToken(ctx, owner)
	if err != nil {
		t.Fatalf("WorkshopByOwnerToken: %v", err)
	}
	byShare, err := s.WorkshopByShareToken(ctx, share)
	if err != nil {
		t.Fatalf("WorkshopByShareToken: %v", err)
	}
	byID, err := s.WorkshopByID(ctx, w.ID)
	if err != nil {
		t.Fatalf("WorkshopByID: %v", err)
	}
	for name, got := range map[string]store.WorkshopRow{"owner": byOwner, "share": byShare, "id": byID} {
		if got.ID != w.ID || got.Title != w.Title || got.Status != w.Status || !got.CreatedAt.Equal(w.CreatedAt) {
			t.Errorf("lookup by %s = %+v, want the created row %+v", name, got, w)
		}
	}

	if _, err := s.WorkshopByOwnerToken(ctx, share); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("WorkshopByOwnerToken(share token) error = %v, want ErrNotFound", err)
	}
	if _, err := s.WorkshopByShareToken(ctx, owner); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("WorkshopByShareToken(owner token) error = %v, want ErrNotFound", err)
	}

	dates, err := s.CandidateDates(ctx, w.ID)
	if err != nil {
		t.Fatalf("CandidateDates: %v", err)
	}
	if len(dates) != 2 {
		t.Fatalf("CandidateDates returned %d rows, want 2", len(dates))
	}
	if !dates[0].StartsAt.Equal(friday) || !dates[1].StartsAt.Equal(saturday) {
		t.Errorf("CandidateDates not ordered by starts_at: %v then %v", dates[0].StartsAt, dates[1].StartsAt)
	}
	for _, d := range dates {
		if d.WorkshopID != w.ID || d.ID == "" || !d.EndsAt.Equal(d.StartsAt.Add(3*time.Hour)) {
			t.Errorf("candidate date = %+v", d)
		}
	}
}

func TestWorkshop_StoresOnlyTokenHashes(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)

	w, owner, share := newWorkshop(t, s, 1, 2)
	var ownerHash, shareHash []byte
	err := s.Pool.QueryRow(ctx, `select owner_token_hash, share_token_hash from workshops where id = $1`, w.ID).Scan(&ownerHash, &shareHash)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ownerHash, store.HashToken(owner)) || !bytes.Equal(shareHash, store.HashToken(share)) {
		t.Errorf("stored hashes are not the SHA-256 of the returned tokens")
	}
	if bytes.Contains(ownerHash, []byte(owner)) || bytes.Contains(shareHash, []byte(share)) {
		t.Errorf("a plaintext token is stored in the database")
	}
}

func TestWorkshop_LookupFailuresAreNotFound(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)

	for _, id := range []string{"00000000-0000-4000-8000-000000000000", "not-a-uuid", ""} {
		if _, err := s.WorkshopByID(ctx, id); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("WorkshopByID(%q) error = %v, want ErrNotFound", id, err)
		}
	}
	if _, err := s.WorkshopByOwnerToken(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("WorkshopByOwnerToken(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestWorkshop_SetStatus(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 1, 2)

	if err := s.SetWorkshopStatus(ctx, w.ID, domain.WorkshopCancelled, domain.ResolutionResolving); err != nil {
		t.Fatalf("SetWorkshopStatus: %v", err)
	}
	got, err := s.WorkshopByID(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.WorkshopCancelled || got.Resolution != domain.ResolutionResolving {
		t.Errorf("workshop is %s/%s, want CANCELLED/RESOLVING", got.Status, got.Resolution)
	}

	if err := s.SetWorkshopStatus(ctx, "00000000-0000-4000-8000-000000000000", domain.WorkshopCancelled, domain.ResolutionOpen); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("SetWorkshopStatus(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestWorkshop_CreateAppliesDefaultPaymentWindow(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)

	w, _, _, err := s.CreateWorkshop(ctx, store.CreateWorkshopParams{
		Title: "Defaults", DurationMin: 60, Timezone: "UTC", Minimum: 1, Capacity: 1, PriceMinor: 500, Currency: "USD",
		Dates: []dateWindow{{StartsAt: saturday, EndsAt: saturday.Add(time.Hour)}},
	})
	if err != nil {
		t.Fatalf("CreateWorkshop without a payment window: %v", err)
	}
	if w.PaymentWindowMin != 1440 {
		t.Errorf("PaymentWindowMin = %d, want the 1440 default", w.PaymentWindowMin)
	}
}

func TestWorkshop_CreateIsAtomic(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)

	_, _, _, err := s.CreateWorkshop(ctx, store.CreateWorkshopParams{
		Title: "Broken", DurationMin: 60, Timezone: "UTC", Minimum: 1, Capacity: 1, PriceMinor: 500, Currency: "USD",
		Dates: []dateWindow{{StartsAt: saturday, EndsAt: saturday.Add(-time.Hour)}},
	})
	if err == nil {
		t.Fatal("CreateWorkshop with ends_at before starts_at succeeded")
	}
	if n := count(t, s, `select count(*) from workshops`); n != 0 {
		t.Errorf("%d workshops left behind by a failed CreateWorkshop, want 0", n)
	}
}

func TestOpenProposal_SetsFundingAndRefusesSecond(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 3, 6)

	if p, err := s.ActiveProposal(ctx, w.ID); err != nil || p != nil {
		t.Fatalf("ActiveProposal before opening = %+v, %v; want nil, nil", p, err)
	}

	opened := openFunding(t, s, w, baseTime)
	if opened.ID == "" || opened.Version != 1 || opened.WorkshopID != w.ID {
		t.Errorf("OpenProposal row = %+v", opened)
	}
	if opened.Minimum != 3 || opened.Capacity != 6 || opened.PriceMinor != 1000 || opened.Currency != "USD" {
		t.Errorf("proposal froze minimum/capacity/price as %d/%d/%d %s", opened.Minimum, opened.Capacity, opened.PriceMinor, opened.Currency)
	}
	if !opened.OpensAt.Equal(baseTime) || !opened.ClosesAt.Equal(baseTime.Add(24*time.Hour)) || !opened.StartsAt.Equal(friday) {
		t.Errorf("proposal times = %v / %v / %v", opened.OpensAt, opened.ClosesAt, opened.StartsAt)
	}

	got, err := s.WorkshopByID(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.WorkshopFunding {
		t.Errorf("workshop status after OpenProposal = %s, want FUNDING", got.Status)
	}

	active, err := s.ActiveProposal(ctx, w.ID)
	if err != nil || active == nil {
		t.Fatalf("ActiveProposal = %+v, %v", active, err)
	}
	if active.ID != opened.ID || active.Version != 1 || !active.ClosesAt.Equal(opened.ClosesAt) {
		t.Errorf("ActiveProposal = %+v, want %+v", *active, opened)
	}

	dates, _ := s.CandidateDates(ctx, w.ID)
	_, err = s.OpenProposal(ctx, store.ProposalRow{
		WorkshopID: w.ID, ChosenDateID: dates[1].ID, PriceMinor: 1000, Currency: "USD", Minimum: 3, Capacity: 6,
		OpensAt: baseTime, ClosesAt: baseTime.Add(24 * time.Hour), StartsAt: dates[1].StartsAt,
	})
	if !errors.Is(err, store.ErrProposalExists) {
		t.Errorf("second OpenProposal error = %v, want ErrProposalExists", err)
	}
	if n := count(t, s, `select count(*) from proposals where workshop_id = $1`, w.ID); n != 1 {
		t.Errorf("%d proposals after a refused second OpenProposal, want 1", n)
	}
	again, _ := s.ActiveProposal(ctx, w.ID)
	if again == nil || again.ID != opened.ID {
		t.Errorf("the refused OpenProposal replaced the active proposal: %+v", again)
	}
}

func TestOpenProposal_UnknownWorkshop(t *testing.T) {
	s := store.TestStore(t)
	_, err := s.OpenProposal(context.Background(), store.ProposalRow{WorkshopID: "00000000-0000-4000-8000-000000000000"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("OpenProposal(unknown workshop) error = %v, want ErrNotFound", err)
	}
}

func TestProposalByID_ReturnsProposalOrNil(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 2, 4)
	opened := openFunding(t, s, w, baseTime)

	got, err := s.ProposalByID(ctx, opened.ID)
	if err != nil || got == nil {
		t.Fatalf("ProposalByID = %+v, %v; want the proposal", got, err)
	}
	if *got != opened {
		t.Errorf("ProposalByID = %+v, want %+v", *got, opened)
	}
	for _, id := range []string{missingUUID, "not-a-uuid"} {
		if p, err := s.ProposalByID(ctx, id); err != nil || p != nil {
			t.Errorf("ProposalByID(%s) = %+v, %v; want nil, nil", id, p, err)
		}
	}
}

func TestParticipants_CreateAndLookup(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 1, 3)
	other, _, _ := newWorkshop(t, s, 1, 3)

	ana, anaToken, err := s.CreateParticipant(ctx, w.ID, "Ana")
	if err != nil {
		t.Fatalf("CreateParticipant: %v", err)
	}
	if ana.ID == "" || ana.WorkshopID != w.ID || ana.DisplayName != "Ana" || len(anaToken) != 43 {
		t.Errorf("CreateParticipant = %+v token len %d", ana, len(anaToken))
	}
	ben, benToken, err := s.CreateParticipant(ctx, w.ID, "Ben")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateParticipant(ctx, other.ID, "Cy"); err != nil {
		t.Fatal(err)
	}

	got, err := s.ParticipantByToken(ctx, anaToken)
	if err != nil || got != ana {
		t.Errorf("ParticipantByToken(ana) = %+v, %v; want %+v", got, err, ana)
	}
	if got, err := s.ParticipantByToken(ctx, benToken); err != nil || got.ID != ben.ID {
		t.Errorf("ParticipantByToken(ben) = %+v, %v", got, err)
	}
	if _, err := s.ParticipantByToken(ctx, "unknown"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("ParticipantByToken(unknown) error = %v, want ErrNotFound", err)
	}

	var hash []byte
	if err := s.Pool.QueryRow(ctx, `select access_token_hash from participants where id = $1`, ana.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hash, store.HashToken(anaToken)) {
		t.Errorf("stored participant hash is not the SHA-256 of the token")
	}

	list, err := s.ParticipantsByWorkshop(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0] != ana || list[1] != ben {
		t.Errorf("ParticipantsByWorkshop = %+v, want [ana ben] in creation order", list)
	}
	empty, err := s.ParticipantsByWorkshop(ctx, "00000000-0000-4000-8000-000000000000")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("ParticipantsByWorkshop(none) = %#v, %v; want empty non-nil slice", empty, err)
	}
}

func TestAvailability_SaveCorrectAndLatest(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 1, 3)
	ana := newParticipant(t, s, w, "Ana")
	ben := newParticipant(t, s, w, "Ben")

	if a, err := s.LatestAvailabilityByParticipant(ctx, ana.ID); err != nil || a != nil {
		t.Fatalf("LatestAvailabilityByParticipant before saving = %+v, %v; want nil, nil", a, err)
	}

	first, err := s.SaveAvailability(ctx, store.AvailabilityRow{
		ParticipantID: ana.ID, RawMessage: "Fridays are bad", Interpretation: json.RawMessage(`{"per_date":[],"needs_clarification":false}`),
		ModelMeta: json.RawMessage(`{"provider":"fake"}`),
	})
	if err != nil {
		t.Fatalf("SaveAvailability: %v", err)
	}
	if first.ID == "" || first.CreatedAt.IsZero() || first.Corrected != nil || first.NeedsClarification {
		t.Errorf("SaveAvailability row = %+v", first)
	}
	assertJSONEqual(t, "model_meta", first.ModelMeta, json.RawMessage(`{"provider":"fake"}`))

	bare, err := s.SaveAvailability(ctx, store.AvailabilityRow{
		ParticipantID: ben.ID, RawMessage: "any time", Interpretation: json.RawMessage(`{"per_date":[]}`), NeedsClarification: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, "default model_meta", bare.ModelMeta, json.RawMessage(`{}`))
	if !bare.NeedsClarification {
		t.Errorf("NeedsClarification was not stored")
	}

	second, err := s.SaveAvailability(ctx, store.AvailabilityRow{
		ParticipantID: ana.ID, RawMessage: "Saturday only", Interpretation: json.RawMessage(`{"per_date":[{"verdict":"available"}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	latest, err := s.LatestAvailabilityByParticipant(ctx, ana.ID)
	if err != nil || latest == nil || latest.ID != second.ID || latest.RawMessage != "Saturday only" {
		t.Fatalf("LatestAvailabilityByParticipant = %+v, %v; want the second message", latest, err)
	}

	if err := s.CorrectAvailability(ctx, second.ID, ben.ID, json.RawMessage(`{"per_date":[]}`)); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("CorrectAvailability by another participant error = %v, want ErrNotFound", err)
	}
	if err := s.CorrectAvailability(ctx, second.ID, ana.ID, json.RawMessage(`{"per_date":[{"verdict":"unavailable"}]}`)); err != nil {
		t.Fatalf("CorrectAvailability: %v", err)
	}
	latest, _ = s.LatestAvailabilityByParticipant(ctx, ana.ID)
	assertJSONEqual(t, "corrected", latest.Corrected, json.RawMessage(`{"per_date":[{"verdict":"unavailable"}]}`))
	assertJSONEqual(t, "interpretation untouched", latest.Interpretation, json.RawMessage(`{"per_date":[{"verdict":"available"}]}`))

	all, err := s.AvailabilitiesByWorkshop(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("AvailabilitiesByWorkshop returned %d rows, want the latest of each of 2 participants", len(all))
	}
	byParticipant := map[string]store.AvailabilityRow{}
	for _, a := range all {
		byParticipant[a.ParticipantID] = a
	}
	if byParticipant[ana.ID].ID != second.ID || byParticipant[ben.ID].ID != bare.ID {
		t.Errorf("AvailabilitiesByWorkshop returned %v, want ana's second message and ben's only message", all)
	}
}

func TestTransitionWorkshop_RefusesWhenStateMoved(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 1, 2)
	openFunding(t, s, w, baseTime)
	funding := []domain.WorkshopStatus{domain.WorkshopFunding}
	want := func(status domain.WorkshopStatus, resolution domain.Resolution) {
		t.Helper()
		got, err := s.WorkshopByID(ctx, w.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != status || got.Resolution != resolution {
			t.Fatalf("workshop is %s/%s, want %s/%s", got.Status, got.Resolution, status, resolution)
		}
	}

	if ok, err := s.TransitionWorkshop(ctx, w.ID, funding, nil, domain.WorkshopConfirmed, domain.ResolutionOpen); err != nil || !ok {
		t.Fatalf("TransitionWorkshop(FUNDING -> CONFIRMED) = %v, %v; want true", ok, err)
	}
	want(domain.WorkshopConfirmed, domain.ResolutionOpen)
	if ok, err := s.TransitionWorkshop(ctx, w.ID, funding, nil, domain.WorkshopCancelled, domain.ResolutionResolving); err != nil || ok {
		t.Fatalf("TransitionWorkshop(FUNDING -> CANCELLED) on a CONFIRMED workshop = %v, %v; want false", ok, err)
	}
	want(domain.WorkshopConfirmed, domain.ResolutionOpen)

	open := []domain.Resolution{domain.ResolutionOpen}
	if ok, err := s.TransitionWorkshop(ctx, w.ID, nil, open, domain.WorkshopConfirmed, domain.ResolutionNeedsAttention); err != nil || !ok {
		t.Fatalf("TransitionWorkshop(any/OPEN -> NEEDS_ATTENTION) = %v, %v; want true", ok, err)
	}
	if ok, err := s.TransitionWorkshop(ctx, w.ID, nil, open, domain.WorkshopConfirmed, domain.ResolutionSettled); err != nil || ok {
		t.Fatalf("second TransitionWorkshop(any/OPEN) = %v, %v; want false", ok, err)
	}
	want(domain.WorkshopConfirmed, domain.ResolutionNeedsAttention)

	for _, id := range []string{"00000000-0000-4000-8000-000000000000", "not-a-uuid"} {
		if ok, err := s.TransitionWorkshop(ctx, id, nil, nil, domain.WorkshopCancelled, domain.ResolutionOpen); err != nil || ok {
			t.Errorf("TransitionWorkshop(%q) = %v, %v; want false, nil", id, ok, err)
		}
	}
}

func TestShortenPaymentWindow_OnlyMovesTheCloseEarlier(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 1, 3)
	p := openFunding(t, s, w, baseTime)

	now := baseTime.Add(2 * time.Hour)
	got, err := s.ShortenPaymentWindow(ctx, p.ID, now)
	if err != nil || got.ID != p.ID || !got.ClosesAt.Equal(now) {
		t.Fatalf("ShortenPaymentWindow(now) = %+v, %v; want closes_at %s", got, err, now)
	}
	if again, err := s.ShortenPaymentWindow(ctx, p.ID, now.Add(time.Hour)); err != nil || !again.ClosesAt.Equal(now) {
		t.Errorf("ShortenPaymentWindow(later) = %+v, %v; want closes_at kept at %s", again, err, now)
	}
	if active, err := s.ActiveProposal(ctx, w.ID); err != nil || active == nil || !active.ClosesAt.Equal(now) {
		t.Errorf("ActiveProposal after shortening = %+v, %v", active, err)
	}
	if _, err := s.ShortenPaymentWindow(ctx, missingUUID, now); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("ShortenPaymentWindow(unknown) error = %v, want ErrNotFound", err)
	}
}
