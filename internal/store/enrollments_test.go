package store_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

func TestReserveSeat_ConcurrentRequestsNeverExceedCapacity(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 3, 6)
	p := openFunding(t, s, w, baseTime)

	const requests = 20
	var participants []string
	for _, name := range participantNames(requests) {
		participants = append(participants, newParticipant(t, s, w, name).ID)
	}

	type outcome struct {
		created bool
		err     error
	}
	outcomes := make([]outcome, requests)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, created, err := s.ReserveSeatAndCreateEnrollment(ctx, reserveParams(w, p, participants[i], baseTime.Add(time.Hour)))
			outcomes[i] = outcome{created: created, err: err}
		}()
	}
	close(start)
	wg.Wait()

	var created, full int
	for i, o := range outcomes {
		switch {
		case o.err == nil && o.created:
			created++
		case errors.Is(o.err, domain.ErrCapacityFull):
			full++
		default:
			t.Errorf("request %d: created=%v err=%v; want created or ErrCapacityFull", i, o.created, o.err)
		}
	}
	if created != 6 || full != 14 {
		t.Errorf("created %d and refused %d requests, want 6 created and 14 ErrCapacityFull", created, full)
	}
	if n := count(t, s, `select count(*) from enrollments where workshop_id = $1`, w.ID); n != 6 {
		t.Errorf("count(enrollments) = %d, want 6", n)
	}
}

func TestReserveSeat_ConcurrentRequestsFromOneParticipantCreateOneEnrollment(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 1, 6)
	p := openFunding(t, s, w, baseTime)
	ana := newParticipant(t, s, w, "Ana")

	const tabs = 6
	type outcome struct {
		id      string
		created bool
		err     error
	}
	outcomes := make([]outcome, tabs)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range tabs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			e, created, err := s.ReserveSeatAndCreateEnrollment(ctx, reserveParams(w, p, ana.ID, baseTime.Add(time.Hour)))
			outcomes[i] = outcome{id: e.ID, created: created, err: err}
		}()
	}
	close(start)
	wg.Wait()

	created := 0
	for i, o := range outcomes {
		if o.err != nil {
			t.Fatalf("request %d: %v", i, o.err)
		}
		if o.created {
			created++
		}
		if o.id != outcomes[0].id {
			t.Errorf("request %d got enrollment %s, request 0 got %s", i, o.id, outcomes[0].id)
		}
	}
	if created != 1 {
		t.Errorf("%d requests created an enrollment, want exactly 1", created)
	}
	if n := count(t, s, `select count(*) from enrollments`); n != 1 {
		t.Errorf("count(enrollments) = %d, want 1", n)
	}
}

func TestReserveSeat_SecondActiveEnrollmentReturnsExisting(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 1, 3)
	p := openFunding(t, s, w, baseTime)
	ana := newParticipant(t, s, w, "Ana")
	now := baseTime.Add(time.Hour)

	first, created, err := s.ReserveSeatAndCreateEnrollment(ctx, reserveParams(w, p, ana.ID, now))
	if err != nil || !created {
		t.Fatalf("first reservation = %+v, created=%v, err=%v", first, created, err)
	}
	second, created, err := s.ReserveSeatAndCreateEnrollment(ctx, reserveParams(w, p, ana.ID, now.Add(time.Minute)))
	if err != nil {
		t.Fatalf("second reservation: %v", err)
	}
	if created {
		t.Errorf("second reservation reported created = true")
	}
	if second.ID != first.ID || second.Status != domain.EnrollmentSeatReserved || !second.SeatExpiresAt.Equal(first.SeatExpiresAt) {
		t.Errorf("second reservation = %+v, want the first enrollment %+v untouched", second, first)
	}
	if n := count(t, s, `select count(*) from enrollments`); n != 1 {
		t.Errorf("count(enrollments) = %d, want 1", n)
	}

	got, err := s.ActiveEnrollmentByParticipant(ctx, ana.ID)
	if err != nil || got == nil || got.ID != first.ID {
		t.Errorf("ActiveEnrollmentByParticipant = %+v, %v; want %s", got, err, first.ID)
	}
}

