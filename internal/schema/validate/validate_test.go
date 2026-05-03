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
