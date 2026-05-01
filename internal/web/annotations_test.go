package web

import (
	"testing"

	"github.com/dshills/speccritic/internal/schema"
)

func TestBuildAnnotatedSpecMapsEvidenceRange(t *testing.T) {
	report := &schema.Report{Issues: []schema.Issue{{
		ID:       "ISSUE-0001",
		Severity: schema.SeverityCritical,
		Title:    "Bad range",
		Evidence: []schema.Evidence{{LineStart: 2, LineEnd: 3}},
	}}}
	annotated, err := BuildAnnotatedSpec("one\ntwo\nthree\n", report, schema.SeverityInfo)
	if err != nil {
		t.Fatalf("BuildAnnotatedSpec: %v", err)
	}
	if len(annotated.Lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(annotated.Lines))
	}
	if len(annotated.Lines[0].FindingRefs) != 0 {
		t.Fatal("line 1 should not be annotated")
	}
	if annotated.Lines[1].HighestSeverity != schema.SeverityCritical {
		t.Fatalf("line 2 severity = %s", annotated.Lines[1].HighestSeverity)
	}
	if annotated.Lines[2].FindingRefs[0].ID != "ISSUE-0001" {
		t.Fatalf("line 3 ref = %q", annotated.Lines[2].FindingRefs[0].ID)
	}
}

func TestBuildAnnotatedSpecSortsAndFiltersRefs(t *testing.T) {
	report := &schema.Report{Issues: []schema.Issue{
		{ID: "ISSUE-0002", Severity: schema.SeverityInfo, Title: "Info", Evidence: []schema.Evidence{{LineStart: 1, LineEnd: 1}}},
		{ID: "ISSUE-0001", Severity: schema.SeverityWarn, Title: "Warn", Evidence: []schema.Evidence{{LineStart: 1, LineEnd: 1}}},
	}}
	annotated, err := BuildAnnotatedSpec("one", report, schema.SeverityWarn)
	if err != nil {
		t.Fatalf("BuildAnnotatedSpec: %v", err)
	}
	refs := annotated.Lines[0].FindingRefs
	if len(refs) != 1 || refs[0].ID != "ISSUE-0001" {
		t.Fatalf("refs = %#v, want only warn issue", refs)
	}
}

func TestBuildAnnotatedSpecIncludesQuestions(t *testing.T) {
	report := &schema.Report{Questions: []schema.Question{{
		ID:       "Q-0001",
		Severity: schema.SeverityCritical,
		Question: "Clarify?",
		Evidence: []schema.Evidence{{LineStart: 1, LineEnd: 1}},
	}}}
	annotated, err := BuildAnnotatedSpec("one", report, schema.SeverityInfo)
	if err != nil {
		t.Fatalf("BuildAnnotatedSpec: %v", err)
	}
	if annotated.Lines[0].FindingRefs[0].Kind != "question" {
		t.Fatalf("kind = %q", annotated.Lines[0].FindingRefs[0].Kind)
	}
}

func TestBuildAnnotatedSpecRejectsInvalidEvidence(t *testing.T) {
	report := &schema.Report{Issues: []schema.Issue{{
		ID:       "ISSUE-0001",
		Severity: schema.SeverityCritical,
		Evidence: []schema.Evidence{{LineStart: 2, LineEnd: 2}},
	}}}
	if _, err := BuildAnnotatedSpec("one", report, schema.SeverityInfo); err == nil {
		t.Fatal("expected invalid evidence error")
	}
}

func TestSplitSpecLinesMatchesTrailingNewlineBehavior(t *testing.T) {
	lines := splitSpecLines("one\n\n")
	if len(lines) != 2 || lines[0] != "one" || lines[1] != "" {
		t.Fatalf("lines = %#v", lines)
	}
}
