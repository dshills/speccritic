SpecCritic — CLI Specification

1. Purpose

SpecCritic is a CLI tool that evaluates software specifications (SPEC.md) as formal contracts and determines whether they are:
	•	complete,
	•	internally consistent,
	•	unambiguous,
	•	testable,
	•	and safe to implement.

It is designed to run before planning, and repeatedly during spec refinement, by either a human or a coding agent.

SpecCritic does not design solutions.
SpecCritic does not generate plans.
SpecCritic identifies defects in the specification itself.

⸻

2. Core Philosophy

A specification is invalid if:

Two competent engineers could implement it differently and both believe they followed it.

SpecCritic assumes:
	•	Specs define what must be true, not how to do it
	•	Vague language is a defect, not a style issue
	•	Unverifiable requirements are invalid requirements
	•	Missing failure modes are bugs
	•	Silence is ambiguity

SpecCritic behaves like a hostile contract lawyer, not a collaborator.

⸻

3. Non-Goals (Phase 1)
	•	No code review
	•	No plan review
	•	No browsing or repo inspection
	•	No speculative “best practices”
	•	No rewriting the spec wholesale
	•	No implementation suggestions beyond minimal corrective examples

⸻

4. Intended Workflow

Human + Agent Loop
	1.	User writes or updates SPEC.md
	2.	Coding agent runs:

speccritic check SPEC.md


	3.	If issues exist:
	•	Agent either:
	•	revises the spec directly, or
	•	asks the user targeted clarification questions
	4.	Repeat until SpecCritic verdict is acceptable
	5.	Only then does the agent generate a plan

SpecCritic is designed to be cheap to run and safe to loop.

⸻

5. CLI Interface

Command

speccritic check SPEC.md [flags]

Flags

Flag	Description
--format	json (default) or md
--out	Write output to file instead of stdout
--context <file>	Optional grounding documents (architecture notes, glossary, constraints)
--profile <name>	Specification profile (default: general)
--strict	Forbid unstated assumptions
--fail-on <level>	Exit non-zero if verdict ≥ level
--severity-threshold	Minimum issue severity to emit
--patch-out <file>	Emit suggested minimal spec edits as diff
--temperature	Default 0.2
--max-tokens	Hard cap for response
--offline	Fail if no LLM configured
--verbose	Execution tracing
--debug	Dump redacted prompt

Exit Codes

Code	Meaning
0	Spec acceptable
2	Spec invalid per --fail-on
3	Input error
4	LLM/provider error
5	Invalid model output


⸻

6. Input Handling

SPEC Input
	•	Markdown or plain text
	•	Line numbers must be preserved
	•	Sections, headings, and numbered requirements should be detected when present

Context Files (Optional)

Used only for grounding, never inference.

Examples:
	•	glossary.md
	•	constraints.md
	•	regulatory notes
	•	architecture overview

If context is referenced in critique, it must be cited explicitly.

⸻

7. Redaction

Before sending input to the LLM:
	•	Redact secrets (keys, tokens, passwords)
	•	Replace with [REDACTED]
	•	Preserve semantic shape

Enabled by default.

⸻

8. Output: Canonical JSON Schema (v1)

Top-Level Structure

{
  "tool": "speccritic",
  "version": "1.0",
  "input": {
    "spec_file": "SPEC.md",
    "spec_hash": "sha256:...",
    "context_files": [],
    "profile": "general",
    "strict": true
  },
  "summary": {
    "verdict": "INVALID",
    "score": 61,
    "critical_count": 3,
    "warn_count": 6,
    "info_count": 4
  },
  "issues": [],
  "questions": [],
  "patches": [],
  "meta": {
    "model": "provider/model",
    "temperature": 0.2
  }
}


⸻

9. Verdicts

Verdict	Meaning
VALID	Spec is internally consistent and testable
VALID_WITH_GAPS	Spec usable but requires clarification
INVALID	Spec cannot be safely implemented

Rules:
	•	Any CRITICAL issue → verdict ≥ INVALID
	•	Verdict logic must be deterministic

⸻

10. Issue Model

{
  "id": "ISSUE-0012",
  "severity": "CRITICAL",
  "category": "NON_TESTABLE_REQUIREMENT",
  "title": "Performance requirement is not measurable",
  "description": "The spec requires the system to be 'fast' without defining metrics or conditions.",
  "evidence": [
    {
      "path": "SPEC.md",
      "line_start": 47,
      "line_end": 48,
      "quote": "The system must provide fast responses."
    }
  ],
  "impact": "Implementations cannot be verified or compared.",
  "recommendation": "Define concrete latency targets and measurement conditions.",
  "blocking": true,
  "tags": ["acceptance", "performance"]
}

