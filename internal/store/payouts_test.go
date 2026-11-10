package store_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
	"github.com/aalvaropc/commitpool/internal/store/storetest"
)

const payoutEmail = "iris@example.com"

var (
	afterClose = baseTime.Add(25 * time.Hour)
	afterStart = saturday.Add(time.Minute)
)

type payoutFixture struct {
	s        *store.Store
	w        store.WorkshopRow
	p        store.ProposalRow
	seats    []store.EnrollmentRow
	attempts []store.AttemptRow
}

func newPayoutWorkshop(t *testing.T, s *store.Store, email string) payoutFixture {
	t.Helper()
	ctx := context.Background()
	w, _, _, err := s.CreateWorkshop(ctx, store.CreateWorkshopParams{
		Title: "Life drawing", DurationMin: 120, Timezone: "America/New_York",
		Minimum: 2, Capacity: 6, PriceMinor: 1000, Currency: "USD", PaymentWindowMin: 1440,
		PayoutEmail: email,
		Dates:       []dateWindow{{StartsAt: saturday, EndsAt: saturday.Add(2 * time.Hour)}},
	})
	if err != nil {
		t.Fatalf("CreateWorkshop: %v", err)
	}
	if w.PayoutEmail != email || w.PayoutLockedAt != nil {
		t.Fatalf("created workshop payout = %q locked %v, want %q unlocked", w.PayoutEmail, w.PayoutLockedAt, email)
	}
	return payoutFixture{s: s, w: w, p: openFunding(t, s, w, baseTime)}
}

func (f *payoutFixture) seat(t *testing.T, name string, status domain.EnrollmentStatus, net *int64) store.EnrollmentRow {
	t.Helper()
	ctx := context.Background()
	en := reserve(t, f.s, f.w, f.p, newParticipant(t, f.s, f.w, name).ID, baseTime)
	a, err := f.s.CreateAttempt(ctx, en.ID, 1000, "USD")
	if err != nil {
		t.Fatalf("CreateAttempt: %v", err)
	}
	if status != domain.EnrollmentSeatReserved {
		a, err = f.s.UpdateAttemptFunc(ctx, a.ID, func(row *store.AttemptRow) error {
			order, auth, capture := "O-"+a.ID, "A-"+a.ID, "C-"+a.ID
			row.OrderID, row.AuthorizationID, row.CaptureID = &order, &auth, &capture
			if net != nil {
				fee := 1000 - *net
				row.CaptureFeeMinor, row.CaptureNetMinor = &fee, net
			}
			return nil
		})
		if err != nil {
			t.Fatalf("UpdateAttemptFunc: %v", err)
		}
		mustTransition(t, f.s, en.ID, []domain.EnrollmentStatus{domain.EnrollmentSeatReserved}, status)
	}
	f.seats = append(f.seats, en)
	f.attempts = append(f.attempts, a)
	return en
}

func (f *payoutFixture) paid(t *testing.T, name string, net int64) store.EnrollmentRow {
	t.Helper()
	return f.seat(t, name, domain.EnrollmentPaid, &net)
}

func (f *payoutFixture) confirm(t *testing.T) {
	t.Helper()
	ok, err := f.s.TransitionWorkshop(context.Background(), f.w.ID,
		[]domain.WorkshopStatus{domain.WorkshopFunding}, nil, domain.WorkshopConfirmed, domain.ResolutionOpen)
	if err != nil || !ok {
		t.Fatalf("confirm = %v, %v", ok, err)
	}
}

func readyPayout(t *testing.T, s *store.Store) payoutFixture {
	t.Helper()
	f := newPayoutWorkshop(t, s, payoutEmail)
	f.paid(t, "Ana", 916)
	f.paid(t, "Ben", 916)
	f.paid(t, "Cleo", 916)
	f.confirm(t)
	return f
}