func TestReserveSeat_RefusesWhenNotFunding(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)

	t.Run("collecting", func(t *testing.T) {
		w, _, _ := newWorkshop(t, s, 1, 3)
		ana := newParticipant(t, s, w, "Ana")
		_, created, err := s.ReserveSeatAndCreateEnrollment(ctx, store.ReserveParams{
			WorkshopID: w.ID, ParticipantID: ana.ID, ProposalID: "00000000-0000-4000-8000-000000000000",
			ConsentVersion: "consent-v1", Now: baseTime, SeatTTL: time.Minute,
		})
		if !errors.Is(err, store.ErrProposalNotOpen) || created {
			t.Errorf("COLLECTING workshop: created=%v err=%v; want ErrProposalNotOpen", created, err)
		}
	})

	t.Run("window closed", func(t *testing.T) {
		w, _, _ := newWorkshop(t, s, 1, 3)
		p := openFunding(t, s, w, baseTime)
		ana := newParticipant(t, s, w, "Ana")

		for name, now := range map[string]time.Time{
			"after closes_at": p.ClosesAt.Add(time.Second),
			"at closes_at":    p.ClosesAt,
		} {
			_, created, err := s.ReserveSeatAndCreateEnrollment(ctx, reserveParams(w, p, ana.ID, now))
			if !errors.Is(err, store.ErrProposalNotOpen) || created {
				t.Errorf("%s: created=%v err=%v; want ErrProposalNotOpen", name, created, err)
			}
		}
		if n := count(t, s, `select count(*) from enrollments where workshop_id = $1`, w.ID); n != 0 {
			t.Errorf("%d enrollments created while payments were closed, want 0", n)
		}

		if _, created, err := s.ReserveSeatAndCreateEnrollment(ctx, reserveParams(w, p, ana.ID, p.ClosesAt.Add(-time.Second))); err != nil || !created {
			t.Errorf("one second before closes_at: created=%v err=%v; want a new enrollment", created, err)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		w, _, _ := newWorkshop(t, s, 1, 3)
		p := openFunding(t, s, w, baseTime)
		ana := newParticipant(t, s, w, "Ana")
		if err := s.SetWorkshopStatus(ctx, w.ID, domain.WorkshopCancelled, domain.ResolutionResolving); err != nil {
			t.Fatal(err)
		}
		_, created, err := s.ReserveSeatAndCreateEnrollment(ctx, reserveParams(w, p, ana.ID, baseTime.Add(time.Hour)))
		if !errors.Is(err, store.ErrProposalNotOpen) || created {
			t.Errorf("CANCELLED workshop: created=%v err=%v; want ErrProposalNotOpen", created, err)
		}
	})

	t.Run("proposal of another workshop", func(t *testing.T) {
		w, _, _ := newWorkshop(t, s, 1, 3)
		openFunding(t, s, w, baseTime)
		other, _, _ := newWorkshop(t, s, 1, 3)
		foreign := openFunding(t, s, other, baseTime)
		ana := newParticipant(t, s, w, "Ana")

		_, created, err := s.ReserveSeatAndCreateEnrollment(ctx, reserveParams(w, foreign, ana.ID, baseTime.Add(time.Hour)))
		if !errors.Is(err, store.ErrProposalNotOpen) || created {
			t.Errorf("foreign proposal: created=%v err=%v; want ErrProposalNotOpen", created, err)
		}
	})

	t.Run("unknown workshop", func(t *testing.T) {
		_, _, err := s.ReserveSeatAndCreateEnrollment(ctx, store.ReserveParams{WorkshopID: "00000000-0000-4000-8000-000000000000"})
		if !errors.Is(err, store.ErrNotFound) {
			t.Errorf("unknown workshop: err=%v; want ErrNotFound", err)
		}
	})
}

func TestReserveSeat_StoresSeatWindowAndConsent(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 1, 3)
	p := openFunding(t, s, w, baseTime)
	ana := newParticipant(t, s, w, "Ana")
	now := baseTime.Add(90 * time.Minute)

	e, created, err := s.ReserveSeatAndCreateEnrollment(ctx, store.ReserveParams{
		WorkshopID: w.ID, ParticipantID: ana.ID, ProposalID: p.ID, ConsentVersion: "consent-v1", Now: now, SeatTTL: 45 * time.Minute,
	})
	if err != nil || !created {
		t.Fatalf("ReserveSeatAndCreateEnrollment: created=%v err=%v", created, err)
	}
	if e.ID == "" || e.WorkshopID != w.ID || e.ParticipantID != ana.ID || e.ProposalID != p.ID {
		t.Errorf("enrollment ids = %+v", e)
	}
	if e.Status != domain.EnrollmentSeatReserved || e.ConsentVersion != "consent-v1" {
		t.Errorf("status %s, consent version %q", e.Status, e.ConsentVersion)
	}
	if !e.ConsentAt.Equal(now) || !e.SeatReservedAt.Equal(now) || !e.SeatExpiresAt.Equal(now.Add(45*time.Minute)) {
		t.Errorf("consent_at %v, seat_reserved_at %v, seat_expires_at %v; want %v, %v, %v", e.ConsentAt, e.SeatReservedAt, e.SeatExpiresAt, now, now, now.Add(45*time.Minute))
	}
	if e.ApprovalCancelledAt != nil || e.ReleasedAt != nil {
		t.Errorf("approval_cancelled_at %v / released_at %v set on a new enrollment", e.ApprovalCancelledAt, e.ReleasedAt)
	}

	stored, err := s.EnrollmentByID(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID != e.ID || stored.Status != e.Status || !stored.SeatExpiresAt.Equal(e.SeatExpiresAt) || !stored.ConsentAt.Equal(e.ConsentAt) {
		t.Errorf("EnrollmentByID = %+v, want %+v", stored, e)
	}
}

func TestReserveSeat_FreedSeatsCanBeReserved(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 1, 2)
	p := openFunding(t, s, w, baseTime)
	now := baseTime.Add(time.Hour)
	ana, ben, cy, dee := newParticipant(t, s, w, "Ana"), newParticipant(t, s, w, "Ben"), newParticipant(t, s, w, "Cy"), newParticipant(t, s, w, "Dee")

	anaE := reserve(t, s, w, p, ana.ID, now)
	reserve(t, s, w, p, ben.ID, now)
	if _, _, err := s.ReserveSeatAndCreateEnrollment(ctx, reserveParams(w, p, cy.ID, now)); !errors.Is(err, domain.ErrCapacityFull) {
		t.Fatalf("third reservation error = %v, want ErrCapacityFull", err)
	}

	mustTransition(t, s, anaE.ID, []domain.EnrollmentStatus{domain.EnrollmentSeatReserved}, domain.EnrollmentReleased)
	cyE := reserve(t, s, w, p, cy.ID, now)

	mustTransition(t, s, cyE.ID, []domain.EnrollmentStatus{domain.EnrollmentSeatReserved}, domain.EnrollmentAuthorized)
	if _, _, err := s.ReserveSeatAndCreateEnrollment(ctx, reserveParams(w, p, dee.ID, now)); !errors.Is(err, domain.ErrCapacityFull) {
		t.Errorf("an AUTHORIZED enrollment must keep its seat: error = %v, want ErrCapacityFull", err)
	}

	if _, created, err := s.ReserveSeatAndCreateEnrollment(ctx, reserveParams(w, p, ana.ID, now)); !errors.Is(err, domain.ErrCapacityFull) || created {
		t.Errorf("released participant reserving into a full workshop: created=%v err=%v, want ErrCapacityFull", created, err)
	}
}

