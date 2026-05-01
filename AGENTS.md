# AGENTS.md

This file provides guidance to Codex (Codex.ai/code) when working with code in this repository.

## Project Overview

**SpecCritic** is a Go CLI tool that evaluates software specifications (SPEC.md files) as formal contracts, determining whether they are complete, internally consistent, unambiguous, testable, and safe to implement. It behaves like a "hostile contract lawyer" — not a collaborator.

The authoritative specification is in `specs/SPEC.md`.

## Code Quality

After writing or modifying code, run `prism review staged` before committing.
If findings are severity high, fix them before proceeding.
For security-sensitive changes, use compare mode:
```sh
prism review staged --compare openai:gpt-5.2,gemini:gemini-3-flash-preview
```

## Build, Test, and Lint Commands

This project is in the specification phase — no implementation exists yet. When implemented in Go, standard commands will be:

```sh
go build ./...
go test ./...
go vet ./...
```

For a single test:
```sh
go test ./internal/<package> -run TestName
```

## Intended Architecture

**Language:** Go

**Package layout:**
```
cmd/speccritic/       # CLI entry point (cobra or similar)
internal/spec/        # Spec file reading, line-numbering, section parsing
internal/context/     # Optional grounding document loading
internal/redact/      # Secret redaction (keys, tokens, passwords → [REDACTED])
internal/profile/     # Profile rule loading (general, backend-api, regulated-system, event-driven)
internal/llm/         # LLM provider abstraction, prompt construction, retry logic
internal/schema/      # JSON output schema validation
internal/review/      # Scoring, verdict calculation, issue/question models
internal/render/      # Output formatting (JSON / Markdown)
internal/patch/       # Patch diff generation
```

**Data flow:**
1. Read spec + optional context files
2. Redact secrets (always before LLM call)
3. Add line numbers to spec content
4. Load profile rules
5. Build LLM prompt (spec with line numbers + context + profile rules + anti-hallucination instructions)
6. Call LLM (JSON-only output required; one retry with repair prompt allowed)
7. Validate JSON against schema; validate evidence line bounds
8. Calculate score (start 100, −20 per CRITICAL, −7 per WARN, −2 per INFO, clamped at 0) and determine verdict
9. Emit formatted output (stdout or `--out` file)

## CLI Interface

```
speccritic check SPEC.md [flags]
```

Key flags: `--format`, `--out`, `--context`, `--profile`, `--strict`, `--fail-on`, `--severity-threshold`, `--patch-out`, `--temperature`, `--max-tokens`, `--offline`, `--verbose`, `--debug`

Exit codes: `0` = acceptable, `2` = invalid per `--fail-on`, `3` = input error, `4` = LLM/provider error, `5` = invalid model output

## Core Invariants

- Verdict is deterministic: any CRITICAL issue forces verdict ≥ INVALID
- Redaction is always applied before any LLM call
- LLM output must be JSON-only matching the v1 schema — no prose
- Evidence references must include valid line bounds into the actual spec file
- Patches are advisory and minimal (never wholesale rewrites)
- Strict mode (`--strict`): silence = ambiguity; any required assumption → CRITICAL

## Testing Strategy (from SPEC.md)

- **Unit tests:** redaction correctness, line-number integrity, schema validation failures, deterministic scoring, patch diff correctness
- **Golden tests:** known-bad spec → expected CRITICAL findings; known-good spec → VALID verdict
- **Integration tests:** mock LLM provider, validate end-to-end CLI behavior including exit codes

## Code Search Protocol

Use this decision tree — in order — before reading any source file:

### Structural questions → atlas (always first)
- "Where is X defined?" → `atlas find symbol X --agent`
- "What calls X?" → `atlas who-calls X --agent`
- "What does X call?" → `atlas calls X --agent`
- "What implements interface X?" → `atlas implementations X --agent`
- "Which tests cover X?" → `atlas tests-for X --agent`
- "What routes exist?" → `atlas list routes --agent`
- "What changed?" → `atlas index --since HEAD~1 && atlas stale --agent`

### Before reading a large file → summarize first
`atlas summarize file <path> --agent`
Only read the file directly if the summary is insufficient.

### Content/pattern questions → rg
- Error strings, log messages, string literals
- Comments, TODOs, inline notes
- Non-Go/TS files (YAML, SQL, Markdown)
- Unstaged files not yet indexed

### Never read source files to answer these questions
If atlas has the answer, do not use Read or Bash(cat).
Atlas is authoritative — its index is maintained by a PostToolUse hook on Write/Edit/MultiEdit.
