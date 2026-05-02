package incremental

import (
	"testing"

	"github.com/dshills/speccritic/internal/redact"
	"github.com/dshills/speccritic/internal/schema"
)

func TestReuseFindingsExactUnchangedSection(t *testing.T) {
	raw := "# Spec\n## Behavior\nThe API must return JSON.\n"
	plan, err := PlanChanges(raw, raw, testPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	report := reportWithIssue(issueAt("ISSUE-0001", 3, "The API must return JSON."))
	result, err := ReuseFindings(ReuseInput{
		Plan:            plan,
		Previous:        report,
		CurrentRaw:      raw,
		CurrentRedacted: redact.Redact(raw),
		Config:          Config{SeverityThreshold: "info", MaxRemapFailureRatio: 0.25},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 1 {
		t.Fatalf("issues = %#v dropped=%#v", result.Issues, result.Dropped)
	}
	if !hasTag(result.Issues[0].Tags, TagReused) {
		t.Fatalf("tags = %#v", result.Issues[0].Tags)
	}
}

func TestReuseFindingsMovedSectionRemapsLines(t *testing.T) {
	previous := "# Spec\n## A\nThe API must return JSON.\n## B\nOther text.\n"
	current := "# Spec\n## B\nOther text.\n## A\nThe API must return JSON.\n"
	plan, err := PlanChanges(previous, current, testPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	report := reportWithIssue(issueAt("ISSUE-0001", 3, "The API must return JSON."))
	result, err := ReuseFindings(ReuseInput{
		Plan:            plan,
		Previous:        report,
		CurrentRaw:      current,
		CurrentRedacted: redact.Redact(current),
		Config:          Config{SeverityThreshold: "info", MaxRemapFailureRatio: 0.25},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 1 {
		t.Fatalf("issues = %#v dropped=%#v", result.Issues, result.Dropped)
	}
	if result.Issues[0].Evidence[0].LineStart != 5 {
		t.Fatalf("line = %d, want 5", result.Issues[0].Evidence[0].LineStart)
	}
}

func TestReuseFindingsDropsDeletedSection(t *testing.T) {
	previous := "# Spec\n## Old\nThe API must return JSON.\n"
	current := "# Spec\n"
	plan, err := PlanChanges(previous, current, testPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	report := reportWithIssue(issueAt("ISSUE-0001", 3, "The API must return JSON."))
	result, err := ReuseFindings(ReuseInput{
		Plan:            plan,
		Previous:        report,
		CurrentRaw:      current,
		CurrentRedacted: redact.Redact(current),
		Config:          Config{SeverityThreshold: "info", MaxRemapFailureRatio: 0.25},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 0 || len(result.Dropped) != 1 || result.Dropped[0].Reason != "deleted_section" {
		t.Fatalf("result = %#v", result)
	}
	if result.RemapCandidates != 0 || result.RemapFailures != 0 {
		t.Fatalf("deleted findings should not count as remap failures: %#v", result)
	}
}

func TestReuseFindingsRejectsRedactedEvidence(t *testing.T) {
	raw := "# Spec\n## Secret\npassword: hunter2\n"
	plan, err := PlanChanges(raw, raw, testPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	report := reportWithIssue(issueAt("ISSUE-0001", 3, "[REDACTED]"))
	result, err := ReuseFindings(ReuseInput{
		Plan:            plan,
		Previous:        report,
		CurrentRaw:      raw,
		CurrentRedacted: redact.Redact(raw),
		Config:          Config{SeverityThreshold: "info", MaxRemapFailureRatio: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 0 || len(result.Dropped) != 1 || result.Dropped[0].Reason != "quote_mismatch" {
		t.Fatalf("result = %#v", result)
	}
}

func TestReuseFindingsFiltersSeverity(t *testing.T) {
	raw := "# Spec\n## Behavior\nThe API must return JSON.\n"
	plan, err := PlanChanges(raw, raw, testPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	issue := issueAt("ISSUE-0001", 3, "The API must return JSON.")
	issue.Severity = schema.SeverityInfo
	result, err := ReuseFindings(ReuseInput{
		Plan:            plan,
		Previous:        reportWithIssue(issue),
		CurrentRaw:      raw,
		CurrentRedacted: raw,
		Config:          Config{SeverityThreshold: "warn", MaxRemapFailureRatio: 0.25},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 0 || result.Dropped[0].Reason != "severity_filter" {
		t.Fatalf("result = %#v", result)
	}
}

func TestReuseFindingsFallbackOnRemapFailureRatio(t *testing.T) {
	raw := "# Spec\n## Behavior\nThe API must return JSON.\n"
	plan, err := PlanChanges(raw, raw, testPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	report := reportWithIssue(issueAt("ISSUE-0001", 3, "text that is gone"))
	result, err := ReuseFindings(ReuseInput{
		Plan:            plan,
		Previous:        report,
		CurrentRaw:      raw,
		CurrentRedacted: raw,
		Config:          Config{SeverityThreshold: "info", MaxRemapFailureRatio: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback == nil || result.Fallback.Code != "remap_failure_ratio" {
		t.Fatalf("fallback = %#v result=%#v", result.Fallback, result)
	}
}

func TestReuseFindingsPreflightDuplicateRequiresSamePath(t *testing.T) {
	raw := "# Spec\n## Behavior\nThe API must return JSON.\n"
	plan, err := PlanChanges(raw, raw, testPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	issue := issueAt("ISSUE-0001", 3, "The API must return JSON.")
	preflight := issueAt("PREFLIGHT-0001", 3, "The API must return JSON.")
	preflight.Evidence[0].Path = "other.md"
	result, err := ReuseFindings(ReuseInput{
		Plan:            plan,
		Previous:        reportWithIssue(issue),
		CurrentRaw:      raw,
		CurrentRedacted: raw,
		Config:          Config{SeverityThreshold: "info", MaxRemapFailureRatio: 0.25},
		PreflightIssues: []schema.Issue{preflight},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 1 {
		t.Fatalf("issues = %#v dropped=%#v", result.Issues, result.Dropped)
	}
}

func TestReuseFindingsPreflightDuplicateUsesRemappedLines(t *testing.T) {
	previous := "# Spec\n## A\nThe API must return JSON.\n## B\nOther text.\n"
	current := "# Spec\n## B\nOther text.\n## A\nThe API must return JSON.\n"
	plan, err := PlanChanges(previous, current, testPlanConfig())
	if err != nil {
		t.Fatal(err)
	}
	issue := issueAt("ISSUE-0001", 3, "The API must return JSON.")
	preflight := issueAt("PREFLIGHT-0001", 5, "The API must return JSON.")
	result, err := ReuseFindings(ReuseInput{
		Plan:            plan,
		Previous:        reportWithIssue(issue),
		CurrentRaw:      current,
		CurrentRedacted: current,
		Config:          Config{SeverityThreshold: "info", MaxRemapFailureRatio: 0.25},
		PreflightIssues: []schema.Issue{preflight},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 0 || len(result.Dropped) != 1 || result.Dropped[0].Reason != "preflight_duplicate" {
		t.Fatalf("result = %#v", result)
	}
}

func reportWithIssue(issue schema.Issue) *schema.Report {
	return &schema.Report{
		Input:     schema.Input{SeverityThreshold: "info"},
		Issues:    []schema.Issue{issue},
		Questions: nil,
	}
}

func issueAt(id string, line int, quote string) schema.Issue {
	return schema.Issue{
		ID:             id,
		Severity:       schema.SeverityWarn,
		Category:       schema.CategoryUnspecifiedConstraint,
		Title:          "Finding",
		Description:    "desc",
		Evidence:       []schema.Evidence{{Path: "SPEC.md", LineStart: line, LineEnd: line, Quote: quote}},
		Impact:         "impact",
		Recommendation: "rec",
	}
}
