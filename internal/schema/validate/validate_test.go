package validate

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/schema"
)

const validJSON = `{
  "tool": "speccritic",
  "version": "1.0",
  "input": {},
  "summary": {},
  "issues": [
    {
      "id": "ISSUE-0001",
      "severity": "CRITICAL",
      "category": "NON_TESTABLE_REQUIREMENT",
      "title": "Test issue",
      "description": "desc",
      "evidence": [{"path": "SPEC.md", "line_start": 1, "line_end": 2, "quote": "q"}],
      "impact": "imp",
      "recommendation": "rec",
      "blocking": true,
      "tags": []
    }
  ],
  "questions": [],
  "patches": [],
  "meta": {}
}`

func TestParse_ValidReport(t *testing.T) {
	r, err := Parse(validJSON, 10)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(r.Issues) != 1 {
		t.Errorf("expected 1 issue, got %d", len(r.Issues))
	}
}

func TestParse_StripsFences(t *testing.T) {
	fenced := "```json\n" + validJSON + "\n```"
	r, err := Parse(fenced, 10)
	if err != nil {
		t.Fatalf("Parse with fences: %v", err)
	}
	if r == nil {
		t.Error("expected non-nil report")
	}
}

func TestParse_InvalidJSON(t *testing.T) {
	_, err := Parse("{not valid json}", 10)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestParse_InvalidSeverity(t *testing.T) {
	bad := strings.Replace(validJSON, `"CRITICAL"`, `"BLOCKER"`, 1)
	_, err := Parse(bad, 10)
	if err == nil {
		t.Error("expected error for invalid severity, got nil")
	}
}

func TestParse_InvalidIssueIDFormat(t *testing.T) {
	bad := strings.Replace(validJSON, `"ISSUE-0001"`, `"ISS-1"`, 1)
	_, err := Parse(bad, 10)
	if err == nil {
		t.Error("expected error for bad issue ID format, got nil")
	}
}

func TestParse_EvidenceLineBeyondSpec(t *testing.T) {
	// line_end=2 but lineCount=1
	_, err := Parse(validJSON, 1)
	if err == nil {
		t.Error("expected error when evidence line_end exceeds lineCount, got nil")
	}
}

func TestParse_InvalidCategory(t *testing.T) {
	bad := strings.Replace(validJSON, `"NON_TESTABLE_REQUIREMENT"`, `"MADE_UP_CATEGORY"`, 1)
	_, err := Parse(bad, 10)
	if err == nil {
		t.Error("expected error for invalid category, got nil")
	}
}

func TestParse_MissingTitle(t *testing.T) {
	bad := strings.Replace(validJSON, `"title": "Test issue"`, `"title": ""`, 1)
	_, err := Parse(bad, 10)
	if err == nil {
		t.Error("expected error for missing title, got nil")
	}
}

const validJSONWithQuestion = `{
  "tool": "speccritic",
  "version": "1.0",
  "input": {},
  "summary": {},
  "issues": [],
  "questions": [
    {
      "id": "Q-0001",
      "severity": "CRITICAL",
      "question": "What is the latency target?",
      "why_needed": "Non-testable otherwise",
      "blocks": [],
      "evidence": [{"path": "SPEC.md", "line_start": 1, "line_end": 1, "quote": "fast"}]
    }
  ],
  "patches": [],
  "meta": {}
}`

func TestParse_ValidQuestion(t *testing.T) {
	r, err := Parse(validJSONWithQuestion, 10)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(r.Questions) != 1 {
		t.Errorf("expected 1 question, got %d", len(r.Questions))
	}
}

func TestParse_InvalidQuestionIDFormat(t *testing.T) {
	bad := strings.Replace(validJSONWithQuestion, `"Q-0001"`, `"Q1"`, 1)
	_, err := Parse(bad, 10)
	if err == nil {
		t.Error("expected error for bad question ID format, got nil")
	}
}

func TestParse_InvalidConvergenceStatus(t *testing.T) {
	bad := strings.Replace(validJSON, `"meta": {}`, `"meta": {"convergence":{"enabled":true,"mode":"auto","status":"bad"}}`, 1)
	_, err := Parse(bad, 10)
	if err == nil || !strings.Contains(err.Error(), "meta.convergence.status") {
		t.Fatalf("expected convergence status error, got %v", err)
	}
}

func TestParse_ValidCompletionMeta(t *testing.T) {
	raw := strings.Replace(validJSON, `"meta": {}`, `"meta": {"completion":{"enabled":true,"mode":"auto","template":"backend-api","generated_patches":1,"skipped_suggestions":2,"open_decisions":3}}`, 1)
	report, err := Parse(raw, 10)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if report.Meta.Completion == nil || report.Meta.Completion.Template != "backend-api" {
		t.Fatalf("completion meta = %#v", report.Meta.Completion)
	}
}

func TestParse_InvalidCompletionMode(t *testing.T) {
	raw := strings.Replace(validJSON, `"meta": {}`, `"meta": {"completion":{"enabled":true,"mode":"bad","template":"backend-api"}}`, 1)
	_, err := Parse(raw, 10)
	if err == nil || !strings.Contains(err.Error(), "meta.completion.mode") {
		t.Fatalf("expected completion mode error, got %v", err)
	}
}

func TestParse_InvalidCompletionTemplate(t *testing.T) {
	raw := strings.Replace(validJSON, `"meta": {}`, `"meta": {"completion":{"enabled":true,"mode":"auto","template":"profile"}}`, 1)
	_, err := Parse(raw, 10)
	if err == nil || !strings.Contains(err.Error(), "meta.completion.template") {
		t.Fatalf("expected completion template error, got %v", err)
	}
}

func TestParse_InvalidCompletionCounter(t *testing.T) {
	raw := strings.Replace(validJSON, `"meta": {}`, `"meta": {"completion":{"enabled":true,"mode":"auto","template":"backend-api","generated_patches":-1}}`, 1)
	_, err := Parse(raw, 10)
	if err == nil || !strings.Contains(err.Error(), "meta.completion.generated_patches") {
		t.Fatalf("expected completion counter error, got %v", err)
	}
}

func TestParse_InvalidPatchIssueID(t *testing.T) {
	raw := strings.Replace(validJSON, `"patches": []`, `"patches": [{"issue_id":"ISSUE-9999","before":"old","after":"new"}]`, 1)
	_, err := Parse(raw, 10)
	if err == nil || !strings.Contains(err.Error(), "does not reference a current issue") {
		t.Fatalf("expected patch issue reference error, got %v", err)
	}
}

func TestParse_InvalidPatchShape(t *testing.T) {
	raw := strings.Replace(validJSON, `"patches": []`, `"patches": [{"issue_id":"ISSUE-0001","before":"","after":"new"}]`, 1)
	_, err := Parse(raw, 10)
	if err == nil || !strings.Contains(err.Error(), "before is required") {
		t.Fatalf("expected patch before error, got %v", err)
	}
}

const twoIssueJSON = `{"issues":[
  {"id":"ISSUE-0001","severity":"CRITICAL","category":"NON_TESTABLE_REQUIREMENT","title":"First","description":"d","evidence":[{"path":"SPEC.md","line_start":1,"line_end":1,"quote":"q"}],"impact":"i","recommendation":"r","blocking":true,"tags":[]},
  {"id":"ISSUE-0002","severity":"WARN","category":"AMBIGUOUS_BEHAVIOR","title":"Second","description":"d","evidence":[{"path":"SPEC.md","line_start":2,"line_end":2,"quote":"q"}],"impact":"i","recommendation":"r","blocking":false,"tags":[]}
],"questions":[{"id":"Q-0001","severity":"WARN","question":"Why?","why_needed":"w","blocks":[],"evidence":[{"path":"SPEC.md","line_start":1,"line_end":1,"quote":"q"}]}],"patches":[]}`

func TestParseResponse_CompleteDocument(t *testing.T) {
	res, err := ParseResponse("```json\n"+twoIssueJSON+"\n```", Options{LineCount: 10})
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if res.Incomplete != nil {
		t.Fatalf("Incomplete = %v, want nil", res.Incomplete)
	}
	if len(res.Report.Issues) != 2 || len(res.Report.Questions) != 1 {
		t.Fatalf("issues=%d questions=%d, want 2/1", len(res.Report.Issues), len(res.Report.Questions))
	}
}

func TestParseResponse_KeepsFindingsBeforeTruncation(t *testing.T) {
	cases := map[string]struct {
		cutAt         string
		wantIssues    int
		wantQuestions int
	}{
		"inside second issue":   {cutAt: `"title":"Second"`, wantIssues: 1},
		"between issues":        {cutAt: `{"id":"ISSUE-0002"`, wantIssues: 1},
		"inside first issue":    {cutAt: `"title":"First"`, wantIssues: 0},
		"inside first question": {cutAt: `"why_needed"`, wantIssues: 2},
		"after questions array": {cutAt: `,"patches"`, wantIssues: 2, wantQuestions: 1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			idx := strings.Index(twoIssueJSON, tc.cutAt)
			if idx < 0 {
				t.Fatalf("cut marker %q not found", tc.cutAt)
			}
			res, err := ParseResponse(twoIssueJSON[:idx], Options{LineCount: 10})
			if err != nil {
				t.Fatalf("ParseResponse: %v", err)
			}
			if res.Incomplete == nil {
				t.Fatal("Incomplete = nil, want truncation reported")
			}
			if !strings.HasPrefix(res.Incomplete.Error(), "JSON parse failed") {
				t.Errorf("Incomplete = %q, want JSON parse failed prefix", res.Incomplete)
			}
			if len(res.Report.Issues) != tc.wantIssues || len(res.Report.Questions) != tc.wantQuestions {
				t.Errorf("issues=%d questions=%d, want %d/%d", len(res.Report.Issues), len(res.Report.Questions), tc.wantIssues, tc.wantQuestions)
			}
		})
	}
}

