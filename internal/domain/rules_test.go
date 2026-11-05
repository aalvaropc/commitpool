package domain_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
)

var (
	opensAt  = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	closesAt = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	startsAt = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
)

func workshopIn(status domain.WorkshopStatus, resolution domain.Resolution) domain.Workshop {
	return domain.Workshop{
		ID:            "w1",
		Title:         "Go workshop",
		Timezone:      "UTC",
		Minimum:       3,
		Capacity:      6,
		Price:         domain.Money{Minor: 1000, Currency: "USD"},
		Status:        status,
		Resolution:    resolution,
		PaymentWindow: 24 * time.Hour,
	}
}

func proposalFor(w domain.Workshop) domain.Proposal {
	return domain.Proposal{
		ID:       "p1",
		Version:  1,
		DateID:   "d1",
		OpensAt:  opensAt,
		ClosesAt: closesAt,
		StartsAt: startsAt,
		Price:    w.Price,
		Minimum:  w.Minimum,
		Capacity: w.Capacity,
	}
}

func enr(id string, status domain.EnrollmentStatus) domain.Enrollment {
	return domain.Enrollment{ID: id, ParticipantID: "participant-" + id, Status: status}
}

func TestEnrollmentStatus_TerminalAndOccupies(t *testing.T) {
	tests := []struct {
		status   domain.EnrollmentStatus
		terminal bool
		occupies bool
	}{
		{domain.EnrollmentSeatReserved, false, true},
		{domain.EnrollmentAuthorized, false, true},
		{domain.EnrollmentCapturing, false, true},
		{domain.EnrollmentCapturePending, false, true},
		{domain.EnrollmentPaid, false, true},
		{domain.EnrollmentCaptureFailed, true, false},
		{domain.EnrollmentVoiding, false, false},
		{domain.EnrollmentVoided, true, false},
		{domain.EnrollmentRefunding, false, false},
		{domain.EnrollmentRefundPending, false, false},
		{domain.EnrollmentRefunded, true, false},
		{domain.EnrollmentRefundFailed, true, false},
		{domain.EnrollmentNeedsAttention, true, false},
		{domain.EnrollmentReleased, true, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.Terminal(); got != tt.terminal {
				t.Errorf("Terminal() = %v, want %v", got, tt.terminal)
			}
			if got := tt.status.Occupies(); got != tt.occupies {
				t.Errorf("Occupies() = %v, want %v", got, tt.occupies)
			}
		})
	}
}

func TestStatusValues_AreTheContractStrings(t *testing.T) {
	got := []string{
		string(domain.WorkshopCollecting), string(domain.WorkshopFunding), string(domain.WorkshopConfirmed), string(domain.WorkshopCancelled),
		string(domain.ResolutionOpen), string(domain.ResolutionResolving), string(domain.ResolutionSettled), string(domain.ResolutionNeedsAttention),
		string(domain.EnrollmentSeatReserved), string(domain.EnrollmentAuthorized), string(domain.EnrollmentCapturing), string(domain.EnrollmentCapturePending),
		string(domain.EnrollmentPaid), string(domain.EnrollmentCaptureFailed), string(domain.EnrollmentVoiding), string(domain.EnrollmentVoided),
		string(domain.EnrollmentRefunding), string(domain.EnrollmentRefundPending), string(domain.EnrollmentRefunded), string(domain.EnrollmentRefundFailed),
		string(domain.EnrollmentNeedsAttention), string(domain.EnrollmentReleased),
		string(domain.ActionNone), string(domain.ActionRefund), string(domain.ActionKeepSeat),
	}
	want := []string{
		"COLLECTING", "FUNDING", "CONFIRMED", "CANCELLED",
		"OPEN", "RESOLVING", "SETTLED", "NEEDS_ATTENTION",
		"SEAT_RESERVED", "AUTHORIZED", "CAPTURING", "CAPTURE_PENDING",
		"PAID", "CAPTURE_FAILED", "VOIDING", "VOIDED",
		"REFUNDING", "REFUND_PENDING", "REFUNDED", "REFUND_FAILED",
		"NEEDS_ATTENTION", "RELEASED",
		"none", "refund", "keep_seat",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("status strings = %v, want %v", got, want)
	}
}

