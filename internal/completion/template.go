package completion

import (
	"fmt"
	"strings"

	"github.com/dshills/speccritic/internal/schema"
)

type Mode string

const (
	ModeAuto Mode = schema.CompletionModeAuto
	ModeOn   Mode = schema.CompletionModeOn
	ModeOff  Mode = schema.CompletionModeOff
)

type Status string

const (
	StatusPatchGenerated               Status = "patch_generated"
	StatusSkippedNoSafeLocation        Status = "skipped_no_safe_location"
	StatusSkippedOverlap               Status = "skipped_overlap"
	StatusSkippedRedaction             Status = "skipped_redaction"
	StatusSkippedLimit                 Status = "skipped_limit"
	StatusSkippedOpenDecisionsDisabled Status = "skipped_open_decisions_disabled"
)

type Config struct {
	Suggestions   bool
	Mode          Mode
	Template      string
	MaxPatches    int
	OpenDecisions bool
}

type Template struct {
	Name     string
	Sections []TemplateSection
}

type TemplateSection struct {
	Heading          string
	Subheadings      []string
	Placeholders     []Placeholder
	Categories       []schema.Category
	PreflightRuleIDs []string
	Order            int
}

type Placeholder struct {
	Text              string
	RequiresDecision  bool
	RelevanceKeywords []string
}

type Candidate struct {
	SourceIssueID     string
	SourceQuestionIDs []string
	Template          string
	Section           string
	// TargetLine is the 1-based target line in the current spec. Zero means unknown.
	TargetLine   int
	Status       Status
	Text         string
	Severity     schema.Severity
	SectionOrder int
}

type Result struct {
	Candidates []Candidate
	Patches    []schema.Patch
	Meta       schema.CompletionMeta
}

// GetTemplate returns a built-in completion template. The special name
// "profile" resolves to selectedProfile.
func GetTemplate(name string, selectedProfile string) (*Template, error) {
	if name == "" || name == schema.CompletionTemplateProfile {
		name = selectedProfile
	}
	switch name {
	case schema.CompletionTemplateGeneral, "":
		tmpl := generalTemplate()
		return &tmpl, nil
	case schema.CompletionTemplateBackendAPI:
		tmpl := backendAPITemplate()
		return &tmpl, nil
	case schema.CompletionTemplateRegulatedSystem:
		tmpl := regulatedSystemTemplate()
		return &tmpl, nil
	case schema.CompletionTemplateEventDriven:
		tmpl := eventDrivenTemplate()
		return &tmpl, nil
	default:
		return nil, fmt.Errorf("unknown completion template %q: valid templates are %s", name, strings.Join(schema.CompletionInputTemplateNames(), ", "))
	}
}

func section(order int, heading string, categories []schema.Category, ruleIDs []string, placeholders ...Placeholder) TemplateSection {
	return TemplateSection{
		Heading:          heading,
		Subheadings:      []string{},
		Categories:       categories,
		PreflightRuleIDs: ruleIDs,
		Placeholders:     placeholders,
		Order:            order,
	}
}

func decision(text string, keywords ...string) Placeholder {
	return Placeholder{Text: text, RequiresDecision: true, RelevanceKeywords: keywords}
}

func note(text string, keywords ...string) Placeholder {
	return Placeholder{Text: text, RelevanceKeywords: keywords}
}