func TestReserveSeat_ReleasedParticipantGetsANewEnrollment(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 1, 3)
	p := openFunding(t, s, w, baseTime)
	ana := newParticipant(t, s, w, "Ana")
	now := baseTime.Add(time.Hour)

	old := reserve(t, s, w, p, ana.ID, now)
	mustTransition(t, s, old.ID, []domain.EnrollmentStatus{domain.EnrollmentSeatReserved}, domain.EnrollmentReleased)

	if active, err := s.ActiveEnrollmentByParticipant(ctx, ana.ID); err != nil || active != nil {
		t.Fatalf("ActiveEnrollmentByParticipant after release = %+v, %v; want nil, nil", active, err)
	}
	fresh, created, err := s.ReserveSeatAndCreateEnrollment(ctx, reserveParams(w, p, ana.ID, now.Add(time.Minute)))
	if err != nil || !created {
		t.Fatalf("reserving again after release: created=%v err=%v", created, err)
	}
	if fresh.ID == old.ID {
		t.Errorf("released enrollment was reused instead of creating a new one")
	}
}

func TestEnrollment_ActiveMatchesPartialIndex(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 1, 20)
	p := openFunding(t, s, w, baseTime)

	statuses := []domain.EnrollmentStatus{
		domain.EnrollmentSeatReserved, domain.EnrollmentAuthorized, domain.EnrollmentCapturing, domain.EnrollmentCapturePending,
		domain.EnrollmentPaid, domain.EnrollmentCaptureFailed, domain.EnrollmentVoiding, domain.EnrollmentVoided,
		domain.EnrollmentRefunding, domain.EnrollmentRefundPending, domain.EnrollmentRefunded, domain.EnrollmentRefundFailed,
		domain.EnrollmentNeedsAttention, domain.EnrollmentReleased,
	}
	const insert = `insert into enrollments (workshop_id, participant_id, proposal_id, status, consent_at, consent_version, seat_reserved_at, seat_expires_at)
		values ($1, $2, $3, $4, $5, 'consent-v1', $5, $5)`

	for i, status := range statuses {
		who := newParticipant(t, s, w, participantNames(len(statuses))[i])
		if _, err := s.Pool.Exec(ctx, insert, w.ID, who.ID, p.ID, string(status), baseTime); err != nil {
			t.Fatalf("insert %s enrollment: %v", status, err)
		}

		_, err := s.Pool.Exec(ctx, insert, w.ID, who.ID, p.ID, string(domain.EnrollmentSeatReserved), baseTime)
		var pgErr *pgconn.PgError
		blockedByIndex := errors.As(err, &pgErr) && pgErr.Code == "23505"
		if err != nil && !blockedByIndex {
			t.Fatalf("second insert after %s: %v", status, err)
		}
		if wantBlocked := !status.Terminal(); blockedByIndex != wantBlocked {
			t.Errorf("%s: unique index blocks a second enrollment = %v, but Terminal() = %v", status, blockedByIndex, status.Terminal())
		}

		active, err := s.ActiveEnrollmentByParticipant(ctx, who.ID)
		if err != nil {
			t.Fatalf("ActiveEnrollmentByParticipant(%s): %v", status, err)
		}
		switch {
		case status.Terminal() && !blockedByIndex && (active == nil || active.Status != domain.EnrollmentSeatReserved):
			t.Errorf("%s: active enrollment = %+v, want the second SEAT_RESERVED one", status, active)
		case !status.Terminal() && (active == nil || active.Status != status):
			t.Errorf("%s: active enrollment = %+v, want the %s one", status, active, status)
		}
	}
}

func TestEnrollment_Lookups(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 1, 5)
	p := openFunding(t, s, w, baseTime)
	now := baseTime.Add(time.Hour)
	ana, ben := newParticipant(t, s, w, "Ana"), newParticipant(t, s, w, "Ben")

	a := reserve(t, s, w, p, ana.ID, now)
	b := reserve(t, s, w, p, ben.ID, now)

	list, err := s.EnrollmentsByWorkshop(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != a.ID || list[1].ID != b.ID {
		t.Errorf("EnrollmentsByWorkshop = %+v, want [ana ben] in creation order", list)
	}
	if none, err := s.EnrollmentsByWorkshop(ctx, "00000000-0000-4000-8000-000000000000"); err != nil || none == nil || len(none) != 0 {
		t.Errorf("EnrollmentsByWorkshop(none) = %#v, %v; want empty non-nil slice", none, err)
	}

	for _, id := range []string{"00000000-0000-4000-8000-000000000000", "not-a-uuid", ""} {
		if _, err := s.EnrollmentByID(ctx, id); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("EnrollmentByID(%q) error = %v, want ErrNotFound", id, err)
		}
	}
	if got, err := s.ActiveEnrollmentByParticipant(ctx, "00000000-0000-4000-8000-000000000000"); err != nil || got != nil {
		t.Errorf("ActiveEnrollmentByParticipant(unknown) = %+v, %v; want nil, nil", got, err)
	}
}

