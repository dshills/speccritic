package preflight

import (
	"testing"

	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/spec"
)

func TestAcronymRuleAllowsCommonTechnicalTerms(t *testing.T) {
	result := runBuiltin(t, "SPEC.md", "The API returns JSON to the UI over HTTP.")
	if findIssue(result.Issues, "PREFLIGHT-ACRONYM-001") != nil {
		t.Fatalf("common acronym was flagged: %#v", result.Issues)
	}
}

func TestAcronymRuleDetectsUndefinedAcronym(t *testing.T) {
	result := runBuiltin(t, "SPEC.md", "The FOO adapter must run.")
	requireIssue(t, result.Issues, "PREFLIGHT-ACRONYM-001", schema.SeverityWarn, 1)
}

func TestAcronymRuleHonorsParentheticalDefinition(t *testing.T) {
	result := runBuiltin(t, "SPEC.md", "The Fraud Observation Operator (FOO) must run.\nFOO emits events.")
	if findIssue(result.Issues, "PREFLIGHT-ACRONYM-001") != nil {
		t.Fatalf("defined acronym was flagged: %#v", result.Issues)
	}
}

func TestAcronymRuleHonorsGlossaryDefinition(t *testing.T) {
	result := runBuiltin(t, "SPEC.md", "## Glossary\nFOO: Fraud Observation Operator\n## Requirements\nFOO emits events.")
	if findIssue(result.Issues, "PREFLIGHT-ACRONYM-001") != nil {
		t.Fatalf("glossary acronym was flagged: %#v", result.Issues)
	}
}

func TestMeasurableRuleDetectsMissingValue(t *testing.T) {
	result := runBuiltin(t, "SPEC.md", "The API timeout must be configurable.")
	requireIssue(t, result.Issues, "PREFLIGHT-MEASURABLE-001", schema.SeverityWarn, 1)
}

func TestMeasurableRuleAllowsConcreteValue(t *testing.T) {
	result := runBuiltin(t, "SPEC.md", "The API timeout must be 5 seconds.")
	if findIssue(result.Issues, "PREFLIGHT-MEASURABLE-001") != nil {
		t.Fatalf("concrete measurable value was flagged: %#v", result.Issues)
	}
}

func TestMeasurableRuleIgnoresUnrelatedNumber(t *testing.T) {
	result := runBuiltin(t, "SPEC.md", "The system has 2 APIs, but latency must be fast.")
	requireIssue(t, result.Issues, "PREFLIGHT-MEASURABLE-001", schema.SeverityWarn, 1)
}

func TestMeasurableRuleAllowsNearbyLeadingValue(t *testing.T) {
	result := runBuiltin(t, "SPEC.md", "P99 200ms latency is required.")
	if findIssue(result.Issues, "PREFLIGHT-MEASURABLE-001") != nil {
		t.Fatalf("nearby leading measurable value was flagged: %#v", result.Issues)
	}
}

func TestMeasurableRuleAllowsPercentage(t *testing.T) {
	result := runBuiltin(t, "SPEC.md", "Availability must be 99.9%.")
	if findIssue(result.Issues, "PREFLIGHT-MEASURABLE-001") != nil {
		t.Fatalf("percentage measurable value was flagged: %#v", result.Issues)
	}
}

func TestAcronymRuleOnlyReportsFirstUndefinedUse(t *testing.T) {
	result, err := Run(spec.New("SPEC.md", "FOO starts.\nFOO stops."), Config{Enabled: true, Profile: "general"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	count := 0
	for _, issue := range result.Issues {
		if issue.ID == "PREFLIGHT-ACRONYM-001" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("acronym issue count = %d, want 1", count)
	}
}
