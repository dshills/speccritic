# SpecCritic Section Chunking and Parallel LLM Review Specification

## 1. Purpose

SpecCritic section chunking is an execution strategy for reviewing large specifications by splitting the spec into bounded section chunks and reviewing those chunks with parallel LLM calls.

The feature exists to reduce wall-clock latency and improve reliability on large specs. It must preserve the existing SpecCritic contract: same schema, same scoring rules, same line-number evidence validation, same redaction behavior, and same CLI/web outputs.

## 2. Goals

- Reduce wall-clock review time for large specs by reviewing independent chunks concurrently.
- Keep small specs on the existing single-call path.
- Preserve exact line-number evidence into the original spec.
- Include enough global context in each chunk review to avoid local-only misreadings.
- Merge chunk findings into one normal `schema.Report`.
- Deduplicate findings that appear in multiple chunks.
- Support deterministic retries for failed or invalid chunk outputs.
- Respect provider rate limits and user-configured concurrency.
- Keep the CLI and web UX compatible with existing behavior.

## 3. Non-Goals

- No change to the JSON output schema.
- No replacement of preflight; preflight still runs before LLM review.
- No semantic embedding search in the first version.
- No automatic spec rewriting.
- No provider-specific batching API dependency.
- No hidden relaxing of evidence validation, verdict computation, or redaction.
- No attempt to split requirements below the Markdown section level unless a section is too large.

## 4. User Workflow

1. User runs `speccritic check SPEC.md` or submits a spec in the web UI.
2. SpecCritic loads, redacts, and line-numbers the spec.
3. SpecCritic runs deterministic preflight.
4. If an LLM review is still needed, SpecCritic chooses review mode:
   - single-call review for small specs,
   - chunked review for specs above the configured line or token thresholds,
   - forced chunked review when requested.
5. SpecCritic splits the spec into stable chunks.
6. SpecCritic reviews chunks concurrently within the configured concurrency limit.
7. SpecCritic validates every chunk response against the existing schema and evidence bounds.
8. SpecCritic merges chunk results, deduplicates overlapping findings, recomputes summary, and renders normal output.

## 5. CLI Interface

Existing behavior remains the default for small specs.

New flags:

| Flag | Default | Description |
|------|---------|-------------|
| `--chunking` | `auto` | Chunking mode: `auto`, `on`, or `off`. |
| `--chunk-lines` | `180` | Target maximum source lines per chunk before overlap. |
| `--chunk-overlap` | `20` | Number of neighboring lines included before and after each chunk for context. |
| `--chunk-min-lines` | `120` | Minimum line count before `auto` may use chunking. |
| `--chunk-token-threshold` | `4000` | Estimated prompt-token count before `auto` may use chunking. |
| `--chunk-concurrency` | `3` | Maximum number of concurrent chunk LLM calls. |
| `--synthesis-line-threshold` | `240` | Minimum total spec line count before a no-finding chunked review may run synthesis. |
| `--chunk-timeout` | same as normal provider timeout | Optional per-chunk timeout override. |

Mode behavior:

- `--chunking off` always uses the existing single-call LLM path.
- `--chunking on` always uses chunking when an LLM call is needed.
- `--chunking auto` uses chunking only when the redacted spec has at least `--chunk-min-lines` lines or the estimated prompt size is at least `--chunk-token-threshold` tokens.

Token estimation:

- The first implementation must use a deterministic local estimate of one token per four UTF-8 bytes of prompt text, rounded up.
- The estimate includes system prompt, context prompt, table of contents, selected glossary/definitions text, preflight context, and the full redacted spec for the single-call alternative.
- Implementations may later replace the estimator with provider-specific token counters only when the fallback estimator remains available.

Validation:

- `--chunk-lines` must be greater than `0`.
- `--chunk-overlap` must be `>= 0` and less than `--chunk-lines`.
- `--chunk-min-lines` must be `>= 0`.
- `--chunk-token-threshold` must be greater than `0`.
- `--chunk-concurrency` must be between `1` and `16`.
- `--synthesis-line-threshold` must be `>= 0`.
- Invalid chunking flags are input errors and exit with code `3`.

## 6. Web Interface

The first web version may keep chunking automatic with no visible controls.

If controls are exposed later, they must be advanced settings:

- chunking mode selector: `auto`, `on`, `off`,
- concurrency stepper or select,
- target chunk size input.

The web result must not expose chunk internals as separate reports. It must show one merged review result.

## 7. Chunk Model

