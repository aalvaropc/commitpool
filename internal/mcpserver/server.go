package mcpserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aalvaropc/commitpool/internal/clock"
	"github.com/aalvaropc/commitpool/internal/inproc"
)

type Config struct {
	Secret  []byte
	Clock   clock.Clock
	Log     *slog.Logger
	Version string
}

type Server struct {
	cfg Config
	api http.Handler
	srv *mcp.Server
}

func New(cfg Config) *Server {
	if cfg.Secret == nil {
		cfg.Secret = make([]byte, 32)
		_, _ = rand.Read(cfg.Secret)
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.Real{}
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	s := &Server{cfg: cfg}
	s.srv = mcp.NewServer(&mcp.Implementation{Name: "commitpool", Title: "CommitPool", Version: cfg.Version}, &mcp.ServerOptions{
		Instructions: instructions,
	})
	s.addTools()
	return s
}

func (s *Server) Attach(api http.Handler) { s.api = api }

func (s *Server) MCP() *mcp.Server { return s.srv }

const maxBody = 64 << 10

func (s *Server) HTTPHandler() http.Handler {
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s.srv },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: maxBody})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.Body != nil {
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
			if err != nil {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			if trimmed := bytes.TrimLeft(body, " \t\r\n"); len(trimmed) > 0 && trimmed[0] == '[' {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"batch requests are not supported; send one request at a time"}}`))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		h.ServeHTTP(w, r)
	})
}

const instructions = `CommitPool runs paid workshops that only happen when enough people commit.
An organizer proposes candidate dates; participants say in plain words when they can come; CommitPool
reads that, recommends a date, and once the organizer opens payments each participant authorizes a
PayPal hold. Holds are charged only when the minimum is reached; otherwise every hold is released and
every charge refunded. Links are capabilities: an owner link controls the workshop and a participant
link is a person's seat, so keep both private. You can prepare everything, but money moves only when
the person approves at PayPal, and you must show terms to the person and get their yes before
confirm_open_payments or get_checkout_link.`

type apiError struct {
	Status  int
	Code    string `json:"error"`
	Message string `json:"message"`
	Reason  string `json:"reason"`
}

func (e *apiError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("%s (%s, %s)", e.Message, e.Code, e.Reason)
	}
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}

func (s *Server) call(ctx context.Context, method, path string, body, out any) error {
	if s.api == nil {
		return errors.New("mcpserver: no API attached")
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(inproc.With(ctx), method, path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := &recorder{header: http.Header{}, status: http.StatusOK}
	s.api.ServeHTTP(rec, req)
	if rec.status >= 400 {
		ae := &apiError{Status: rec.status}
		if err := json.Unmarshal(rec.body.Bytes(), ae); err != nil || ae.Message == "" {
			ae.Code, ae.Message = "http_"+fmt.Sprint(rec.status), http.StatusText(rec.status)
		}
		return ae
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

var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

var linkKinds = map[string]string{"owner": "owner", "participant": "p", "share": "w"}

func token(link, kind string) (string, error) {
	link = strings.TrimSpace(link)
	segment := linkKinds[kind]
	if tokenPattern.MatchString(link) {
		return link, nil
	}
	u, err := url.Parse(link)
	if err == nil {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 2 && parts[0] == segment && tokenPattern.MatchString(parts[1]) {
			return parts[1], nil
		}
	}
	return "", fmt.Errorf("that is not a CommitPool %s link (it looks like …/%s/<token>)", kind, segment)
}

func logged[In, Out any](s *Server, name string, h mcp.ToolHandlerFor[In, Out]) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		start := time.Now()
		res, out, err := h(ctx, req, in)
		s.cfg.Log.Info("mcp tool", "tool", name, "ok", err == nil, "duration_ms", time.Since(start).Milliseconds())
		return res, out, err
	}
}