func wantNotReady(t *testing.T, err error, reason string) {
	t.Helper()
	var nr *store.ErrPayoutNotReady
	if !errors.As(err, &nr) || nr.Reason != reason {
		t.Fatalf("BeginPayout = %v, want not ready: %s", err, reason)
	}
}

func TestBeginPayout_FreezesTheNetOfPaidSeatsAndLocksTheWorkshop(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	f := readyPayout(t, s)
	f.seat(t, "Dee", domain.EnrollmentRefunded, ptr(int64(916)))
	f.seat(t, "Eli", domain.EnrollmentVoided, nil)
	f.seat(t, "Fay", domain.EnrollmentCaptureFailed, nil)

	po, created, err := s.BeginPayout(ctx, f.w.ID, afterStart, false)
	if err != nil || !created {
		t.Fatalf("BeginPayout = %+v, %v, %v", po, created, err)
	}
	want := store.PayoutRow{
		ID: po.ID, WorkshopID: f.w.ID, SenderBatchID: "cp-" + f.w.ID, Receiver: payoutEmail,
		AmountMinor: 2748, GrossMinor: 3000, FeeMinor: 252, Currency: "USD", Seats: 3,
		Status: domain.PayoutCreated, CreatedAt: po.CreatedAt,
	}
	if po.ID == "" || !po.CreatedAt.Equal(afterStart) {
		t.Fatalf("payout = %+v, want an id created at %s", po, afterStart)
	}
	if po != want {
		t.Errorf("payout = %+v\nwant %+v", po, want)
	}
	w, err := s.WorkshopByID(ctx, f.w.ID)
	if err != nil || w.PayoutLockedAt == nil || !w.PayoutLockedAt.Equal(afterStart) {
		t.Fatalf("workshop lock = %v, %v; want %s", w.PayoutLockedAt, err, afterStart)
	}

	again, created, err := s.BeginPayout(ctx, f.w.ID, afterStart.Add(time.Hour), false)
	if err != nil || created || again != po {
		t.Errorf("second BeginPayout = %+v, %v, %v; want the same row, not created", again, created, err)
	}
}

func TestBeginPayout_CountsOnlyTheLatestAttempt(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	f := readyPayout(t, s)
	older := f.attempts[0]
	if _, err := s.Pool.Exec(ctx, `update payment_attempts set attempt_no = 2 where id = $1`, older.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx,
		`insert into payment_attempts (enrollment_id, attempt_no, amount_minor, currency, capture_id, capture_fee_minor, capture_net_minor)
		 values ($1, 1, 1000, 'USD', 'C-older', 500, 500)`, f.seats[0].ID); err != nil {
		t.Fatal(err)
	}
	po, _, err := s.BeginPayout(ctx, f.w.ID, afterStart, false)
	if err != nil || po.AmountMinor != 2748 || po.Seats != 3 {
		t.Fatalf("BeginPayout = %+v, %v; want 27.48 over 3 seats from the latest attempts", po, err)
	}
}

