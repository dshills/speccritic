# SpecCritic Incremental Rerun Implementation Plan

## Phase 1 — Incremental Types and Previous Report Loading

### Goal

Add the core incremental data structures and validate previous SpecCritic JSON reports without changing CLI or web behavior.

### Tasks

1. Add `internal/incremental`.
2. Define:
   - `Config`,
   - `Mode`,
   - `Plan`,
   - `SectionChange`,
   - `ReviewRange`,
   - `ReuseRange`,
   - `FallbackReason`,
   - `Metadata`.
3. Add mode values:
   - `auto`,
   - `on`,
   - `off`.
4. Implement previous report loading from JSON using the existing schema types.
5. Validate previous report compatibility:
   - supported schema version,
   - matching profile,
   - matching strict mode,
   - compatible severity threshold,
   - valid issue, question, category, severity, evidence, and ID values,
   - compatible profile/rule-pack hash when available,
   - compatible redaction config hash when available.
6. Treat `input.spec_file` as informational, not as a reuse blocker.
7. Reject non-standard issue/question IDs for incremental reuse.
8. Add tests for valid previous reports, invalid JSON, incompatible schema, profile mismatch, strict mismatch, threshold transitions, invalid IDs, and missing evidence.

### Review Gate

- `go test ./internal/incremental`
- `go test ./...`
- `prism review staged`

### Acceptance

The new package can load and validate a previous SpecCritic report and return deterministic compatibility results without invoking the LLM.

## Phase 2 — Markdown Section Identity and Diff Planning

### Goal

Build deterministic section matching and change planning over current and previous spec text.

### Tasks

1. Reuse or extend existing Markdown heading parsing from the spec/chunking code.
2. Define section identity from:
   - normalized heading text,
   - heading level,
   - heading path,
   - sibling occurrence index,
   - source line range,
   - content hash,
   - local anchor hash.
3. Normalize headings by trimming whitespace, stripping Markdown attributes such as `{#id}`, and collapsing internal spaces.
4. Implement section matching order:
   - exact heading path plus content hash,
   - exact content hash plus normalized heading text,
   - local anchor hash plus line-neighborhood similarity,
   - ambiguous when multiple candidates remain,
   - added when no previous section matches.
5. Implement classifications:
   - `unchanged`,
   - `changed`,
   - `added`,
   - `deleted`,
   - `moved`,
   - `renamed`,
   - `ambiguous`.
6. Use normalized Levenshtein similarity threshold `0.90` over samples from the beginning, middle, and end of large sections; for sections with 30 or fewer non-empty lines, use the full section content as the sample.
7. Build review ranges for changed, added, renamed, ambiguous, and unsafe moved sections.
8. Build reuse ranges for unchanged and safely moved sections.
9. Add context windows controlled by `--incremental-context-lines`.
10. Estimate prompt tokens during planning using a provider tokenizer when available, otherwise `ceil(characters / 3)`, plus a 20% safety buffer for prompt framing and profile rules.
11. Coalesce adjacent review ranges only when the estimated final prompt remains below 80% of the configured chunk token threshold.
12. Add tests for duplicate sibling headings, renamed headings, moved sections, added/deleted sections, ambiguous matches, context bounds, token estimates, and coalescing limits.

### Review Gate

- `go test ./internal/incremental`
- `go test ./internal/chunk/...`
- `go test ./...`
- `prism review staged`

### Acceptance

The planner can produce stable review and reuse ranges for edited Markdown specs, with no invalid line bounds or ambiguous silent reuse.

## Phase 3 — Evidence Remapping and Finding Reuse

### Goal

Safely reuse prior findings only when their evidence still maps to the current spec.

### Tasks

1. Implement evidence remapping for unchanged sections.
2. Implement evidence remapping for safely moved sections using section-level line offsets.
3. Validate remapped evidence against current spec text.
4. Validate remapped evidence against current redacted spec text.
5. Fail closed for generic or unverifiable redaction placeholders.
6. Drop findings tied to deleted text.
7. Filter reused findings below the current severity threshold.
8. Preserve original IDs for reused findings.
9. Add `incremental-reused` tags to reused findings.
10. Compute remap failure ratio excluding findings tied to deleted sections.
11. Implement fallback decisions for:
    - excessive changed line ratio,
    - excessive remap failure ratio,
    - ambiguous section containing prior findings,
    - incompatible previous report metadata.
