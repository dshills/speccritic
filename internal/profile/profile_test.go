package profile

import (
	"strings"
	"testing"
)

func TestGet_AllNamedProfiles(t *testing.T) {
	names := []string{"general", "backend-api", "regulated-system", "event-driven"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			p, err := Get(name)
			if err != nil {
				t.Fatalf("Get(%q): %v", name, err)
			}
			if p == nil {
				t.Fatalf("Get(%q) returned nil profile", name)
			}
			if p.Name == "" {
				t.Errorf("profile name is empty")
			}
		})
	}
}

func TestGet_EmptyNameReturnsGeneral(t *testing.T) {
	p, err := Get("")
	if err != nil {
		t.Fatalf("Get(''): %v", err)
	}
	if p.Name != "general" {
		t.Errorf("expected general, got %q", p.Name)
	}
}

func TestGet_UnknownName(t *testing.T) {
	_, err := Get("nonexistent-profile")
	if err == nil {
		t.Error("expected error for unknown profile, got nil")
	}
}

func TestFormatRulesForPrompt_ContainsInvariants(t *testing.T) {
	p, _ := Get("backend-api")
	rules := p.FormatRulesForPrompt()
	if !strings.Contains(rules, "endpoint") {
		t.Errorf("expected backend-api invariants in prompt rules: %q", rules)
	}
}

func TestFormatRulesForPrompt_GeneralEmpty(t *testing.T) {
	// general has no required sections, so output must still be non-empty
	// (it has forbidden phrases and invariants)
	p, _ := Get("general")
	rules := p.FormatRulesForPrompt()
	if rules == "" {
		t.Error("expected non-empty rules for general profile")
	}
}