func TestBeginPayout_RefusesUntilEveryRuleHolds(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		setup  func(t *testing.T, s *store.Store) payoutFixture
		now    time.Time
		early  bool
		reason string
	}{
		{"funding", func(t *testing.T, s *store.Store) payoutFixture {
			f := newPayoutWorkshop(t, s, payoutEmail)
			f.paid(t, "Ana", 916)
			return f
		}, afterStart, false, domain.PayoutNotConfirmed},
		{"collecting", func(t *testing.T, s *store.Store) payoutFixture {
			w, _, _ := newWorkshop(t, s, 2, 6)
			return payoutFixture{s: s, w: w}
		}, afterStart, false, domain.PayoutNotConfirmed},
		{"no payout email", func(t *testing.T, s *store.Store) payoutFixture {
			f := newPayoutWorkshop(t, s, "")
			f.paid(t, "Ana", 916)
			f.paid(t, "Ben", 916)
			f.confirm(t)
			return f
		}, afterStart, false, domain.PayoutNoEmail},
		{"flagged workshop", func(t *testing.T, s *store.Store) payoutFixture {
			f := readyPayout(t, s)
			if err := s.SetWorkshopStatus(ctx, f.w.ID, domain.WorkshopConfirmed, domain.ResolutionNeedsAttention); err != nil {
				t.Fatal(err)
			}
			return f
		}, afterStart, false, domain.PayoutNeedsAttention},
		{"payments open", readyPayout, baseTime.Add(time.Hour), true, domain.PayoutPaymentsOpen},
		{"before the start", readyPayout, afterClose, false, domain.PayoutNotStarted},
		{"a capture in flight", func(t *testing.T, s *store.Store) payoutFixture {
			f := readyPayout(t, s)
			f.seat(t, "Dee", domain.EnrollmentCapturePending, nil)
			return f
		}, afterStart, false, domain.PayoutInFlight},
		{"a hold not yet voided", func(t *testing.T, s *store.Store) payoutFixture {
			f := readyPayout(t, s)
			f.seat(t, "Dee", domain.EnrollmentAuthorized, nil)
			return f
		}, afterStart, false, domain.PayoutInFlight},
		{"a refund in flight", func(t *testing.T, s *store.Store) payoutFixture {
			f := readyPayout(t, s)
			f.seat(t, "Dee", domain.EnrollmentRefundPending, ptr(int64(916)))
			return f
		}, afterStart, false, domain.PayoutInFlight},
		{"a flagged seat", func(t *testing.T, s *store.Store) payoutFixture {
			f := readyPayout(t, s)
			if _, err := s.FlagEnrollment(ctx, f.seats[1].ID, "test"); err != nil {
				t.Fatal(err)
			}
			return f
		}, afterStart, false, domain.PayoutFlagged},
		{"a paid seat without its fees", func(t *testing.T, s *store.Store) payoutFixture {
			f := readyPayout(t, s)
			f.seat(t, "Dee", domain.EnrollmentPaid, nil)
			return f
		}, afterStart, false, domain.PayoutFeesMissing},
		{"fees took everything", func(t *testing.T, s *store.Store) payoutFixture {
			f := newPayoutWorkshop(t, s, payoutEmail)
			f.paid(t, "Ana", 0)
			f.confirm(t)
			return f
		}, afterStart, false, domain.PayoutNothingToPay},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := storetest.New(t)
			f := c.setup(t, s)
			_, _, err := s.BeginPayout(ctx, f.w.ID, c.now, c.early)
			wantNotReady(t, err, c.reason)
			if n := count(t, s, `select count(*) from payouts`); n != 0 {
				t.Errorf("a refused payout left %d rows", n)
			}
			if w, _ := s.WorkshopByID(ctx, f.w.ID); w.PayoutLockedAt != nil {
				t.Errorf("a refused payout locked the workshop")
			}
		})
	}
}

func TestBeginPayout_EarlySkipsOnlyTheStart(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	f := readyPayout(t, s)
	po, created, err := s.BeginPayout(ctx, f.w.ID, afterClose, true)
	if err != nil || !created || !po.Early {
		t.Fatalf("early BeginPayout = %+v, %v, %v; want an early payout", po, created, err)
	}
}

func TestBeginPayout_LockStopsTheOrganizerCancel(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	f := readyPayout(t, s)
	if _, _, err := s.BeginPayout(ctx, f.w.ID, afterClose, true); err != nil {
		t.Fatal(err)
	}
	ok, err := s.CancelWorkshop(ctx, f.w.ID, domain.WorkshopConfirmed, domain.ResolutionOpen, domain.ResolutionResolving, "too late", afterClose)
	if err != nil || ok {
		t.Fatalf("CancelWorkshop after the payout lock = %v, %v; want refused", ok, err)
	}
}