func TestTransitionEnrollment_CAS(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 1, 3)
	p := openFunding(t, s, w, baseTime)
	ana := newParticipant(t, s, w, "Ana")
	e := reserve(t, s, w, p, ana.ID, baseTime.Add(time.Hour))

	status := func() domain.EnrollmentStatus {
		t.Helper()
		got, err := s.EnrollmentByID(ctx, e.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got.Status
	}

	ok, err := s.TransitionEnrollment(ctx, e.ID, []domain.EnrollmentStatus{domain.EnrollmentAuthorized}, domain.EnrollmentCapturing)
	if err != nil || ok {
		t.Fatalf("transition from the wrong state = %v, %v; want false, nil", ok, err)
	}
	if got := status(); got != domain.EnrollmentSeatReserved {
		t.Fatalf("a refused transition changed the status to %s", got)
	}

	ok, err = s.TransitionEnrollment(ctx, e.ID, []domain.EnrollmentStatus{domain.EnrollmentReleased, domain.EnrollmentSeatReserved}, domain.EnrollmentAuthorized)
	if err != nil || !ok {
		t.Fatalf("transition from one of several allowed states = %v, %v; want true, nil", ok, err)
	}
	if got := status(); got != domain.EnrollmentAuthorized {
		t.Errorf("status = %s, want AUTHORIZED", got)
	}

	ok, err = s.TransitionEnrollment(ctx, e.ID, []domain.EnrollmentStatus{domain.EnrollmentSeatReserved}, domain.EnrollmentAuthorized)
	if err != nil || ok {
		t.Errorf("repeating a transition = %v, %v; want false, nil", ok, err)
	}
	ok, err = s.TransitionEnrollment(ctx, e.ID, nil, domain.EnrollmentCapturing)
	if err != nil || ok {
		t.Errorf("transition with an empty from-list = %v, %v; want false, nil", ok, err)
	}
	ok, err = s.TransitionEnrollment(ctx, "00000000-0000-4000-8000-000000000000", []domain.EnrollmentStatus{domain.EnrollmentSeatReserved}, domain.EnrollmentAuthorized)
	if err != nil || ok {
		t.Errorf("transition of an unknown enrollment = %v, %v; want false, nil", ok, err)
	}

	before, _ := s.EnrollmentByID(ctx, e.ID)
	if before.ReleasedAt != nil {
		t.Fatalf("released_at is set before any release: %v", before.ReleasedAt)
	}
	var createdAt, updatedAt time.Time
	if err := s.Pool.QueryRow(ctx, `select created_at, updated_at from enrollments where id = $1`, e.ID).Scan(&createdAt, &updatedAt); err != nil {
		t.Fatal(err)
	}
	if !updatedAt.After(createdAt) {
		t.Errorf("updated_at %v was not bumped past created_at %v by a transition", updatedAt, createdAt)
	}

	mustTransition(t, s, e.ID, []domain.EnrollmentStatus{domain.EnrollmentAuthorized}, domain.EnrollmentReleased)
	released, _ := s.EnrollmentByID(ctx, e.ID)
	if released.Status != domain.EnrollmentReleased || released.ReleasedAt == nil {
		t.Errorf("after moving to RELEASED: status %s, released_at %v; want RELEASED with released_at set", released.Status, released.ReleasedAt)
	}
}

func TestTransitionEnrollment_ExactlyOneConcurrentWinner(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 1, 3)
	p := openFunding(t, s, w, baseTime)
	e := reserve(t, s, w, p, newParticipant(t, s, w, "Ana").ID, baseTime.Add(time.Hour))

	const racers = 12
	wins := make([]bool, racers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ok, err := s.TransitionEnrollment(ctx, e.ID, []domain.EnrollmentStatus{domain.EnrollmentSeatReserved}, domain.EnrollmentAuthorized)
			if err != nil {
				t.Errorf("racer %d: %v", i, err)
			}
			wins[i] = ok
		}()
	}
	close(start)
	wg.Wait()

	won := 0
	for _, ok := range wins {
		if ok {
			won++
		}
	}
	if won != 1 {
		t.Errorf("%d racers won the transition, want exactly 1", won)
	}
}

func TestReleaseExpiredSeats_ReleasesOnlyExpiredReserved(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 1, 6)
	p := openFunding(t, s, w, baseTime)
	other, _, _ := newWorkshop(t, s, 1, 6)
	otherP := openFunding(t, s, other, baseTime)

	reserveFor := func(w store.WorkshopRow, p store.ProposalRow, name string, ttl time.Duration) store.EnrollmentRow {
		t.Helper()
		e, created, err := s.ReserveSeatAndCreateEnrollment(ctx, store.ReserveParams{
			WorkshopID: w.ID, ParticipantID: newParticipant(t, s, w, name).ID, ProposalID: p.ID,
			ConsentVersion: "consent-v1", Now: baseTime.Add(time.Hour), SeatTTL: ttl,
		})
		if err != nil || !created {
			t.Fatalf("reserve %s: created=%v err=%v", name, created, err)
		}
		return e
	}

	expired := reserveFor(w, p, "expired", 30*time.Minute)
	alsoExpired := reserveFor(w, p, "also expired", 10*time.Minute)
	live := reserveFor(w, p, "live", 2*time.Hour)
	boundary := reserveFor(w, p, "boundary", 40*time.Minute)
	authorized := reserveFor(w, p, "authorized", 5*time.Minute)
	mustTransition(t, s, authorized.ID, []domain.EnrollmentStatus{domain.EnrollmentSeatReserved}, domain.EnrollmentAuthorized)
	elsewhere := reserveFor(other, otherP, "elsewhere", 5*time.Minute)

	now := baseTime.Add(time.Hour + 40*time.Minute)
	n, err := s.ReleaseExpiredSeats(ctx, w.ID, now)
	if err != nil {
		t.Fatalf("ReleaseExpiredSeats: %v", err)
	}
	if n != 2 {
		t.Errorf("ReleaseExpiredSeats = %d, want 2", n)
	}

	want := map[string]domain.EnrollmentStatus{
		expired.ID:     domain.EnrollmentReleased,
		alsoExpired.ID: domain.EnrollmentReleased,
		live.ID:        domain.EnrollmentSeatReserved,
		boundary.ID:    domain.EnrollmentSeatReserved,
		authorized.ID:  domain.EnrollmentAuthorized,
		elsewhere.ID:   domain.EnrollmentSeatReserved,
	}
	for id, status := range want {
		got, err := s.EnrollmentByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != status {
			t.Errorf("enrollment %s is %s, want %s", id, got.Status, status)
		}
		if status == domain.EnrollmentReleased && (got.ReleasedAt == nil || !got.ReleasedAt.Equal(now)) {
			t.Errorf("released enrollment %s has released_at %v, want %v", id, got.ReleasedAt, now)
		}
		if status != domain.EnrollmentReleased && got.ReleasedAt != nil {
			t.Errorf("untouched enrollment %s has released_at %v", id, got.ReleasedAt)
		}
	}

	if n, err := s.ReleaseExpiredSeats(ctx, w.ID, now); err != nil || n != 0 {
		t.Errorf("second ReleaseExpiredSeats = %d, %v; want 0, nil", n, err)
	}
}