func TestParseResponse_NotJSONIsIncompleteWithNothingRead(t *testing.T) {
	for _, raw := range []string{"", "I cannot review this document.", `{"issues":[`} {
		res, err := ParseResponse(raw, Options{LineCount: 10})
		if err != nil {
			t.Fatalf("ParseResponse(%q): %v", raw, err)
		}
		if res.Incomplete == nil {
			t.Errorf("ParseResponse(%q): Incomplete = nil", raw)
		}
		if len(res.Report.Issues)+len(res.Report.Questions) != 0 {
			t.Errorf("ParseResponse(%q): findings recovered from nothing", raw)
		}
	}
}

func TestParseResponse_DropsOnlyTheInvalidFinding(t *testing.T) {
	cases := map[string]struct {
		old, new   string
		wantReason string
	}{
		"invalid severity": {`"severity":"WARN","category":"AMBIGUOUS_BEHAVIOR"`, `"severity":"HIGH","category":"AMBIGUOUS_BEHAVIOR"`, "invalid severity"},
		"unknown category": {`"category":"AMBIGUOUS_BEHAVIOR"`, `"category":"VIBES"`, "unknown category"},
		"missing title":    {`"title":"Second"`, `"title":""`, "title is required"},
		"line past spec":   {`"line_start":2,"line_end":2`, `"line_start":2,"line_end":99`, "exceeds spec line count"},
		"wrong field type": {`"line_start":2`, `"line_start":"two"`, "cannot unmarshal"},
		"null element":     {`{"id":"ISSUE-0002","severity":"WARN","category":"AMBIGUOUS_BEHAVIOR","title":"Second","description":"d","evidence":[{"path":"SPEC.md","line_start":2,"line_end":2,"quote":"q"}],"impact":"i","recommendation":"r","blocking":false,"tags":[]}`, `null`, "invalid severity"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			raw := strings.Replace(twoIssueJSON, tc.old, tc.new, 1)
			if raw == twoIssueJSON {
				t.Fatalf("replacement %q did not apply", tc.old)
			}
			res, err := ParseResponse(raw, Options{LineCount: 10})
			if err != nil {
				t.Fatalf("ParseResponse: %v", err)
			}
			if len(res.Report.Issues) != 1 || res.Report.Issues[0].Title != "First" {
				t.Fatalf("issues = %#v, want only the valid finding", res.Report.Issues)
			}
			if len(res.Report.Questions) != 1 {
				t.Errorf("questions = %d, want the valid question kept", len(res.Report.Questions))
			}
			if len(res.Dropped) != 1 || !strings.Contains(res.Dropped[0], tc.wantReason) || !strings.HasPrefix(res.Dropped[0], "issue[1]") {
				t.Errorf("dropped = %q, want one issue[1] entry mentioning %q", res.Dropped, tc.wantReason)
			}
			if res.Report.Meta.DroppedFindings != 1 {
				t.Errorf("meta dropped = %d, want 1", res.Report.Meta.DroppedFindings)
			}
		})
	}
}

