package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/schema"
)

// A report written by one run must load as the baseline of a later run. The
// tests below write a report with runCheck and read it back through
// --convergence-from and --incremental-from.

// previousReportFlags returns the test flags with the incremental and
// convergence settings at their command-line defaults.
func previousReportFlags(t *testing.T) checkFlags {
	t.Helper()
	flags := runCheckFlags()
	flags.out = filepath.Join(t.TempDir(), "out.json")
	flags.incrementalMode = "auto"
	flags.incrementalMaxChangeRatio = 0.35
	flags.incrementalMaxRemapFailureRatio = 0.25
	flags.incrementalContextLines = 20
	flags.incrementalStrictReuse = true
	flags.convergenceMode = "auto"
	flags.convergenceReport = true
	return flags
}

// specSpellings are the two ways a caller names the spec: a path relative to
// the working directory, and the absolute path an agent usually passes.
var specSpellings = []struct {
	name     string
	absolute bool
}{
	{"relative spec path", false},
	{"absolute spec path", true},
}

// badSpecCopy puts a copy of bad_spec.md in a temporary directory and returns
// the path to pass to runCheck. For a relative path it makes that directory
// the working directory until the test ends.
func badSpecCopy(t *testing.T, absolute bool) string {
	t.Helper()
	data, err := os.ReadFile(specPath("bad_spec.md"))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "bad_spec.md")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	if absolute {
		return path
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	return "bad_spec.md"
}

// preflightOnlyReport writes a preflight-only report for spec and returns it
// with the file it was written to.
func preflightOnlyReport(t *testing.T, spec string, completion bool) (schema.Report, string) {
	t.Helper()
	flags := previousReportFlags(t)
	flags.preflight = true
	flags.preflightMode = "only"
	flags.completionSuggestions = completion
	if err := runCheck(spec, flags); err != nil {
		t.Fatalf("preflight-only run: %v", err)
	}
	report := readJSONReport(t, flags.out)
	if report.Meta.Model != "preflight" || len(report.Issues) == 0 {
		t.Fatalf("model = %q issues = %d, want a preflight-only report with findings", report.Meta.Model, len(report.Issues))
	}
	for _, issue := range report.Issues {
		if !strings.HasPrefix(issue.ID, "PREFLIGHT-") {
			t.Fatalf("issue ID = %q, want a preflight rule ID", issue.ID)
		}
	}
	return report, flags.out
}

// reviewReport writes a single-call review of spec, with preflight findings
// merged in, and returns it with the file it was written to.
func reviewReport(t *testing.T, spec string) (schema.Report, string) {
	t.Helper()
	flags := previousReportFlags(t)
	flags.preflight = true
	if err := runCheck(spec, flags); err != nil {
		t.Fatalf("review run: %v", err)
	}
	report := readJSONReport(t, flags.out)
	if countIDs(report.Issues, "PREFLIGHT-") == 0 || countIDs(report.Issues, "ISSUE-") == 0 {
		t.Fatalf("issues = %v, want preflight and model findings", issueIDs(report.Issues))
	}
	return report, flags.out
}

func countIDs(issues []schema.Issue, prefix string) int {
	n := 0
	for _, issue := range issues {
		if strings.HasPrefix(issue.ID, prefix) {
			n++
		}
	}
	return n
}

func issueIDs(issues []schema.Issue) []string {
	ids := make([]string, 0, len(issues))
	for _, issue := range issues {
		ids = append(ids, issue.ID)
	}
	return ids
}

// wantEvidencePath fails the test unless every evidence entry names want.
func wantEvidencePath(t *testing.T, report schema.Report, want string) {
	t.Helper()
	for _, issue := range report.Issues {
		for _, ev := range issue.Evidence {
			if ev.Path != want {
				t.Errorf("%s evidence path = %q, want %q", issue.ID, ev.Path, want)
			}
		}
	}
}

// wantAllStillOpen fails the test unless convergence matched every current
// finding to the previous report.
func wantAllStillOpen(t *testing.T, report schema.Report) {
	t.Helper()
	meta := report.Meta.Convergence
	if meta == nil || meta.Status != schema.ConvergenceStatusComplete {
		t.Fatalf("convergence meta = %#v, want status complete", meta)
	}
	if meta.Current.StillOpen != len(report.Issues) || meta.Current.New != 0 || meta.Previous.Resolved != 0 {
		t.Fatalf("convergence current = %+v previous = %+v, want all %d findings still open", meta.Current, meta.Previous, len(report.Issues))
	}
}

func wantExitCode3(t *testing.T, err error, msg string) {
	t.Helper()
	var ee *exitErr
	if !asExitErr(err, &ee) || ee.code != 3 || !strings.Contains(err.Error(), msg) {
		t.Fatalf("error = %v, want exit code 3 with %q", err, msg)
	}
}

