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

type Error struct {
	HTTPStatus int
	Name       string
	Issue      string
	DebugID    string
	Message    string
	Details    []ErrorDetail
	Links      []Link
}

type ErrorDetail struct {
	Field       string
	Value       string
	Location    string
	Issue       string
	Description string
	Links       []Link
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
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	var un *unconfirmedError
	return errors.As(err, &un)
}

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
			Field       string `json:"field"`
			Value       string `json:"value"`
			Location    string `json:"location"`
			Issue       string `json:"issue"`
			Description string `json:"description"`
			Link        []Link `json:"link"`
			Links       []Link `json:"links"`
		} `json:"details"`
		Links       []Link `json:"links"`
		OAuthError  string `json:"error"`
		OAuthDetail string `json:"error_description"`
	}
	if json.Unmarshal(body, &b) != nil {
		return e
	}
	e.Name, e.Message, e.DebugID, e.Links = b.Name, b.Message, b.DebugID, b.Links
	if e.Name == "" {
		e.Name = b.OAuthError
	}
	if e.Message == "" {
		e.Message = b.OAuthDetail
	}
	if len(b.Details) > 0 {
		e.Issue = b.Details[0].Issue
	}
	for _, d := range b.Details {
		e.Details = append(e.Details, ErrorDetail{
			Field: d.Field, Value: d.Value, Location: d.Location, Issue: d.Issue, Description: d.Description,
			Links: append(d.Link, d.Links...),
		})
	}
	return e
}
