# SpecCritic Deterministic Preflight Implementation Plan

## Phase 1 — Rule Engine Skeleton

### Goal

Create the local preflight package and prove it can emit deterministic findings without touching CLI or web behavior.

### Tasks

1. Add `internal/preflight`.
2. Define `Config`, `Result`, `Rule`, and matcher interfaces.
3. Add rule metadata fields:
   - ID,
   - title,
   - description,
   - severity,
   - category,
   - profiles,
   - tags,
   - blocking.
4. Implement profile filtering.
5. Implement deterministic finding sorting by line, severity, rule ID.
6. Implement evidence construction from line-numbered spec input.
7. Add unit tests for empty rule sets, profile filtering, sorting, and evidence bounds.

### Review Gate

- `go test ./internal/preflight`
- `go test ./...`
- `prism review staged`

### Acceptance

The package can run an empty or mock rule set and return stable `schema.Issue` values with valid evidence.

## Phase 2 — Text Pattern Rules

### Goal

Catch the highest-signal obvious defects with simple line-based matching.

### Tasks

1. Add placeholder rules for:
   - `TODO`,
   - `TBD`,
   - `FIXME`,
   - `???`,
   - `[placeholder]`,
   - `coming soon`,
   - `to be defined`.
2. Add vague-language rules for:
   - `fast`,
   - `quick`,
   - `reasonable`,
   - `as needed`,
   - `where appropriate`,
   - `user-friendly`,
   - `robust`,
   - `scalable`,
   - `secure`,
   - `intuitive`.
3. Add weak-requirement rules for:
   - `should`,
   - `may`,
   - `might`,
   - `can`,
   - `try to`,
   - `best effort`.
4. Add strict-mode severity escalation for weak requirements.
5. Add an examples/anti-pattern suppressor for lines under headings that clearly document bad examples.
6. Add focused tests for each rule family.

### Review Gate

- `go test ./internal/preflight`
- `go test ./...`
- `prism review staged`

### Acceptance

Known bad lines produce specific findings, known anti-pattern examples do not produce noisy findings, and all findings include exact line evidence.

## Phase 3 — Structural Rules

### Goal

Detect missing sections for the general and specialized profiles.

### Tasks

1. Reuse existing heading/section parsing where available.
2. Add a normalized heading matcher.
3. Add general profile required-section groups:
   - purpose or goals,
   - non-goals or out-of-scope,
   - requirements or functional behavior,
   - acceptance criteria or testability.
4. Add backend API profile required-section groups:
   - authentication or authorization,
   - endpoints or routes,
   - request/response schemas,
   - error responses,
   - rate limits or abuse handling.
5. Add regulated-system profile required-section groups:
   - audit trail,
   - data retention,
   - access control,
   - compliance or regulatory constraints.
6. Add event-driven profile required-section groups:
   - event schema,
   - delivery guarantees,
   - retry or dead-letter behavior,
   - consumer failure behavior.
7. Add tests for present, missing, synonym, and nested-heading cases.

### Review Gate

- `go test ./internal/preflight`
- `go test ./...`
- `prism review staged`

### Acceptance

Each supported profile emits deterministic missing-section findings with useful recommendations and valid fallback evidence.

## Phase 4 — Acronym and Measurable Criteria Rules

### Goal

Catch common defects that require light document-wide context but no model reasoning.

### Tasks

1. Add acronym extraction for all-caps tokens with at least two letters.
2. Treat acronyms as defined when:
   - they appear in parentheses after words, or
   - they appear in a glossary/definitions section, or
   - they are in a built-in allow-list.
3. Add first-use undefined acronym findings.
4. Add measurable-domain detectors for:
   - latency,
   - throughput,
   - availability,
   - retry timing,
   - timeout behavior,
   - retention period,
   - rate limit,
   - file size limit.
5. Add numeric/unit detection for:
   - integers and decimals,
   - percentages,
   - durations,
   - byte sizes,
   - explicit enums.
6. Emit findings when measurable domains appear without measurable criteria.
7. Add tests for definitions, allow-list behavior, and measurable criteria.

### Review Gate

