package store_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
	"github.com/aalvaropc/commitpool/internal/store/storetest"
)

const approvalTTL = 30 * time.Minute

func proposalFor(w store.WorkshopRow, d store.CandidateDateRow, opensAt time.Time) store.ProposalRow {
	return store.ProposalRow{
		WorkshopID: w.ID, ChosenDateID: d.ID, PriceMinor: w.PriceMinor, Currency: w.Currency,
		Minimum: w.Minimum, Capacity: w.Capacity, OpensAt: opensAt, ClosesAt: opensAt.Add(24 * time.Hour), StartsAt: d.StartsAt,
	}
}

func firstDate(t *testing.T, s *store.Store, w store.WorkshopRow) store.CandidateDateRow {
	t.Helper()
	dates, err := s.CandidateDates(context.Background(), w.ID)
	if err != nil || len(dates) == 0 {
		t.Fatalf("CandidateDates: %v (%d dates)", err, len(dates))
	}
	return dates[0]
}

func TestOpenApproval_RequestSnapshotsTheTermsAndReusesAnOpenOne(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	w, _, _ := newWorkshop(t, s, 2, 6)
	d := firstDate(t, s, w)

	a, created, err := s.RequestOpenApproval(ctx, w, d.ID, baseTime, approvalTTL)
	if err != nil || !created {
		t.Fatalf("RequestOpenApproval = %+v, created %v, %v", a, created, err)
	}
	if a.WorkshopID != w.ID || a.DateID != d.ID || a.PriceMinor != 1000 || a.Currency != "USD" || a.Minimum != 2 || a.Capacity != 6 ||
		a.PaymentWindowMin != 1440 || !a.CreatedAt.Equal(baseTime) || !a.ExpiresAt.Equal(baseTime.Add(approvalTTL)) ||
		a.ApprovedAt != nil || a.UsedAt != nil || !bytes.Equal(a.TermsHash, w.OpenTermsHash(d.ID)) {
		t.Fatalf("approval = %+v, want the workshop's terms, pending, expiring in 30 minutes", a)
	}

	again, created, err := s.RequestOpenApproval(ctx, w, d.ID, baseTime.Add(time.Minute), approvalTTL)
	if err != nil || created || again.ID != a.ID {
		t.Fatalf("second request = %s, created %v, %v; want the same open approval", again.ID, created, err)
	}

	later, created, err := s.RequestOpenApproval(ctx, w, d.ID, baseTime.Add(approvalTTL), approvalTTL)
	if err != nil || !created || later.ID == a.ID {
		t.Fatalf("request after expiry = %s, created %v, %v; want a new approval", later.ID, created, err)
	}

	got, err := s.OpenApproval(ctx, w.ID, a.ID)
	if err != nil || got.ID != a.ID {
		t.Fatalf("OpenApproval = %+v, %v", got, err)
	}
	other, _, _ := newWorkshop(t, s, 2, 6)
	if _, err := s.OpenApproval(ctx, other.ID, a.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("OpenApproval from another workshop: error = %v, want ErrNotFound", err)
	}
	if _, err := s.OpenApproval(ctx, w.ID, "not-a-uuid"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("OpenApproval(not-a-uuid): error = %v, want ErrNotFound", err)
	}
}

func TestOpenApproval_TermsHashCoversEveryLockedTerm(t *testing.T) {
	w := store.WorkshopRow{PriceMinor: 1000, Currency: "USD", Minimum: 2, Capacity: 6, PaymentWindowMin: 1440}
	base := w.OpenTermsHash("date-a")
	edits := map[string]func(*store.WorkshopRow, *string){
		"date":     func(_ *store.WorkshopRow, d *string) { *d = "date-b" },
		"price":    func(w *store.WorkshopRow, _ *string) { w.PriceMinor = 1001 },
		"currency": func(w *store.WorkshopRow, _ *string) { w.Currency = "EUR" },
		"minimum":  func(w *store.WorkshopRow, _ *string) { w.Minimum = 3 },
		"capacity": func(w *store.WorkshopRow, _ *string) { w.Capacity = 7 },
		"window":   func(w *store.WorkshopRow, _ *string) { w.PaymentWindowMin = 60 },
	}
	for name, edit := range edits {
		changed, date := w, "date-a"
		edit(&changed, &date)
		if bytes.Equal(changed.OpenTermsHash(date), base) {
			t.Errorf("changing the %s keeps the same terms hash", name)
		}
	}
	if !bytes.Equal(w.OpenTermsHash("date-a"), base) {
		t.Error("the terms hash is not stable")
	}
}

func TestOpenApproval_ApproveOnlyWhilePendingAndTheTermsHold(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	w, _, _ := newWorkshop(t, s, 2, 6)
	d := firstDate(t, s, w)
	current := w.OpenTermsHash(d.ID)

	a, _, err := s.RequestOpenApproval(ctx, w, d.ID, baseTime, approvalTTL)
	if err != nil {
		t.Fatal(err)
	}
	changed := w
	changed.PriceMinor++
	if _, err := s.ApproveOpen(ctx, w.ID, a.ID, changed.OpenTermsHash(d.ID), baseTime); !errors.Is(err, store.ErrApprovalTermsChanged) {
		t.Fatalf("approve with changed terms: error = %v, want ErrApprovalTermsChanged", err)
	}
	if _, err := s.ApproveOpen(ctx, w.ID, a.ID, current, baseTime.Add(approvalTTL)); !errors.Is(err, store.ErrApprovalExpired) {
		t.Fatalf("approve at expiry: error = %v, want ErrApprovalExpired", err)
	}
	other, _, _ := newWorkshop(t, s, 2, 6)
	if _, err := s.ApproveOpen(ctx, other.ID, a.ID, current, baseTime); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("approve from another workshop: error = %v, want ErrNotFound", err)
	}

	approved, err := s.ApproveOpen(ctx, w.ID, a.ID, current, baseTime.Add(5*time.Minute))
	if err != nil || approved.ApprovedAt == nil || !approved.ApprovedAt.Equal(baseTime.Add(5*time.Minute)) {
		t.Fatalf("ApproveOpen = %+v, %v; want approved at +5m", approved, err)
	}
	again, err := s.ApproveOpen(ctx, w.ID, a.ID, current, baseTime.Add(6*time.Minute))
	if err != nil || !again.ApprovedAt.Equal(*approved.ApprovedAt) {
		t.Fatalf("second ApproveOpen = %+v, %v; want the first approval kept", again, err)
	}
	if err := approved.Check(current, baseTime.Add(6*time.Minute)); err != nil {
		t.Fatalf("Check on an approved approval = %v, want nil", err)
	}
}