func TestRunCheck_PreflightOnlyReportRoundTrips(t *testing.T) {
	fixture := readFixture(t, "anthropic_response_bad.json")
	for _, spelling := range specSpellings {
		t.Run(spelling.name, func(t *testing.T) {
			spec := badSpecCopy(t, spelling.absolute)
			prev, prevFile := preflightOnlyReport(t, spec, false)
			wantEvidencePath(t, prev, "bad_spec.md")

			t.Run("convergence-from", func(t *testing.T) {
				flags := previousReportFlags(t)
				flags.preflight = true
				flags.preflightMode = "only"
				flags.convergenceFrom = prevFile
				flags.convergenceMode = "on"
				if err := runCheck(spec, flags); err != nil {
					t.Fatalf("runCheck: %v", err)
				}
				wantAllStillOpen(t, readJSONReport(t, flags.out))
			})

			// A preflight-only report loads, but it holds no model review, so
			// reusing it would skip the review of every unchanged section.
			t.Run("incremental-from in on mode", func(t *testing.T) {
				setTestEnv(t)
				setupMockAnthropicServer(t, fixture)
				flags := previousReportFlags(t)
				flags.preflight = true
				flags.incrementalFrom = prevFile
				flags.incrementalMode = "on"
				wantExitCode3(t, runCheck(spec, flags), "no model review to reuse")
			})

			t.Run("incremental-from in auto mode", func(t *testing.T) {
				setTestEnv(t)
				setupMockAnthropicServer(t, fixture)
				flags := previousReportFlags(t)
				flags.preflight = true
				flags.incrementalFrom = prevFile
				flags.incrementalReport = true
				if err := runCheck(spec, flags); err != nil {
					t.Fatalf("runCheck: %v", err)
				}
				report := readJSONReport(t, flags.out)
				if report.Meta.Usage == nil || report.Meta.Usage.Calls != 1 || report.Meta.Incremental != nil {
					t.Fatalf("usage = %+v incremental = %+v, want a full review", report.Meta.Usage, report.Meta.Incremental)
				}
				if countIDs(report.Issues, "ISSUE-") == 0 || countIDs(report.Issues, "PREFLIGHT-") != len(prev.Issues) {
					t.Fatalf("issues = %v, want model findings beside the %d preflight findings", issueIDs(report.Issues), len(prev.Issues))
				}
			})
		})
	}
}

func TestRunCheck_ReviewReportRoundTrips(t *testing.T) {
	fixture := readFixture(t, "anthropic_response_bad.json")
	for _, spelling := range specSpellings {
		t.Run(spelling.name, func(t *testing.T) {
			setTestEnv(t)
			setupMockAnthropicServer(t, fixture)
			spec := badSpecCopy(t, spelling.absolute)
			prev, prevFile := reviewReport(t, spec)
			wantEvidencePath(t, prev, "bad_spec.md")

			t.Run("convergence-from", func(t *testing.T) {
				flags := previousReportFlags(t)
				flags.preflight = true
				flags.convergenceFrom = prevFile
				flags.convergenceMode = "on"
				if err := runCheck(spec, flags); err != nil {
					t.Fatalf("runCheck: %v", err)
				}
				wantAllStillOpen(t, readJSONReport(t, flags.out))
			})

			t.Run("incremental-from", func(t *testing.T) {
				flags := previousReportFlags(t)
				flags.preflight = true
				flags.incrementalFrom = prevFile
				flags.incrementalMode = "on"
				flags.incrementalReport = true
				if err := runCheck(spec, flags); err != nil {
					t.Fatalf("runCheck: %v", err)
				}
				wantReusedReview(t, prev, readJSONReport(t, flags.out))
			})
		})
	}
}

// wantReusedReview fails the test unless report is an incremental rerun of an
// unchanged spec: no model call, the previous model findings reused under
// their IDs, and the preflight findings produced again.
func wantReusedReview(t *testing.T, prev, report schema.Report) {
	t.Helper()
	meta := report.Meta.Incremental
	if report.Meta.Usage != nil || meta == nil || meta.Fallback {
		t.Fatalf("usage = %+v incremental = %+v, want reuse without a model call", report.Meta.Usage, meta)
	}
	if meta.ReusedIssues == 0 || meta.ReusedIssues != countIDs(report.Issues, "ISSUE-") {
		t.Fatalf("reused issues = %d, issues = %v", meta.ReusedIssues, issueIDs(report.Issues))
	}
	if got, want := countIDs(report.Issues, "PREFLIGHT-"), countIDs(prev.Issues, "PREFLIGHT-"); got != want {
		t.Fatalf("preflight findings = %d, want %d: %v", got, want, issueIDs(report.Issues))
	}
	for _, issue := range report.Issues {
		if strings.HasPrefix(issue.ID, "ISSUE-") && !reportHasIssue(prev.Issues, issue.ID) {
			t.Errorf("%s is not an ID from the previous report", issue.ID)
		}
	}
	wantEvidencePath(t, report, "bad_spec.md")
}