func TestCounts_TallyFields(t *testing.T) {
	es := []domain.Enrollment{
		enr("e1", domain.EnrollmentSeatReserved),
		enr("e2", domain.EnrollmentAuthorized),
		enr("e3", domain.EnrollmentAuthorized),
		enr("e4", domain.EnrollmentCapturing),
		enr("e5", domain.EnrollmentCapturePending),
		enr("e6", domain.EnrollmentCapturePending),
		enr("e7", domain.EnrollmentPaid),
		enr("e8", domain.EnrollmentPaid),
		enr("e9", domain.EnrollmentPaid),
		enr("e10", domain.EnrollmentCaptureFailed),
		enr("e11", domain.EnrollmentReleased),
		enr("e12", domain.EnrollmentRefunded),
		enr("e13", domain.EnrollmentVoiding),
	}
	want := domain.Tally{Reserved: 1, Authorized: 2, Capturing: 1, Pending: 2, Paid: 3, Failed: 1, Occupying: 9}
	if got := domain.Counts(es); got != want {
		t.Fatalf("Counts() = %+v, want %+v", got, want)
	}
}

func TestReserveSeat_PendingOccupiesCapacity(t *testing.T) {
	full := []domain.Enrollment{
		enr("e1", domain.EnrollmentPaid), enr("e2", domain.EnrollmentPaid), enr("e3", domain.EnrollmentPaid),
		enr("e4", domain.EnrollmentCapturePending), enr("e5", domain.EnrollmentCapturePending),
		enr("e6", domain.EnrollmentSeatReserved),
	}
	if err := domain.ReserveSeat(domain.Counts(full), 6); !errors.Is(err, domain.ErrCapacityFull) {
		t.Fatalf("ReserveSeat with 6 occupying of 6 = %v, want ErrCapacityFull", err)
	}

	withRelease := slices.Clone(full)
	withRelease[5] = enr("e6", domain.EnrollmentReleased)
	if err := domain.ReserveSeat(domain.Counts(withRelease), 6); err != nil {
		t.Fatalf("ReserveSeat with 5 occupying of 6 = %v, want nil", err)
	}
}

