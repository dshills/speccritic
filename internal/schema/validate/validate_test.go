package validate

import (
	"strings"
	"testing"
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

func TestParseResponse_InvalidFindingIsAnError(t *testing.T) {
	bad := strings.Replace(twoIssueJSON, `"severity":"WARN","category":"AMBIGUOUS_BEHAVIOR"`, `"severity":"HIGH","category":"AMBIGUOUS_BEHAVIOR"`, 1)
	if _, err := ParseResponse(bad, Options{LineCount: 10}); err == nil || !strings.Contains(err.Error(), "invalid severity") {
		t.Fatalf("error = %v, want invalid severity", err)
	}
	wrongType := strings.Replace(twoIssueJSON, `"line_start":2`, `"line_start":"two"`, 1)
	if _, err := ParseResponse(wrongType, Options{LineCount: 10}); err == nil || !strings.HasPrefix(err.Error(), "JSON parse failed") {
		t.Fatalf("error = %v, want JSON parse failed", err)
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