- `go test ./internal/preflight`
- `go test ./...`
- `prism review staged`

### Acceptance

Undefined acronym and measurable-criteria findings are useful enough to include by default without producing excessive false positives on existing specs.

## Phase 5 — CLI Integration

### Goal

Run preflight before the LLM in `speccritic check`.

### Tasks

1. Add CLI flags:
   - `--preflight`,
   - `--preflight-mode`,
   - `--preflight-profile`.
2. Parse and validate modes:
   - `warn`,
   - `gate`,
   - `only`.
3. Run preflight after redaction and line numbering, before LLM prompt construction.
4. Implement `warn` mode:
   - include preflight findings,
   - continue to LLM.
5. Implement `gate` mode:
   - skip LLM when blocking preflight defects exist,
   - otherwise continue to LLM.
6. Implement `only` mode:
   - never call LLM,
   - do not require model configuration.
7. Merge and deduplicate preflight and LLM findings.
8. Apply normal scoring and verdict logic.
9. Add CLI integration tests with mock providers.

### Review Gate

- `go test ./...`
- `go test ./cmd/speccritic/...`
- `prism review staged`

### Acceptance

Users can run `speccritic check SPEC.md --preflight-mode only` without credentials and receive normal JSON/Markdown output.

## Phase 6 — Renderer Updates

### Goal

Make preflight findings visible without changing the core report schema.

### Tasks

1. Ensure JSON output includes preflight findings in `issues` with tag `preflight`.
2. Add Markdown `Preflight Findings` subsection when preflight findings exist.
3. Preserve existing issue ordering for non-preflight findings.
4. Add renderer tests for JSON and Markdown output.

### Review Gate

- `go test ./internal/render/...`
- `go test ./...`
- `prism review staged`

### Acceptance

Preflight findings are machine-readable through existing JSON and visually distinct in Markdown output.

## Phase 7 — Web Integration

### Goal

Expose preflight findings in the browser workflow.

### Tasks

1. Run preflight from web checks using the selected profile.
2. Use default mode `warn`.
3. Add server-side support for optional `preflight_mode`.
4. Display a `Preflight` label on preflight findings.
5. Preserve modal issue detail behavior for preflight findings.
6. Add web handler tests for preflight findings in rendered results.

### Review Gate

- `go test ./internal/web/...`
- `go test ./...`
- `prism review staged`

### Acceptance

A web upload containing TODO/vague/missing-section defects displays preflight findings before or alongside LLM findings.

## Phase 8 — Performance and False Positive Audit

### Goal

Keep the rule set fast and low-noise enough to run by default.

### Tasks

1. Add benchmark for a 10,000-line synthetic spec.
2. Add golden tests for representative good and bad specs.
3. Run preflight against existing `specs/SPEC.md` and `specs/web/SPEC.md`.
4. Tune rules that produce noisy findings.
5. Document known limitations.

### Review Gate

- `go test ./internal/preflight -bench .`
- `go test ./...`
- `prism review staged`

### Acceptance

Preflight runs under the target latency and produces actionable findings on existing project specs without excessive noise.

## Phase 9 — Documentation

### Goal

Document how users should apply preflight to reduce latency and iteration count.

### Tasks

1. Update `README.md` with:
   - purpose of preflight,
   - `--preflight-mode only`,
   - `--preflight-mode gate`,
   - recommended workflow.
2. Add examples:
   - quick local check without credentials,
   - CI preflight gate,
   - full LLM review after preflight passes.
3. Update web documentation if controls are exposed.
4. Add changelog or release notes if the project has one by then.

### Review Gate

- `go test ./...`
- `prism review staged`

### Acceptance

Users can discover and use preflight as a fast first pass before paying for a full LLM review.

## Recommended Initial Slice

For the first implementation PR, keep the scope small:

1. `internal/preflight` skeleton.
2. Placeholder rules.
3. Vague-language rules.
4. `--preflight-mode only`.
5. JSON and Markdown output through existing renderers.
6. README quick-start example.

This slice delivers immediate value: users can run a no-credential, sub-second check that catches obvious spec defects before invoking the model.
