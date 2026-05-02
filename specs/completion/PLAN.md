# SpecCritic Profile Completion Templates and Patch Suggestions Implementation Plan

## Non-Goals

- Do not implement LLM-assisted completion in this plan.
- Do not add user-defined templates loaded from disk.
- Do not add automatic patch application.
- Do not change default CLI or web behavior when completion flags are absent.
- Do not alter review scoring, verdicts, finding counts, convergence classification, or fail-on behavior.

## Terminology

- ATX means the Markdown heading form that starts a line with one or more `#` characters.
- CLI means command-line interface.
- OPEN DECISION means a placeholder requiring user judgment before implementation.
- REDACTED means the literal replacement marker inserted by SpecCritic redaction.
- ISSUE means a SpecCritic issue identifier prefix.
- UI means user interface.

## Acceptance Criteria

- Completion is disabled by default and existing tests continue to pass.
- All four built-in templates are exposed through a typed API.
- Completion patches are deterministic for the same spec, report, profile, and completion options.
- Unsafe candidates are skipped and counted rather than emitted.
- JSON output includes `meta.completion` only when completion runs.
- Markdown, patch export, and web rendering label completion suggestions as `draft/advisory`.
- The full test suite passes after each phase.

## Phase 1 — Completion Configuration and Schema Metadata

### Goal

Add completion configuration, CLI/env parsing, and report metadata without generating suggestions or changing existing default behavior.

### Tasks

1. Add completion fields to the CLI flag state:
   - `completionSuggestions`,
   - `completionMode`,
   - `completionTemplate`,
   - `completionMaxPatches`,
   - `completionOpenDecisions`.
2. Add CLI flags:
   - `--completion-suggestions`,
   - `--completion-mode`,
   - `--completion-template`,
   - `--completion-max-patches`,
   - `--completion-open-decisions`.
3. Add environment variable loading:
   - `SPECCRITIC_COMPLETION_SUGGESTIONS`,
   - `SPECCRITIC_COMPLETION_MODE`,
   - `SPECCRITIC_COMPLETION_TEMPLATE`,
   - `SPECCRITIC_COMPLETION_MAX_PATCHES`,
   - `SPECCRITIC_COMPLETION_OPEN_DECISIONS`.
4. Implement precedence:
   - explicit CLI values override environment values,
   - environment values override built-in defaults,
   - `completion-mode=off` disables completion,
   - `completion-mode=on` enables completion even when `completion-suggestions=false`,
   - `completion-mode=auto` runs only when completion suggestions are requested.
5. Validate:
   - mode is `auto`, `on`, or `off`,
   - template is `profile`, `general`, `backend-api`, `regulated-system`, or `event-driven`,
   - max patches is `>= 0`,
   - invalid CLI/env values exit with code `3`.
6. Add `schema.CompletionMeta`.
7. Add optional `Meta.Completion *CompletionMeta`.
8. Add schema validation for:
   - mode enum,
   - template enum,
   - non-negative counters,
   - valid omission when completion is not enabled.
9. Add unit tests for CLI defaults, env defaults, CLI-over-env precedence, invalid values, and schema validation.

### Review Gate

- `go test ./cmd/speccritic`
- `go test ./internal/schema/...`
- `go test ./...`
- `prism review staged`

### Acceptance

Completion flags and metadata are accepted by the application, but completion remains behaviorally inert until later phases. Default runs produce byte-for-byte equivalent JSON shape except for no new `meta.completion` field.

## Phase 2 — Template Package and Built-In Profiles

### Goal

Create typed built-in completion templates for all supported profiles.

### Tasks

1. Add `internal/completion`.
2. Define:
   - `Mode`,
   - `Config`,
   - `Template`,
   - `TemplateSection`,
   - `Placeholder`,
   - `Candidate`,
   - `Status`,
   - `Result`.
3. Add status values:
   - `patch_generated`,
   - `skipped_no_safe_location`,
   - `skipped_overlap`,
   - `skipped_redaction`,
   - `skipped_limit`,
   - `skipped_open_decisions_disabled`.