func TestBeginPayout_RacesTheCancelAndOnlyOneWins(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	for i := range 50 {
		f := readyPayout(t, s)
		var (
			wg        sync.WaitGroup
			cancelled bool
			cancelErr error
			payoutErr error
			created   bool
		)
		wg.Add(2)
		start := make(chan struct{})
		go func() {
			defer wg.Done()
			<-start
			cancelled, cancelErr = s.CancelWorkshop(ctx, f.w.ID, domain.WorkshopConfirmed, domain.ResolutionOpen, domain.ResolutionResolving, "race", afterClose)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, created, payoutErr = s.BeginPayout(ctx, f.w.ID, afterClose, true)
		}()
		close(start)
		wg.Wait()
		var nr *store.ErrPayoutNotReady
		if cancelErr != nil || (payoutErr != nil && !errors.As(payoutErr, &nr)) {
			t.Fatalf("iteration %d: cancel %v, payout %v", i, cancelErr, payoutErr)
		}
		if cancelled == created {
			t.Fatalf("iteration %d: cancelled %v and paid out %v; want exactly one", i, cancelled, created)
		}
		if cancelled && (nr == nil || nr.Reason != domain.PayoutNotConfirmed) {
			t.Fatalf("iteration %d: the payout lost to a cancel with %v, want not_confirmed", i, payoutErr)
		}
		w, err := s.WorkshopByID(ctx, f.w.ID)
		if err != nil {
			t.Fatal(err)
		}
		if (w.Status == domain.WorkshopCancelled) != cancelled || (w.PayoutLockedAt != nil) != created {
			t.Fatalf("iteration %d: workshop %s locked %v after cancelled %v created %v", i, w.Status, w.PayoutLockedAt, cancelled, created)
		}
	}
}

func TestPayout_SentFailedAndChecked(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	f := readyPayout(t, s)
	po, _, err := s.BeginPayout(ctx, f.w.ID, afterStart, false)
	if err != nil {
		t.Fatal(err)
	}

	sent, err := s.MarkPayoutSent(ctx, po.ID, "BATCH1", "PENDING", afterStart)
	if err != nil || sent.Status != domain.PayoutSent || orEmpty(sent.PayoutBatchID) != "BATCH1" || orEmpty(sent.BatchStatus) != "PENDING" || sent.SentAt == nil {
		t.Fatalf("MarkPayoutSent = %+v, %v", sent, err)
	}
	if again, err := s.MarkPayoutSent(ctx, po.ID, "BATCH1", "", afterStart.Add(time.Hour)); err != nil || !again.SentAt.Equal(*sent.SentAt) {
		t.Errorf("MarkPayoutSent again = %+v, %v; want the same batch kept", again, err)
	}
	if _, err := s.MarkPayoutSent(ctx, po.ID, "BATCH2", "", afterStart); !errors.Is(err, store.ErrPayoutConflict) {
		t.Errorf("MarkPayoutSent with another batch = %v, want ErrPayoutConflict", err)
	}
	if ok, err := s.MarkPayoutUnsent(ctx, po.ID, domain.PayoutFailed, "X", "d", afterStart); err != nil || ok {
		t.Errorf("MarkPayoutUnsent on a sent payout = %v, %v; want refused", ok, err)
	}

	byRef := func(batch, sender, item string) string {
		t.Helper()
		row, err := s.PayoutByPayPalRef(ctx, batch, sender, item)
		if err != nil {
			t.Fatal(err)
		}
		if row == nil {
			return ""
		}
		return row.ID
	}
	if byRef("BATCH1", "", "") != po.ID || byRef("", "cp-"+f.w.ID, "") != po.ID || byRef("", "", "") != "" || byRef("OTHER", "", "") != "" {
		t.Errorf("PayoutByPayPalRef does not find the payout by its batch or sender id")
	}

	checked := afterStart.Add(time.Minute)
	before, after, err := s.RecordPayoutCheck(ctx, po.ID, store.PayoutCheck{
		Status: domain.PayoutUnclaimed, BatchStatus: "SUCCESS", ItemID: "ITEM1", ItemStatus: "UNCLAIMED", TransactionID: "TX1", ItemIssue: "RECEIVER_UNREGISTERED",
	}, checked)
	if err != nil || before.Status != domain.PayoutSent || after.Status != domain.PayoutUnclaimed || orEmpty(after.Issue) != "RECEIVER_UNREGISTERED" ||
		orEmpty(after.PayoutItemID) != "ITEM1" || after.CompletedAt != nil {
		t.Fatalf("unclaimed check = %+v -> %+v, %v", before, after, err)
	}
	if byRef("", "", "ITEM1") != po.ID {
		t.Errorf("PayoutByPayPalRef does not find the payout by its item id")
	}
	_, after, err = s.RecordPayoutCheck(ctx, po.ID, store.PayoutCheck{Status: domain.PayoutSucceeded, BatchStatus: "SUCCESS", ItemStatus: "SUCCESS"}, checked)
	if err != nil || after.Status != domain.PayoutSucceeded || after.Issue != nil || after.CompletedAt == nil || orEmpty(after.TransactionID) != "TX1" {
		t.Fatalf("succeeded check = %+v, %v", after, err)
	}
	_, after, err = s.RecordPayoutCheck(ctx, po.ID, store.PayoutCheck{Status: domain.PayoutSent, BatchStatus: "PROCESSING", ItemStatus: "PENDING"}, checked)
	if err != nil || after.Status != domain.PayoutSucceeded {
		t.Errorf("a pending read after success = %s, %v; want it kept succeeded", after.Status, err)
	}

	toCheck, err := s.PayoutsToCheck(ctx)
	if err != nil || len(toCheck) != 0 {
		t.Errorf("PayoutsToCheck = %d, %v; a succeeded payout needs no check", len(toCheck), err)
	}
}

