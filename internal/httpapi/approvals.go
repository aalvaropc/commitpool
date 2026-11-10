package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/inproc"
	"github.com/aalvaropc/commitpool/internal/store"
)

const (
	approvalTTL           = 30 * time.Minute
	approvalKeep          = 10 * time.Minute
	approvalCookieName    = "commitpool_approval"
	approvalCookieContext = "commitpool open approval\n"
	uuidLength            = 36
)

const (
	approvalPending  = "pending"
	approvalApproved = "approved"
	approvalUsed     = "used"
	approvalExpired  = "expired"
	approvalChanged  = "changed"
	approvalClosed   = "closed"
)

var (
	errApprovalNeedsPage = newError(http.StatusForbidden, "approval_needs_page",
		"Only someone with the organizer link can approve, on the approve page in a browser. An assistant's tools cannot.")
	errApprovalPending = conflict("approval_pending",
		"The organizer has not approved these terms yet: they open the approve link and tap Approve.")
	errApprovalExpired = conflict("approval_expired",
		"This request expired. Ask your assistant to prepare the terms again; it will send a new link.")
	errApprovalUsed = conflict("approval_used",
		"These terms were already used to open reservations.")
	errApprovalTermsChanged = conflict("terms_changed",
		"The workshop's terms changed after this request was made, so it can no longer be approved.")
	errApprovalClosed = conflict("approval_closed",
		"Reservations are already open or the workshop was cancelled, so there is nothing to approve.")
)

type approvalDTO struct {
	ID               string     `json:"id"`
	State            string     `json:"state"`
	DateID           string     `json:"date_id"`
	DateLabel        string     `json:"date_label"`
	StartsAt         time.Time  `json:"starts_at"`
	EndsAt           time.Time  `json:"ends_at"`
	Price            moneyDTO   `json:"price"`
	Minimum          int        `json:"minimum"`
	Capacity         int        `json:"capacity"`
	PaymentWindowMin int        `json:"payment_window_min"`
	PayoutEmail      string     `json:"payout_email"`
	ExpiresAt        time.Time  `json:"expires_at"`
	ApprovedAt       *time.Time `json:"approved_at"`
	UsedAt           *time.Time `json:"used_at"`
}

type approvalViewDTO struct {
	Approval   approvalDTO `json:"approval"`
	Workshop   workshopDTO `json:"workshop"`
	ApproveURL string      `json:"approve_url"`
}

func approvalErr(err error) error {
	switch {
	case errors.Is(err, store.ErrApprovalPending):
		return errApprovalPending
	case errors.Is(err, store.ErrApprovalExpired):
		return errApprovalExpired
	case errors.Is(err, store.ErrApprovalUsed):
		return errApprovalUsed
	case errors.Is(err, store.ErrApprovalTermsChanged):
		return errApprovalTermsChanged
	}
	return notFoundIfMissing(err)
}

func approvalState(a store.OpenApprovalRow, ws store.WorkshopRow, now time.Time) string {
	if a.UsedAt != nil {
		return approvalUsed
	}
	if ws.Status != domain.WorkshopCollecting {
		return approvalClosed
	}
	switch err := a.Check(ws.OpenTermsHash(a.DateID), now); {
	case err == nil:
		return approvalApproved
	case errors.Is(err, store.ErrApprovalTermsChanged):
		return approvalChanged
	case errors.Is(err, store.ErrApprovalExpired):
		return approvalExpired
	}
	return approvalPending
}