A chunk is a bounded view over the original redacted spec:

```go
type Chunk struct {
    ID          string
    Path        string
    LineStart   int
    LineEnd     int
    ContextFrom int
    ContextTo   int
    HeadingPath []string
    Numbered    string
}
```

Definitions:

- `LineStart` and `LineEnd` are the primary review range.
- `ContextFrom` and `ContextTo` include overlap lines that may be read for context.
- Findings may cite only lines inside `LineStart..LineEnd`.
- Overlap lines are context-only and must not be cited unless they are inside the primary range for that chunk.
- `HeadingPath` records the containing Markdown heading hierarchy.
- `ID` is stable for a given spec version and uses source line bounds, for example `CHUNK-0004-L121-L188`.

## 8. Chunking Algorithm

SpecCritic must prefer Markdown structure:

1. Parse Markdown headings from the redacted spec.
2. Build section spans using heading start lines and the next sibling/ancestor boundary.
3. Use top-level sections as initial candidates.
4. If a section exceeds `--chunk-lines`, split it by nested headings.
5. If a nested section still exceeds `--chunk-lines`, split by paragraph/list boundaries.
6. If a single paragraph/list item exceeds `--chunk-lines`, split by line range as a last resort.
7. Add overlap context before and after each chunk.
8. Preserve original line numbers in all chunk prompts.

Chunking must be deterministic for identical input and flags.

Chunks should target `--chunk-lines` primary lines, but Markdown section integrity takes precedence unless a section would exceed twice the target size.

## 9. Global Context

Each chunk prompt must include:

- the normal system prompt,
- selected profile rules,
- strict-mode instructions when enabled,
- preflight known findings context when present,
- a generated table of contents for the full spec,
- global definitions/glossary sections when detected,
- the chunk's heading path,
- the chunk's numbered primary lines,
- overlap lines marked as context-only.

The table of contents must include heading text and line ranges only. It must not include full text for every section.

Glossary and definitions sections may be included in full when they are below the configured context budget. If too large, include only their headings and line ranges.

## 10. Prompt Contract

Each chunk LLM call must be told:

- Review only the primary range for defects.
- Use overlap and global context only to interpret the primary range.
- Cite only primary-range line numbers.
- Return JSON matching the existing schema.
- Use normal issue IDs, but IDs are temporary before merge.
- Do not emit score or verdict.
- Emit a brief chunk summary in `meta.chunk_summary`.
- Add tag `chunk:<CHUNK-ID>` to every issue emitted from a chunk.
- Add tag `cross-section` when the issue depends on another section outside the primary range.
- Add clarification questions only when the question blocks implementing the primary range.

Chunk summary requirements:

- `meta.chunk_summary` must be a string of at most 600 characters.
- It must describe the reviewed primary range, important local concepts, and referenced external sections.
- It must not include findings, score, verdict, or implementation advice.
- If the model omits `meta.chunk_summary`, the repair prompt must request the missing summary while preserving the original findings.
- Summaries are used only as synthesis input and are not rendered in normal JSON, Markdown, or web output.

## 11. Cross-Section Checks

Some defects require more than one section:

- contradictions,
- terminology inconsistency,
- missing interface referenced from another section,
- ordering across workflows,
- global invariant gaps.

The first version must handle cross-section issues in two ways:

1. Each chunk receives the table of contents and glossary context.
2. After chunk reviews complete, SpecCritic runs one synthesis LLM call over:
   - merged chunk findings,
   - table of contents,
   - preflight findings,
   - a bounded list of high-risk section summaries from `meta.chunk_summary`.

The synthesis call must not re-review the whole spec. It may:

- identify duplicates,
- identify contradictions between findings,
- add cross-section issues with valid evidence lines,
- ask clarification questions.

The synthesis call is skipped when chunking produces no findings and the redacted spec has fewer than `--synthesis-line-threshold` lines. With the default threshold of `240`, a 239-line chunked review with no findings skips synthesis, while a 240-line chunked review may run synthesis. If any chunk produces findings, synthesis runs unless chunking is disabled or a later explicit synthesis-disable option is added.

## 12. Parallel Execution

SpecCritic must use bounded parallelism.

Requirements:

- Start at most `--chunk-concurrency` provider calls at a time.
- Preserve deterministic merge order regardless of completion order.
- Cancel outstanding chunk calls when the parent context is cancelled.
- Retry invalid chunk JSON once using the existing repair prompt pattern.
- Retry transient provider errors according to existing provider retry policy if available.
- If one chunk fails permanently, the whole check fails with provider/model-output error.
- Log chunk start/end in verbose mode without printing spec text.

