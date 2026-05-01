# SpecCritic Section Chunking and Parallel LLM Review Implementation Plan

## Phase 1 — Chunk Planning Package

### Goal

Create deterministic section chunk planning without touching CLI, web, or LLM execution.

### Tasks

1. Add `internal/chunk`.
2. Define:
   - `Config`,
   - `Chunk`,
   - `Plan`,
   - `Heading`,
   - `Section`.
3. Implement Markdown heading extraction from redacted spec lines.
4. Implement section span construction.
5. Implement chunk planning by top-level section.
6. Implement nested-heading split for oversized sections.
7. Implement paragraph/list fallback split for oversized leaf sections.
8. Add overlap context bounds.
9. Generate stable chunk IDs from source line ranges.
10. Add validation for line ranges, overlap, and chunk IDs.

### Review Gate

- `go test ./internal/chunk`
- `go test ./...`
- `prism review staged`

### Acceptance

The package can turn a Markdown spec into deterministic chunks with primary line ranges, context ranges, heading paths, and no invalid bounds.

## Phase 2 — Chunk Prompt Construction

### Goal

Build chunk-specific prompts while preserving the existing system prompt and schema contract.

### Tasks

1. Add chunk prompt builder in `internal/llm` or a new `internal/chunkreview` package.
2. Reuse existing system prompt construction.
3. Generate a table of contents from the chunk plan.
4. Detect glossary/definitions sections.
5. Include bounded glossary text when within budget.
6. Mark overlap lines as context-only.
7. Mark primary lines as reviewable.
8. Add prompt instructions:
   - cite only primary range,
   - tag every issue with `chunk:<CHUNK-ID>`,
   - add `cross-section` for findings dependent on external sections,
   - emit `meta.chunk_summary` with at most 600 characters,
   - return existing JSON schema only.
9. Include bounded known-preflight context using the existing preflight orchestration.
10. Add prompt tests that assert:
    - primary lines appear,
    - context-only lines are labeled,
    - table of contents appears,
    - chunk summary instruction appears,
    - no unredacted text is introduced.

### Review Gate

- `go test ./internal/llm`
- `go test ./internal/chunk/...`
- `go test ./...`
- `prism review staged`

### Acceptance

Each chunk prompt contains enough global context to review one primary range and clearly prevents citing overlap-only lines.

## Phase 3 — Chunk Response Validation

### Goal

Validate chunk model output against both the existing report schema and chunk primary-range constraints.

### Tasks

1. Reuse existing JSON parse/schema validation.
2. Add chunk evidence validation:
   - line start/end must be inside original spec,
   - normal chunk findings must cite only primary range,
   - synthesis findings may cite any original line.
3. Validate `meta.chunk_summary` for chunk responses.
4. Validate `chunk:<CHUNK-ID>` tags for chunk issues.
5. Reject or repair missing chunk tags.
6. Preserve existing one-retry repair behavior for invalid JSON.
7. Add unit tests for:
   - valid primary evidence,
   - overlap-only evidence rejection,
   - out-of-range evidence rejection,
   - missing chunk tag repair path,
   - missing chunk summary repair path,
   - valid synthesis evidence outside one chunk.

### Review Gate

- `go test ./internal/chunk/...`
- `go test ./internal/schema/...`
- `go test ./...`
- `prism review staged`

### Acceptance

Chunk output cannot smuggle invalid evidence into the final report.

## Phase 4 — Bounded Parallel Executor

### Goal

Execute chunk LLM calls concurrently without unbounded goroutines or nondeterministic merge behavior.

### Tasks

1. Add a chunk review executor.
2. Accept:
   - provider,
   - chunk plan,
   - prompt builder,
   - concurrency limit,
   - parent context.