func TestParseResponse_NothingUsableIsAnError(t *testing.T) {
	onlyBad := `{"issues":[{"id":"ISSUE-0001","severity":"HIGH","category":"AMBIGUOUS_BEHAVIOR","title":"t","evidence":[]}],"questions":[],"patches":[]}`
	_, err := ParseResponse(onlyBad, Options{LineCount: 10})
	if err == nil || !strings.Contains(err.Error(), "no usable findings") || !strings.Contains(err.Error(), "invalid severity") {
		t.Fatalf("error = %v, want no usable findings with the reason", err)
	}
	// A response that reports nothing is usable: the spec had no defects.
	res, err := ParseResponse(`{"issues":[],"questions":[],"patches":[]}`, Options{LineCount: 10})
	if err != nil || res.Incomplete != nil || len(res.Dropped) != 0 {
		t.Fatalf("empty report: err=%v incomplete=%v dropped=%v", err, res.Incomplete, res.Dropped)
	}
}

func TestParseResponse_KeepsFindingsWhateverTheirIDs(t *testing.T) {
	raw := strings.Replace(twoIssueJSON, `"id":"ISSUE-0002"`, `"id":"ISSUE-0001"`, 1)
	raw = strings.Replace(raw, `"id":"Q-0001"`, `"id":"question one"`, 1)
	res, err := ParseResponse(raw, Options{LineCount: 10})
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if len(res.Report.Issues) != 2 || len(res.Report.Questions) != 1 || len(res.Dropped) != 0 {
		t.Fatalf("issues=%d questions=%d dropped=%v, want everything kept", len(res.Report.Issues), len(res.Report.Questions), res.Dropped)
	}
}