4. Implement `GetTemplate(name string, selectedProfile string) (*Template, error)`.
5. Add built-in template `general` with exact headings from the SPEC.
6. Add built-in template `backend-api` with exact headings from the SPEC.
7. Add built-in template `regulated-system` with exact headings from the SPEC.
8. Add built-in template `event-driven` with exact headings from the SPEC.
9. Map template sections to:
   - schema categories,
   - preflight rule IDs where known,
   - profile-specific order.
10. Add tests for:
   - all supported template lookups,
   - `profile` resolution,
   - unknown template rejection,
   - unsupported selected profile rejection,
   - exact canonical heading order,
   - no duplicate section headings within a template.

### Review Gate

- `go test ./internal/completion`
- `go test ./...`
- `prism review staged`

### Acceptance

All four built-in templates are available through one typed API and no completion text is assembled outside the completion package.

## Phase 3 — Markdown Section Analysis and Safe Patch Targeting

### Goal

Build deterministic target selection for section insertion and incomplete-section updates.

### Tasks

1. Reuse existing spec parsing where possible; otherwise add section-tree helpers in `internal/completion`.
2. Parse ATX Markdown headings and build a section tree with:
   - heading text,
   - level,
   - start line,
   - end line,
   - body line span,
   - parent path.
3. Normalize heading text for matching while preserving original text for patches.
4. Implement missing-section detection against canonical template headings.
5. Implement materially incomplete section detection:
   - no non-heading line containing `must`,
   - no non-heading line containing `shall`,
   - no non-heading line containing `OPEN DECISION`,
   - no numbered requirement ID,
   - no bullet containing a question mark.
6. Implement stable insertion target selection:
   - after nearest lower-order template section,
   - before nearest higher-order template section,
   - end of document when no template section exists,
   - skip duplicate target headings,
   - skip non-unique `before` text.
7. Implement insertion-after patch text:
   - `after = before + "\n\n" + insertedContent`.
8. Implement subsection append target selection.
9. Add tests for:
   - normal ordered insertion,
   - insertion before higher-order section,
   - insertion at document end,
   - duplicate heading skip,
   - non-unique `before` skip,
   - incomplete section detection,
   - complete section no-op,
   - Markdown heading-level preservation.

### Review Gate

- `go test ./internal/completion`
- `go test ./internal/spec`
- `go test ./...`
- `prism review staged`

### Acceptance

The completion package must identify stable, unique patch targets without generating unsafe or whole-document edits.

## Phase 4 — Candidate Generation and Traceability

### Goal

Generate deterministic completion candidates from current issues/questions and templates.

### Tasks

1. Define the completion input struct containing exactly the SPEC-defined input set.
2. Convert current issues and linked questions into source records.
3. Define profile gap detection:
   - missing-section preflight issue for selected profile,
   - supported issue categories,
   - linked questions through `blocks`.
4. Map source issues to template sections by:
   - preflight rule ID,
   - category,
   - title/description keyword match,
   - profile template fallback for missing-section issues.
5. Implement `source_issue_id` and `source_question_ids`.
6. Enforce that every candidate has exactly one source issue.
7. Add `completion-suggested` tag to source issues only for candidates that later emit a patch.
8. Generate section skeleton text with:
   - canonical heading,
   - subsection placeholders,
   - `OPEN DECISION:` placeholders when required.
9. Implement `--completion-open-decisions=false` candidate skipping.
10. Implement deterministic candidate sort:
   - target line ascending,
   - severity `CRITICAL`, `WARN`, `INFO`,
   - source issue ID ascending,
   - section order ascending,
   - generated text lexical order.
11. Add tests for:
   - missing-section mapping,
   - category mapping,
   - linked question traceability,
   - unlinked question ignored,
   - single source issue enforcement,
   - open-decision placeholder format,
   - open decisions disabled skip,
   - deterministic candidate ordering.

### Review Gate