12. Add tests for exact reuse, moved reuse, deleted finding drops, redacted evidence behavior, severity filtering, fallback thresholds, and zero-prior-finding ratio.

### Review Gate

- `go test ./internal/incremental`
- `go test ./...`
- `prism review staged`

### Acceptance

No prior finding can appear in the final report unless it was validated against current line numbers and current redacted text.

## Phase 4 — Incremental Prompt Construction and LLM Execution

### Goal

Review only changed ranges while giving the model enough context to avoid duplicate findings.

### Tasks

1. Add an incremental prompt builder in `internal/incremental` or a narrow app-level package.
2. Reuse the existing profile/system prompt construction.
3. Include:
   - current spec table of contents,
   - changed primary range text with current line numbers,
   - bounded context lines,
   - selected prior findings,
   - compact document-level finding summary,
   - `Previously Identified Issues` block,
   - `Current Review Task` block.
4. Select prior finding context by token budget, prioritizing proximity to changed lines and severity as tie-breaker.
5. Instruct the model to cite current spec lines.
6. Instruct the model to emit JSON only using the existing schema.
7. Require tags:
   - `incremental-review`,
   - `range:<RANGE-ID>`.
8. Preserve chunk tags when chunking is also active.
9. Allow findings to cite context lines, but mark them for merge-time deduplication.
10. Use existing retry and repair behavior for invalid model output.
11. Add tests for prompt contents, current line numbering, prior finding selection, omitted finding summaries, required tags, and repair prompts.

### Review Gate

- `go test ./internal/incremental`
- `go test ./internal/llm/...`
- `go test ./...`
- `prism review staged`

### Acceptance

Incremental LLM calls receive only bounded current spec ranges plus relevant prior context, and invalid output follows the existing repair path.

## Phase 5 — Merge, Deduplication, Scoring, and Metadata

### Goal

Merge current preflight findings, reused findings, and new LLM findings into one canonical current report.

### Tasks

1. Add incremental merge functions or extend existing merge code without changing full-review behavior.
2. Merge sources in deterministic order:
   - current preflight findings,
   - reused prior findings,
   - new incremental findings,
   - synthesis findings when chunking uses synthesis.
3. Deduplicate new findings against reused and remapped prior findings.
4. Preserve stable IDs for reused findings and duplicates of prior findings.
5. Allocate new issue IDs only in this final sequential merge step, using `max(existing ISSUE-000N) + 1` after all parallel LLM calls have completed.
6. Allocate new question IDs only in this final sequential merge step, using `max(existing Q-000N) + 1` after all parallel LLM calls have completed.
7. Retire IDs from fixed or dropped findings.
8. Keep improved current descriptions, evidence, or patches for duplicates only when they validate against current spec text.
9. Recompute score, verdict, and counts from the merged current findings.
10. Generate patches only from current spec text.
11. Reuse advisory patches only for unchanged sections with exact line remapping and matching `before` text.
12. Add `schema.Meta.Incremental` support for optional JSON metadata.
13. Emit `meta.incremental` only when `--incremental-report=true` and JSON output is selected.
14. Include mandatory metadata fields:
    - `enabled`,
    - `previous_spec_hash`,
    - `mode`,
    - `fallback`,
    - `reviewed_sections`,
    - `reused_sections`,
    - `reused_issues`,
    - `reused_questions`,
    - `dropped_findings`,
    - `changed_line_ratio`.
15. Add tests for merge order, duplicate preservation, ID allocation, patch validation, metadata omission by default, and metadata shape when enabled.

### Review Gate

- `go test ./internal/incremental`
- `go test ./internal/schema/...`
- `go test ./internal/app/...`
- `go test ./...`
- `prism review staged`

### Acceptance