func TestParseResponse_EvidencePath(t *testing.T) {
	escaping := strings.Replace(twoIssueJSON, `"path":"SPEC.md","line_start":2`, `"path":"../../etc/passwd","line_start":2`, 1)

	// With no spec path given, the model's path is checked.
	res, err := ParseResponse(escaping, Options{LineCount: 10})
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if len(res.Report.Issues) != 1 || len(res.Dropped) != 1 || !strings.Contains(res.Dropped[0], "local relative path") {
		t.Fatalf("issues=%d dropped=%q, want the escaping path dropped", len(res.Report.Issues), res.Dropped)
	}

	// With a spec path, every evidence entry gets it. A path that would fail
	// the locality check is reduced to its base name.
	for specPath, want := range map[string]string{
		"specs/api/SPEC.md":   "specs/api/SPEC.md",
		"/work/specs/SPEC.md": "SPEC.md",
		"../shared/SPEC.md":   "SPEC.md",
	} {
		res, err := ParseResponse(escaping, Options{LineCount: 10, SpecPath: specPath})
		if err != nil {
			t.Fatalf("ParseResponse(%s): %v", specPath, err)
		}
		if len(res.Report.Issues) != 2 || len(res.Dropped) != 0 {
			t.Fatalf("%s: issues=%d dropped=%q, want both kept", specPath, len(res.Report.Issues), res.Dropped)
		}
		for _, issue := range res.Report.Issues {
			if issue.Evidence[0].Path != want {
				t.Errorf("%s: issue %q evidence path = %q, want %q", specPath, issue.Title, issue.Evidence[0].Path, want)
			}
		}
		if got := res.Report.Questions[0].Evidence[0].Path; got != want {
			t.Errorf("%s: question evidence path = %q, want %q", specPath, got, want)
		}
	}
}

func TestParseResponse_CallerChecksCanDropAndAdjust(t *testing.T) {
	res, err := ParseResponse(twoIssueJSON, Options{
		LineCount: 10,
		CheckIssue: func(issue *schema.Issue) error {
			if issue.Evidence[0].LineStart > 1 {
				return errors.New("outside the reviewed range")
			}
			issue.Tags = append(issue.Tags, "stamped")
			return nil
		},
		CheckQuestion: func(*schema.Question) error { return errors.New("questions not wanted") },
	})
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if len(res.Report.Issues) != 1 || res.Report.Issues[0].Tags[0] != "stamped" {
		t.Fatalf("issues = %#v, want the first issue kept and stamped", res.Report.Issues)
	}
	if len(res.Report.Questions) != 0 || len(res.Dropped) != 2 {
		t.Fatalf("questions=%d dropped=%q, want the question and second issue dropped", len(res.Report.Questions), res.Dropped)
	}
	if !strings.Contains(res.Dropped[0], "issue[1]: outside the reviewed range") {
		t.Errorf("dropped[0] = %q", res.Dropped[0])
	}
}