3. Use a worker pool or semaphore with bounded concurrency.
4. Preserve result slots by chunk index.
5. Cancel outstanding work when parent context is cancelled.
6. Retry invalid model output once with a repair prompt.
7. Return provider errors using existing app error kinds.
8. Add verbose logging hooks for chunk start/end.
9. Add tests with a mock provider proving:
   - all chunks are called,
   - maximum concurrency is honored,
   - completion order does not change merge order,
   - cancellation stops pending work,
   - one permanent chunk failure fails the review.

### Review Gate

- `go test ./internal/chunk/...`
- `go test ./internal/app/...`
- `go test ./...`
- `prism review staged`

### Acceptance

Chunk reviews run in parallel, respect concurrency, and produce deterministic ordered raw chunk reports.

## Phase 5 — Merge and Dedup Engine

### Goal

Merge chunk reports into one canonical issue/question/patch set.

### Tasks

1. Add merge package or app-level merge functions.
2. Normalize issue IDs after merge.
3. Preserve tags:
   - `chunk:<CHUNK-ID>`,
   - `chunked-review`,
   - `cross-section`,
   - `synthesis`.
4. Deduplicate exact and overlapping findings.
5. Merge duplicate tags without mutating input slices.
6. Keep higher severity when duplicates disagree.
7. Prefer more specific recommendations when duplicates differ.
8. Merge questions deterministically.
9. Validate patch `before` text against original spec.
10. Recompute report summary once after final merge.
11. Add tests for:
    - deterministic ordering,
    - duplicate overlap,
    - severity conflict,
    - tag preservation,
    - patch validation,
    - no input slice aliasing.

### Review Gate

- `go test ./internal/app/...`
- `go test ./...`
- `prism review staged`

### Acceptance

The final report is stable, schema-compatible, and free of duplicate chunk findings.

## Phase 6 — Synthesis Call

### Goal

Add a bounded cross-section synthesis pass for defects that chunk-local calls may miss.

### Tasks

1. Build synthesis prompt from:
   - table of contents,
   - merged chunk findings,
   - preflight findings,
   - bounded high-risk section summaries from `meta.chunk_summary`.
2. Instruct synthesis not to re-review the whole spec.
3. Allow synthesis findings to cite any original line.
4. Tag synthesis issues with `synthesis`.
5. Merge synthesis issues through the same dedup engine.
6. Skip synthesis when:
   - chunking produces no findings and the spec is below the default `240` line synthesis threshold,
   - chunking is disabled,
   - user config disables synthesis in a later advanced option.
7. Add mock-provider tests for synthesis:
   - synthesis call receives merged findings,
   - synthesis call receives chunk summaries,
   - synthesis findings merge into final report,
   - duplicate synthesis findings collapse.

### Review Gate

- `go test ./internal/app/...`
- `go test ./...`
- `prism review staged`

### Acceptance

Chunked review can still catch cross-section defects without sending the entire spec for a second full review.

## Phase 7 — CLI Integration

### Goal

Expose chunking in `speccritic check` while preserving the existing default for small specs.

### Tasks

1. Add flags:
   - `--chunking`,
   - `--chunk-lines`,
   - `--chunk-overlap`,
   - `--chunk-min-lines`,
   - `--chunk-token-threshold`,
   - `--chunk-concurrency`,
   - `--synthesis-line-threshold`,
   - `--chunk-timeout`.
2. Add environment defaults if consistent with existing flag/env behavior:
   - `SPECCRITIC_CHUNKING`,
   - `SPECCRITIC_CHUNK_LINES`,
   - `SPECCRITIC_CHUNK_OVERLAP`,
   - `SPECCRITIC_CHUNK_MIN_LINES`,
   - `SPECCRITIC_CHUNK_TOKEN_THRESHOLD`,
   - `SPECCRITIC_CHUNK_CONCURRENCY`,
   - `SPECCRITIC_SYNTHESIS_LINE_THRESHOLD`.
3. Validate flags and return exit `3` on invalid values.
   - `--chunk-token-threshold` must be greater than `0`.
   - `--synthesis-line-threshold` must be `>= 0`.