func (s *server) requestApproval(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.ownerWorkshop(r)
	if err != nil {
		return err
	}
	var req struct {
		DateID string `json:"date_id"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	if _, err := s.proposalOn(ctx, ws, req.DateID); err != nil {
		return err
	}
	a, created, err := s.Store.RequestOpenApproval(ctx, ws, req.DateID, s.Clock.Now(), approvalTTL, approvalKeep)
	if err != nil {
		return err
	}
	view, err := s.approvalView(ctx, r, ws, a)
	if err != nil {
		return err
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, view)
	return nil
}

func (s *server) showApproval(w http.ResponseWriter, r *http.Request) error {
	ws, a, err := s.ownerApproval(r)
	if err != nil {
		return err
	}
	view, err := s.approvalView(r.Context(), r, ws, a)
	if err != nil {
		return err
	}
	if view.Approval.State == approvalPending && !inproc.Is(r.Context()) {
		s.setApprovalCookie(w, r, a)
	}
	writeJSON(w, http.StatusOK, view)
	return nil
}

func (s *server) approveOpen(w http.ResponseWriter, r *http.Request) error {
	if inproc.Is(r.Context()) {
		return errApprovalNeedsPage
	}
	ws, a, err := s.ownerApproval(r)
	if err != nil {
		return err
	}
	if !s.fromApprovalPage(r, a) {
		return errApprovalNeedsPage
	}
	if a.UsedAt != nil {
		return errApprovalUsed
	}
	if ws.Status != domain.WorkshopCollecting {
		return errApprovalClosed
	}
	ctx := r.Context()
	approved, err := s.Store.ApproveOpen(ctx, ws.ID, a.ID, s.Clock.Now(), approvalTTL)
	if err != nil {
		return approvalErr(err)
	}
	s.Log.Info("organizer approved opening reservations", "workshop_id", ws.ID, "approval_id", a.ID)
	view, err := s.approvalView(ctx, r, ws, approved)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, view)
	return nil
}

func (s *server) openApproved(w http.ResponseWriter, r *http.Request) error {
	ws, a, err := s.ownerApproval(r)
	if err != nil {
		return err
	}
	if a.UsedAt != nil {
		return errApprovalUsed
	}
	if ws.Status != domain.WorkshopCollecting {
		return errApprovalClosed
	}
	if err := a.Check(ws.OpenTermsHash(a.DateID), s.Clock.Now()); err != nil {
		return approvalErr(err)
	}
	ctx := r.Context()
	row, err := s.proposalOn(ctx, ws, a.DateID)
	if err != nil {
		return err
	}
	opened, err := s.Store.OpenProposalWithApproval(ctx, row, a.ID)
	if err != nil && !errors.Is(err, store.ErrProposalExists) {
		return approvalErr(err)
	}
	return s.writeOpened(ctx, w, ws, opened, err)
}

func (s *server) ownerApproval(r *http.Request) (store.WorkshopRow, store.OpenApprovalRow, error) {
	ws, err := s.ownerWorkshop(r)
	if err != nil {
		return store.WorkshopRow{}, store.OpenApprovalRow{}, err
	}
	id := r.PathValue("id")
	if !validUUID(id) {
		return store.WorkshopRow{}, store.OpenApprovalRow{}, errNotFound
	}
	a, err := s.Store.OpenApproval(r.Context(), ws.ID, id)
	if err != nil {
		return store.WorkshopRow{}, store.OpenApprovalRow{}, notFoundIfMissing(err)
	}
	return ws, a, nil
}

func validUUID(id string) bool {
	if len(id) != uuidLength {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
				return false
			}
		}
	}
	return true
}

func (s *server) approvalView(ctx context.Context, r *http.Request, ws store.WorkshopRow, a store.OpenApprovalRow) (approvalViewDTO, error) {
	loc, err := time.LoadLocation(ws.Timezone)
	if err != nil {
		return approvalViewDTO{}, err
	}
	dates, err := s.Store.CandidateDates(ctx, ws.ID)
	if err != nil {
		return approvalViewDTO{}, err
	}
	d := approvalDTO{
		ID: a.ID, State: approvalState(a, ws, s.Clock.Now()), DateID: a.DateID,
		Price: money(a.PriceMinor, a.Currency), Minimum: a.Minimum, Capacity: a.Capacity,
		PaymentWindowMin: a.PaymentWindowMin, PayoutEmail: ws.PayoutEmail, ExpiresAt: a.ExpiresAt.UTC(), ApprovedAt: a.ApprovedAt, UsedAt: a.UsedAt,
	}
	for _, date := range dates {
		if date.ID == a.DateID {
			d.StartsAt, d.EndsAt, d.DateLabel = date.StartsAt.UTC(), date.EndsAt.UTC(), dateLabel(date, loc)
		}
	}
	return approvalViewDTO{Approval: d, Workshop: publicWorkshopView(ws), ApproveURL: s.Cfg.BaseURL + "/owner/" + r.PathValue("token") + "/approve/" + a.ID}, nil
}

func approvalPath(r *http.Request, id string) string {
	return "/api/v1/owner/" + r.PathValue("token") + "/approvals/" + id
}

func (s *server) approvalCookieValue(a store.OpenApprovalRow) string {
	m := hmac.New(sha256.New, s.signingKey)
	m.Write([]byte(approvalCookieContext + a.WorkshopID + "\n" + a.ID))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *server) setApprovalCookie(w http.ResponseWriter, r *http.Request, a store.OpenApprovalRow) {
	left := int(a.ExpiresAt.Sub(s.Clock.Now()).Seconds())
	if left <= 0 {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     approvalCookieName,
		Value:    s.approvalCookieValue(a),
		Path:     approvalPath(r, a.ID),
		MaxAge:   left,
		HttpOnly: true,
		Secure:   strings.HasPrefix(s.Cfg.BaseURL, "https://"),
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *server) fromApprovalPage(r *http.Request, a store.OpenApprovalRow) bool {
	if !s.fromSameOriginPage(r) {
		return false
	}
	c, err := r.Cookie(approvalCookieName)
	return err == nil && hmac.Equal([]byte(c.Value), []byte(s.approvalCookieValue(a)))
}
