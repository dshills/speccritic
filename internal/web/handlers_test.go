package web

import (
	"bytes"
	"context"
	"encoding/json"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/app"
	"github.com/dshills/speccritic/internal/llm"
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
	issues := []schema.Issue{{
		ID:          "ISSUE-0001",
		Severity:    schema.SeverityCritical,
		Category:    schema.CategoryNonTestableRequirement,
		Title:       "Vague",
		Description: "desc",
		Evidence:    []schema.Evidence{{LineStart: 1, LineEnd: 1}},
	}}
	if req.Preflight {
		issues = append([]schema.Issue{{
			ID:          "PREFLIGHT-TODO-001",
			Severity:    schema.SeverityCritical,
			Category:    schema.CategoryUnspecifiedConstraint,
			Title:       "Placeholder",
			Description: "desc",
			Evidence:    []schema.Evidence{{LineStart: 1, LineEnd: 1}},
			Tags:        []string{"preflight", "preflight-rule:PREFLIGHT-TODO-001"},
		}}, issues...)
	}
	return &app.CheckResult{
		OriginalSpec: req.SpecText,
		PatchDiff:    "# patch\n",
		Report: &schema.Report{
			Tool:    "speccritic",
			Version: "test",
			Input:   schema.Input{SeverityThreshold: req.SeverityThreshold},
			Summary: schema.Summary{Verdict: schema.VerdictInvalid, Score: 80, CriticalCount: 1},
			Issues:  issues,
			Meta:    schema.Meta{Model: "openai:gpt-5", Incremental: incrementalMetaForRequest(req), Convergence: convergenceMetaForRequest(req)},
		},
	}, nil
}

func incrementalMetaForRequest(req app.CheckRequest) *schema.IncrementalMeta {
	if req.IncrementalFromText == "" {
		return nil
	}
	return &schema.IncrementalMeta{Enabled: true, Mode: req.IncrementalMode, ReusedIssues: 1}
}

