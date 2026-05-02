# SpecCritic Convergence Tracking Implementation Plan

## Phase 1 — Convergence Types and Previous Report Loading

### Goal

Add the core convergence data structures and previous-report validation without changing CLI, web, or review behavior.

### Tasks

1. Add `internal/convergence`.
2. Define:
   - `Mode`,
   - `Config`,
   - `Status`,
   - `FindingKind`,
   - `FindingStatus`,
   - `HistoricalStatus`,
   - `TrackedFinding`,
   - `Match`,
   - `Result`,
   - `Summary`,
   - `Compatibility`.
3. Add mode values:
   - `auto`,
   - `on`,
   - `off`.
4. Add convergence status values:
   - `complete`,
   - `partial`,
   - `unavailable`.
5. Add current finding status values:
   - `new`,
   - `still_open`,
   - `untracked`.
6. Add previous finding status values:
   - `resolved`,
   - `dropped`,
   - `untracked`.
7. Implement previous report loading from SpecCritic JSON using existing schema types.
8. Validate previous report compatibility:
   - `tool = "speccritic"`,
   - supported schema version,
   - `input.spec_hash` present,
   - `input.profile` present,
   - `input.strict` present,
   - `input.severity_threshold` present,
   - valid issues and questions,
   - valid evidence line bounds when evidence is present,
   - valid summary shape,
   - compatible redaction config hash when available.
9. Treat provider/model and spec file path differences as non-blocking.
10. Return structured compatibility results rather than only errors so `auto` mode can emit partial or unavailable metadata.
11. Add tests for valid previous reports, invalid JSON, wrong tool, unsupported schema, missing hash, invalid findings, profile/strict/threshold drift, and redaction hash mismatch.

### Review Gate

- `go test ./internal/convergence`
- `go test ./...`
- `prism review staged`

### Acceptance

The new package can load and validate a previous report and produce deterministic compatibility information without invoking an LLM or changing current review output.

## Phase 2 — Finding Normalization and Fingerprints

### Goal

Convert issues and questions into deterministic tracked findings that can be compared across review runs.

### Tasks

1. Implement conversion from `schema.Issue` to `TrackedFinding`.
2. Implement conversion from `schema.Question` to `TrackedFinding`.
3. Preserve:
   - kind,
   - source id,
   - severity,
   - category,
   - title or question text,
   - evidence line range,
   - tags,
   - source index for deterministic tie-breaking.
4. Implement normalized text handling:
   - trim whitespace,
   - collapse whitespace to one ASCII space,
   - lowercase category and severity,
   - normalize title/question text,
   - normalize evidence excerpts when available.
5. Strip volatile tags from fingerprint input:
   - `chunk:<ID>`,
   - `range:<ID>`,
   - `incremental-reused`,
   - provider repair tags.
6. Do not include issue IDs or question IDs in the primary fingerprint.
7. Add optional section-path support when callers can provide a line-to-section map.
8. Compute stable fingerprints for both issues and questions.
9. Add tests for issue fingerprints, question fingerprints, whitespace normalization, volatile tag stripping, severity/category normalization, and ID-independent matching.

### Review Gate

- `go test ./internal/convergence`
- `go test ./...`
- `prism review staged`

### Acceptance

Equivalent findings from different reports produce the same fingerprint even when provider IDs, volatile tags, or whitespace differ.

## Phase 3 — Deterministic Matching Engine

### Goal

Match previous and current tracked findings deterministically and safely classify ambiguous cases.

### Tasks

1. Implement matching order:
   - exact stable identity tag when present,
   - exact normalized fingerprint,
   - evidence remap match when mapping information is available,
   - normalized text similarity,
   - no match.
2. Add stable identity tag parsing only for explicitly supported tags. Do not infer stability from `ISSUE-000N` or `Q-000N`.
3. Implement normalized Levenshtein similarity.
4. Require title/question similarity of at least `0.92`.
5. Require evidence excerpt similarity of at least `0.88` when excerpts are available.
6. Allow severity escalation and downgrade to match when evidence and text are stable.
7. Allow category drift only through evidence remap or high text similarity.
8. Ensure one previous finding matches at most one current finding.
9. Resolve candidate conflicts by highest score only when the winner is at least `0.05` higher than the next candidate.
10. Mark ambiguous matches as `untracked` instead of guessing.
11. Add deterministic ordering for all maps and candidate lists.
12. Add tests for exact fingerprint matches, stable identity matches, severity drift, category drift, ambiguous candidates, one-to-one matching, and deterministic output ordering.

