# SpecCritic Incremental Rerun Specification

## 1. Purpose

SpecCritic incremental rerun is an execution mode that reviews only changed sections of a specification by reusing a previous SpecCritic result as baseline context.

The feature exists to reduce repeated review latency, provider cost, and token usage during iterative specification editing. It must preserve SpecCritic's core contract: same output schema, same scoring and verdict rules, same evidence validation, same redaction behavior, and same CLI/web result semantics.

## 2. Goals

- Reuse a previous SpecCritic JSON report when reviewing a revised spec.
- Detect changed, added, deleted, and moved Markdown sections.
- Re-run deterministic preflight against the full current spec.
- Re-run LLM review only for sections whose relevant content changed.
- Preserve still-valid prior findings for unchanged sections.
- Drop or mark prior findings whose evidence no longer maps to current spec lines.
- Produce one normal `schema.Report` for the current spec.
- Reduce provider calls for common edit-review-fix loops.
- Work with both single-call and chunked review architecture.
- Keep the CLI and web UI compatible with existing full-review behavior.

## 3. Non-Goals

- No new public output schema version in the first implementation.
- No semantic diffing with embeddings.
- No automatic acceptance of prior findings whose evidence cannot be remapped.
- No hidden mutation of the spec or prior report.
- No attempt to prove that unchanged text has unchanged meaning when surrounding changed text invalidates it.
- No persistent server-side database requirement.
- No replacement for full review; users must always be able to force a full rerun.

## 4. User Workflow

1. User runs a normal review and saves JSON output.
2. User edits the spec.
3. User runs incremental review with the revised spec and the previous JSON report.
4. SpecCritic loads and validates the previous report.
5. SpecCritic computes a structural diff between previous and current spec sections.
6. SpecCritic runs full deterministic preflight on the current spec.
7. SpecCritic reviews changed sections with the LLM when an LLM review is required.
8. SpecCritic remaps and reuses eligible prior LLM findings for unchanged sections.
9. SpecCritic merges preflight, reused prior findings, new findings, and new questions.
10. SpecCritic recomputes summary score, verdict, counts, metadata, and renders the normal output.

## 5. Functional Behavior

Incremental rerun must behave as a bounded optimization over the normal full-review pipeline. It must not change what a valid SpecCritic report means.

Required behavior:

- When no incremental flags are present, SpecCritic must execute the existing full-review path with no observable behavior change.
- When incremental mode is active, SpecCritic must read the current spec, redact it, run deterministic preflight across the full current spec, and build the final report from current spec line numbers.
- The previous report may be used only as evidence-bearing context for reuse, deduplication, and identity preservation. It must never replace validation against the current spec.
- Changed, added, renamed, ambiguous, and unsafe moved sections must be reviewed from current spec text.
- Unchanged and safely moved sections may reuse prior findings only when evidence remapping, redaction checks, severity filtering, and profile compatibility checks pass.
- Findings tied to deleted text must be removed from the final report.
- Findings that cannot be safely remapped must be dropped or cause fallback according to `--incremental-mode`; they must not be silently retained.
- Final score, verdict, counts, patches, and exports must be computed from the merged current report, not copied from the prior report.
- In `auto` mode, SpecCritic must prefer full review over incremental reuse whenever reuse safety cannot be proven.
- In `on` mode, SpecCritic must fail with an input error instead of silently falling back.
- In `off` mode, SpecCritic must ignore `--incremental-from` and run a full review.

## 6. CLI Interface

Existing full-review behavior remains the default.

New flags:

| Flag | Default | Description |
|------|---------|-------------|
| `--incremental-from` | empty | Path to previous SpecCritic JSON report. Enables incremental rerun. |
| `--incremental-mode` | `auto` | Incremental mode: `auto`, `on`, or `off`. |
| `--incremental-max-change-ratio` | `0.35` | Maximum changed-section line ratio allowed before falling back to full review. |
| `--incremental-max-remap-failure-ratio` | `0.25` | Maximum prior-finding remap failure ratio allowed before falling back to full review. |
| `--incremental-context-lines` | `20` | Neighboring unchanged lines included around each changed section. |
| `--incremental-strict-reuse` | `true` | Reuse prior findings only when evidence remaps exactly or by unchanged line hash. |
| `--incremental-report` | `false` | Include optional incremental execution details in `meta.incremental`. |

Mode behavior:

- `--incremental-mode off` ignores `--incremental-from` and runs a full review.
- `--incremental-mode on` requires `--incremental-from`; if incremental planning fails, exit with code `3` instead of falling back.
- `--incremental-mode auto` uses incremental review when safe and falls back to full review when safety gates fail.

Validation:

- `--incremental-from` must point to valid JSON output produced by SpecCritic.
- Previous report `input.spec_hash` must identify the spec content that produced the prior report. It may equal the current spec hash; in that case incremental mode may reuse eligible findings without LLM calls unless other parameters require review.
- `--incremental-max-change-ratio` must be `> 0` and `<= 1`.
- `--incremental-max-remap-failure-ratio` must be `>= 0` and `<= 1`.
- `--incremental-context-lines` must be `>= 0`.
- Incremental review is incompatible with `--format md` as the previous input; previous input must be JSON.
- Invalid incremental flags are input errors and exit with code `3`.

## 7. Web Interface

The first web version may omit incremental controls.

When exposed, the web UI must provide:

- previous result upload input accepting SpecCritic JSON,
- mode selector: `auto`, `on`, `off`,
- visible fallback notice when `auto` runs a full review,
- result metadata showing reused, re-reviewed, dropped, and fallback counts.

The web UI must not store previous results server-side unless explicitly added by a later spec.

## 8. Previous Result Contract

The previous result must be a SpecCritic JSON report containing:

- `tool = "speccritic"`,
- compatible schema version,
- `input.spec_file`,
- `input.spec_hash`,
- `input.profile`,
- `input.strict`,
- `input.severity_threshold`,
- `issues`,
- `questions`,
- `patches`,
- `meta.model`,
- `meta.temperature`.

When available, the previous report should also contain `meta.redaction_config_hash`. If both reports expose this value and it differs, incremental reuse must fall back to full review in `auto` mode or fail in `on` mode.

`input.spec_file` is informational for reuse. A path mismatch must not reject incremental mode when the previous report hash, profile, strict mode, severity threshold, and schema version are compatible.

Incremental reuse must be rejected or fall back to full review when:

- schema version is unsupported,
- profile differs,
- strict mode differs,
- severity threshold is less strict than the previous run and would require findings that may have been omitted from the previous report,
- previous report lacks evidence line ranges,
- previous report contains invalid issue/question/category/severity/id values,
- current review uses a different profile rule pack that changes prompt semantics.

Provider/model differences do not automatically invalidate reuse, but the final metadata must identify the current provider/model used for newly reviewed sections.

If the current severity threshold is more strict than the previous run, incremental reuse is allowed, but reused findings below the current threshold must be filtered before scoring. If the current severity threshold is less strict than the previous run, unchanged sections may reuse previous findings only as context and must be re-reviewed for newly visible lower-severity findings.

## 9. Section Identity

Incremental rerun operates over Markdown sections.

A section identity includes:

- heading text normalized by trimming whitespace, stripping Markdown heading attributes such as `{#custom-id}`, and collapsing internal spaces,
- heading level,
- heading path from root to section,
- source line range,
- content hash excluding line numbers,
- local anchor hash derived from heading path and first non-empty content lines.

Matching order:

1. Match by exact heading path and content hash.
2. Match by exact content hash and normalized heading text when a parent heading changed.
3. Match by local anchor hash plus line-neighborhood similarity.
4. Classify as `ambiguous` when multiple candidates remain above the match threshold.
5. Classify as `added` when no previous section matches the current section.