- `go test ./internal/completion`
- `go test ./internal/preflight`
- `go test ./...`
- `prism review staged`

### Acceptance

Given a current report and spec, completion candidate generation is traceable, deterministic, and contains no unlinked suggestions.

## Phase 5 — Patch Safety, Limits, and Metadata

### Goal

Turn safe candidates into `schema.Patch` entries and compute `meta.completion`.

### Tasks

1. Validate `before` text with `strings.Count(originalSpec, before) == 1`.
2. Validate non-empty `after`.
3. Detect overlapping edit ranges after candidate sorting.
4. Keep the first sorted overlapping candidate and mark later candidates `skipped_overlap`.
5. Apply redaction safety:
   - skip when `before` contains `[REDACTED]`,
   - skip when `after` matches secret detector patterns.
6. Apply `--completion-max-patches` after exact-match, overlap, and redaction checks.
7. Mark overflow candidates `skipped_limit`.
8. Add emitted patches to the existing report `patches` array without removing model-provided patches.
9. Preserve normal advisory patches before completion patches, then order completion patches by the SPEC sort order.
10. Mutate source issue tags to include `completion-suggested` only for emitted completion patches.
11. Compute `schema.CompletionMeta`:
    - `enabled`,
    - `mode`,
    - `template`,
    - `generated_patches`,
    - `skipped_suggestions`,
    - `open_decisions`.
12. Ensure `generated_patches + skipped_suggestions` equals considered candidates.
13. In `completion-mode=on`, return input error and exit code `3` when a blocking missing-section candidate cannot emit a safe patch.
14. Add tests for:
    - exact unique before,
    - absent before,
    - duplicate before,
    - overlap handling,
    - redaction skip,
    - max patch limit,
    - zero max patch limit,
    - metadata counts,
    - issue tagging,
    - mode-on required failure.

### Review Gate

- `go test ./internal/completion`
- `go test ./internal/redact`
- `go test ./internal/app`
- `go test ./...`
- `prism review staged`

### Acceptance

Only safe completion patches are emitted, unsafe candidates are counted and reportable, and metadata is complete and deterministic.

## Phase 6 — App Pipeline Integration

### Goal

Run completion generation after normal review paths without changing existing review semantics.

### Tasks

1. Add completion config fields to `app.CheckRequest`.
2. Add completion execution after:
   - preflight-only report construction,
   - single LLM report validation,
   - chunk merge and synthesis merge,
   - incremental merge,
   - convergence annotation.
3. Ensure completion runs after final current issues/questions are known.
4. Ensure completion does not change:
   - score,
   - verdict,
   - issue counts,
   - question counts,
   - convergence classification,
   - fail-on decision.
5. Ensure completion runs in:
   - normal full review,
   - preflight-only,
   - preflight gate,
   - chunked review,
   - incremental review.
6. Ensure completion does not run when disabled.
7. Preserve redaction behavior by using redacted review inputs for generated text and original current spec only for safe patch location matching when allowed by existing patch behavior.
8. Add app tests for:
   - disabled path unchanged,
   - preflight-only completion,
   - full review completion with fake provider,
   - chunked completion,
   - incremental reused finding eligibility,
   - convergence unaffected,
   - mode-on failure exit path.

### Review Gate

- `go test ./internal/app`
- `go test ./internal/chunk`
- `go test ./internal/incremental`
- `go test ./internal/convergence`
- `go test ./...`
- `prism review staged`

### Acceptance

Completion suggestions are generated from every supported review mode while all review semantics remain unchanged.

## Phase 7 — Patch Export and Markdown Rendering

### Goal

Render completion suggestions clearly in Markdown and patch files.

### Tasks

1. Update patch diff generation to distinguish completion patches by source issue tag.
2. Emit `# completion patch for ISSUE-XXXX` comments for completion patches.
3. Preserve existing `# patch for ISSUE-XXXX` comments for normal advisory patches.
4. Preserve existing warning format when `before` cannot be located.
5. Update Markdown renderer:
   - add `Completion Suggestions` section when `meta.completion.enabled=true`,
   - omit the section when completion metadata is absent,
   - show `draft/advisory`,
   - show source issue ID,
   - show linked question IDs,
   - show generated versus skipped status,
   - show metadata counts.