Provider safety:

- Do not exceed configured concurrency.
- Do not create unbounded goroutines.
- Do not retry all chunks at once after a rate-limit error.

## 13. Merge and Deduplication

After chunk calls complete, SpecCritic must:

1. Validate every evidence range against the original spec line count.
2. Reject evidence outside the chunk primary range unless emitted by the synthesis call.
3. Normalize chunk issue IDs into stable final IDs:
   - `ISSUE-0001`, `ISSUE-0002`, ...
   - ordered by severity descending, line start ascending, category, title.
4. Preserve original chunk IDs in tags.
5. Merge all questions and patches.
6. Deduplicate findings.
7. Recompute score, verdict, and counts once after merge.

Duplicate detection must include:

- exact same category,
- overlapping evidence line ranges,
- same or highly similar title,
- same preflight duplicate tag when present.

When duplicates differ in severity:

- keep the higher severity,
- preserve both tags,
- preserve the more specific recommendation when one exists,
- prefer LLM issue text over preflight issue text when the LLM explicitly confirmed the preflight issue.

## 14. Output Semantics

JSON output remains the existing report schema.

Additional tags may appear:

- `chunk:<CHUNK-ID>`
- `chunked-review`
- `cross-section`
- `synthesis`

Markdown and web output may show normal findings only. They do not need a separate chunk view in the first version.

## 15. Interaction With Preflight

Preflight always runs before chunked review when enabled.

If preflight mode is:

- `only`: no LLM chunking occurs.
- `gate`: chunking runs only when no blocking preflight issue exists.
- `warn`: chunking receives bounded preflight context.

Preflight findings must be merged exactly once into the final report.

## 16. Interaction With Patch Generation

Patch generation remains advisory.

Chunk-level patches are allowed only when:

- `before` text is fully inside the chunk primary range,
- `before` appears exactly once in the original redacted spec,
- the replacement is minimal.

Patch merge must preserve existing patch diff generation behavior.

## 17. Performance Requirements

Targets for specs large enough to trigger chunking:

- wall-clock time should improve by at least 30% compared with single-call review when provider latency dominates,
- memory usage must remain proportional to spec size plus chunk count,
- chunk planning must complete in under 100 ms for 10,000-line specs,
- final merge must complete in under 100 ms for 1,000 findings.

The implementation must include benchmark coverage for chunk planning and merge.

## 18. Error Handling

Chunking errors map to existing error kinds:

- invalid flags: input error, exit `3`,
- chunk planning bug or invalid chunk evidence: model-output error when caused by model response, input error when caused by impossible user configuration,
- provider failure: provider error, exit `4`,
- invalid model output after retry: model-output error, exit `5`.

The user must receive one clear error, not partial chunk results.

## 19. Observability

Verbose mode must print:

- chunking mode selected,
- chunk count,
- target chunk lines,
- concurrency,
- per-chunk start and completion,
- synthesis start and completion when used.

Debug mode may include chunk prompts, following the existing debug safety model. Debug output must use redacted text only.

## 20. Testing Requirements

Unit tests:

- Markdown section parser,
- nested heading chunking,
- oversized section splitting,
- overlap bounds,
- primary-range evidence enforcement,
- deterministic chunk IDs,
- merge ordering,
- deduplication,
- final ID renumbering,
- flag validation.

Integration tests:

- mock provider receives multiple chunk calls,
- concurrency limit is honored,
- invalid chunk JSON is repaired once,
- one failing chunk fails the whole check,
- chunking off preserves existing single-call behavior,
- chunking only runs after preflight gate allows it,
- web checks use auto chunking.

Golden tests:

- large good spec returns valid merged output,
- large bad spec emits expected findings across sections,
- duplicate findings across overlapping chunks appear once.

Benchmarks:

- chunk planning on 10,000-line spec,
- merge of 1,000 synthetic findings,
- end-to-end mock chunked review.

## 21. Acceptance Criteria

- Small specs continue to use the existing single-call path by default.
- Large specs in `auto` mode use bounded parallel chunk calls.
- Final output remains compatible with existing JSON, Markdown, and web consumers.
- Findings cite original spec lines and never cite context-only overlap lines.
- Duplicate chunk findings are merged deterministically.
- `go test ./...` passes.
- Chunk planning and merge benchmarks meet the targets.