func TestPayout_UnsentOutcomesAndHealth(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	failed := readyPayout(t, s)
	unknown := readyPayout(t, s)
	for _, f := range []payoutFixture{failed, unknown} {
		if _, _, err := s.BeginPayout(ctx, f.w.ID, afterStart, false); err != nil {
			t.Fatal(err)
		}
	}
	pf, _ := s.PayoutByWorkshop(ctx, failed.w.ID)
	pu, _ := s.PayoutByWorkshop(ctx, unknown.w.ID)
	if ok, err := s.MarkPayoutUnsent(ctx, pf.ID, domain.PayoutFailed, "INSUFFICIENT_FUNDS", "dbg", afterStart); err != nil || !ok {
		t.Fatalf("MarkPayoutUnsent(failed) = %v, %v", ok, err)
	}
	if ok, err := s.MarkPayoutUnsent(ctx, pu.ID, domain.PayoutUnknown, "USER_BUSINESS_ERROR", "dbg2", afterStart); err != nil || !ok {
		t.Fatalf("MarkPayoutUnsent(unknown) = %v, %v", ok, err)
	}
	if _, err := s.MarkPayoutUnsent(ctx, pu.ID, domain.PayoutSent, "", "", afterStart); err == nil {
		t.Errorf("MarkPayoutUnsent(sent) succeeded")
	}
	h, err := s.Health(ctx)
	if err != nil || h.PayoutsFailed != 1 || h.PayoutsUnknown != 1 {
		t.Fatalf("Health = %+v, %v; want one failed and one unknown payout", h, err)
	}
	if toCheck, _ := s.PayoutsToCheck(ctx); len(toCheck) != 0 {
		t.Errorf("an unknown payout without a batch id cannot be read, yet PayoutsToCheck lists it")
	}
	recovered, err := s.MarkPayoutSent(ctx, pu.ID, "BATCH9", "", afterStart)
	if err != nil || recovered.Status != domain.PayoutSent || recovered.Issue != nil || recovered.DebugID != nil {
		t.Fatalf("recovering the unknown batch = %+v, %v", recovered, err)
	}
	if toCheck, _ := s.PayoutsToCheck(ctx); len(toCheck) != 1 || toCheck[0].ID != pu.ID {
		t.Errorf("PayoutsToCheck = %+v, want the recovered payout", toCheck)
	}
}