func TestParseResponse_UnusablePatchesAndMetaAreSkippedQuietly(t *testing.T) {
	raw := strings.Replace(twoIssueJSON, `"patches":[]`, `"patches":[
		{"issue_id":"ISSUE-0001","before":"a","after":"b"},
		{"issue_id":"","before":"a","after":"b"},
		{"issue_id":"ISSUE-0002","before":" ","after":"b"},
		{"issue_id":"ISSUE-0002","before":"a","after":""},
		{"issue_id":7,"before":"a","after":"b"},
		{"issue_id":"ISSUE-0404","before":"x","after":"y"}
	],"meta":{"chunk_summary":["not","a","string"],"model":"made-up"}`, 1)
	res, err := ParseResponse(raw, Options{LineCount: 10})
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if len(res.Dropped) != 0 || len(res.Report.Issues) != 2 {
		t.Fatalf("dropped=%q issues=%d, want no findings dropped", res.Dropped, len(res.Report.Issues))
	}
	// The patch for an issue this response does not hold is kept: it may
	// belong to an earlier part of a continued response.
	if len(res.Report.Patches) != 2 || res.Report.Patches[1].IssueID != "ISSUE-0404" {
		t.Fatalf("patches = %#v, want the two well-formed patches", res.Report.Patches)
	}
	if res.Report.Meta.ChunkSummary != "" || res.Report.Meta.Model != "" {
		t.Fatalf("meta = %#v, want model-supplied meta ignored", res.Report.Meta)
	}
}

func TestParseResponse_EvidenceBoundsApplyToRecoveredFindings(t *testing.T) {
	idx := strings.Index(twoIssueJSON, `"title":"Second"`)
	if _, err := ParseResponse(twoIssueJSON[:idx], Options{LineCount: 0}); err != nil {
		t.Fatalf("ParseResponse without bounds: %v", err)
	}
	beyond := strings.Replace(twoIssueJSON[:idx], `"line_start":1,"line_end":1`, `"line_start":1,"line_end":99`, 1)
	if _, err := ParseResponse(beyond, Options{LineCount: 10}); err == nil || !strings.Contains(err.Error(), "exceeds spec line count") {
		t.Fatalf("error = %v, want line bound error", err)
	}
}

func TestParseResponse_WrongShapeIsAnErrorNotATruncation(t *testing.T) {
	firstIssueEnd := strings.Index(twoIssueJSON, `{"id":"ISSUE-0002"`)
	afterFirstIssue := strings.TrimRight(strings.TrimSpace(twoIssueJSON[:firstIssueEnd]), ",")
	cases := map[string]string{
		"top-level array":           `[]`,
		"issues is an object":       `{"issues":{}}`,
		"issues is a scalar":        `{"issues":3}`,
		"questions is a string":     afterFirstIssue + `],"questions":"none"}`,
		"patches is an object":      afterFirstIssue + `],"questions":[],"patches":{"issue_id":"ISSUE-0001"}}`,
		"second report after first": twoIssueJSON + "\n" + twoIssueJSON,
		"array after the report":    twoIssueJSON + ` []`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			res, err := ParseResponse(raw, Options{LineCount: 10})
			if err == nil {
				t.Fatalf("error = nil (incomplete=%v), want a shape error", res.Incomplete)
			}
			if !strings.HasPrefix(err.Error(), "JSON parse failed") {
				t.Errorf("error = %q, want JSON parse failed prefix", err)
			}
		})
	}
}

func TestParseResponse_IgnoresTextAfterTheReport(t *testing.T) {
	res, err := ParseResponse(twoIssueJSON+"\n```\nLet me know if you need more detail.", Options{LineCount: 10})
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if res.Incomplete != nil || len(res.Report.Issues) != 2 {
		t.Fatalf("incomplete=%v issues=%d, want a complete report with 2 issues", res.Incomplete, len(res.Report.Issues))
	}
}