func TestReleaseExpiredSeats_KeepsASeatWhoseAuthorizationIsPending(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 1, 6)
	p := openFunding(t, s, w, baseTime)
	reserved := baseTime.Add(time.Hour)
	withAuthorization := func(name, status string) store.EnrollmentRow {
		t.Helper()
		en := reserve(t, s, w, p, newParticipant(t, s, w, name).ID, reserved)
		a, err := s.CreateAttempt(ctx, en.ID, p.PriceMinor, p.Currency)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpdateAttemptFunc(ctx, a.ID, func(row *store.AttemptRow) error {
			row.AuthorizationID, row.AuthorizationStatus = ptr("AUTH-"+name), ptr(status)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return en
	}
	pending := withAuthorization("pending", "PENDING")
	voided := withAuthorization("voided", "VOIDED")
	noAttempt := reserve(t, s, w, p, newParticipant(t, s, w, "no attempt").ID, reserved)

	n, err := s.ReleaseExpiredSeats(ctx, w.ID, reserved.Add(24*time.Hour))
	if err != nil || n != 2 {
		t.Fatalf("ReleaseExpiredSeats = %d, %v; want 2, nil", n, err)
	}
	for id, want := range map[string]domain.EnrollmentStatus{
		pending.ID:   domain.EnrollmentSeatReserved,
		voided.ID:    domain.EnrollmentReleased,
		noAttempt.ID: domain.EnrollmentReleased,
	} {
		got, err := s.EnrollmentByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != want {
			t.Errorf("enrollment %s is %s, want %s", id, got.Status, want)
		}
	}

	refs, err := s.LatestAttemptsWithPendingAuthorization(ctx)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := s.LatestAttempt(ctx, pending.ID)
	if err != nil || latest == nil {
		t.Fatalf("LatestAttempt = %v, %v", latest, err)
	}
	if want := []store.AttemptRef{{AttemptID: latest.ID, WorkshopID: w.ID}}; !slices.Equal(refs, want) {
		t.Errorf("LatestAttemptsWithPendingAuthorization = %+v, want %+v", refs, want)
	}
}

func TestReReserveSeat(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	setup := func(t *testing.T, capacity int) (store.WorkshopRow, store.ProposalRow) {
		t.Helper()
		w, _, _ := newWorkshop(t, s, 1, capacity)
		return w, openFunding(t, s, w, baseTime)
	}
	released := func(t *testing.T, w store.WorkshopRow, p store.ProposalRow, name string) store.EnrollmentRow {
		t.Helper()
		e := reserve(t, s, w, p, newParticipant(t, s, w, name).ID, baseTime.Add(time.Hour))
		mustTransition(t, s, e.ID, []domain.EnrollmentStatus{domain.EnrollmentSeatReserved}, domain.EnrollmentReleased)
		return e
	}
	later := baseTime.Add(2 * time.Hour)

	t.Run("with capacity", func(t *testing.T) {
		w, p := setup(t, 2)
		e := released(t, w, p, "Ana")

		ok, err := s.ReReserveSeat(ctx, e.ID, later, 30*time.Minute)
		if err != nil || !ok {
			t.Fatalf("ReReserveSeat = %v, %v; want true, nil", ok, err)
		}
		got, _ := s.EnrollmentByID(ctx, e.ID)
		if got.Status != domain.EnrollmentSeatReserved || got.ReleasedAt != nil {
			t.Errorf("after re-reserving: status %s, released_at %v", got.Status, got.ReleasedAt)
		}
		if !got.SeatReservedAt.Equal(later) || !got.SeatExpiresAt.Equal(later.Add(30*time.Minute)) {
			t.Errorf("seat window %v - %v, want %v - %v", got.SeatReservedAt, got.SeatExpiresAt, later, later.Add(30*time.Minute))
		}
		if !got.ConsentAt.Equal(e.ConsentAt) || got.ConsentVersion != e.ConsentVersion {
			t.Errorf("re-reserving rewrote the original consent: %v %q", got.ConsentAt, got.ConsentVersion)
		}

		again, err := s.ReReserveSeat(ctx, e.ID, later, 30*time.Minute)
		if err != nil || again {
			t.Errorf("re-reserving an enrollment that is no longer released = %v, %v; want false, nil", again, err)
		}
	})

	t.Run("without capacity", func(t *testing.T) {
		w, p := setup(t, 1)
		e := released(t, w, p, "Ana")
		reserve(t, s, w, p, newParticipant(t, s, w, "Ben").ID, baseTime.Add(time.Hour))

		ok, err := s.ReReserveSeat(ctx, e.ID, later, 30*time.Minute)
		if err != nil || ok {
			t.Errorf("ReReserveSeat on a full workshop = %v, %v; want false, nil", ok, err)
		}
		if got, _ := s.EnrollmentByID(ctx, e.ID); got.Status != domain.EnrollmentReleased {
			t.Errorf("status = %s, want it to stay RELEASED", got.Status)
		}
	})

	t.Run("payments closed", func(t *testing.T) {
		w, p := setup(t, 2)
		e := released(t, w, p, "Ana")
		ok, err := s.ReReserveSeat(ctx, e.ID, p.ClosesAt, 30*time.Minute)
		if err != nil || ok {
			t.Errorf("ReReserveSeat at closes_at = %v, %v; want false, nil", ok, err)
		}
	})

	t.Run("workshop cancelled", func(t *testing.T) {
		w, p := setup(t, 2)
		e := released(t, w, p, "Ana")
		if err := s.SetWorkshopStatus(ctx, w.ID, domain.WorkshopCancelled, domain.ResolutionResolving); err != nil {
			t.Fatal(err)
		}
		ok, err := s.ReReserveSeat(ctx, e.ID, later, 30*time.Minute)
		if err != nil || ok {
			t.Errorf("ReReserveSeat on a cancelled workshop = %v, %v; want false, nil", ok, err)
		}
	})

	t.Run("confirmed workshop while payments are open", func(t *testing.T) {
		w, p := setup(t, 2)
		e := released(t, w, p, "Ana")
		closed := released(t, w, p, "Ben")
		if err := s.SetWorkshopStatus(ctx, w.ID, domain.WorkshopConfirmed, domain.ResolutionOpen); err != nil {
			t.Fatal(err)
		}
		ok, err := s.ReReserveSeat(ctx, e.ID, p.ClosesAt.Add(-time.Minute), 30*time.Minute)
		if err != nil || !ok {
			t.Errorf("ReReserveSeat in a CONFIRMED workshop before close = %v, %v; want true, nil", ok, err)
		}
		ok, err = s.ReReserveSeat(ctx, closed.ID, p.ClosesAt, 30*time.Minute)
		if err != nil || ok {
			t.Errorf("ReReserveSeat in a CONFIRMED workshop at closes_at = %v, %v; want false, nil", ok, err)
		}
		if got, _ := s.EnrollmentByID(ctx, closed.ID); got.Status != domain.EnrollmentReleased {
			t.Errorf("status after the refused re-reservation = %s, want RELEASED", got.Status)
		}
	})

	t.Run("confirmed and full", func(t *testing.T) {
		w, p := setup(t, 1)
		e := released(t, w, p, "Ana")
		reserve(t, s, w, p, newParticipant(t, s, w, "Ben").ID, baseTime.Add(time.Hour))
		if err := s.SetWorkshopStatus(ctx, w.ID, domain.WorkshopConfirmed, domain.ResolutionOpen); err != nil {
			t.Fatal(err)
		}
		ok, err := s.ReReserveSeat(ctx, e.ID, later, 30*time.Minute)
		if err != nil || ok {
			t.Errorf("ReReserveSeat in a full CONFIRMED workshop = %v, %v; want false, nil", ok, err)
		}
	})

	t.Run("participant already holds a newer enrollment", func(t *testing.T) {
		w, p := setup(t, 3)
		e := released(t, w, p, "Ana")
		fresh := reserve(t, s, w, p, e.ParticipantID, later)

		ok, err := s.ReReserveSeat(ctx, e.ID, later, 30*time.Minute)
		if err != nil || ok {
			t.Errorf("ReReserveSeat next to an active enrollment = %v, %v; want false, nil", ok, err)
		}
		if active, _ := s.ActiveEnrollmentByParticipant(ctx, e.ParticipantID); active == nil || active.ID != fresh.ID {
			t.Errorf("the participant's active enrollment changed: %+v", active)
		}
	})

	t.Run("not released", func(t *testing.T) {
		w, p := setup(t, 2)
		e := reserve(t, s, w, p, newParticipant(t, s, w, "Ana").ID, baseTime.Add(time.Hour))
		ok, err := s.ReReserveSeat(ctx, e.ID, later, 30*time.Minute)
		if err != nil || ok {
			t.Errorf("ReReserveSeat on a SEAT_RESERVED enrollment = %v, %v; want false, nil", ok, err)
		}
	})

	t.Run("unknown enrollment", func(t *testing.T) {
		if _, err := s.ReReserveSeat(ctx, "00000000-0000-4000-8000-000000000000", later, time.Minute); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("ReReserveSeat(unknown) error = %v, want ErrNotFound", err)
		}
	})

	t.Run("concurrent re-reservations never exceed capacity", func(t *testing.T) {
		w, p := setup(t, 2)
		var ids []string
		for _, name := range participantNames(8) {
			ids = append(ids, released(t, w, p, name).ID)
		}
		results := make([]bool, len(ids))
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range ids {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				ok, err := s.ReReserveSeat(ctx, ids[i], later, 30*time.Minute)
				if err != nil {
					t.Errorf("ReReserveSeat %d: %v", i, err)
				}
				results[i] = ok
			}()
		}
		close(start)
		wg.Wait()

		won := 0
		for _, ok := range results {
			if ok {
				won++
			}
		}
		if won != 2 {
			t.Errorf("%d re-reservations succeeded, want exactly capacity (2)", won)
		}
		if n := count(t, s, `select count(*) from enrollments where workshop_id = $1 and status = 'SEAT_RESERVED'`, w.ID); n != 2 {
			t.Errorf("%d SEAT_RESERVED rows, want 2", n)
		}
	})
}