func TestDuePayouts_ConfirmedWithAnEmailFromTheStartUntilSent(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	due := readyPayout(t, s)
	noEmail := newPayoutWorkshop(t, s, "")
	noEmail.paid(t, "Ana", 916)
	noEmail.paid(t, "Ben", 916)
	noEmail.confirm(t)
	funding := newPayoutWorkshop(t, s, payoutEmail)

	ids := func(now time.Time) string {
		t.Helper()
		got, err := s.DuePayouts(ctx, now)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(got, ",")
	}
	if got := ids(afterClose); got != "" {
		t.Errorf("DuePayouts before the start = %q, want none", got)
	}
	if got := ids(afterStart); got != due.w.ID {
		t.Errorf("DuePayouts after the start = %q, want %s (not %s or %s)", got, due.w.ID, noEmail.w.ID, funding.w.ID)
	}
	po, _, err := s.BeginPayout(ctx, due.w.ID, afterStart, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(afterStart); got != due.w.ID {
		t.Errorf("a created, unsent payout is still due; DuePayouts = %q", got)
	}
	if _, err := s.MarkPayoutSent(ctx, po.ID, "B1", "", afterStart); err != nil {
		t.Fatal(err)
	}
	if got := ids(afterStart); got != "" {
		t.Errorf("DuePayouts after sending = %q, want none", got)
	}
}

func TestPaidAttemptsMissingFees_OnlyLatestPaidCaptures(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	f := readyPayout(t, s)
	missing := f.seat(t, "Dee", domain.EnrollmentPaid, nil)
	f.seat(t, "Eli", domain.EnrollmentRefunded, nil)
	rows, err := s.PaidAttemptsMissingFees(ctx, f.w.ID)
	if err != nil || len(rows) != 1 || rows[0].EnrollmentID != missing.ID {
		t.Fatalf("PaidAttemptsMissingFees = %+v, %v; want Dee's attempt only", rows, err)
	}
}

func TestOpenTermsHash_NamesThePayoutEmailOnlyWhenThereIsOne(t *testing.T) {
	w := store.WorkshopRow{PriceMinor: 1000, Currency: "USD", Minimum: 2, Capacity: 6, PaymentWindowMin: 1440}
	legacy := sha256.Sum256([]byte(strings.Join([]string{"date-a", "1000", "USD", "2", "6", "1440"}, "|")))
	if !bytes.Equal(w.OpenTermsHash("date-a"), legacy[:]) {
		t.Fatal("a workshop without a payout email changed its terms hash; approvals requested before payouts would no longer verify")
	}
	with := w
	with.PayoutEmail = payoutEmail
	other := w
	other.PayoutEmail = "someone@example.com"
	if bytes.Equal(with.OpenTermsHash("date-a"), legacy[:]) || bytes.Equal(with.OpenTermsHash("date-a"), other.OpenTermsHash("date-a")) {
		t.Error("the payout email does not change the terms hash")
	}
}

func TestPurge_DropsOldPayoutChecks(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	old := baseTime.Add(-40 * 24 * time.Hour)
	for i, kind := range []string{store.KindPayoutCheck, store.KindPayout} {
		key := kind + ":" + strconv.Itoa(i)
		if _, err := s.Pool.Exec(ctx,
			`insert into jobs (kind, idempotency_key, status, run_at, updated_at) values ($1, $2, 'succeeded', $3, $3)`, kind, key, old); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.PurgeSucceededJobs(ctx, baseTime)
	if err != nil || n != 1 {
		t.Fatalf("PurgeSucceededJobs = %d, %v; want the payout check only", n, err)
	}
	if count(t, s, `select count(*) from jobs where kind = 'payout'`) != 1 {
		t.Error("the payout job was purged")
	}
}

func orEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
