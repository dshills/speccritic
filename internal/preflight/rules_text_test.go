package preflight

import (
	"testing"

	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/spec"
)

func TestBuiltinRulesPlaceholderDetection(t *testing.T) {
	result := runBuiltin(t, "SPEC.md", "## Requirements\nTODO define retries")
	requireIssue(t, result.Issues, "PREFLIGHT-TODO-001", schema.SeverityCritical, 2)
}

func TestBuiltinRulesVagueLanguageDetection(t *testing.T) {
	result := runBuiltin(t, "SPEC.md", "The API must be fast.")
	requireIssue(t, result.Issues, "PREFLIGHT-VAGUE-001", schema.SeverityWarn, 1)
}

func TestBuiltinRulesWeakRequirementDetection(t *testing.T) {
	result := runBuiltin(t, "SPEC.md", "The service should retry failed requests.")
	requireIssue(t, result.Issues, "PREFLIGHT-WEAK-001", schema.SeverityWarn, 1)
}

func TestBuiltinRulesStrictModeEscalatesWeakRequirements(t *testing.T) {
	s := spec.New("SPEC.md", "The service should retry failed requests.")
	result, err := Run(s, Config{Enabled: true, Profile: "general", Strict: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	issue := requireIssue(t, result.Issues, "PREFLIGHT-WEAK-001", schema.SeverityCritical, 1)
	if !issue.Blocking {
		t.Fatal("strict weak requirement should be blocking")
	}
}

func TestBuiltinRulesSuppressesAntiPatternExamples(t *testing.T) {
	result := runBuiltin(t, "SPEC.md", "## Anti-pattern examples\nBad: The system must be fast.\n## Requirements\nThe system must be robust.")
	for _, issue := range result.Issues {
		if issue.ID == "PREFLIGHT-VAGUE-001" && issue.Evidence[0].LineStart == 2 {
			t.Fatal("vague rule fired inside anti-pattern section")
		}
	}
	requireIssue(t, result.Issues, "PREFLIGHT-VAGUE-001", schema.SeverityWarn, 4)
}

func TestBuiltinRulesHonorsIgnoreIDs(t *testing.T) {
	s := spec.New("SPEC.md", "TODO define behavior")
	result, err := Run(s, Config{Enabled: true, Profile: "general", IgnoreIDs: []string{"PREFLIGHT-TODO-001"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if findIssue(result.Issues, "PREFLIGHT-TODO-001") != nil {
		t.Fatal("ignored rule emitted an issue")
	}
}

func runBuiltin(t *testing.T, name, text string) Result {
	t.Helper()
	result, err := Run(spec.New(name, text), Config{Enabled: true, Profile: "general"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return result
}

func requireIssue(t *testing.T, issues []schema.Issue, id string, severity schema.Severity, line int) schema.Issue {
	t.Helper()
	issue := findIssue(issues, id)
	if issue == nil {
		t.Fatalf("missing issue %s in %#v", id, issues)
	}
	if issue.Severity != severity {
		t.Fatalf("%s severity = %s, want %s", id, issue.Severity, severity)
	}
	if len(issue.Evidence) != 1 || issue.Evidence[0].LineStart != line {
		t.Fatalf("%s evidence = %#v, want line %d", id, issue.Evidence, line)
	}
	return *issue
}

func findIssue(issues []schema.Issue, id string) *schema.Issue {
	for i := range issues {
		if issues[i].ID == id {
			return &issues[i]
		}
	}
	return nil
}
