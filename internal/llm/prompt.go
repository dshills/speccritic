package llm

import (
	"fmt"
	"strings"

	ctx "github.com/dshills/speccritic/internal/context"
	"github.com/dshills/speccritic/internal/profile"
	"github.com/dshills/speccritic/internal/spec"
)

const systemPromptBase = `You are a specification auditor. Your job is to identify defects in software specifications.

Defect categories you must check:
- NON_TESTABLE_REQUIREMENT: requirement cannot be verified by a test
- AMBIGUOUS_BEHAVIOR: two engineers could implement differently
- CONTRADICTION: two statements cannot both be true
- MISSING_FAILURE_MODE: what happens when X fails is not stated
- UNDEFINED_INTERFACE: a referenced interface has no specification
- MISSING_INVARIANT: a property that must always hold is not stated
- SCOPE_LEAK: spec describes implementation, not behavior
- ORDERING_UNDEFINED: sequence of operations is ambiguous
- TERMINOLOGY_INCONSISTENT: same concept named differently
- UNSPECIFIED_CONSTRAINT: implicit constraint not made explicit
- ASSUMPTION_REQUIRED: must assume something unstated to implement

Severity rules:
- CRITICAL: must be resolved before implementation can begin
- WARN: should be resolved; implementation possible but risky
- INFO: note for clarity; does not block implementation

Anti-hallucination rules:
- Only cite lines that exist in the provided spec (lines are prefixed L1:, L2:, etc.)
- Do not invent requirements not present in the spec
- Do not suggest architectural solutions
- Every issue must have at least one evidence block with valid line numbers
- Issue IDs must follow format ISSUE-XXXX (four digits, zero-padded)
- Question IDs must follow format Q-XXXX

Output rules:
- Return JSON only — no prose, no markdown fences, no explanation
- JSON must match the provided schema exactly
- Do not include score or verdict — those are computed externally`

const strictModeText = `
STRICT MODE ENABLED: Treat all silence as ambiguity. Any behavior not explicitly
stated must be flagged. Any assumption required to implement must be filed as CRITICAL.
Label all uncertain findings with tag "assumption".`

const schemaExample = `{
  "issues": [
    {
      "id": "ISSUE-0001",
      "severity": "CRITICAL",
      "category": "NON_TESTABLE_REQUIREMENT",
      "title": "Short title describing the defect",
      "description": "Detailed explanation of the defect",
      "evidence": [{"path": "SPEC.md", "line_start": 10, "line_end": 12, "quote": "exact text from spec"}],
      "impact": "What goes wrong if this is not fixed",
      "recommendation": "Minimal corrective action",
      "blocking": true,
      "tags": []
    }
  ],
  "questions": [
    {
      "id": "Q-0001",
      "severity": "CRITICAL",
      "question": "Specific question that must be answered before implementation",
      "why_needed": "Why this question blocks implementation",
      "blocks": ["REQ-001"],
      "evidence": [{"path": "SPEC.md", "line_start": 10, "line_end": 12, "quote": "exact text"}]
    }
  ],
  "patches": [
    {
      "issue_id": "ISSUE-0001",
      "before": "exact text from spec to be replaced",
      "after": "corrected minimal replacement text"
    }
  ]
}`

// BuildSystemPrompt constructs the system prompt with optional profile rules
// and strict mode injection.
func BuildSystemPrompt(p *profile.Profile, strict bool) string {
	var sb strings.Builder
	sb.WriteString(systemPromptBase)

	if strict {
		sb.WriteString(strictModeText)
	}

	if p != nil {
		rules := p.FormatRulesForPrompt()
		if rules != "" {
			sb.WriteString("\n\n")
			sb.WriteString(rules)
		}
	}

	return sb.String()
}

// BuildUserPrompt constructs the user prompt with the spec, optional context
// files, and the JSON schema example.
func BuildUserPrompt(s *spec.Spec, contextFiles []ctx.ContextFile) string {
	var sb strings.Builder

	sb.WriteString("Analyze the following specification for defects.\n\n")

	sb.WriteString(fmt.Sprintf("<spec file=%q>\n", s.Path))
	sb.WriteString(s.Numbered)
	if !strings.HasSuffix(s.Numbered, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("</spec>\n")

	if len(contextFiles) > 0 {
		sb.WriteString("\n")
		sb.WriteString(ctx.FormatForPrompt(contextFiles))
	}

	sb.WriteString("\nReturn your findings as JSON with this structure:\n")
	sb.WriteString(schemaExample)

	return sb.String()
}
