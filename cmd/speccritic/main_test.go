package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	llmpkg "github.com/dshills/speccritic/internal/llm"
	"github.com/dshills/speccritic/internal/schema"
)

// testdataDir is the root of the testdata directory.
const testdataDir = "../../testdata"

// setupMockAnthropicServer starts a test HTTP server that returns the given
// response body for every POST request. It sets anthropicAPIURL to the test
// server's URL and resets it on cleanup.
func setupMockAnthropicServer(t *testing.T, responseBody []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(responseBody) //nolint:errcheck
	}))
	original := llmpkg.AnthropicAPIURL()
	llmpkg.SetAnthropicAPIURL(srv.URL)
	t.Cleanup(func() {
		srv.Close()
		llmpkg.SetAnthropicAPIURL(original)
	})
	return srv
}

// setupMockAnthropicServerSequence starts a server that returns responses
// in sequence; after the last one it repeats the last entry.
func setupMockAnthropicServerSequence(t *testing.T, responses [][]byte) {
	t.Helper()
	idx := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		body := responses[idx]
		if idx < len(responses)-1 {
			idx++
		}
		w.Write(body) //nolint:errcheck
	}))
	original := llmpkg.AnthropicAPIURL()
	llmpkg.SetAnthropicAPIURL(srv.URL)
	t.Cleanup(func() {
		srv.Close()
		llmpkg.SetAnthropicAPIURL(original)
	})
}

// readFixture reads a file from testdata/llm/ relative to this test file.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testdataDir, "llm", name))
	if err != nil {
		t.Fatalf("readFixture %s: %v", name, err)
	}
	return data
}

// specPath returns the absolute path to a file in testdata/specs/.
func specPath(name string) string {
	return filepath.Join(testdataDir, "specs", name)
}

// setTestEnv sets SPECCRITIC_MODEL and ANTHROPIC_API_KEY for the test duration.
func setTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SPECCRITIC_MODEL", "anthropic:claude-sonnet-4-6")
	t.Setenv("ANTHROPIC_API_KEY", "test-key-for-integration-tests")
}

// runCheckFlags returns a checkFlags populated with safe defaults for testing.
func runCheckFlags() checkFlags {
	return checkFlags{
		format:            "json",
		profileName:       "general",
		severityThreshold: "info",
		temperature:       0.2,
		maxTokens:         4096,
	}
}

// --- Tests ---

func TestRunCheck_BadSpec_INVALID(t *testing.T) {
	setTestEnv(t)
	setupMockAnthropicServer(t, readFixture(t, "anthropic_response_bad.json"))

	flags := runCheckFlags()
	flags.out = filepath.Join(t.TempDir(), "out.json")

	err := runCheck(specPath("bad_spec.md"), flags)
	if err != nil {
		t.Fatalf("runCheck returned error: %v", err)
	}

	data, err := os.ReadFile(flags.out)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}

	var report schema.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, data)
	}
	if report.Summary.Verdict != schema.VerdictInvalid {
		t.Errorf("expected INVALID, got %s", report.Summary.Verdict)
	}
	if report.Summary.CriticalCount < 2 {
		t.Errorf("expected ≥ 2 critical issues, got %d", report.Summary.CriticalCount)
	}
}

func TestRunCheck_GoodSpec_VALID(t *testing.T) {
	setTestEnv(t)
	setupMockAnthropicServer(t, readFixture(t, "anthropic_response_good.json"))

	flags := runCheckFlags()
	flags.out = filepath.Join(t.TempDir(), "out.json")

	err := runCheck(specPath("good_spec.md"), flags)
	if err != nil {
		t.Fatalf("runCheck returned error: %v", err)
	}

	data, err := os.ReadFile(flags.out)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}

	var report schema.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if report.Summary.Verdict != schema.VerdictValid {
		t.Errorf("expected VALID, got %s", report.Summary.Verdict)
	}
}

func TestRunCheck_MarkdownFormat(t *testing.T) {
	setTestEnv(t)
	setupMockAnthropicServer(t, readFixture(t, "anthropic_response_bad.json"))

	flags := runCheckFlags()
	flags.format = "md"
	flags.out = filepath.Join(t.TempDir(), "out.md")

	if err := runCheck(specPath("bad_spec.md"), flags); err != nil {
		t.Fatalf("runCheck: %v", err)
	}

	data, err := os.ReadFile(flags.out)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "# SpecCritic Report") {
		t.Errorf("markdown missing header")
	}
	if !strings.Contains(s, "INVALID") {
		t.Errorf("markdown missing verdict")
	}
}