4. Route app checks:
   - preflight-only skips chunking,
   - preflight gate may skip chunking,
   - chunking off uses existing single-call path,
   - chunking auto chooses by line threshold or the default `4000` estimated-token threshold,
   - chunking on forces chunk path.
5. Implement deterministic prompt token estimation using one token per four UTF-8 bytes, rounded up.
6. Preserve `--debug` behavior with redacted chunk prompts.
7. Add integration tests with mock provider:
   - small spec single-call default,
   - large spec auto chunked,
   - token-heavy spec auto chunked before line threshold,
   - forced chunking,
   - chunking off,
   - invalid flags.

### Review Gate

- `go test ./cmd/speccritic/...`
- `go test ./internal/app/...`
- `go test ./...`
- `prism review staged`

### Acceptance

CLI users can opt into chunking, and large specs use chunking automatically without changing output format.

## Phase 8 — Web Integration

### Goal

Allow web checks to benefit from automatic chunking without adding visual complexity.

### Tasks

1. Use app chunking auto mode for web checks.
2. Set conservative web defaults:
   - concurrency no higher than CLI default,
   - same chunk size defaults,
   - same timeout behavior.
3. Keep one merged result in the UI.
4. Ensure modal issue detail works for chunk-tagged findings.
5. Add optional advanced form fields only if needed.
6. Add web handler tests:
   - large upload uses chunking auto request fields,
   - final rendered findings include chunk-tagged issues,
   - error behavior remains sanitized.

### Review Gate

- `go test ./internal/web/...`
- `go test ./...`
- `prism review staged`

### Acceptance

The web UI reviews large specs faster without exposing partial chunk reports.

## Phase 9 — Benchmarks and Tuning

### Goal

Prove chunk planning, merge, and mock end-to-end execution are fast and deterministic.

### Tasks

1. Add benchmark for chunk planning on a 10,000-line synthetic spec.
2. Add benchmark for merging 1,000 synthetic findings.
3. Add mock end-to-end chunked review benchmark.
4. Tune allocation hot spots.
5. Run existing preflight benchmark to ensure no regression.
6. Document benchmark numbers in the implementation notes or commit message.

### Review Gate

- `go test ./internal/chunk -bench . -benchmem`
- `go test ./internal/app -bench Chunk -benchmem`
- `go test ./internal/preflight -bench BenchmarkRunTenThousandLineSpec -benchmem`
- `go test ./...`
- `prism review staged`

### Acceptance

Chunk planning and merge meet the performance targets in the spec, and mock end-to-end chunking shows expected parallel speedup characteristics.

## Phase 10 — Documentation

### Goal

Document when chunking helps, how to configure it, and what output differences users may see.

### Tasks

1. Update `README.md`:
   - purpose of chunking,
   - default `auto` behavior,
   - CLI flags,
   - recommended settings,
   - interaction with preflight.
2. Add examples:
   - force chunking for a large spec,
   - disable chunking for debugging,
   - tune concurrency for rate-limited providers.
3. Add limitations:
   - cross-section defects may still require synthesis,
   - chunking improves wall-clock latency but may increase total provider calls,
   - provider rate limits can reduce benefit.
4. Update web docs if controls are exposed.

### Review Gate

- `go test ./...`
- `prism review staged`

### Acceptance

Users can understand when chunking runs, how to tune it, and how it affects speed/cost tradeoffs.

## Recommended Initial Slice

For the first implementation PR, keep the scope smaller than the full plan:

1. Chunk planner.
2. Chunk prompt builder.
3. Bounded parallel executor with mock-provider tests.
4. Merge without synthesis.
5. CLI flags:
   - `--chunking`,
   - `--chunk-concurrency`,
   - `--chunk-lines`,
   - `--chunk-token-threshold`,
   - `--synthesis-line-threshold`.
6. README quick-start for large specs.

This slice should deliver the main wall-clock speedup while keeping cross-section synthesis and advanced tuning for a follow-up.