func TestWorkshopIDsWithExpiredSeats_ListsEachWorkshopOnce(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	if ids, err := s.WorkshopIDsWithExpiredSeats(ctx, baseTime); err != nil || ids == nil || len(ids) != 0 {
		t.Fatalf("WorkshopIDsWithExpiredSeats on an empty database = %v, %v; want an empty list", ids, err)
	}

	two, _, _ := newWorkshop(t, s, 1, 6)
	twoP := openFunding(t, s, two, baseTime)
	one, _, _ := newWorkshop(t, s, 1, 6)
	oneP := openFunding(t, s, one, baseTime)
	authorized, _, _ := newWorkshop(t, s, 1, 6)
	authorizedP := openFunding(t, s, authorized, baseTime)
	released, _, _ := newWorkshop(t, s, 1, 6)
	releasedP := openFunding(t, s, released, baseTime)
	newWorkshop(t, s, 1, 6)

	reserve(t, s, two, twoP, newParticipant(t, s, two, "Expired").ID, baseTime)
	reserve(t, s, two, twoP, newParticipant(t, s, two, "Also expired").ID, baseTime)
	reserve(t, s, one, oneP, newParticipant(t, s, one, "Expired").ID, baseTime)
	reserve(t, s, one, oneP, newParticipant(t, s, one, "Live").ID, baseTime.Add(2*time.Hour))
	held := reserve(t, s, authorized, authorizedP, newParticipant(t, s, authorized, "Authorized").ID, baseTime)
	mustTransition(t, s, held.ID, []domain.EnrollmentStatus{domain.EnrollmentSeatReserved}, domain.EnrollmentAuthorized)
	gone := reserve(t, s, released, releasedP, newParticipant(t, s, released, "Released").ID, baseTime)
	mustTransition(t, s, gone.ID, []domain.EnrollmentStatus{domain.EnrollmentSeatReserved}, domain.EnrollmentReleased)

	ids, err := s.WorkshopIDsWithExpiredSeats(ctx, baseTime.Add(time.Hour))
	if err != nil {
		t.Fatalf("WorkshopIDsWithExpiredSeats: %v", err)
	}
	want := []string{two.ID, one.ID}
	slices.Sort(want)
	if !slices.Equal(ids, want) {
		t.Errorf("WorkshopIDsWithExpiredSeats = %v, want %v (each workshop with an expired SEAT_RESERVED seat, once)", ids, want)
	}
}