### Review Gate

- `go test ./internal/convergence`
- `go test ./...`
- `prism review staged`

### Acceptance

The matcher produces repeatable match results and never assigns the same previous finding to multiple current findings.

## Phase 4 — Status Classification and Summary Metadata

### Goal

Classify current and previous findings and compute convergence summaries without mutating the active report.

### Tasks

1. Implement classification for current findings:
   - matched current findings become `still_open`,
   - unmatched current findings become `new`,
   - unsafe or ambiguous current findings become `untracked`.
2. Implement classification for previous findings:
   - matched previous findings are represented through the current `still_open` finding,
   - unmatched previous findings become `resolved`, `dropped`, or `untracked`.
3. Add review-coverage inputs so full review, preflight-only review, and incremental review can classify resolution safely.
4. For full review, treat all current spec text as reviewed.
5. For preflight-only review, mark prior LLM findings `untracked` unless they match current preflight findings or deleted content.
6. For incremental review, use incremental metadata to classify reused findings as `still_open` and changed-range prior findings as `resolved` only when the relevant range was reviewed.
7. Treat threshold-filtered prior findings as `dropped`, not `resolved`.
8. Produce counts:
   - current `new`,
   - current `still_open`,
   - current `untracked`,
   - previous `resolved`,
   - previous `dropped`,
   - previous `untracked`.
9. Produce counts by severity and by finding kind.
10. Produce notes explaining partial or unavailable comparison state.
11. Add tests for full-review resolution, preflight-only untracked prior LLM findings, incremental reused findings, threshold drops, deleted-content drops, and summary counts.

### Review Gate

- `go test ./internal/convergence`
- `go test ./internal/incremental`
- `go test ./...`
- `prism review staged`

### Acceptance

Convergence classification is complete for supported review modes, and resolved historical findings never appear in active `issues` or `questions`.

## Phase 5 — Schema and Render Integration

### Goal

Expose convergence metadata in JSON and Markdown while preserving existing output when convergence is not requested.

### Tasks

1. Add `schema.ConvergenceMeta`.
2. Add optional `Meta.Convergence *ConvergenceMeta`.
3. Define JSON fields:
   - `enabled`,
   - `mode`,
   - `status`,
   - `previous_spec_hash`,
   - `current_spec_hash`,
   - `current`,
   - `previous`,
   - `by_severity`,
   - `by_kind`,
   - `notes`.
4. Add optional resolved historical findings under metadata only if implementation includes them in the first pass.
5. Add optional per-active-finding convergence data only if it can be added without breaking schema validation. If not, keep per-finding status in metadata maps keyed by current finding id.
6. Update schema validation to accept `meta.convergence` and reject invalid convergence enum values.
7. Update JSON rendering to include convergence metadata only when requested.
8. Update Markdown rendering to include a concise convergence summary before active findings.
9. Keep scoring, verdict, issue arrays, question arrays, and patches unchanged by convergence metadata.
10. Add tests for metadata omission by default, JSON metadata shape, invalid enum rejection, Markdown summary rendering, and unchanged scoring/verdict.

### Review Gate

- `go test ./internal/schema/...`
- `go test ./internal/render/...`
- `go test ./internal/convergence`
- `go test ./...`
- `prism review staged`

### Acceptance

Reports can include optional convergence metadata without changing the normal report contract or active finding semantics.

## Phase 6 — App and CLI Integration

### Goal

Expose convergence tracking through `speccritic check` while preserving default CLI behavior.

### Tasks

1. Add app request fields:
   - `ConvergenceFrom`,
   - `ConvergenceFromText`,
   - `ConvergenceMode`,
   - `ConvergenceStrict`,
   - `ConvergenceReport`.