The line-neighborhood similarity threshold is `0.90` using normalized Levenshtein similarity over a sample of up to 30 non-empty section lines drawn from the beginning, middle, and end of the section. A renamed section is a section whose content similarity is at least `0.90`, content hash differs, heading text differs, and no stronger match is available.

Section classification:

| Classification | Meaning |
|----------------|---------|
| `unchanged` | Heading path and content hash match previous spec. |
| `changed` | Heading path matches but content hash changed. |
| `added` | Current section has no previous match. |
| `deleted` | Previous section has no current match. |
| `moved` | Content hash matches but heading path or line range changed. |
| `renamed` | Content similarity is at least `0.90`, content hash differs, and heading text changed. |
| `ambiguous` | Multiple possible matches or unstable low-confidence match. |

Moved unchanged sections may reuse findings only after evidence line remapping succeeds.

Renamed or ambiguous sections must be reviewed unless strict reuse can prove unchanged content and remap evidence safely.

## 10. Change Planning

The incremental planner produces:

```go
type IncrementalPlan struct {
    PreviousHash string
    CurrentHash  string
    Mode         string
    Sections     []SectionChange
    ReviewRanges []ReviewRange
    ReuseRanges  []ReuseRange
    Fallback     *FallbackReason
}
```

Rules:

- Added and changed sections are review ranges.
- Deleted sections are not reviewed, and prior findings attached only to deleted ranges are dropped.
- Unchanged sections are reuse ranges.
- Moved sections are reuse ranges only if evidence can be remapped; otherwise review ranges.
- Adjacent changed sections should be coalesced when their context windows overlap, unless a conservative token estimate for the final prompt would exceed 80% of the configured chunk token threshold. The estimate must include system prompt, profile rules, context lines, prior-finding context, and section text.
- Review ranges must include `--incremental-context-lines` on both sides, bounded by spec limits.
- Evidence emitted by new LLM calls may cite any current spec line included in the prompt. Findings that cite context-only lines must be deduplicated against existing and remapped prior findings before scoring.

Safety fallback:

- If changed primary lines divided by total current lines exceeds `--incremental-max-change-ratio`, `auto` falls back to full review.
- If the prior-finding remap failure ratio exceeds `--incremental-max-remap-failure-ratio`, `auto` falls back to full review. If the previous report contains zero findings, the remap failure ratio is defined as `0.0`.
- If section matching is ambiguous for any section containing prior findings, `auto` falls back to full review. If an ambiguous current section has no associated prior findings, it is treated as an added changed range and cannot reuse prior findings.
- If the previous report profile or strict mode differs, `auto` falls back to full review.
- In `on` mode, any fallback condition is an input error.

## 11. Finding Reuse

Prior findings may be reused only when all of these are true:

- finding is not tagged `preflight`,
- finding evidence maps to current spec lines,
- mapped evidence quote still matches current spec text after redaction,
- containing section is unchanged or safely moved,
- finding category and severity are still valid,
- finding is not tied to deleted text,
- current preflight did not emit a duplicate deterministic finding for the same defect,
- current severity threshold does not filter out the finding.

Reused findings must keep their original IDs.

When collisions occur:

- prior reused issue IDs keep their original IDs,
- new LLM issue IDs use `max(existing ISSUE-000N values) + 1` after scanning all reused and newly generated issue IDs; IDs of fixed or dropped findings must be retired and not reassigned,
- question IDs use `max(existing Q-000N values) + 1` after scanning all reused and newly generated question IDs; IDs of fixed or dropped questions must be retired and not reassigned,
- non-standard issue or question IDs make the previous report invalid for incremental reuse because stable SpecCritic IDs are required to preserve finding identity.

Reused findings must include tag `incremental-reused`.