func convergenceMetaForRequest(req app.CheckRequest) *schema.ConvergenceMeta {
	if req.ConvergenceFromText == "" || !req.ConvergenceReport {
		return nil
	}
	return &schema.ConvergenceMeta{
		Enabled: true,
		Mode:    req.ConvergenceMode,
		Status:  schema.ConvergenceStatusComplete,
		Current: schema.ConvergenceCurrentCounts{New: 1, StillOpen: 2},
		Previous: schema.ConvergenceHistoricalCounts{
			Resolved: 1,
		},
	}
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
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "openai")
	t.Setenv("SPECCRITIC_LLM_MODEL", "gpt-5")
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
	cookies := map[string]string{}
	for _, cookie := range rec.Result().Cookies() {
		cookies[cookie.Name] = cookie.Value
	}
	if cookies["speccritic_session"] == "" || cookies["speccritic_session"] != cookies["speccritic_form"] {
		t.Fatalf("session/form cookies should share nonce: %#v", cookies)
	}
	body := rec.Body.String()
	for _, want := range []string{"SpecCritic", "Model", `name="llm_provider"`, `value="openai"`, `selected`, `name="llm_model"`, `value="gpt-5"`, `name="spec_file"`, `required`, `name="previous_result"`, `name="incremental_base_file"`, `name="incremental_mode"`, `name="convergence_mode"`, `name="preflight"`, `checked`, `button type="submit"`, `disabled`, `name="csrf_token"`, `id="status"`, `id="annotated-spec"`, `id="issue-modal"`, `role="dialog"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("index body missing %q", want)
		}
	}
	for _, unwanted := range []string{`name="spec_text"`, `name="spec_path"`, `name="editor"`, `data-open-editor`} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("index body should not include %q", unwanted)
		}
	}
}

func TestAssets(t *testing.T) {
	server, err := NewServer(DefaultConfig())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	for _, tc := range []struct {
		path        string
		contentType string
	}{
		{"/assets/style.css", "text/css"},
		{"/assets/app.js", "text/javascript"},
		{"/assets/favicon.svg", "image/svg+xml"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s status = %d", tc.path, rec.Code)
			continue
		}
		contentType := rec.Header().Get("Content-Type")
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil {
			t.Errorf("%s invalid content-type %q: %v", tc.path, contentType, err)
		} else if mediaType != tc.contentType {
			t.Errorf("%s content-type = %q, want %q", tc.path, mediaType, tc.contentType)
		}
	}
}

func TestModelsEndpoint(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"gpt-5"},{"id":"text-embedding-3-large"}]}`))
	}))
	defer modelServer.Close()
	old := llm.OpenAIModelsAPIURLForTest()
	llm.SetOpenAIModelsAPIURL(modelServer.URL)
	t.Cleanup(func() { llm.SetOpenAIModelsAPIURL(old) })
	server, err := NewServer(DefaultConfig())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/models?provider=openai", nil)
	req.AddCookie(&http.Cookie{Name: "speccritic_session", Value: "same"})
	req.AddCookie(&http.Cookie{Name: "speccritic_form", Value: "same"})
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var payload modelsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Provider != "openai" || payload.DefaultModel != "gpt-4o" || len(payload.Models) != 1 || payload.Models[0].ID != "gpt-5" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestModelsEndpointRequiresSessionCookies(t *testing.T) {
	server, err := NewServer(DefaultConfig())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/models?provider=openai", nil)
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
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
	if !checker.req.Preflight || checker.req.PreflightMode != "warn" || checker.req.PreflightProfile != checker.req.Profile {
		t.Fatalf("preflight request = enabled %t mode %q profile %q check profile %q", checker.req.Preflight, checker.req.PreflightMode, checker.req.PreflightProfile, checker.req.Profile)
	}
	if checker.req.Chunking != "auto" || checker.req.ChunkLines == 0 || checker.req.ChunkConcurrency == 0 {
		t.Fatalf("chunking request = mode %q lines %d concurrency %d", checker.req.Chunking, checker.req.ChunkLines, checker.req.ChunkConcurrency)
	}
	if checker.req.LLMProvider != llm.DefaultProvider || checker.req.LLMModel != llm.DefaultModel {
		t.Fatalf("model request = %q/%q, want defaults", checker.req.LLMProvider, checker.req.LLMModel)
	}
	if !strings.Contains(rec.Body.String(), "INVALID") {
		t.Fatalf("response missing verdict: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "openai") || !strings.Contains(rec.Body.String(), "gpt-5") {
		t.Fatalf("response missing provider/model display: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "ISSUE-0001") {
		t.Fatalf("response missing issue: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `data-modal-target="#issue-modal"`) {
		t.Fatalf("response missing modal issue target: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Preflight") {
		t.Fatalf("response missing preflight label: %s", rec.Body.String())
	}
}

func TestCheckStubAcceptsProviderAndModel(t *testing.T) {
	checker := &fakeChecker{}
	server, err := NewServerWithChecker(DefaultConfig(), checker)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	body, contentType := multipartSpecRequest(t, "The system must work.", map[string]string{
		"llm_provider": "OpenAI",
		"llm_model":    "gpt-5",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/checks", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(&http.Cookie{Name: "speccritic_session", Value: "session"})
	req.AddCookie(&http.Cookie{Name: "speccritic_form", Value: "same"})
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if checker.req.LLMProvider != "openai" || checker.req.LLMModel != "gpt-5" {
		t.Fatalf("model request = %q/%q, want openai/gpt-5", checker.req.LLMProvider, checker.req.LLMModel)
	}
}

func TestCheckStubAcceptsIncrementalUploads(t *testing.T) {
	checker := &fakeChecker{}
	server, err := NewServerWithChecker(DefaultConfig(), checker)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writer.WriteField("csrf_token", "same")
	writer.WriteField("incremental_mode", "on")
	writer.WriteField("convergence_mode", "on")
	writeMultipartFile(t, writer, "spec_file", "SPEC.md", "# Spec\n")
	writeMultipartFile(t, writer, "previous_result", "previous.json", `{"tool":"speccritic"}`)
	writeMultipartFile(t, writer, "incremental_base_file", "old.md", "# Old\n")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/checks", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: "speccritic_session", Value: "session"})
	req.AddCookie(&http.Cookie{Name: "speccritic_form", Value: "same"})
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if checker.req.IncrementalFromText == "" || checker.req.IncrementalBaseText == "" || checker.req.IncrementalMode != "on" {
		t.Fatalf("incremental request = %#v", checker.req)
	}
	if checker.req.ConvergenceFromText == "" || checker.req.ConvergenceMode != "on" || !checker.req.ConvergenceReport {
		t.Fatalf("convergence request = %#v", checker.req)
	}
	if !strings.Contains(rec.Body.String(), "Incremental") {
		t.Fatalf("response missing incremental metadata: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Convergence") || !strings.Contains(rec.Body.String(), "Still open") {
		t.Fatalf("response missing convergence metadata: %s", rec.Body.String())
	}
}

func TestCheckStubRequiresPreviousUploadForConvergenceOn(t *testing.T) {
	checker := &fakeChecker{}
	server, err := NewServerWithChecker(DefaultConfig(), checker)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	body, contentType := multipartSpecRequest(t, "The system must work.", map[string]string{
		"convergence_mode": "on",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/checks", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(&http.Cookie{Name: "speccritic_session", Value: "session"})
	req.AddCookie(&http.Cookie{Name: "speccritic_form", Value: "same"})
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCheckStubDefaultsModelForSelectedProvider(t *testing.T) {
	checker := &fakeChecker{}
	server, err := NewServerWithChecker(DefaultConfig(), checker)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	body, contentType := multipartSpecRequest(t, "The system must work.", map[string]string{
		"llm_provider": "openai",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/checks", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(&http.Cookie{Name: "speccritic_session", Value: "session"})
	req.AddCookie(&http.Cookie{Name: "speccritic_form", Value: "same"})
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if checker.req.LLMProvider != "openai" || checker.req.LLMModel != "gpt-4o" {
		t.Fatalf("model request = %q/%q, want openai/gpt-4o", checker.req.LLMProvider, checker.req.LLMModel)
	}
}

func TestCheckStubUsesSerialChunkingForGemini(t *testing.T) {
	checker := &fakeChecker{}
	server, err := NewServerWithChecker(DefaultConfig(), checker)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	body, contentType := multipartSpecRequest(t, "The system must work.", map[string]string{
		"llm_provider": "gemini",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/checks", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(&http.Cookie{Name: "speccritic_session", Value: "session"})
	req.AddCookie(&http.Cookie{Name: "speccritic_form", Value: "same"})
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if checker.req.ChunkConcurrency != 1 {
		t.Fatalf("chunk concurrency = %d, want 1 for gemini", checker.req.ChunkConcurrency)
	}
}

func TestCheckStubInfersProviderFromModel(t *testing.T) {
	checker := &fakeChecker{}
	server, err := NewServerWithChecker(DefaultConfig(), checker)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	body, contentType := multipartSpecRequest(t, "The system must work.", map[string]string{
		"llm_model": "gpt-5",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/checks", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(&http.Cookie{Name: "speccritic_session", Value: "session"})
	req.AddCookie(&http.Cookie{Name: "speccritic_form", Value: "same"})
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if checker.req.LLMProvider != "openai" || checker.req.LLMModel != "gpt-5" {
		t.Fatalf("model request = %q/%q, want openai/gpt-5", checker.req.LLMProvider, checker.req.LLMModel)
	}
}

func TestSplitModelDisplay(t *testing.T) {
	for _, tc := range []struct {
		input        string
		wantProvider string
		wantModel    string
	}{
		{input: "openai:gpt-5", wantProvider: "openai", wantModel: "gpt-5"},
		{input: "preflight", wantProvider: "Provider", wantModel: "preflight"},
		{input: "", wantProvider: "Provider", wantModel: "Unknown"},
		{input: "anthropic:", wantProvider: "anthropic", wantModel: "Unknown"},
		{input: " OpenAI : GPT-4 ", wantProvider: "OpenAI", wantModel: "GPT-4"},
	} {
		provider, model := splitModelDisplay(tc.input)
		if provider != tc.wantProvider || model != tc.wantModel {
			t.Fatalf("splitModelDisplay(%q) = %q/%q, want %q/%q", tc.input, provider, model, tc.wantProvider, tc.wantModel)
		}
	}
}

func TestConfiguredModelDisplay(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "")
	t.Setenv("SPECCRITIC_LLM_MODEL", "")
	provider, model := configuredModelDisplay()
	if provider != llm.DefaultProvider || model != llm.DefaultModel {
		t.Fatalf("default configured model = %q/%q", provider, model)
	}

	t.Setenv("SPECCRITIC_LLM_PROVIDER", "openai")
	t.Setenv("SPECCRITIC_LLM_MODEL", "gpt-5")
	provider, model = configuredModelDisplay()
	if provider != "openai" || model != "gpt-5" {
		t.Fatalf("configured model = %q/%q", provider, model)
	}

	t.Setenv("SPECCRITIC_LLM_PROVIDER", "gemini")
	t.Setenv("SPECCRITIC_LLM_MODEL", "")
	provider, model = configuredModelDisplay()
	if provider != "gemini" || model != "gemini-2.0-flash" {
		t.Fatalf("partial configured model = %q/%q", provider, model)
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

func TestCheckStubAllowsDisablingPreflight(t *testing.T) {
	checker := &fakeChecker{}
	server, err := NewServerWithChecker(DefaultConfig(), checker)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	body, contentType := multipartSpecRequest(t, "The system must work.", map[string]string{"preflight": "false"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/checks", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(&http.Cookie{Name: "speccritic_session", Value: "session"})
	req.AddCookie(&http.Cookie{Name: "speccritic_form", Value: "same"})
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if checker.req.Preflight {
		t.Fatal("preflight = true, want false")
	}
	if strings.Contains(rec.Body.String(), "Preflight") {
		t.Fatalf("response should not include preflight label: %s", rec.Body.String())
	}
}

func TestCheckStubAcceptsPreflightMode(t *testing.T) {
	checker := &fakeChecker{}
	server, err := NewServerWithChecker(DefaultConfig(), checker)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	body, contentType := multipartSpecRequest(t, "The system must work.", map[string]string{"preflight_mode": "gate"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/checks", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(&http.Cookie{Name: "speccritic_session", Value: "session"})
	req.AddCookie(&http.Cookie{Name: "speccritic_form", Value: "same"})
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if checker.req.PreflightMode != "gate" {
		t.Fatalf("preflight mode = %q, want gate", checker.req.PreflightMode)
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

func multipartSpecRequest(t *testing.T, specText string, fields ...map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("csrf_token", "same"); err != nil {
		t.Fatalf("write csrf field: %v", err)
	}
	for _, group := range fields {
		for name, value := range group {
			if err := writer.WriteField(name, value); err != nil {
				t.Fatalf("write %s field: %v", name, err)
			}
		}
	}
	writeMultipartFile(t, writer, "spec_file", "SPEC.md", specText)
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &body, writer.FormDataContentType()
}

func writeMultipartFile(t *testing.T, writer *multipart.Writer, field, name, text string) {
	t.Helper()
	part, err := writer.CreateFormFile(field, name)
	if err != nil {
		t.Fatalf("create %s file part: %v", field, err)
	}
	if _, err := part.Write([]byte(text)); err != nil {
		t.Fatalf("write %s file part: %v", field, err)
	}
}
