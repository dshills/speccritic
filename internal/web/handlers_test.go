package web

import (
	"bytes"
	"context"
	"mime/multipart"
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
		OriginalSpec: req.SpecText,
		PatchDiff:    "# patch\n",
		Report: &schema.Report{
			Tool:    "speccritic",
			Version: "test",
			Input:   schema.Input{SeverityThreshold: req.SeverityThreshold},
			Summary: schema.Summary{Verdict: schema.VerdictInvalid, Score: 80, CriticalCount: 1},
			Issues: []schema.Issue{{
				ID:          "ISSUE-0001",
				Severity:    schema.SeverityCritical,
				Category:    schema.CategoryNonTestableRequirement,
				Title:       "Vague",
				Description: "desc",
				Evidence:    []schema.Evidence{{LineStart: 1, LineEnd: 1}},
			}},
		},
	}, nil
}

func TestExportEndpoints(t *testing.T) {
	checker := &fakeChecker{}
	server, err := NewServerWithChecker(DefaultConfig(), checker)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	id := createStoredCheck(t, server)

	for _, tc := range []struct {
		path        string
		contentType string
		want        string
	}{
		{"/checks/" + id + "/export.json", "application/json", `"tool": "speccritic"`},
		{"/checks/" + id + "/export.md", "text/markdown; charset=utf-8", "SpecCritic Report"},
		{"/checks/" + id + "/patch.diff", "text/x-diff; charset=utf-8", "# patch"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d", tc.path, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != tc.contentType {
			t.Fatalf("%s content type = %q", tc.path, got)
		}
		if !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("%s body missing %q: %s", tc.path, tc.want, rec.Body.String())
		}
	}
}

func createStoredCheck(t *testing.T, server *Server) string {
	t.Helper()
	body, contentType := multipartSpecRequest(t, "The system must work.")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/checks", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(&http.Cookie{Name: "speccritic_session", Value: "session"})
	req.AddCookie(&http.Cookie{Name: "speccritic_form", Value: "same"})
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("post status = %d", rec.Code)
	}
	if len(server.store.order) == 0 {
		t.Fatal("stored check ID missing")
	}
	return server.store.order[len(server.store.order)-1]
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
	for _, want := range []string{"SpecCritic", `name="spec_file"`, `required`, `button type="submit"`, `disabled`, `name="csrf_token"`, `id="status"`, `id="annotated-spec"`, `id="issue-modal"`, `role="dialog"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("index body missing %q", want)
		}
	}
	if strings.Contains(body, `name="spec_text"`) {
		t.Fatal("index body should not include manual spec text input")
	}
}

func TestAssets(t *testing.T) {
	server, err := NewServer(DefaultConfig())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	for _, path := range []string{"/assets/style.css", "/assets/app.js"} {
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

	body, contentType := multipartSpecRequest(t, "The system must work.")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/checks", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(&http.Cookie{Name: "speccritic_session", Value: "session"})
	req.AddCookie(&http.Cookie{Name: "speccritic_form", Value: "same"})
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if checker.req.SpecText != "The system must work." {
		t.Fatalf("checker spec text = %q", checker.req.SpecText)
	}
	if !strings.Contains(rec.Body.String(), "INVALID") {
		t.Fatalf("response missing verdict: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "ISSUE-0001") {
		t.Fatalf("response missing issue: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `data-modal-target="#issue-modal"`) {
		t.Fatalf("response missing modal issue target: %s", rec.Body.String())
	}
}

func TestCheckStubRequiresUploadedSpec(t *testing.T) {
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

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "uploaded spec file is required") {
		t.Fatalf("response missing upload requirement: %s", rec.Body.String())
	}
	if checker.req.SpecText != "" {
		t.Fatalf("checker should not be called, got spec %q", checker.req.SpecText)
	}
}

func TestIssueDetail(t *testing.T) {
	checker := &fakeChecker{}
	server, err := NewServerWithChecker(DefaultConfig(), checker)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	body, contentType := multipartSpecRequest(t, "The system must work.")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/checks", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(&http.Cookie{Name: "speccritic_session", Value: "session"})
	req.AddCookie(&http.Cookie{Name: "speccritic_form", Value: "same"})
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("post status = %d", rec.Code)
	}

	if len(server.store.order) == 0 {
		t.Fatal("stored check ID missing")
	}
	id := server.store.order[0]

	detail := httptest.NewRecorder()
	detailReq := httptest.NewRequest(http.MethodGet, "/checks/"+id+"/issues/ISSUE-0001", nil)
	server.Handler().ServeHTTP(detail, detailReq)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status = %d", detail.Code)
	}
	if !strings.Contains(detail.Body.String(), "Vague") {
		t.Fatalf("detail missing issue: %s", detail.Body.String())
	}
	if !strings.Contains(detail.Body.String(), `id="issue-modal-title"`) {
		t.Fatalf("detail missing modal title: %s", detail.Body.String())
	}
}

func multipartSpecRequest(t *testing.T, specText string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("csrf_token", "same"); err != nil {
		t.Fatalf("write csrf field: %v", err)
	}
	part, err := writer.CreateFormFile("spec_file", "SPEC.md")
	if err != nil {
		t.Fatalf("create spec file part: %v", err)
	}
	if _, err := part.Write([]byte(specText)); err != nil {
		t.Fatalf("write spec file part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &body, writer.FormDataContentType()
}