func TestOpenApproval_OpensReservationsOnceAndOnlyAfterApproval(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	w, _, _ := newWorkshop(t, s, 2, 6)
	d := firstDate(t, s, w)
	current := w.OpenTermsHash(d.ID)
	a, _, err := s.RequestOpenApproval(ctx, w, d.ID, baseTime, approvalTTL)
	if err != nil {
		t.Fatal(err)
	}
	opensAt := baseTime.Add(2 * time.Minute)

	if _, err := s.OpenProposalWithApproval(ctx, proposalFor(w, d, opensAt), a.ID, current); !errors.Is(err, store.ErrApprovalPending) {
		t.Fatalf("open before approval: error = %v, want ErrApprovalPending", err)
	}
	if p, err := s.ActiveProposal(ctx, w.ID); err != nil || p != nil {
		t.Fatalf("a refused open left a proposal: %+v, %v", p, err)
	}

	if _, err := s.ApproveOpen(ctx, w.ID, a.ID, current, baseTime.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	dates, err := s.CandidateDates(ctx, w.ID)
	if err != nil || len(dates) < 2 {
		t.Fatalf("CandidateDates: %v (%d dates)", err, len(dates))
	}
	other := dates[1]
	if _, err := s.OpenProposalWithApproval(ctx, proposalFor(w, other, opensAt), a.ID, w.OpenTermsHash(other.ID)); !errors.Is(err, store.ErrApprovalTermsChanged) {
		t.Fatalf("open on another date: error = %v, want ErrApprovalTermsChanged", err)
	}

	opened, err := s.OpenProposalWithApproval(ctx, proposalFor(w, d, opensAt), a.ID, current)
	if err != nil || opened.ChosenDateID != d.ID || !opened.OpensAt.Equal(opensAt) {
		t.Fatalf("OpenProposalWithApproval = %+v, %v", opened, err)
	}
	used, err := s.OpenApproval(ctx, w.ID, a.ID)
	if err != nil || used.UsedAt == nil || !used.UsedAt.Equal(opensAt) {
		t.Fatalf("approval after opening = %+v, %v; want it used", used, err)
	}
	ws, err := s.WorkshopByID(ctx, w.ID)
	if err != nil || ws.Status != domain.WorkshopFunding {
		t.Fatalf("workshop after opening = %s, %v; want FUNDING", ws.Status, err)
	}
	if n := count(t, s, `select count(*) from jobs where workshop_id = $1 and kind = $2`, w.ID, store.KindClose); n != 1 {
		t.Errorf("close jobs = %d, want 1", n)
	}
	if _, err := s.OpenProposalWithApproval(ctx, proposalFor(w, d, opensAt), a.ID, current); !errors.Is(err, store.ErrApprovalUsed) {
		t.Fatalf("reusing the approval: error = %v, want ErrApprovalUsed", err)
	}
	if _, err := s.ApproveOpen(ctx, w.ID, a.ID, current, opensAt); !errors.Is(err, store.ErrApprovalUsed) {
		t.Fatalf("approving a used approval: error = %v, want ErrApprovalUsed", err)
	}
}

func TestOpenApproval_OpenRefusedWhenExpiredOrTermsChanged(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	w, _, _ := newWorkshop(t, s, 2, 6)
	d := firstDate(t, s, w)
	current := w.OpenTermsHash(d.ID)
	a, _, err := s.RequestOpenApproval(ctx, w, d.ID, baseTime, approvalTTL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApproveOpen(ctx, w.ID, a.ID, current, baseTime.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	if _, err := s.OpenProposalWithApproval(ctx, proposalFor(w, d, baseTime.Add(approvalTTL)), a.ID, current); !errors.Is(err, store.ErrApprovalExpired) {
		t.Fatalf("open at expiry: error = %v, want ErrApprovalExpired", err)
	}
	changed := w
	changed.Capacity++
	if _, err := s.OpenProposalWithApproval(ctx, proposalFor(changed, d, baseTime.Add(2*time.Minute)), a.ID, changed.OpenTermsHash(d.ID)); !errors.Is(err, store.ErrApprovalTermsChanged) {
		t.Fatalf("open with changed terms: error = %v, want ErrApprovalTermsChanged", err)
	}
	if p, err := s.ActiveProposal(ctx, w.ID); err != nil || p != nil {
		t.Fatalf("a refused open left a proposal: %+v, %v", p, err)
	}
	other, _, _ := newWorkshop(t, s, 2, 6)
	od := firstDate(t, s, other)
	if _, err := s.OpenProposalWithApproval(ctx, proposalFor(other, od, baseTime.Add(2*time.Minute)), a.ID, other.OpenTermsHash(od.ID)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("open another workshop with this approval: error = %v, want ErrNotFound", err)
	}
}