func TestReserveSeat_AllowedInConfirmedWorkshopBeforeClose(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	confirmed := func(capacity int) (store.WorkshopRow, store.ProposalRow) {
		t.Helper()
		w, _, _ := newWorkshop(t, s, 1, capacity)
		p := openFunding(t, s, w, baseTime)
		if err := s.SetWorkshopStatus(ctx, w.ID, domain.WorkshopConfirmed, domain.ResolutionOpen); err != nil {
			t.Fatal(err)
		}
		return w, p
	}

	w, p := confirmed(2)
	beforeClose := p.ClosesAt.Add(-time.Minute)
	for _, name := range []string{"Ana", "Ben"} {
		if _, created, err := s.ReserveSeatAndCreateEnrollment(ctx, reserveParams(w, p, newParticipant(t, s, w, name).ID, beforeClose)); err != nil || !created {
			t.Fatalf("%s in a CONFIRMED workshop before close: created=%v err=%v; want a seat", name, created, err)
		}
	}
	if _, created, err := s.ReserveSeatAndCreateEnrollment(ctx, reserveParams(w, p, newParticipant(t, s, w, "Cleo").ID, beforeClose)); !errors.Is(err, domain.ErrCapacityFull) || created {
		t.Errorf("CONFIRMED and full: created=%v err=%v; want ErrCapacityFull", created, err)
	}

	w, p = confirmed(3)
	for name, now := range map[string]time.Time{"at closes_at": p.ClosesAt, "after closes_at": p.ClosesAt.Add(time.Second)} {
		if _, created, err := s.ReserveSeatAndCreateEnrollment(ctx, reserveParams(w, p, newParticipant(t, s, w, name).ID, now)); !errors.Is(err, store.ErrProposalNotOpen) || created {
			t.Errorf("CONFIRMED %s: created=%v err=%v; want ErrProposalNotOpen", name, created, err)
		}
	}
	if n := count(t, s, `select count(*) from enrollments where workshop_id = $1`, w.ID); n != 0 {
		t.Errorf("%d enrollments created after the window closed, want 0", n)
	}
}