// Before preflight evidence used the same path rule as model findings, a
// report held the spec path as it was given. Such a report must still load.
func TestRunCheck_ReportWithAbsoluteEvidencePathRoundTrips(t *testing.T) {
	setTestEnv(t)
	setupMockAnthropicServer(t, readFixture(t, "anthropic_response_bad.json"))
	spec := badSpecCopy(t, true)
	prev, prevFile := reviewReport(t, spec)
	for i, issue := range prev.Issues {
		if strings.HasPrefix(issue.ID, "PREFLIGHT-") {
			prev.Issues[i].Evidence[0].Path = spec
		}
	}
	data, err := json.Marshal(prev)
	if err != nil {
		t.Fatalf("encode report: %v", err)
	}
	if err := os.WriteFile(prevFile, data, 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}

	t.Run("convergence-from", func(t *testing.T) {
		flags := previousReportFlags(t)
		flags.preflight = true
		flags.convergenceFrom = prevFile
		flags.convergenceMode = "on"
		if err := runCheck(spec, flags); err != nil {
			t.Fatalf("runCheck: %v", err)
		}
		wantAllStillOpen(t, readJSONReport(t, flags.out))
	})

	t.Run("incremental-from", func(t *testing.T) {
		flags := previousReportFlags(t)
		flags.preflight = true
		flags.incrementalFrom = prevFile
		flags.incrementalMode = "on"
		flags.incrementalReport = true
		if err := runCheck(spec, flags); err != nil {
			t.Fatalf("runCheck: %v", err)
		}
		wantReusedReview(t, prev, readJSONReport(t, flags.out))
	})
}

// A completion patch names the issue it came from, which for a preflight
// finding is a rule ID rather than an ISSUE number.
func TestRunCheck_ReportWithCompletionPatchLoads(t *testing.T) {
	spec := badSpecCopy(t, true)
	prev, prevFile := preflightOnlyReport(t, spec, true)
	if len(prev.Patches) == 0 || !strings.HasPrefix(prev.Patches[0].IssueID, "PREFLIGHT-") {
		t.Fatalf("patches = %#v, want a completion patch naming a preflight rule", prev.Patches)
	}

	flags := previousReportFlags(t)
	flags.preflight = true
	flags.preflightMode = "only"
	flags.convergenceFrom = prevFile
	flags.convergenceMode = "on"
	if err := runCheck(spec, flags); err != nil {
		t.Fatalf("runCheck: %v", err)
	}
	meta := readJSONReport(t, flags.out).Meta.Convergence
	if meta == nil || meta.Status != schema.ConvergenceStatusComplete {
		t.Fatalf("convergence meta = %#v, want status complete", meta)
	}
}

// The severity threshold filters the issues a report lists but not its
// patches, so a report can hold a patch whose issue is not listed.
func TestRunCheck_SeverityFilteredReportLoads(t *testing.T) {
	setTestEnv(t)
	setupMockAnthropicServer(t, anthropicResponse(t, `{"issues":[
		{"id":"ISSUE-0001","severity":"CRITICAL","category":"CONTRADICTION","title":"Authentication requirement contradicts itself","evidence":[{"path":"bad_spec.md","line_start":10,"line_end":10,"quote":"Users must be authenticated."}],"blocking":true,"tags":[]},
		{"id":"ISSUE-0002","severity":"INFO","category":"AMBIGUOUS_BEHAVIOR","title":"Scope of processing is not stated","evidence":[{"path":"bad_spec.md","line_start":12,"line_end":12,"quote":"All data shall be processed."}],"blocking":false,"tags":[]}
	],"questions":[],"patches":[{"issue_id":"ISSUE-0002","before":"All data shall be processed.","after":"All uploaded records shall be processed."}]}`))
	spec := badSpecCopy(t, true)

	first := previousReportFlags(t)
	first.severityThreshold = "warn"
	if err := runCheck(spec, first); err != nil {
		t.Fatalf("first run: %v", err)
	}
	prev := readJSONReport(t, first.out)
	if len(prev.Patches) != 1 || reportHasIssue(prev.Issues, prev.Patches[0].IssueID) {
		t.Fatalf("issues = %v patches = %#v, want a patch whose issue was filtered out", issueIDs(prev.Issues), prev.Patches)
	}

	t.Run("convergence-from", func(t *testing.T) {
		flags := previousReportFlags(t)
		flags.severityThreshold = "warn"
		flags.convergenceFrom = first.out
		flags.convergenceMode = "on"
		if err := runCheck(spec, flags); err != nil {
			t.Fatalf("runCheck: %v", err)
		}
	})

	t.Run("incremental-from", func(t *testing.T) {
		flags := previousReportFlags(t)
		flags.severityThreshold = "warn"
		flags.incrementalFrom = first.out
		flags.incrementalMode = "on"
		if err := runCheck(spec, flags); err != nil {
			t.Fatalf("runCheck: %v", err)
		}
	})
}

// anthropicResponse wraps text, the model's reply, in an Anthropic API
// response body.
func anthropicResponse(t *testing.T, text string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"id":          "msg_test",
		"model":       "claude-test",
		"type":        "message",
		"role":        "assistant",
		"content":     []map[string]string{{"type": "text", "text": text}},
		"stop_reason": "end_turn",
		"usage":       map[string]int{"input_tokens": 100, "output_tokens": 200},
	})
	if err != nil {
		t.Fatalf("encode response: %v", err)
	}
	return body
}
