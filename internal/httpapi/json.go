package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
)

type apiError struct {
	status  int
	code    string
	message string
	reason  string
}

func (e *apiError) Error() string { return e.code + ": " + e.message }

func newError(status int, code, message string) *apiError {
	return &apiError{status: status, code: code, message: message}
}

func badRequest(code, format string, args ...any) *apiError {
	return newError(http.StatusBadRequest, code, fmt.Sprintf(format, args...))
}

func conflict(code, message string) *apiError {
	return newError(http.StatusConflict, code, message)
}

var (
	errNotFound             = newError(http.StatusNotFound, "not_found", "Nothing was found at this address.")
	errInternal             = newError(http.StatusInternalServerError, "internal_error", "Something went wrong on our side. Please try again.")
	errBodyTooLarge         = newError(http.StatusRequestEntityTooLarge, "body_too_large", "The request body is larger than 64 KiB.")
	errInvalidJSON          = badRequest("invalid_json", "The request body is not a valid JSON object for this endpoint.")
	errUnsupportedMediaType = newError(http.StatusUnsupportedMediaType, "unsupported_media_type", "Send the request body as application/json.")
	errRateLimited          = newError(http.StatusTooManyRequests, "rate_limited", "Too many requests from this address; try again in a minute.")
	errAIBusy               = newError(http.StatusServiceUnavailable, "ai_busy", "Many messages are being read right now; please try again in a moment.")
	errMethodRefused        = newError(http.StatusMethodNotAllowed, "method_not_allowed", "This address only answers GET requests.")
)

type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Reason  string `json:"reason,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, e *apiError) {
	writeJSON(w, e.status, errorBody{Error: e.code, Message: e.message, Reason: e.reason})
}

func decode(r *http.Request, dst any) error {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return errUnsupportedMediaType
	}
	dec := json.NewDecoder(r.Body)
	err := dec.Decode(dst)
	if err == nil {
		if extra := dec.Decode(&struct{}{}); !errors.Is(extra, io.EOF) {
			err = extra
			if err == nil {
				err = errors.New("trailing data")
			}
		}
	}
	var tooLarge *http.MaxBytesError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &tooLarge):
		return errBodyTooLarge
	default:
		return errInvalidJSON
	}
}
