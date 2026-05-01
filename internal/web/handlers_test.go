package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/app"
	"github.com/dshills/speccritic/internal/schema"
)

type fakeChecker struct {
	req app.CheckRequest
	err error
}

func (f *fakeChecker) Check(_ context.Context, req app.CheckRequest) (*app.CheckResult, error) {
	f.req = req
	if f.err != nil {
		return nil, f.err
	}
	return &app.CheckResult{
		Report: &schema.Report{
			Summary: schema.Summary{Verdict: schema.VerdictValid, Score: 100},
		},
	}, nil
}

func TestIndex(t *testing.T) {
	server, err := NewServer(DefaultConfig())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing security header")
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Fatal("expected session cookie")
	}
	body := rec.Body.String()
	for _, want := range []string{"SpecCritic", `name="spec_text"`, `name="csrf_token"`, `id="status"`, `id="annotated-spec"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("index body missing %q", want)
		}
	}
}

func TestAssets(t *testing.T) {
	server, err := NewServer(DefaultConfig())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	for _, path := range []string{"/assets/style.css", "/assets/htmx.min.js"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d", path, rec.Code)
		}
	}
}

func TestCheckStubRequiresNonce(t *testing.T) {
	server, err := NewServer(DefaultConfig())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	form := url.Values{"csrf_token": {"wrong"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/checks", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "speccritic_session", Value: "right"})
	req.AddCookie(&http.Cookie{Name: "speccritic_form", Value: "right"})
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestCheckStubAcceptedNonce(t *testing.T) {
	checker := &fakeChecker{}
	server, err := NewServerWithChecker(DefaultConfig(), checker)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	form := url.Values{"csrf_token": {"same"}, "spec_text": {"The system must work."}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/checks", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "speccritic_session", Value: "session"})
	req.AddCookie(&http.Cookie{Name: "speccritic_form", Value: "same"})
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if checker.req.SpecText != "The system must work." {
		t.Fatalf("checker spec text = %q", checker.req.SpecText)
	}
	if !strings.Contains(rec.Body.String(), "VALID") {
		t.Fatalf("response missing verdict: %s", rec.Body.String())
	}
}