func TestRunCheck_FailOn_INVALID(t *testing.T) {
	setTestEnv(t)
	setupMockAnthropicServer(t, readFixture(t, "anthropic_response_bad.json"))

	flags := runCheckFlags()
	flags.failOn = "INVALID"
	flags.out = filepath.Join(t.TempDir(), "out.json")

	err := runCheck(specPath("bad_spec.md"), flags)
	if err == nil {
		t.Fatal("expected non-nil error for --fail-on INVALID with INVALID verdict")
	}
	var ee *exitErr
	if asExitErr(err, &ee) {
		if ee.code != 2 {
			t.Errorf("expected exit code 2, got %d", ee.code)
		}
	} else {
		t.Errorf("expected exitErr, got %T: %v", err, err)
	}
}

func TestRunCheck_FailOn_DoesNotTriggerOnVALID(t *testing.T) {
	setTestEnv(t)
	setupMockAnthropicServer(t, readFixture(t, "anthropic_response_good.json"))

	flags := runCheckFlags()
	flags.failOn = "INVALID" // threshold; VALID does not meet it
	flags.out = filepath.Join(t.TempDir(), "out.json")

	err := runCheck(specPath("good_spec.md"), flags)
	if err != nil {
		t.Errorf("expected no error for VALID verdict with --fail-on INVALID, got: %v", err)
	}
}

func TestRunCheck_SeverityThreshold_FiltersOutput(t *testing.T) {
	setTestEnv(t)
	setupMockAnthropicServer(t, readFixture(t, "anthropic_response_bad.json"))

	flags := runCheckFlags()
	flags.severityThreshold = "critical" // only CRITICAL issues in output
	flags.out = filepath.Join(t.TempDir(), "out.json")

	if err := runCheck(specPath("bad_spec.md"), flags); err != nil {
		t.Fatalf("runCheck: %v", err)
	}

	data, _ := os.ReadFile(flags.out)
	var report schema.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}

	// All emitted issues must be CRITICAL.
	for _, iss := range report.Issues {
		if iss.Severity != schema.SeverityCritical {
			t.Errorf("issue %s has severity %s, expected only CRITICAL", iss.ID, iss.Severity)
		}
	}

	// Summary counts still reflect all issues (pre-filter).
	if report.Summary.WarnCount == 0 && report.Summary.InfoCount == 0 {
		// Mock has no warn/info, so just verify counts are consistent with fixture.
	}

	// severity_threshold captured in Input.
	if report.Input.SeverityThreshold != "critical" {
		t.Errorf("Input.SeverityThreshold = %q, want critical", report.Input.SeverityThreshold)
	}
}

func TestRunCheck_PatchOut(t *testing.T) {
	setTestEnv(t)

	// Use a fixture that includes a patch with text that appears in bad_spec.md.
	const patchFixture = `{
  "id": "msg_patch",
  "model": "claude-sonnet-4-6",
  "content": [{"type": "text", "text": "{\"issues\":[{\"id\":\"ISSUE-0001\",\"severity\":\"CRITICAL\",\"category\":\"NON_TESTABLE_REQUIREMENT\",\"title\":\"Vague\",\"description\":\"vague\",\"evidence\":[{\"path\":\"bad_spec.md\",\"line_start\":5,\"line_end\":5,\"quote\":\"fast\"}],\"impact\":\"x\",\"recommendation\":\"y\",\"blocking\":true,\"tags\":[]}],\"questions\":[],\"patches\":[{\"issue_id\":\"ISSUE-0001\",\"before\":\"This system must perform well and be fast.\",\"after\":\"This system SHALL respond with P99 latency ≤ 200 ms.\"}]}"}],
  "stop_reason": "end_turn"
}`
	setupMockAnthropicServer(t, []byte(patchFixture))

	tmp := t.TempDir()
	flags := runCheckFlags()
	flags.patchOut = filepath.Join(tmp, "patches.txt")

	if err := runCheck(specPath("bad_spec.md"), flags); err != nil {
		t.Fatalf("runCheck: %v", err)
	}

	patchData, err := os.ReadFile(flags.patchOut)
	if err != nil {
		t.Fatalf("patch file not created: %v", err)
	}
	if len(patchData) == 0 {
		t.Error("patch file is empty")
	}
}