6. Avoid trusted Markdown/HTML rendering for generated suggestion text.
7. Add tests for:
   - patch comment format,
   - normal patch comment unchanged,
   - missing-before warning unchanged,
   - Markdown section omission by default,
   - Markdown section rendering with completion,
   - escaped suggestion text.

### Review Gate

- `go test ./internal/patch`
- `go test ./internal/render`
- `go test ./...`
- `prism review staged`

### Acceptance

Users must be able to distinguish completion patches from normal advisory patches in Markdown and patch export, and existing patch behavior remains compatible.

## Phase 8 — CLI End-to-End Behavior

### Goal

Wire the CLI flags through `app.CheckRequest` and verify user-visible behavior.

### Tasks

1. Pass completion CLI/env config into `app.CheckRequest`.
2. Update CLI help text.
3. Add README CLI documentation for completion suggestions.
4. Add command tests for:
   - default completion disabled,
   - `--completion-suggestions`,
   - `--completion-mode on`,
   - `--completion-mode off`,
   - named template override,
   - env defaults,
   - invalid env rejected,
   - invalid CLI flag rejected,
   - `--patch-out` includes completion patches,
   - exit code `3` for required unsafe completion in mode `on`.
5. Add golden CLI fixture for backend API missing error responses.

### Review Gate

- `go test ./cmd/speccritic`
- `go test ./internal/app`
- `go test ./...`
- `prism review staged`

### Acceptance

CLI users must be able to enable, disable, configure, and export completion suggestions with documented, tested behavior.

## Phase 9 — Web UI Integration

### Goal

Expose completion controls and rendered completion output in the HTMX web UI.

### Tasks

1. Add form controls:
   - completion suggestions toggle,
   - completion mode selector,
   - completion template selector,
   - completion max patches numeric input.
2. Disable completion controls while a check is running.
3. Parse completion inputs in web handlers.
4. Pass completion config into `app.CheckRequest`.
5. Show completion metadata counts in the summary when present.
6. Show related completion suggestions in finding detail modals.
7. Include completion patches in patch export.
8. Render generated suggestion text through `html/template`.
9. Display the exact label `draft/advisory` next to each suggestion.
10. Add web tests for:
    - controls present,
    - controls disabled during running state where testable,
    - request parsing,
    - summary metadata,
    - finding modal related suggestions,
    - patch export includes completion patches,
    - HTML escaping.

### Review Gate

- `go test ./internal/web`
- `go test ./internal/app`
- `go test ./...`
- `prism review staged`

### Acceptance

The web UI must request and display completion suggestions without writing uploaded specs or generated patches to disk.

## Phase 10 — Final Validation and Documentation

### Goal

Validate the complete feature against the SPEC and document its operational behavior.

### Tasks

1. Update README:
   - completion purpose,
   - CLI flags,
   - env vars,
   - patch export behavior,
   - web controls,
   - advisory-only warning.
2. Add or update examples for:
   - backend API completion patch export,
   - preflight-only completion,
   - completion with incremental rerun.
3. Run full test suite.
4. Run SpecCritic against `specs/completion/SPEC.md` using local preflight mode.
5. Run SpecCritic against a representative bad backend API fixture with completion enabled.
6. Confirm disabled mode produces no `meta.completion`.
7. Confirm completion mode does not alter score/verdict/fail-on behavior.
8. Confirm no completion output contains unredacted secret fixture content.

### Review Gate

- `go test ./...`
- `go run ./cmd/speccritic check specs/completion/SPEC.md --format md --preflight-mode only`
- `prism review staged`

### Acceptance

The feature is documented, tested end to end, and satisfies the SPEC acceptance criteria without changing default CLI or web behavior.