Severity
	•	INFO
	•	WARN
	•	CRITICAL

Categories (Phase 1)
	•	NON_TESTABLE_REQUIREMENT
	•	AMBIGUOUS_BEHAVIOR
	•	CONTRADICTION
	•	MISSING_FAILURE_MODE
	•	UNDEFINED_INTERFACE
	•	MISSING_INVARIANT
	•	SCOPE_LEAK
	•	ORDERING_UNDEFINED
	•	TERMINOLOGY_INCONSISTENT
	•	UNSPECIFIED_CONSTRAINT
	•	ASSUMPTION_REQUIRED

⸻

11. Questions Model

Questions are blocking clarification requests, not suggestions.

{
  "id": "Q-0005",
  "severity": "CRITICAL",
  "question": "What are the acceptable latency bounds for this endpoint?",
  "why_needed": "The requirement is currently non-testable.",
  "blocks": ["REQ-004"],
  "evidence": [
    {
      "path": "SPEC.md",
      "line_start": 45,
      "line_end": 48,
      "quote": "The system must be fast."
    }
  ]
}

Rules:
	•	Questions must be minimal
	•	No “would you like to…” phrasing
	•	If unanswered, implementation is unsafe

⸻

12. Patches (Optional)

SpecCritic may propose minimal textual corrections, never rewrites.

Example:

- The system must be fast.
+ The system must respond within 250ms p95 under normal load.

Rules:
	•	Patches must be additive or minimally substitutive
	•	Every patch must reference a specific issue
	•	Patches are advisory, not authoritative

⸻

13. Scoring

Deterministic scoring:
	•	Start: 100
	•	−20 per CRITICAL
	•	−7 per WARN
	•	−2 per INFO
	•	Clamp at 0

Score exists for CI gating, not ego.

⸻

14. Profiles

Built-In Profiles (v1)
	•	general (default)
	•	backend-api
	•	regulated-system
	•	event-driven
	•	(Future) davin-go-spec

Profiles define:
	•	Required sections
	•	Forbidden ambiguity
	•	Domain-specific invariants

Profiles are rules, not preferences.

⸻

15. Strict Mode

When --strict is enabled:
	•	No inferred behavior
	•	Silence = ambiguity
	•	All assumptions must be flagged
	•	Any assumption required to implement → CRITICAL

Additionally:
	•	SpecCritic must label uncertain findings with tags += ["assumption"]
	•	Severity capped only by impact, not confidence

⸻

16. LLM Interaction Contract

Output Rules
	•	JSON only
	•	Must match schema
	•	No prose outside JSON

Prompt Must Include
	•	Spec with line numbers
	•	Context files (clearly delimited)
	•	Profile rules
	•	Explicit anti-hallucination rules
	•	Severity definitions

Validation
	•	Strict JSON parse
	•	Schema validation
	•	Evidence line bounds validation
	•	One retry allowed with repair prompt

⸻

17. Architecture (Go)

Suggested Packages

cmd/speccritic
internal/spec
internal/context
internal/redact
internal/profile
internal/llm
internal/schema
internal/review
internal/render
internal/patch

Data Flow
	1.	Read spec + contexts
	2.	Redact
	3.	Line-number
	4.	Load profile
	5.	Build prompt
	6.	Call LLM
	7.	Validate JSON
	8.	Score + verdict
	9.	Emit output

⸻

18. Testing Requirements

Unit Tests
	•	Redaction correctness
	•	Line-number integrity
	•	Schema validation failures
	•	Deterministic scoring
	•	Patch diff correctness

Golden Tests
	•	Known bad spec → expected CRITICAL findings
	•	Known good spec → VALID verdict

Integration Test
	•	Mock LLM provider
	•	Validate CLI behavior end-to-end

⸻

19. Security & Privacy
	•	No telemetry by default
	•	No prompt logging unless --debug
	•	Redaction always applied before LLM call

⸻

20. Acceptance Criteria (Phase 1)

SpecCritic is complete when:
	•	It reliably flags non-testable requirements
	•	It detects contradictions within the spec
	•	It produces actionable clarification questions
	•	It can gate CI via exit codes
	•	It can be run repeatedly without drift
	•	It improves downstream planning quality measurably

⸻

21. Relationship to PlanCritic

SpecCritic is authoritative.
PlanCritic is derivative.

The intended chain is:

SPEC.md
   ↓
SpecCritic (is this a valid contract?)
   ↓
PlanCritic (does the plan honor the contract?)
   ↓
Implementation

That chain is rare. That’s the edge.