New LLM findings must be deduplicated before scoring against reused findings and remapped prior findings from the same current review range. A new finding is a duplicate when it has the same category and evidence range that overlaps after remapping both findings to current spec lines and verifying that their normalized evidence quotes refer to matching current text, or when its normalized title and evidence quote similarity are both at least `0.95` against a prior finding with the same severity and remapped section identity. Duplicates keep the prior finding ID; the merge may use the new finding's clearer description, evidence, or patch when it is valid against current spec text.

Findings dropped due to remap failure, severity filtering, or deduplication must not appear in the final `issues` or `questions` arrays. When `--incremental-report=true`, dropped counts must appear under `meta.incremental.dropped_findings`.

## 12. LLM Prompt Contract

Incremental LLM calls must receive:

- global system prompt for the selected profile,
- current spec table of contents,
- changed section text with current spec line numbers,
- bounded unchanged context around the changed section,
- relevant prior findings from neighboring unchanged sections,
- a `Previously Identified Issues` block containing selected prior findings as context only,
- a compact document-level summary of omitted findings by severity, including IDs and titles for omitted `CRITICAL` findings,
- a `Current Review Task` block containing only the changed primary ranges that may produce new findings,
- explicit instruction that findings must cite current spec line numbers,
- explicit instruction not to repeat reused prior findings unless the changed text creates a new defect.

Relevant prior findings are findings whose remapped evidence is in the same section, parent section, child section, or within `--incremental-context-lines` of a changed primary range. Prior findings are selected by remaining token budget rather than a fixed count. When more findings match than fit, prioritize findings by distance to changed primary lines, with severity as the tie-breaker. The prompt must include a count of omitted relevant findings by severity.

The prompt must require JSON-only output matching the existing schema.

For section-level incremental calls, every new issue must include tags:

- `incremental-review`,
- `range:<RANGE-ID>`.

If chunking is also active, chunk tags remain required:

- `chunk:<CHUNK-ID>`.

The final merged report may include both `incremental-review` and `chunked-review` tags.

## 13. Preflight Interaction

Preflight always runs against the full current spec.

Rules:

- Preflight findings are never reused from the prior report.
- Current preflight findings participate in scoring and verdict as usual.
- If `--preflight-mode only`, incremental LLM review is skipped even when `--incremental-from` is set.
- If `--preflight-mode gate` finds blocking defects, LLM review is skipped and prior LLM findings are not reused unless explicitly allowed by a later spec.
- Duplicate handling between current preflight and reused LLM findings must follow existing preflight deduplication rules.

## 14. Chunking Interaction

Incremental review may use chunking inside changed review ranges.

Rules:

- If a changed range exceeds `--chunk-lines`, split that range using existing chunk planning.
- Chunk evidence restrictions still apply.
- Synthesis should run only when changed ranges plus reused high-risk neighboring findings exceed the synthesis threshold.
- Synthesis prompt must include summaries of changed ranges, reused prior findings, and a compact table of unchanged top-level sections. It must not include the full unchanged spec.
- If chunking is `off`, each changed review range is reviewed as one incremental call.

## 15. Output Semantics

The final result is a normal SpecCritic report for the current spec.

Required behavior:

- `input.spec_hash` is the current spec hash.
- `input.spec_file` is the current spec file.
- score and verdict are recomputed from final merged findings.
- counts include current preflight findings, reused findings, and new findings.
- patch generation only uses current spec text.
- advisory patches from prior findings are reused only for unchanged sections whose line remapping is exact and whose `before` text matches current spec text. Patches for moved, changed, renamed, or ambiguous sections must be regenerated from current spec text or omitted.

`meta.incremental` is optional in the current schema version. It must be omitted unless `--incremental-report=true` and the output format is JSON. Markdown output may render the same information as human-readable text, but it must not define a separate metadata contract.

When `meta.incremental` is present, these fields are mandatory:

```json
"incremental": {
  "enabled": true,
  "previous_spec_hash": "...",
  "mode": "auto",
  "fallback": false,
  "reviewed_sections": 3,
  "reused_sections": 12,
  "reused_issues": 8,
  "reused_questions": 2,
  "dropped_findings": 1,
  "changed_line_ratio": 0.12
}
```