func TestMarkApprovalCancelled_StampsOnlyAReservedSeat(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 1, 3)
	p := openFunding(t, s, w, baseTime)
	reservedAt := baseTime.Add(time.Hour)
	e := reserve(t, s, w, p, newParticipant(t, s, w, "Ana").ID, reservedAt)

	cancelledAt := reservedAt.Add(5 * time.Minute)
	ok, err := s.MarkApprovalCancelled(ctx, e.ID, cancelledAt)
	if err != nil || !ok {
		t.Fatalf("MarkApprovalCancelled = %v, %v; want true, nil", ok, err)
	}
	got, err := s.EnrollmentByID(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ApprovalCancelledAt == nil || !got.ApprovalCancelledAt.Equal(cancelledAt) {
		t.Errorf("approval_cancelled_at = %v, want %v", got.ApprovalCancelledAt, cancelledAt)
	}
	if got.Status != domain.EnrollmentSeatReserved || !got.SeatExpiresAt.Equal(e.SeatExpiresAt) || got.ReleasedAt != nil {
		t.Errorf("after the cancel: status %s, seat expires %v, released_at %v; want the seat kept until %v",
			got.Status, got.SeatExpiresAt, got.ReleasedAt, e.SeatExpiresAt)
	}

	again := cancelledAt.Add(time.Minute)
	if ok, err := s.MarkApprovalCancelled(ctx, e.ID, again); err != nil || !ok {
		t.Fatalf("second MarkApprovalCancelled = %v, %v; want true, nil", ok, err)
	}
	if got, _ := s.EnrollmentByID(ctx, e.ID); got.ApprovalCancelledAt == nil || !got.ApprovalCancelledAt.Equal(again) {
		t.Errorf("approval_cancelled_at after a second cancel = %v, want the latest %v", got.ApprovalCancelledAt, again)
	}

	held := reserve(t, s, w, p, newParticipant(t, s, w, "Ben").ID, reservedAt)
	mustTransition(t, s, held.ID, []domain.EnrollmentStatus{domain.EnrollmentSeatReserved}, domain.EnrollmentAuthorized)
	if ok, err := s.MarkApprovalCancelled(ctx, held.ID, cancelledAt); err != nil || ok {
		t.Errorf("MarkApprovalCancelled on an AUTHORIZED enrollment = %v, %v; want false, nil", ok, err)
	}
	if got, _ := s.EnrollmentByID(ctx, held.ID); got.ApprovalCancelledAt != nil {
		t.Errorf("an AUTHORIZED enrollment got approval_cancelled_at %v", got.ApprovalCancelledAt)
	}

	for _, id := range []string{"00000000-0000-4000-8000-000000000000", "not-a-uuid"} {
		if ok, err := s.MarkApprovalCancelled(ctx, id, cancelledAt); err != nil || ok {
			t.Errorf("MarkApprovalCancelled(%q) = %v, %v; want false, nil", id, ok, err)
		}
	}
}

func TestFlagEnrollment_KeepsTheStatusAndIsSticky(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	en := newEnrollment(t, s)

	first, err := s.FlagEnrollment(ctx, en.ID, "capture gave up")
	if err != nil || !first {
		t.Fatalf("FlagEnrollment = %v, %v; want a new flag", first, err)
	}
	again, err := s.FlagEnrollment(ctx, en.ID, "second reason")
	if err != nil || again {
		t.Fatalf("FlagEnrollment again = %v, %v; want no change: the first reason stays", again, err)
	}
	got, err := s.EnrollmentByID(ctx, en.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.EnrollmentSeatReserved || got.NeedsAttentionAt == nil || got.AttentionReason == nil || *got.AttentionReason != "capture gave up" {
		t.Fatalf("flagged enrollment = %s, at %v, reason %v; want SEAT_RESERVED flagged with the first reason", got.Status, got.NeedsAttentionAt, got.AttentionReason)
	}

	cleared, err := s.ClearEnrollmentFlag(ctx, en.ID)
	if err != nil || !cleared {
		t.Fatalf("ClearEnrollmentFlag = %v, %v; want cleared", cleared, err)
	}
	if got, _ = s.EnrollmentByID(ctx, en.ID); got.NeedsAttentionAt != nil || got.AttentionReason != nil {
		t.Errorf("after clearing: at %v, reason %v; want neither", got.NeedsAttentionAt, got.AttentionReason)
	}
}

func TestSchema_RejectsUnknownStatuses(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	en := newEnrollment(t, s)
	for _, q := range []string{
		`update workshops set status = 'BOGUS' where id = $1`,
		`update workshops set resolution = 'BOGUS' where id = $1`,
		`update enrollments set status = 'BOGUS' where workshop_id = $1`,
	} {
		_, err := s.Pool.Exec(ctx, q, en.WorkshopID)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Errorf("%s: err = %v, want a check violation (23514)", q, err)
		}
	}
}

func TestReleaseExpiredSeats_AgreesWithDomainSeatExpired(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	en := newEnrollment(t, s)

	for _, now := range []time.Time{en.SeatExpiresAt, en.SeatExpiresAt.Add(time.Second)} {
		n, err := s.ReleaseExpiredSeats(ctx, en.WorkshopID, now)
		if err != nil {
			t.Fatal(err)
		}
		if want := domain.SeatExpired(en.SeatExpiresAt, now); (n == 1) != want {
			t.Errorf("ReleaseExpiredSeats at %s released %d; domain.SeatExpired = %v", now, n, want)
		}
	}
}

func TestWorkshopIDsWithExpiredSeats_OnlyWhenASeatCanBeReleased(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	en := newEnrollment(t, s)

	ids, err := s.WorkshopIDsWithExpiredSeats(ctx, en.SeatExpiresAt.Add(-time.Second))
	if err != nil || len(ids) != 0 {
		t.Fatalf("before the expiry = %v, %v; want none", ids, err)
	}
	if ids, err = s.WorkshopIDsWithExpiredSeats(ctx, en.SeatExpiresAt); err != nil || len(ids) != 0 {
		t.Fatalf("at the expiry instant = %v, %v; want none: the seat still holds", ids, err)
	}
	ids, err = s.WorkshopIDsWithExpiredSeats(ctx, en.SeatExpiresAt.Add(time.Second))
	if err != nil || len(ids) != 1 || ids[0] != en.WorkshopID {
		t.Fatalf("after the expiry = %v, %v; want the workshop", ids, err)
	}
	a, err := s.CreateAttempt(ctx, en.ID, 1000, "USD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `update payment_attempts set authorization_status = 'PENDING' where id = $1`, a.ID); err != nil {
		t.Fatal(err)
	}
	if ids, err = s.WorkshopIDsWithExpiredSeats(ctx, en.SeatExpiresAt.Add(time.Hour)); err != nil || len(ids) != 0 {
		t.Fatalf("a seat kept for a PENDING authorization = %v, %v; want none: nothing to release", ids, err)
	}
}
