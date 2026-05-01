package preflight

import (
	"testing"

	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/spec"
)

func TestRunRulesEmpty(t *testing.T) {
	s := spec.New("SPEC.md", "one\ntwo")
	result, err := RunRules(s, Config{Enabled: true, Profile: "general"}, nil)
	if err != nil {
		t.Fatalf("RunRules: %v", err)
	}
	if len(result.Issues) != 0 {
		t.Fatalf("issues = %d, want 0", len(result.Issues))
	}
}

func TestRunRulesProfileFiltering(t *testing.T) {
	s := spec.New("SPEC.md", "The API should work.")
	rules := []Rule{
		testRule("PREFLIGHT-TEST-001", []string{"backend-api"}, 1),
		testRule("PREFLIGHT-TEST-002", []string{"general"}, 1),
	}
	result, err := RunRules(s, Config{Enabled: true, Profile: "general"}, rules)
	if err != nil {
		t.Fatalf("RunRules: %v", err)
	}
	if len(result.Issues) != 1 {
		t.Fatalf("issues = %d, want 1", len(result.Issues))
	}
	if result.Issues[0].ID != "PREFLIGHT-TEST-002" {
		t.Fatalf("issue ID = %s", result.Issues[0].ID)
	}
}

func TestRunRulesEvidenceAndTags(t *testing.T) {
	s := spec.New("SPEC.md", "first\nsecond")
	rules := []Rule{testRule("PREFLIGHT-TEST-001", nil, 2)}
	result, err := RunRules(s, Config{Enabled: true, Profile: "general"}, rules)
	if err != nil {
		t.Fatalf("RunRules: %v", err)
	}
	issue := result.Issues[0]
	if issue.Evidence[0].LineStart != 2 || issue.Evidence[0].LineEnd != 2 {
		t.Fatalf("evidence = %d-%d", issue.Evidence[0].LineStart, issue.Evidence[0].LineEnd)
	}
	if issue.Evidence[0].Quote != "second" {
		t.Fatalf("quote = %q", issue.Evidence[0].Quote)
	}
	if !hasTag(issue.Tags, TagPreflight) || !hasTag(issue.Tags, "preflight-rule:PREFLIGHT-TEST-001") {
		t.Fatalf("tags = %#v", issue.Tags)
	}
}

func TestRunRulesInvalidEvidence(t *testing.T) {
	s := spec.New("SPEC.md", "first")
	rules := []Rule{testRule("PREFLIGHT-TEST-001", nil, 2)}
	_, err := RunRules(s, Config{Enabled: true, Profile: "general"}, rules)
	if err == nil {
		t.Fatal("RunRules error = nil, want invalid evidence error")
	}
}

func TestRunRulesSortsDeterministically(t *testing.T) {
	s := spec.New("SPEC.md", "one\ntwo\nthree")
	rules := []Rule{
		testRuleWithSeverity("PREFLIGHT-TEST-003", schema.SeverityInfo, 2),
		testRuleWithSeverity("PREFLIGHT-TEST-002", schema.SeverityCritical, 2),
		testRuleWithSeverity("PREFLIGHT-TEST-001", schema.SeverityWarn, 1),
	}
	result, err := RunRules(s, Config{Enabled: true, Profile: "general"}, rules)
	if err != nil {
		t.Fatalf("RunRules: %v", err)
	}
	got := []string{result.Issues[0].ID, result.Issues[1].ID, result.Issues[2].ID}
	want := []string{"PREFLIGHT-TEST-001", "PREFLIGHT-TEST-002", "PREFLIGHT-TEST-003"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %#v, want %#v", got, want)
		}
	}
}

func TestRunRulesDedupe(t *testing.T) {
	s := spec.New("SPEC.md", "one")
	rules := []Rule{
		testRule("PREFLIGHT-TEST-001", nil, 1),
		testRule("PREFLIGHT-TEST-001", nil, 1),
	}
	result, err := RunRules(s, Config{Enabled: true, Profile: "general"}, rules)
	if err != nil {
		t.Fatalf("RunRules: %v", err)
	}
	if len(result.Issues) != 1 {
		t.Fatalf("issues = %d, want 1", len(result.Issues))
	}
}

func testRule(id string, profiles []string, line int) Rule {
	return testRuleWithSeverity(id, schema.SeverityWarn, line).withProfiles(profiles)
}

func testRuleWithSeverity(id string, severity schema.Severity, line int) Rule {
	return Rule{
		ID:             id,
		Group:          "test",
		Title:          "Test rule",
		Description:    "Test description.",
		Severity:       severity,
		Category:       schema.CategoryAmbiguousBehavior,
		Profiles:       []string{"*"},
		Impact:         "Test impact.",
		Recommendation: "Test recommendation.",
		Matcher: MatcherFunc(func(_ Document, _ Rule, _ Config) []Finding {
			return []Finding{{LineStart: line}}
		}),
	}
}

func (r Rule) withProfiles(profiles []string) Rule {
	r.Profiles = profiles
	return r
}

func hasTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}
