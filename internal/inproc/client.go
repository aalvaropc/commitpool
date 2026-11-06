package inproc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

type Client struct {
	Handler http.Handler
}

type Error struct {
	Status  int
	Code    string `json:"error"`
	Message string `json:"message"`
	Reason  string `json:"reason"`
}

func (e *Error) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("%s (%s, %s)", e.Message, e.Code, e.Reason)
	}
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}

func (c Client) Call(ctx context.Context, method, path string, body, out any) error {
	if c.Handler == nil {
		return errors.New("inproc: no handler attached")
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(With(ctx), method, path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := &recorder{header: http.Header{}, status: http.StatusOK}
	c.Handler.ServeHTTP(rec, req)
	if rec.status >= 400 {
		e := &Error{Status: rec.status}
		if err := json.Unmarshal(rec.body.Bytes(), e); err != nil || e.Message == "" {
			e.Code, e.Message = fmt.Sprintf("http_%d", rec.status), http.StatusText(rec.status)
		}
		return e
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(rec.body.Bytes(), out)
}

type recorder struct {
	header http.Header
	status int
	wrote  bool
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(status int) {
	if !r.wrote {
		r.status, r.wrote = status, true
	}
}

func (r *recorder) Write(p []byte) (int, error) {
	r.wrote = true
	return r.body.Write(p)
}