The final report is a normal schema-valid SpecCritic report for the current spec, with optional incremental metadata only when requested.

## Phase 6 — CLI Integration

### Goal

Expose incremental rerun through `speccritic check` while preserving existing CLI behavior.

### Tasks

1. Add CLI flags:
   - `--incremental-from`,
   - `--incremental-mode`,
   - `--incremental-max-change-ratio`,
   - `--incremental-max-remap-failure-ratio`,
   - `--incremental-context-lines`,
   - `--incremental-strict-reuse`,
   - `--incremental-report`.
2. Validate flag ranges and mode values.
3. Default to full review when incremental flags are absent.
4. Ignore `--incremental-from` when `--incremental-mode=off`.
5. In `auto`, fall back to full review when planning or reuse is unsafe.
6. In `on`, return input error exit code `3` for unsafe fallback conditions.
7. Run preflight against the full current spec before any incremental LLM call.
8. Honor `--preflight-mode only` by skipping incremental LLM review.
9. Honor `--preflight-mode gate` by skipping incremental LLM review when blocking defects exist.
10. Support chunking inside changed review ranges.
11. Add verbose logs for plan summary, fallback reason, reused counts, reviewed ranges, dropped findings, and elapsed time.
12. Add CLI integration tests with mock providers for:
    - no incremental flags,
    - valid incremental reuse,
    - one changed section,
    - auto fallback,
    - on-mode failure,
    - preflight only,
    - gate mode,
    - optional metadata.

### Review Gate

- `go test ./internal/app/...`
- `go test ./cmd/speccritic/...`
- `go test ./...`
- `prism review staged`

### Acceptance

Users can run incremental reviews from the CLI without changing the default full-review contract.

## Phase 7 — Web Integration

### Goal

Expose incremental rerun in the HTMX web UI without storing previous reports server-side.

### Tasks

1. Add optional previous result upload input accepting JSON.
2. Add incremental mode selector:
   - `auto`,
   - `on`,
   - `off`.
3. Add advanced fields for:
   - max change ratio,
   - max remap failure ratio,
   - context lines,
   - incremental metadata output.
4. Clear previous result state when a new spec file is selected.
5. Pass previous report bytes through the request lifecycle only.
6. Display fallback notice when `auto` performs a full review.
7. Display reviewed, reused, dropped, and fallback counts when metadata is available.
8. Keep the existing provider/model picker behavior unchanged.
9. Keep the Check Spec button disabled until a spec file is selected.
10. Add handler tests for uploads, mode parsing, no persistence, fallback notices, and result rendering.

### Review Gate

- `go test ./internal/web/...`
- `go test ./internal/app/...`
- `go test ./...`
- `prism review staged`

### Acceptance

The web UI can run incremental reviews from an uploaded previous JSON result while preserving the existing upload-only spec workflow.

## Phase 8 — End-to-End Verification and Documentation

### Goal

Verify the feature against realistic iterative spec workflows and document how to use it.

### Tasks

1. Add golden fixtures:
   - baseline report,
   - unchanged spec,
   - one changed section,
   - deleted section,
   - moved section,
   - ambiguous restructure.
2. Add end-to-end tests proving:
   - unchanged spec avoids LLM calls,
   - one edited section reviews only that section,
   - deleted section drops attached findings,
   - moved unchanged section preserves reusable findings,
   - ambiguous section with prior findings falls back in `auto`,
   - `on` mode fails instead of falling back.
3. Add README documentation for CLI usage.
4. Add README documentation for web usage when exposed.
5. Document recommended workflow:
   - save JSON baseline,
   - edit spec,
   - rerun with `--incremental-from`,
   - use `auto` for normal work,
   - use `on` for CI enforcement.
6. Document limitations:
   - full review remains the safety fallback,
   - redacted evidence may force re-review,
   - lowered severity threshold may require broader review,
   - moved or changed patches may be regenerated or omitted.
7. Run full project verification.

### Review Gate

- `go test ./...`
- `prism review staged`

### Acceptance

The incremental rerun workflow is covered by tests and documented for both CLI and web users.
