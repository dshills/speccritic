package profile

import (
	"fmt"
	"strings"

	"github.com/dshills/speccritic/internal/schema"
)

// Profile defines the rules for a named evaluation profile.
type Profile struct {
	Name             string
	RequiredSections []string
	ForbiddenPhrases []string
	DomainInvariants []string
	ExtraCategories  []schema.Category
}

// Get returns the built-in profile for the given name.
func Get(name string) (*Profile, error) {
	switch name {
	case "general", "":
		return general(), nil
	case "backend-api":
		return backendAPI(), nil
	case "regulated-system":
		return regulatedSystem(), nil
	case "event-driven":
		return eventDriven(), nil
	default:
		return nil, fmt.Errorf("unknown profile %q: valid profiles are general, backend-api, regulated-system, event-driven", name)
	}
}

// FormatRulesForPrompt returns a string suitable for injection into the LLM system prompt.
func (p *Profile) FormatRulesForPrompt() string {
	if len(p.DomainInvariants) == 0 && len(p.RequiredSections) == 0 && len(p.ForbiddenPhrases) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Profile: %s\n", p.Name))

	if len(p.RequiredSections) > 0 {
		sb.WriteString("\nRequired sections (flag MISSING_INVARIANT if absent):\n")
		for _, s := range p.RequiredSections {
			sb.WriteString(fmt.Sprintf("- %s\n", s))
		}
	}

	if len(p.ForbiddenPhrases) > 0 {
		sb.WriteString("\nForbidden vague phrases (flag NON_TESTABLE_REQUIREMENT or AMBIGUOUS_BEHAVIOR if present):\n")
		for _, ph := range p.ForbiddenPhrases {
			sb.WriteString(fmt.Sprintf("- %q\n", ph))
		}
	}

	if len(p.DomainInvariants) > 0 {
		sb.WriteString("\nDomain invariants (flag MISSING_INVARIANT if violated):\n")
		for _, inv := range p.DomainInvariants {
			sb.WriteString(fmt.Sprintf("- %s\n", inv))
		}
	}

	return sb.String()
}