// evidenceSpec is the spec the responses below claim to review.
const evidenceSpec = "# Spec\nThe service must answer within 200 milliseconds.\nRetries are unlimited.\nThe cache is refreshed hourly.\n"

func evidenceIssue(id, severity string, line int, quote string) string {
	return fmt.Sprintf(`{"id":%q,"severity":%q,"category":"AMBIGUOUS_BEHAVIOR","title":"t","description":"d","evidence":[{"line_start":%d,"line_end":%d,"quote":%q}],"impact":"i","recommendation":"r","blocking":true,"tags":[]}`,
		id, severity, line, line, quote)
}

func TestParseResponse_ChecksEvidenceAgainstTheSpec(t *testing.T) {
	raw := `{"issues":[` + strings.Join([]string{
		evidenceIssue("ISSUE-0001", "CRITICAL", 2, "the service must answer within 200 milliseconds"),
		evidenceIssue("ISSUE-0002", "CRITICAL", 2, "The cache is refreshed hourly."),
		evidenceIssue("ISSUE-0003", "CRITICAL", 3, "Retries stop after five attempts and then fail."),
		`{"id":"ISSUE-0004","severity":"WARN","category":"AMBIGUOUS_BEHAVIOR","title":"no evidence","evidence":[]}`,
	}, ",") + `],"questions":[{"id":"Q-0001","severity":"WARN","question":"How often?","evidence":[{"line_start":1,"line_end":1,"quote":"the cache is refreshed hourly"}]}],"patches":[]}`

	var seenLines []int
	res, err := ParseResponse(raw, Options{
		LineCount: 4,
		SpecText:  evidenceSpec,
		CheckIssue: func(issue *schema.Issue) error {
			seenLines = append(seenLines, issue.Evidence[0].LineStart)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if len(res.Report.Issues) != 3 {
		t.Fatalf("issues = %d, want the one without evidence dropped", len(res.Report.Issues))
	}
	if len(res.Dropped) != 1 || !strings.Contains(res.Dropped[0], "issue[3]: no evidence") {
		t.Errorf("dropped = %q, want the issue with no evidence", res.Dropped)
	}

	verified, moved, unverified := res.Report.Issues[0], res.Report.Issues[1], res.Report.Issues[2]
	if got := verified.Evidence[0]; got.LineStart != 2 || got.Quote != "The service must answer within 200 milliseconds" || len(verified.Tags) != 0 || verified.Severity != schema.SeverityCritical {
		t.Errorf("verified issue = %+v, want the spec's own text and no tags", verified)
	}
	if got := moved.Evidence[0]; got.LineStart != 4 || got.LineEnd != 4 || !slices.Contains(moved.Tags, "evidence-reanchored") {
		t.Errorf("moved issue evidence = %+v tags = %v, want line 4 and the reanchored tag", got, moved.Tags)
	}
	if unverified.Severity != schema.SeverityWarn || unverified.Blocking || !slices.Contains(unverified.Tags, "evidence-unverified") || !slices.Contains(unverified.Tags, "severity-downgraded") {
		t.Errorf("unverified issue = %+v, want it lowered to WARN and tagged", unverified)
	}
	// Caller checks run on the corrected lines.
	if !slices.Equal(seenLines, []int{2, 4, 3}) {
		t.Errorf("caller checks saw lines %v, want [2 4 3]", seenLines)
	}
	if got := res.Report.Questions[0].Evidence[0]; got.LineStart != 4 || got.Quote != "The cache is refreshed hourly" {
		t.Errorf("question evidence = %+v, want it moved to line 4", got)
	}
}

func TestParseResponse_WithoutSpecTextEvidenceIsNotChecked(t *testing.T) {
	raw := `{"issues":[` + evidenceIssue("ISSUE-0001", "CRITICAL", 3, "Retries stop after five attempts and then fail.") +
		`,{"id":"ISSUE-0002","severity":"WARN","category":"AMBIGUOUS_BEHAVIOR","title":"no evidence","evidence":[]}],"questions":[],"patches":[]}`
	res, err := ParseResponse(raw, Options{LineCount: 4})
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if len(res.Report.Issues) != 2 || res.Report.Issues[0].Severity != schema.SeverityCritical || len(res.Report.Issues[0].Tags) != 0 {
		t.Fatalf("issues = %+v, want both kept untouched", res.Report.Issues)
	}
}
