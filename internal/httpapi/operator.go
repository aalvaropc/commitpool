package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

type operatorRequest struct {
	EnrollmentID string `json:"enrollment_id"`
}

func (s *server) operatorEnrollment(r *http.Request, ws store.WorkshopRow, required bool) (*store.EnrollmentRow, error) {
	var req operatorRequest
	if r.ContentLength != 0 {
		if err := decode(r, &req); err != nil {
			return nil, err
		}
	}
	if req.EnrollmentID == "" {
		if required {
			return nil, badRequest("invalid_enrollment", "Name the enrollment with enrollment_id.")
		}
		return nil, nil
	}
	en, err := s.Store.EnrollmentByID(r.Context(), req.EnrollmentID)
	if errors.Is(err, store.ErrNotFound) || err == nil && en.WorkshopID != ws.ID {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	return &en, nil
}

func (s *server) operatorReconcile(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.ownerWorkshop(r)
	if err != nil {
		return err
	}
	en, err := s.operatorEnrollment(r, ws, true)
	if err != nil {
		return err
	}
	key, err := s.Worker.OperatorReconcile(r.Context(), en.ID)
	if err != nil {
		return err
	}
	return s.runAndAnswer(w, r.Context(), ws, en.ID, key)
}

func (s *server) operatorRetryRefund(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.ownerWorkshop(r)
	if err != nil {
		return err
	}
	en, err := s.operatorEnrollment(r, ws, true)
	if err != nil {
		return err
	}
	key, err := s.Worker.RetryRefund(r.Context(), en.ID)
	if errors.Is(err, domain.ErrNotRefundFailed) {
		return conflict("not_refund_failed", "Only a refund that failed can be retried.")
	}
	if err != nil {
		return err
	}
	return s.runAndAnswer(w, r.Context(), ws, en.ID, key)
}

func (s *server) operatorClearFlag(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.ownerWorkshop(r)
	if err != nil {
		return err
	}
	en, err := s.operatorEnrollment(r, ws, false)
	if err != nil {
		return err
	}
	ctx := r.Context()
	if en != nil {
		err = s.Worker.ClearEnrollmentFlag(ctx, en.ID)
	} else {
		err = s.Worker.ClearWorkshopFlag(ctx, ws.ID)
	}
	if errors.Is(err, domain.ErrStillUnresolved) {
		return conflict("still_unresolved", "Something still needs a check or is still in flight; resolve it first.")
	}
	if err != nil {
		return err
	}
	after, err := s.Store.WorkshopByID(ctx, ws.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]workshopDTO{"workshop": ownerWorkshopView(after)})
	return nil
}

func (s *server) runAndAnswer(w http.ResponseWriter, ctx context.Context, ws store.WorkshopRow, enrollmentID, key string) error {
	if err := s.Worker.RunKey(ctx, key); err != nil {
		s.Log.Warn("operator action: the job will run on the worker", "enrollment_id", enrollmentID, "error", err)
	}
	en, err := s.Store.EnrollmentByID(ctx, enrollmentID)
	if err != nil {
		return err
	}
	p, err := s.Store.ActiveProposal(ctx, ws.ID)
	if err != nil {
		return err
	}
	latest, err := s.Store.LatestAttempt(ctx, enrollmentID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]enrollmentDTO{"enrollment": enrollmentView(en, ws, p, latest, s.Clock.Now())})
	return nil
}
