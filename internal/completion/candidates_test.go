package completion

import (
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/schema"
)

func TestGenerateCandidatesMissingSectionMapping(t *testing.T) {
	tmpl := mustTemplate(t, schema.CompletionTemplateGeneral)
	candidates := GenerateCandidates(Input{
		SpecText: "# Spec\n\n## Purpose\nThe system must work.\n",
		Template: tmpl,
		Issues: []schema.Issue{{
			ID:       "PREFLIGHT-STRUCTURE-002",
			Severity: schema.SeverityCritical,
			Category: schema.CategoryUnspecifiedConstraint,
			Title:    "Missing non-goals or out-of-scope section",
			Tags:     []string{"missing-section"},
		}},
		Config: Config{OpenDecisions: true},
	})
	if len(candidates) != 1 {
		t.Fatalf("candidates = %#v", candidates)
	}
	got := candidates[0]
	if got.SourceIssueID != "PREFLIGHT-STRUCTURE-002" || got.Section != "Non-Goals" {
		t.Fatalf("candidate = %#v", got)
	}
	if got.Status != StatusPatchGenerated || got.TargetLine == 0 {
		t.Fatalf("status=%s target=%d", got.Status, got.TargetLine)
	}
	if !strings.Contains(got.Text, "## Non-Goals") || !strings.Contains(got.Text, "OPEN DECISION:") {
		t.Fatalf("text = %q", got.Text)
	}
}

func TestGenerateCandidatesCategoryMapping(t *testing.T) {
	tmpl := mustTemplate(t, schema.CompletionTemplateBackendAPI)
	candidates := GenerateCandidates(Input{
		SpecText: "# API\n\n## Endpoints\nGET /health must return 200.\n",
		Template: tmpl,
		Issues: []schema.Issue{{
			ID:       "ISSUE-0001",
			Severity: schema.SeverityWarn,
			Category: schema.CategoryMissingFailureMode,
			Title:    "Undefined error behavior",
		}},
		Config: Config{OpenDecisions: true},
	})
	if len(candidates) != 1 {
		t.Fatalf("candidates = %#v", candidates)
	}
	if candidates[0].Section != "Error Responses" {
		t.Fatalf("section = %q", candidates[0].Section)
	}
}

func TestGenerateCandidatesKeywordMappingUsesTokenBoundaries(t *testing.T) {
	tmpl := mustTemplate(t, schema.CompletionTemplateBackendAPI)
	candidates := GenerateCandidates(Input{
		SpecText: "# API\n\n## Endpoints\nGET /health must return 200.\n",
		Template: tmpl,
		Issues: []schema.Issue{{
			ID:       "ISSUE-0001",
			Severity: schema.SeverityWarn,
			Category: schema.CategoryScopeLeak,
			Title:    "Rapid mapping behavior is unclear",
		}},
		Config: Config{OpenDecisions: true},
	})
	if len(candidates) != 0 {
		t.Fatalf("candidates = %#v, want no api substring match", candidates)
	}
}

func TestGenerateCandidatesLinkedQuestionTraceability(t *testing.T) {
	tmpl := mustTemplate(t, schema.CompletionTemplateGeneral)
	candidates := GenerateCandidates(Input{
		SpecText: "# Spec\n",
		Template: tmpl,
		Issues: []schema.Issue{{
			ID:       "PREFLIGHT-STRUCTURE-001",
			Severity: schema.SeverityCritical,
			Category: schema.CategoryUnspecifiedConstraint,
			Title:    "Missing purpose or goals section",
		}},
		Questions: []schema.Question{
			{ID: "Q-0002", Blocks: []string{"OTHER"}},
			{ID: "Q-0001", Blocks: []string{"PREFLIGHT-STRUCTURE-001"}},
		},
		Config: Config{OpenDecisions: true},
	})
	if len(candidates) != 1 {
		t.Fatalf("candidates = %#v", candidates)
	}
	if got := candidates[0].SourceQuestionIDs; len(got) != 1 || got[0] != "Q-0001" {
		t.Fatalf("question IDs = %#v", got)
	}
}

func TestGenerateCandidatesOpenDecisionsDisabled(t *testing.T) {
	tmpl := mustTemplate(t, schema.CompletionTemplateGeneral)
	candidates := GenerateCandidates(Input{
		SpecText: "# Spec\n",
		Template: tmpl,
		Issues: []schema.Issue{{
			ID:       "PREFLIGHT-STRUCTURE-001",
			Severity: schema.SeverityCritical,
			Category: schema.CategoryUnspecifiedConstraint,
			Title:    "Missing purpose or goals section",
		}},
		Config: Config{OpenDecisions: false},
	})
	if len(candidates) != 1 {
		t.Fatalf("candidates = %#v", candidates)
	}
	if candidates[0].Status != StatusSkippedOpenDecisionsDisabled {
		t.Fatalf("status = %s", candidates[0].Status)
	}
}

func TestGenerateCandidatesDeterministicOrdering(t *testing.T) {
	tmpl := mustTemplate(t, schema.CompletionTemplateGeneral)
	candidates := GenerateCandidates(Input{
		SpecText: "# Spec\n",
		Template: tmpl,
		Issues: []schema.Issue{
			{ID: "PREFLIGHT-STRUCTURE-004", Severity: schema.SeverityInfo, Category: schema.CategoryNonTestableRequirement, Title: "Missing acceptance criteria"},
			{ID: "PREFLIGHT-STRUCTURE-001", Severity: schema.SeverityCritical, Category: schema.CategoryUnspecifiedConstraint, Title: "Missing purpose"},
			{ID: "PREFLIGHT-STRUCTURE-003", Severity: schema.SeverityWarn, Category: schema.CategoryUnspecifiedConstraint, Title: "Missing requirements"},
		},
		Config: Config{OpenDecisions: true},
	})
	got := candidateIssueIDs(candidates)
	want := []string{"PREFLIGHT-STRUCTURE-001", "PREFLIGHT-STRUCTURE-003", "PREFLIGHT-STRUCTURE-004"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %#v, want %#v", got, want)
	}
}

func TestGenerateCandidatesDeduplicatesSameInsertion(t *testing.T) {
	tmpl := mustTemplate(t, schema.CompletionTemplateGeneral)
	candidates := GenerateCandidates(Input{
		SpecText: "# Spec\n",
		Template: tmpl,
		Issues: []schema.Issue{
			{ID: "PREFLIGHT-STRUCTURE-001", Severity: schema.SeverityCritical, Category: schema.CategoryUnspecifiedConstraint, Title: "Missing purpose"},
			{ID: "ISSUE-0001", Severity: schema.SeverityWarn, Category: schema.CategoryUnspecifiedConstraint, Title: "Missing purpose"},
		},
		Config: Config{OpenDecisions: true},
	})
	if len(candidates) != 1 {
		t.Fatalf("candidates = %#v", candidates)
	}
	if candidates[0].SourceIssueID != "PREFLIGHT-STRUCTURE-001" {
		t.Fatalf("source = %s", candidates[0].SourceIssueID)
	}
}

func mustTemplate(t *testing.T, name string) *Template {
	t.Helper()
	tmpl, err := GetTemplate(name, name)
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	return tmpl
}

func candidateIssueIDs(candidates []Candidate) []string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.SourceIssueID)
	}
	return out
}
