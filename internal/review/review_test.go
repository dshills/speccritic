package review

import (
	"testing"

	"github.com/dshills/speccritic/internal/schema"
)

func makeIssues(severities ...schema.Severity) []schema.Issue {
	issues := make([]schema.Issue, len(severities))
	for i, s := range severities {
		issues[i] = schema.Issue{Severity: s}
	}
	return issues
}

func makeQuestions(severities ...schema.Severity) []schema.Question {
	qs := make([]schema.Question, len(severities))
	for i, s := range severities {
		qs[i] = schema.Question{Severity: s}
	}
	return qs
}

// --- Score tests ---

func TestScore_ThreeCritical(t *testing.T) {
	issues := makeIssues(schema.SeverityCritical, schema.SeverityCritical, schema.SeverityCritical)
	got := Score(issues)
	want := 100 - 3*20 // 40
	if got != want {
		t.Errorf("Score = %d, want %d", got, want)
	}
}

func TestScore_ClampsAtZero(t *testing.T) {
	// 6 CRITICAL = -120 → clamped to 0
	issues := makeIssues(
		schema.SeverityCritical, schema.SeverityCritical, schema.SeverityCritical,
		schema.SeverityCritical, schema.SeverityCritical, schema.SeverityCritical,
	)
	got := Score(issues)
	if got != 0 {
		t.Errorf("Score = %d, want 0 (clamped)", got)
	}
}

func TestScore_Mixed(t *testing.T) {
	// 1 CRITICAL(-20) + 2 WARN(-14) + 1 INFO(-2) = 100-36 = 64
	issues := makeIssues(schema.SeverityCritical, schema.SeverityWarn, schema.SeverityWarn, schema.SeverityInfo)
	got := Score(issues)
	if got != 64 {
		t.Errorf("Score = %d, want 64", got)
	}
}

func TestScore_NoIssues(t *testing.T) {
	got := Score(nil)
	if got != 100 {
		t.Errorf("Score = %d, want 100", got)
	}
}

// --- Verdict tests ---

func TestVerdict_CriticalIssue_Invalid(t *testing.T) {
	v := Verdict(makeIssues(schema.SeverityCritical), nil)
	if v != schema.VerdictInvalid {
		t.Errorf("Verdict = %q, want INVALID", v)
	}
}

func TestVerdict_CriticalQuestion_Invalid(t *testing.T) {
	v := Verdict(nil, makeQuestions(schema.SeverityCritical))
	if v != schema.VerdictInvalid {
		t.Errorf("Verdict = %q, want INVALID (critical question)", v)
	}
}

func TestVerdict_WarnOnly_ValidWithGaps(t *testing.T) {
	v := Verdict(makeIssues(schema.SeverityWarn), nil)
	if v != schema.VerdictValidWithGaps {
		t.Errorf("Verdict = %q, want VALID_WITH_GAPS", v)
	}
}

func TestVerdict_InfoOnly_ValidWithGaps(t *testing.T) {
	v := Verdict(makeIssues(schema.SeverityInfo), nil)
	if v != schema.VerdictValidWithGaps {
		t.Errorf("Verdict = %q, want VALID_WITH_GAPS", v)
	}
}

func TestVerdict_NoIssuesNoQuestions_Valid(t *testing.T) {
	v := Verdict(nil, nil)
	if v != schema.VerdictValid {
		t.Errorf("Verdict = %q, want VALID", v)
	}
}

func TestVerdict_CriticalQuestionPlusWarnIssue_Invalid(t *testing.T) {
	v := Verdict(makeIssues(schema.SeverityWarn), makeQuestions(schema.SeverityCritical))
	if v != schema.VerdictInvalid {
		t.Errorf("Verdict = %q, want INVALID", v)
	}
}

// --- FilterBySeverity tests ---

func TestFilterBySeverity_CriticalThreshold(t *testing.T) {
	issues := makeIssues(schema.SeverityCritical, schema.SeverityWarn, schema.SeverityInfo)
	filtered := FilterBySeverity(issues, schema.SeverityCritical)
	if len(filtered) != 1 {
		t.Errorf("expected 1 issue after CRITICAL filter, got %d", len(filtered))
	}
	if filtered[0].Severity != schema.SeverityCritical {
		t.Errorf("expected CRITICAL issue, got %q", filtered[0].Severity)
	}
}

func TestFilterBySeverity_InfoThreshold_ReturnsAll(t *testing.T) {
	issues := makeIssues(schema.SeverityCritical, schema.SeverityWarn, schema.SeverityInfo)
	filtered := FilterBySeverity(issues, schema.SeverityInfo)
	if len(filtered) != 3 {
		t.Errorf("expected 3 issues with INFO threshold, got %d", len(filtered))
	}
}
