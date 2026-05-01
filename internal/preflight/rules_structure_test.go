package preflight

import (
	"testing"

	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/spec"
)

func TestStructuralRulesGeneralProfilePresent(t *testing.T) {
	text := `# Product Spec

## Purpose
Do a thing.

## Non-goals
Do not do another thing.

## Requirements
The system must work.

## Acceptance Criteria
The behavior is testable.`
	result, err := Run(spec.New("SPEC.md", text), Config{Enabled: true, Profile: "general"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hasIssueGroup(result.Issues, "missing-section") {
		t.Fatalf("unexpected missing section issue: %#v", result.Issues)
	}
}

func TestStructuralRulesGeneralProfileMissing(t *testing.T) {
	result, err := Run(spec.New("SPEC.md", "# Product Spec\n\n## Purpose\nDo a thing."), Config{Enabled: true, Profile: "general"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	requireIssue(t, result.Issues, "PREFLIGHT-STRUCTURE-002", schema.SeverityCritical, 1)
	requireIssue(t, result.Issues, "PREFLIGHT-STRUCTURE-003", schema.SeverityCritical, 1)
	requireIssue(t, result.Issues, "PREFLIGHT-STRUCTURE-004", schema.SeverityCritical, 1)
}

func TestStructuralRulesBackendProfile(t *testing.T) {
	text := `# API Spec
## Purpose
Review specs.
## Non-goals
No accounts.
## Requirements
Check specs.
## Acceptance Criteria
Returns findings.
## Endpoints
POST /checks.
## Error Codes
400 for invalid input.`
	result, err := Run(spec.New("SPEC.md", text), Config{Enabled: true, Profile: "backend-api"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	requireIssue(t, result.Issues, "PREFLIGHT-STRUCTURE-101", schema.SeverityCritical, 1)
	requireIssue(t, result.Issues, "PREFLIGHT-STRUCTURE-103", schema.SeverityCritical, 1)
	requireIssue(t, result.Issues, "PREFLIGHT-STRUCTURE-105", schema.SeverityCritical, 1)
	if findIssue(result.Issues, "PREFLIGHT-STRUCTURE-102") != nil {
		t.Fatal("endpoints section should satisfy backend endpoint requirement")
	}
}

func TestStructuralRulesNestedSynonymHeading(t *testing.T) {
	text := `# Spec
## Goals
Goal.
## Out of Scope
No billing.
## Functional Behavior
Behavior.
## Testing
Tests.`
	result, err := Run(spec.New("SPEC.md", text), Config{Enabled: true, Profile: "general"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hasIssueGroup(result.Issues, "missing-section") {
		t.Fatalf("unexpected missing section issue: %#v", result.Issues)
	}
}

func hasIssueGroup(issues []schema.Issue, group string) bool {
	for _, issue := range issues {
		if hasTag(issue.Tags, group) {
			return true
		}
	}
	return false
}