2. Add CLI flags:
   - `--convergence-from`,
   - `--convergence-mode`,
   - `--convergence-strict`,
   - `--convergence-report`.
3. Add environment defaults:
   - `SPECCRITIC_CONVERGENCE_FROM`,
   - `SPECCRITIC_CONVERGENCE_MODE`,
   - `SPECCRITIC_CONVERGENCE_STRICT`,
   - `SPECCRITIC_CONVERGENCE_REPORT`.
4. Validate convergence flag values.
5. Default to no convergence behavior when convergence flags are absent.
6. In `off` mode, ignore convergence baseline input.
7. In `auto` mode, allow current review to succeed even when convergence comparison is unavailable.
8. In `on` mode, return input error exit code `3` for missing, invalid, or incompatible previous reports.
9. Run convergence comparison after the current report is fully built.
10. Ensure `--fail-on` uses only the current verdict and is not affected by resolved findings.
11. Add verbose logs for previous report loading, comparison status, and convergence counts.
12. Add CLI/app tests for:
    - no convergence flags,
    - valid convergence comparison,
    - invalid previous report in `auto`,
    - invalid previous report in `on`,
    - `off` mode,
    - `--fail-on` unchanged,
    - environment defaults.

### Review Gate

- `go test ./internal/app/...`
- `go test ./cmd/speccritic/...`
- `go test ./internal/convergence`
- `go test ./...`
- `prism review staged`

### Acceptance

Users can request convergence tracking from the CLI, and current review behavior is unchanged unless convergence is explicitly enabled.

## Phase 7 — Web Integration

### Goal

Expose convergence tracking in the HTMX web UI without storing previous reports server-side.

### Tasks

1. Reuse or extend the existing previous JSON upload control.
2. Add UI controls that distinguish:
   - incremental rerun,
   - convergence tracking,
   - both using the same previous report.
3. Add convergence mode selector:
   - `auto`,
   - `on`,
   - `off`.
4. Pass uploaded previous report bytes through the request lifecycle only.
5. Display convergence summary counts in the result summary:
   - `new`,
   - `still_open`,
   - `resolved`,
   - `dropped`,
   - `untracked`.
6. Show convergence status in issue/question detail modals for active findings when per-finding status is available.
7. Add a resolved findings list or tab for historical resolved findings when metadata includes them.
8. Keep active annotations tied only to current spec lines.
9. Keep provider/model picker and existing incremental controls working.
10. Add handler and template tests for uploads, mode parsing, summary rendering, resolved list rendering, and no-persistence behavior.

### Review Gate

- `go test ./internal/web/...`
- `go test ./internal/app/...`
- `go test ./internal/convergence`
- `go test ./...`
- `prism review staged`

### Acceptance

The web UI can show convergence progress for an uploaded previous JSON report while preserving the existing upload-only spec workflow.

## Phase 8 — End-to-End Verification and Documentation

### Goal

Verify convergence tracking against realistic iterative review workflows and document how to use it.

### Tasks

1. Add golden fixtures:
   - baseline report,
   - current report with one still-open finding,
   - current report with one new finding,
   - current report with one resolved finding,
   - current report with threshold-filtered dropped finding,
   - ambiguous matching case.
2. Add end-to-end tests proving:
   - full-review convergence produces expected counts,
   - preflight-only convergence does not claim prior LLM findings are resolved,
   - incremental-plus-convergence marks reused findings as still open,
   - invalid previous report is unavailable in `auto`,
   - invalid previous report fails in `on`,
   - Markdown output includes convergence summary.
3. Update README with CLI usage.
4. Update README with web usage.
5. Document recommended workflow:
   - save JSON baseline,
   - edit spec,
   - run with `--convergence-from`,
   - use convergence counts to track progress.
6. Document limitations:
   - convergence is local report comparison, not a semantic proof,
   - preflight-only runs cannot resolve prior LLM findings,
   - threshold changes can make comparison partial,
   - ambiguous matches become untracked.
7. Run full project verification.

### Review Gate

- `go test ./...`
- `prism review staged`

### Acceptance

The convergence tracking workflow is covered by tests and documented for both CLI and web users.