func TestValidateProposal_StartsAtLeast24hAfterClose(t *testing.T) {
	now := opensAt
	tests := []struct {
		name       string
		mutate     func(w *domain.Workshop, p *domain.Proposal, d *domain.CandidateDate)
		compatible int
		wantErr    error
	}{
		{name: "starts exactly 24h after close", compatible: 3},
		{
			name: "starts one minute short of 24h after close",
			mutate: func(_ *domain.Workshop, p *domain.Proposal, d *domain.CandidateDate) {
				p.StartsAt = startsAt.Add(-time.Minute)
				d.StartsAt = p.StartsAt
			},
			compatible: 3,
			wantErr:    domain.ErrStartsTooSoon,
		},
		{name: "compatible below minimum", compatible: 2, wantErr: domain.ErrNotEnoughCompatible},
		{
			name: "workshop already funding",
			mutate: func(w *domain.Workshop, _ *domain.Proposal, _ *domain.CandidateDate) {
				w.Status = domain.WorkshopFunding
			},
			compatible: 3,
			wantErr:    domain.ErrWorkshopNotCollecting,
		},
		{
			name: "close not equal to open plus window",
			mutate: func(_ *domain.Workshop, p *domain.Proposal, _ *domain.CandidateDate) {
				p.ClosesAt = closesAt.Add(time.Hour)
			},
			compatible: 3,
			wantErr:    domain.ErrWindowMismatch,
		},
		{
			name: "window beyond PayPal's three-day honour period",
			mutate: func(w *domain.Workshop, p *domain.Proposal, d *domain.CandidateDate) {
				w.PaymentWindow = 72*time.Hour + time.Minute
				p.ClosesAt = opensAt.Add(w.PaymentWindow)
				p.StartsAt = p.ClosesAt.Add(48 * time.Hour)
				d.StartsAt = p.StartsAt
			},
			compatible: 3,
			wantErr:    domain.ErrWindowTooLong,
		},
		{
			name: "three-day window honoured",
			mutate: func(w *domain.Workshop, p *domain.Proposal, d *domain.CandidateDate) {
				w.PaymentWindow = 72 * time.Hour
				p.ClosesAt = opensAt.Add(w.PaymentWindow)
				p.StartsAt = p.ClosesAt.Add(48 * time.Hour)
				d.StartsAt = p.StartsAt
			},
			compatible: 3,
		},
		{
			name: "short window honoured",
			mutate: func(w *domain.Workshop, p *domain.Proposal, _ *domain.CandidateDate) {
				w.PaymentWindow = 2 * time.Hour
				p.ClosesAt = opensAt.Add(2 * time.Hour)
			},
			compatible: 3,
		},
		{
			name: "start differs from chosen date",
			mutate: func(_ *domain.Workshop, p *domain.Proposal, _ *domain.CandidateDate) {
				p.StartsAt = startsAt.Add(time.Hour)
			},
			compatible: 3,
			wantErr:    domain.ErrStartsAtMismatch,
		},
		{
			name:       "minimum above capacity",
			mutate:     func(_ *domain.Workshop, p *domain.Proposal, _ *domain.CandidateDate) { p.Minimum = 7 },
			compatible: 7,
			wantErr:    domain.ErrMinimumAboveCapacity,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := workshopIn(domain.WorkshopCollecting, domain.ResolutionOpen)
			p := proposalFor(w)
			date := domain.CandidateDate{ID: "d1", StartsAt: startsAt, EndsAt: startsAt.Add(3 * time.Hour)}
			if tt.mutate != nil {
				tt.mutate(&w, &p, &date)
			}
			err := domain.ValidateProposal(w, p, date, tt.compatible, now)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateProposal() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateProposal() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestShouldStartCaptureRound_NeedsMinimumAuthorizedPlusPaid(t *testing.T) {
	before := closesAt.Add(-time.Hour)
	tests := []struct {
		name   string
		status domain.WorkshopStatus
		tally  domain.Tally
		now    time.Time
		want   bool
	}{
		{"two authorized of three", domain.WorkshopFunding, domain.Tally{Paid: 0, Authorized: 2}, before, false},
		{"three authorized of three", domain.WorkshopFunding, domain.Tally{Paid: 0, Authorized: 3}, before, true},
		{"one paid and two authorized", domain.WorkshopFunding, domain.Tally{Paid: 1, Authorized: 2}, before, true},
		{"minimum already paid", domain.WorkshopFunding, domain.Tally{Paid: 3}, before, false},
		{"exactly at close", domain.WorkshopFunding, domain.Tally{Authorized: 3}, closesAt, false},
		{"after close", domain.WorkshopFunding, domain.Tally{Authorized: 3}, closesAt.Add(time.Minute), false},
		{"workshop still collecting", domain.WorkshopCollecting, domain.Tally{Authorized: 3}, before, false},
		{"workshop confirmed", domain.WorkshopConfirmed, domain.Tally{Authorized: 3}, before, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := workshopIn(tt.status, domain.ResolutionOpen)
			p := proposalFor(w)
			if got := domain.ShouldStartCaptureRound(w, p, tt.tally, tt.now); got != tt.want {
				t.Fatalf("ShouldStartCaptureRound(%+v) = %v, want %v", tt.tally, got, tt.want)
			}
		})
	}
}

func TestAfterCaptureResult_ThreePaidOneFailedConfirms(t *testing.T) {
	tests := []struct {
		name           string
		status         domain.WorkshopStatus
		resolution     domain.Resolution
		tally          domain.Tally
		wantStatus     domain.WorkshopStatus
		wantResolution domain.Resolution
	}{
		{"three paid one failed", domain.WorkshopFunding, domain.ResolutionOpen, domain.Tally{Paid: 3, Failed: 1}, domain.WorkshopConfirmed, domain.ResolutionOpen},
		{"two paid is not enough", domain.WorkshopFunding, domain.ResolutionOpen, domain.Tally{Paid: 2, Failed: 1}, domain.WorkshopFunding, domain.ResolutionOpen},
		{"cancelled workshop is never confirmed", domain.WorkshopCancelled, domain.ResolutionResolving, domain.Tally{Paid: 3}, domain.WorkshopCancelled, domain.ResolutionResolving},
		{"confirmed workshop keeps its resolution", domain.WorkshopConfirmed, domain.ResolutionSettled, domain.Tally{Paid: 4}, domain.WorkshopConfirmed, domain.ResolutionSettled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := workshopIn(tt.status, tt.resolution)
			p := proposalFor(w)
			gotStatus, gotResolution := domain.AfterCaptureResult(w, p, tt.tally)
			if gotStatus != tt.wantStatus || gotResolution != tt.wantResolution {
				t.Fatalf("AfterCaptureResult() = %s, %s; want %s, %s", gotStatus, gotResolution, tt.wantStatus, tt.wantResolution)
			}
		})
	}
}

func assertClose(t *testing.T, got domain.Close, status domain.WorkshopStatus, resolution domain.Resolution, voids, refunds, reconciles []string) {
	t.Helper()
	if got.NewStatus != status || got.Resolution != resolution {
		t.Errorf("status, resolution = %s, %s; want %s, %s", got.NewStatus, got.Resolution, status, resolution)
	}
	if !slices.Equal(got.Voids, voids) {
		t.Errorf("Voids = %v, want %v", got.Voids, voids)
	}
	if !slices.Equal(got.Refunds, refunds) {
		t.Errorf("Refunds = %v, want %v", got.Refunds, refunds)
	}
	if !slices.Equal(got.Reconciles, reconciles) {
		t.Errorf("Reconciles = %v, want %v", got.Reconciles, reconciles)
	}
}

func TestCloseDecision_TwoPaidOfThreeCancelsVoidsAndRefunds(t *testing.T) {
	w := workshopIn(domain.WorkshopFunding, domain.ResolutionOpen)
	p := proposalFor(w)
	es := []domain.Enrollment{
		enr("e1", domain.EnrollmentPaid),
		enr("e2", domain.EnrollmentPaid),
		enr("e3", domain.EnrollmentAuthorized),
		enr("e4", domain.EnrollmentCaptureFailed),
	}
	got := domain.CloseDecision(w, p, es, closesAt)
	assertClose(t, got, domain.WorkshopCancelled, domain.ResolutionResolving, []string{"e3"}, []string{"e1", "e2"}, nil)
}

func TestCloseDecision_PendingCaptureIsReconciledNotCounted(t *testing.T) {
	w := workshopIn(domain.WorkshopFunding, domain.ResolutionOpen)
	p := proposalFor(w)
	es := []domain.Enrollment{
		enr("e1", domain.EnrollmentPaid),
		enr("e2", domain.EnrollmentPaid),
		enr("e3", domain.EnrollmentCapturePending),
		enr("e4", domain.EnrollmentCapturing),
	}
	got := domain.CloseDecision(w, p, es, closesAt)
	assertClose(t, got, domain.WorkshopCancelled, domain.ResolutionResolving, nil, []string{"e1", "e2"}, []string{"e3", "e4"})
}

func TestCloseDecision_ConfirmedWorkshopNeverCancels(t *testing.T) {
	w := workshopIn(domain.WorkshopConfirmed, domain.ResolutionOpen)
	p := proposalFor(w)
	es := []domain.Enrollment{
		enr("e1", domain.EnrollmentPaid),
		enr("e2", domain.EnrollmentPaid),
		enr("e3", domain.EnrollmentPaid),
		enr("e4", domain.EnrollmentAuthorized),
	}
	got := domain.CloseDecision(w, p, es, closesAt)
	assertClose(t, got, domain.WorkshopConfirmed, domain.ResolutionOpen, []string{"e4"}, nil, nil)
}

func TestCloseDecision_ConfirmedKeepsAuthorizedWhoseCaptureStarted(t *testing.T) {
	w := workshopIn(domain.WorkshopConfirmed, domain.ResolutionOpen)
	p := proposalFor(w)
	started := closesAt.Add(-time.Minute)
	inFlight := enr("e4", domain.EnrollmentAuthorized)
	inFlight.CaptureStartedAt = &started
	es := []domain.Enrollment{
		enr("e1", domain.EnrollmentPaid),
		enr("e2", domain.EnrollmentCapturePending),
		enr("e3", domain.EnrollmentAuthorized),
		inFlight,
		enr("e5", domain.EnrollmentCapturing),
	}
	got := domain.CloseDecision(w, p, es, closesAt)
	assertClose(t, got, domain.WorkshopConfirmed, domain.ResolutionOpen, []string{"e3"}, nil, []string{"e2", "e5"})
}

func TestCloseDecision_FundingWithMinimumReachedConfirms(t *testing.T) {
	w := workshopIn(domain.WorkshopFunding, domain.ResolutionOpen)
	p := proposalFor(w)
	es := []domain.Enrollment{
		enr("e1", domain.EnrollmentPaid),
		enr("e2", domain.EnrollmentPaid),
		enr("e3", domain.EnrollmentPaid),
		enr("e4", domain.EnrollmentAuthorized),
		enr("e5", domain.EnrollmentCapturePending),
	}
	got := domain.CloseDecision(w, p, es, closesAt)
	assertClose(t, got, domain.WorkshopConfirmed, domain.ResolutionOpen, []string{"e4"}, nil, []string{"e5"})
}

func TestCloseDecision_NothingToCloseLeavesWorkshopAlone(t *testing.T) {
	for _, status := range []domain.WorkshopStatus{domain.WorkshopCollecting, domain.WorkshopCancelled} {
		t.Run(string(status), func(t *testing.T) {
			w := workshopIn(status, domain.ResolutionResolving)
			p := proposalFor(w)
			es := []domain.Enrollment{enr("e1", domain.EnrollmentPaid), enr("e2", domain.EnrollmentAuthorized)}
			got := domain.CloseDecision(w, p, es, closesAt)
			assertClose(t, got, status, domain.ResolutionResolving, nil, nil, nil)
		})
	}
}

func TestLateCapture_AfterCancelRefunds(t *testing.T) {
	tests := []struct {
		status domain.EnrollmentStatus
		want   domain.Action
	}{
		{domain.EnrollmentPaid, domain.ActionRefund},
		{domain.EnrollmentAuthorized, domain.ActionNone},
		{domain.EnrollmentRefunded, domain.ActionNone},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := domain.LateCaptureAfterCancel(enr("e1", tt.status)); got != tt.want {
				t.Fatalf("LateCaptureAfterCancel(%s) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestLateCapture_AfterConfirmKeepsSeatBeforeStart(t *testing.T) {
	tests := []struct {
		name   string
		status domain.EnrollmentStatus
		now    time.Time
		want   domain.Action
	}{
		{"paid before start", domain.EnrollmentPaid, startsAt.Add(-time.Second), domain.ActionKeepSeat},
		{"paid exactly at start", domain.EnrollmentPaid, startsAt, domain.ActionRefund},
		{"paid after start", domain.EnrollmentPaid, startsAt.Add(time.Hour), domain.ActionRefund},
		{"not paid", domain.EnrollmentCapturePending, startsAt.Add(-time.Hour), domain.ActionNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := domain.LateCaptureAfterConfirm(enr("e1", tt.status), startsAt, tt.now); got != tt.want {
				t.Fatalf("LateCaptureAfterConfirm() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCaptureDeadline_EarlierWins(t *testing.T) {
	tests := []struct {
		name    string
		expires time.Time
		want    time.Time
	}{
		{"authorization expires first", closesAt.Add(-time.Hour), closesAt.Add(-time.Hour)},
		{"close comes first", closesAt.Add(72 * time.Hour), closesAt},
		{"both at the same instant", closesAt, closesAt},
		{"unknown authorization expiry", time.Time{}, closesAt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := domain.CaptureDeadline(closesAt, tt.expires); !got.Equal(tt.want) {
				t.Fatalf("CaptureDeadline() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPaymentsOpen_FundingOrConfirmedBeforeClose(t *testing.T) {
	closes := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	p := domain.Proposal{ClosesAt: closes}
	cases := []struct {
		status domain.WorkshopStatus
		now    time.Time
		want   bool
	}{
		{domain.WorkshopFunding, closes.Add(-time.Minute), true},
		{domain.WorkshopConfirmed, closes.Add(-time.Minute), true},
		{domain.WorkshopFunding, closes, false},
		{domain.WorkshopConfirmed, closes, false},
		{domain.WorkshopConfirmed, closes.Add(time.Hour), false},
		{domain.WorkshopCollecting, closes.Add(-time.Minute), false},
		{domain.WorkshopCancelled, closes.Add(-time.Minute), false},
	}
	for _, c := range cases {
		if got := domain.PaymentsOpen(domain.Workshop{Status: c.status}, p, c.now); got != c.want {
			t.Errorf("PaymentsOpen(%s, %s before close) = %v, want %v", c.status, closes.Sub(c.now), got, c.want)
		}
	}
}

func TestCanCancel(t *testing.T) {
	starts := time.Date(2026, 11, 14, 15, 0, 0, 0, time.UTC)
	before := starts.Add(-time.Hour)
	cases := []struct {
		status   domain.WorkshopStatus
		startsAt *time.Time
		now      time.Time
		want     error
	}{
		{domain.WorkshopCollecting, nil, before, nil},
		{domain.WorkshopFunding, &starts, before, nil},
		{domain.WorkshopConfirmed, &starts, before, nil},
		{domain.WorkshopConfirmed, &starts, starts, domain.ErrWorkshopStarted},
		{domain.WorkshopCancelled, &starts, before, domain.ErrAlreadyCancelled},
	}
	for _, c := range cases {
		if err := domain.CanCancel(c.status, c.startsAt, c.now); !errors.Is(err, c.want) {
			t.Errorf("CanCancel(%s, now %s) = %v, want %v", c.status, c.now, err, c.want)
		}
	}
}