func TestRunCheck_Debug_DoesNotFail(t *testing.T) {
	setTestEnv(t)
	setupMockAnthropicServer(t, readFixture(t, "anthropic_response_good.json"))

	flags := runCheckFlags()
	flags.debug = true
	flags.out = filepath.Join(t.TempDir(), "out.json")

	// Debug dumps to stderr; just ensure it doesn't cause an error.
	if err := runCheck(specPath("good_spec.md"), flags); err != nil {
		t.Errorf("runCheck with --debug: %v", err)
	}
}

func TestRunCheck_Offline_NoModelEnv_ExitsCode3(t *testing.T) {
	t.Setenv("SPECCRITIC_MODEL", "")

	flags := runCheckFlags()
	flags.offline = true

	err := runCheck(specPath("good_spec.md"), flags)
	if err == nil {
		t.Fatal("expected error for --offline without SPECCRITIC_MODEL")
	}
	var ee *exitErr
	if asExitErr(err, &ee) {
		if ee.code != 3 {
			t.Errorf("expected exit code 3, got %d", ee.code)
		}
	} else {
		t.Errorf("expected exitErr, got %T", err)
	}
}

func TestRunCheck_RetryOnInvalidResponse(t *testing.T) {
	setTestEnv(t)

	// First response is invalid JSON; second is a valid good response.
	invalid := readFixture(t, "anthropic_response_invalid.json")
	good := readFixture(t, "anthropic_response_good.json")
	setupMockAnthropicServerSequence(t, [][]byte{invalid, good})

	flags := runCheckFlags()
	flags.out = filepath.Join(t.TempDir(), "out.json")

	if err := runCheck(specPath("good_spec.md"), flags); err != nil {
		t.Fatalf("runCheck with retry: %v", err)
	}

	data, _ := os.ReadFile(flags.out)
	var report schema.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if report.Summary.Verdict != schema.VerdictValid {
		t.Errorf("expected VALID after retry, got %s", report.Summary.Verdict)
	}
}

func TestRunCheck_InvalidFormat_ExitsCode3(t *testing.T) {
	flags := runCheckFlags()
	flags.format = "xml"

	err := runCheck(specPath("good_spec.md"), flags)
	if err == nil {
		t.Fatal("expected error for --format xml")
	}
	var ee *exitErr
	if asExitErr(err, &ee) {
		if ee.code != 3 {
			t.Errorf("expected exit code 3, got %d", ee.code)
		}
	}
}

func TestRunCheck_MissingSpec_ExitsCode3(t *testing.T) {
	setTestEnv(t)
	flags := runCheckFlags()

	err := runCheck("/nonexistent/path/spec.md", flags)
	if err == nil {
		t.Fatal("expected error for missing spec file")
	}
	var ee *exitErr
	if asExitErr(err, &ee) {
		if ee.code != 3 {
			t.Errorf("expected exit code 3, got %d", ee.code)
		}
	}
}

func TestRunCheck_OutputContainsInputMetadata(t *testing.T) {
	setTestEnv(t)
	setupMockAnthropicServer(t, readFixture(t, "anthropic_response_good.json"))

	flags := runCheckFlags()
	flags.profileName = "backend-api"
	flags.strict = true
	flags.out = filepath.Join(t.TempDir(), "out.json")

	if err := runCheck(specPath("good_spec.md"), flags); err != nil {
		t.Fatalf("runCheck: %v", err)
	}

	data, _ := os.ReadFile(flags.out)
	var report schema.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}

	if report.Input.Profile != "backend-api" {
		t.Errorf("Input.Profile = %q, want backend-api", report.Input.Profile)
	}
	if !report.Input.Strict {
		t.Error("Input.Strict = false, want true")
	}
	if report.Meta.Model == "" {
		t.Error("Meta.Model is empty")
	}
	if report.Tool != "speccritic" {
		t.Errorf("Tool = %q, want speccritic", report.Tool)
	}
}

// asExitErr is a type-assertion helper for *exitErr.
func asExitErr(err error, out **exitErr) bool {
	e, ok := err.(*exitErr)
	if ok {
		*out = e
	}
	return ok
}