Field semantics:

- `enabled` is `true` when an incremental run was requested and accepted for execution.
- `previous_spec_hash` is the `input.spec_hash` value from the previous report.
- `mode` is the effective incremental mode: `auto`, `on`, or `off`.
- `fallback` is `true` when `auto` performed a full review after planning.
- `reviewed_sections` is the count of current sections reviewed by LLM calls.
- `reused_sections` is the count of current sections whose findings were eligible for reuse without LLM review.
- `reused_issues` and `reused_questions` count findings carried into the final report after remapping and filtering.
- `dropped_findings` counts prior issues and questions omitted due to deletion, failed remap, severity filtering, or deduplication.
- `changed_line_ratio` is changed primary lines divided by total current spec lines. For an empty current spec, the value is `0.0`.

When `--incremental-report=false`, incremental execution details may be emitted only in verbose logs and must not appear as `meta.incremental`.

## 16. Error Handling

Input errors:

- missing `--incremental-from` in `on` mode,
- unreadable previous report,
- invalid previous report JSON,
- unsupported previous report schema,
- invalid incremental flags,
- unsafe fallback condition in `on` mode.

Provider/model errors:

- changed-range LLM call failure uses existing provider error behavior.
- invalid changed-range model output uses existing retry and invalid model output behavior.

Fallback behavior:

- `auto` may fall back to full review for safety.
- Fallback must be logged in verbose mode.
- `on` must never silently fall back.

## 17. Security and Privacy

- Current and previous specs must be redacted before prompt construction.
- Reused evidence must be validated against the current run's redacted spec text, not against raw previous report text alone.
- Redaction placeholders used in evidence should be deterministic and content-based when the redaction engine can safely do so without exposing sensitive values.
- Evidence comparison for reuse must preserve specific redaction placeholders when redaction configuration hashes match, so findings remain tied to the same redacted entity. A finding whose evidence contains any redaction placeholder is reusable only when the placeholder identity can be verified with deterministic placeholder IDs or an unchanged redaction configuration hash plus exact redacted evidence text. Generic `[REDACTED]` evidence without verifiable placeholder identity is not reusable.
- Previous report content must be treated as untrusted input.
- Previous findings must not bypass evidence validation.
- Previous report patches must not be applied automatically.
- Incremental prompts must not include deleted text unless it is required to explain a changed-range comparison and has been redacted.
- The web UI must not persist uploaded previous reports beyond the request lifecycle.

## 18. Testing Strategy

Unit tests:

- section identity and heading-path matching,
- changed/added/deleted/moved section classification,
- line remapping for unchanged and moved sections,
- reuse eligibility checks,
- collision-safe issue/question renumbering,
- fallback threshold behavior,
- strict mode/profile mismatch behavior.

Golden tests:

- unchanged spec plus previous invalid report reuses findings without LLM call,
- one edited section reviews only that section,
- deleted section drops attached findings,
- moved unchanged section preserves findings with remapped evidence,
- ambiguous section match falls back to full review in `auto`.

Integration tests:

- mock LLM verifies only changed ranges are sent,
- preflight always runs on full current spec,
- chunking splits large changed ranges,
- `--incremental-mode on` fails instead of falling back,
- web upload of previous report produces same report as CLI for the same inputs.

## 19. Acceptance Criteria

- Full review remains the default with no behavior change when incremental flags are absent.
- Incremental review produces schema-valid JSON using the existing renderer.
- Current preflight always runs against the full current spec.
- Unchanged-section findings are reused only after evidence remapping succeeds.
- Changed-section findings are generated from current spec text and current line numbers.
- `auto` falls back to full review when reuse safety cannot be proven.
- `on` fails loudly when reuse safety cannot be proven.
- The CLI and web UI can both run incremental reviews.
- Tests cover reuse, rerun, fallback, and merge behavior.
