package paypal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// Error is a non-2xx answer from PayPal. It never carries the response body.
type Error struct {
	HTTPStatus int
	Name       string
	Issue      string
	DebugID    string
	Message    string
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "paypal: HTTP %d", e.HTTPStatus)
	if e.Name != "" {
		b.WriteString(" " + e.Name)
	}
	if e.Issue != "" {
		b.WriteString(" (" + e.Issue + ")")
	}
	if e.DebugID != "" {
		b.WriteString(" debug_id=" + e.DebugID)
	}
	return b.String()
}

// IsAmbiguous reports whether the outcome of the request is unknown: it may
// or may not have been processed, so the caller must reconcile by reading the
// resource before acting again. That covers timeouts and any transport
// failure, a 5xx, and a 409 (PREVIOUS_REQUEST_IN_PROGRESS). A 429 is not
// ambiguous: the request was rejected before processing, so retrying with the
// same PayPal-Request-Id is safe. Every other 4xx is a definite answer.
func IsAmbiguous(err error) bool {
	if err == nil {
		return false
	}
	var pe *Error
	if errors.As(err, &pe) {
		return pe.HTTPStatus >= 500 || pe.HTTPStatus == 409
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// net.Error covers *url.Error, which is what http.Client returns for every
	// transport failure, as well as bare timeouts and dial errors.
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	var un *unconfirmedError
	return errors.As(err, &un)
}

// Definitive returns PayPal's error when it is a final answer about the
// resource, one asking again will not change: any 4xx except 401 and 403
// (credentials, not the payment), 409 (ambiguous) and 429 (rejected before
// processing). Any other error, or none, is nil.
func Definitive(err error) *Error {
	var pe *Error
	if !errors.As(err, &pe) || pe.HTTPStatus < 400 || pe.HTTPStatus >= 500 {
		return nil
	}
	switch pe.HTTPStatus {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict, http.StatusTooManyRequests:
		return nil
	}
	return pe
}

// unconfirmedError marks a failure after the request was sent, such as a
// truncated response, where PayPal may still have processed it.
type unconfirmedError struct{ err error }

func (e *unconfirmedError) Error() string { return e.err.Error() }
func (e *unconfirmedError) Unwrap() error { return e.err }

func parseError(status int, body []byte) *Error {
	e := &Error{HTTPStatus: status}
	var b struct {
		Name    string `json:"name"`
		Message string `json:"message"`
		DebugID string `json:"debug_id"`
		Details []struct {
			Issue string `json:"issue"`
		} `json:"details"`
		OAuthError  string `json:"error"`
		OAuthDetail string `json:"error_description"`
	}
	if json.Unmarshal(body, &b) != nil {
		return e
	}
	e.Name, e.Message, e.DebugID = b.Name, b.Message, b.DebugID
	if e.Name == "" {
		e.Name = b.OAuthError
	}
	if e.Message == "" {
		e.Message = b.OAuthDetail
	}
	if len(b.Details) > 0 {
		e.Issue = b.Details[0].Issue
	}
	return e
}
