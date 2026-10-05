# SpecCritic Improvements

Candidate improvements to accuracy, speed/throughput, and token usage, in priority order.
Reviewed against commit `77fa8e8`. The five items in `specs/improve.md` (preflight, chunking,
incremental, convergence, completion) are done; this list starts from the code as it stands.

**Ordering.** Payoff per unit of effort, with prerequisites first.

**What the list targets.** Local work is not a bottleneck: the existing benchmarks put redaction,
preflight, chunk planning and merging at 25 ms or less on 2,000–10,000-line specs. All meaningful
latency and cost is in LLM calls, so every item is about call count, tokens per call, wasted
calls, or the quality of what comes back.

**How sure each claim is.**

- Behavior of this codebase was confirmed by reading the cited code. Item 3 was also confirmed by
  running it.
- Token and latency effects are estimates from prompt sizes. Nothing records real usage yet
  (item 4), so treat the numbers as directional.
- Statements about current Claude models come from Anthropic's API reference (cached
  2026-09-25), not from live calls.

| # | Item | Helps | Effort | Status |
|---|------|-------|--------|--------|
| 1 | Stop discarding responses that hit the output cap | Speed, Tokens | Small | Done |
| 2 | Salvage valid findings; stamp bookkeeping fields locally | Speed, Tokens | Small | Done |
| 3 | Fix redaction collapsing lines | Accuracy | Small |  |
| 4 | Record token usage and latency per call | All (measurement) | Small |  |
| 5 | Make requests valid for current models; refresh defaults | Accuracy | Medium |  |
| 6 | Build an eval set before tuning prompts or chunking | Accuracy (measurement) | Medium |  |
| 7 | Enforce the output schema at the provider | Speed, Tokens | Medium |  |
| 8 | Verify and re-anchor evidence locally | Accuracy | Medium |  |
| 9 | Raise the auto-chunking threshold and chunk size | Tokens, Accuracy | Small |  |
| 10 | Give chunk reviewers the whole spec, cached; make the cache hit | Accuracy, Tokens | Large |  |
| 11 | Fix cross-chunk dedupe; let synthesis merge and retract | Accuracy, Tokens | Medium |  |
| 12 | Verify CRITICAL findings before they decide the verdict | Accuracy | Medium |  |
| 13 | Retry transient provider errors; keep completed chunks | Throughput | Small |  |
| 14 | Cache results by content hash | Speed, Tokens | Medium |  |
| 15 | Put the model output on a diet | Tokens, Speed | Medium |  |
| 16 | Add a severity rubric and worked examples to the prompt | Accuracy | Small |  |
| 17 | Bring the incremental path to parity with full review | Accuracy | Small |  |
| 18–22 | Lower priority (tiering, streaming, batch, context ranking, phrase lists) | Mixed | Varies |  |

---

## 1. Stop discarding responses that hit the output cap

**Helps:** Speed, Tokens. **Effort:** Small. **Status:** Done.

**Today.** `--max-tokens` defaults to 4096 (`cmd/speccritic/main.go:117`; the web UI and the
package facade default to 8192). When a review needs more, the JSON is cut off, parsing fails,
and `callWithRetry` re-sends the whole prompt with a doubled cap and regenerates every finding
(`internal/app/check.go:1149-1158`). The first call's input and output are thrown away. If the
doubled cap is still short, the run exits 5. Truncation is inferred from the error text
"unexpected end of JSON input" (`internal/llm/retry.go:11-18`); the providers' `stop_reason` /
`finish_reason` are never parsed. Defect-heavy specs, the ones the tool exists for, are the ones
that overflow.

**Change.**

- Raise the default to about 16,000. The cap costs nothing unless it is used, and
  `defaultMaxTokens` in `internal/llm/provider.go:18` is already 16384; the CLI just overrides it.
- Parse the stop reason into `llm.Response` and use it to detect truncation.
- On truncation, keep the findings that parsed completely and ask only for the remainder, rather
  than starting over.

## 2. Salvage valid findings; stamp bookkeeping fields locally

**Helps:** Speed, Tokens. **Effort:** Small. **Status:** Done.

**Today.** One bad finding rejects the whole response and triggers a full repair call.

- `validateReport` returns on the first error (`internal/schema/validate/validate.go:61-91`).
- A chunk response is rejected when an issue lacks the `chunk:<ID>` tag, when
  `meta.chunk_summary` is missing or over 600 characters, or when one evidence range strays
  outside the primary range (`internal/chunk/validate.go:32-37, 50-52, 76-78`).
- An incremental response is rejected for a missing `incremental-review` or `range:<ID>` tag
  (`internal/incremental/executor.go:139-145`).

The caller already knows every one of those values. Synthesis shows the better pattern: it adds
its own tag after parsing (`internal/chunk/synthesis.go:153-157`).

**Change.** Validate per finding. Drop findings that fail and count them in `meta`; retry only
when nothing usable parses. Add tags, IDs and evidence `path` locally, truncate an over-long
summary instead of rejecting it, and remove the matching instructions from the prompts.

**Effect.** Fewer repair calls (each one re-bills the full prompt and regenerates the full
output) and fewer exit-5 failures.

## 3. Fix redaction collapsing lines

**Helps:** Accuracy. **Effort:** Small. **Confirmed by running.**

**Today.** `redact.Redact` documents that line structure is preserved, but the assignment
patterns allow `\s*` around the separator, and `\s` matches newlines
(`internal/redact/redact.go:48-56`). A YAML-style schema snippet triggers it:

```
  password:
    type: string
```

becomes the single line `  [REDACTED] string`. A seven-line probe with one `password:` key and
one `api_key:` key came back with five lines. The only existing line-count test covers PEM blocks.

**Why it matters.** The chunk and incremental paths number lines from the redacted raw text
(`internal/chunk/chunk.go:125`, `internal/chunk/prompt.go:28`, `internal/incremental/prompt.go:28`),
while `LineCount` comes from the original. Every evidence line after the first collapse is off
relative to the user's file, and non-secret spec content is destroyed before the model sees it.

**Change.** Restrict those patterns to horizontal whitespace (`[ \t]*`) and add a test asserting
the newline count is unchanged for every pattern.

## 4. Record token usage and latency per call

**Helps:** Everything else, by making it measurable. **Effort:** Small.

**Today.** No provider response parses `usage`; `llm.Response` carries only content and model
(`internal/llm/provider.go:46-49`). Nothing reports how many calls ran, how many were repairs,
what was read from or written to cache, or how long each call took. Cache misses are invisible.

**Change.** Parse input, output, cache-read and cache-write tokens plus the stop reason from all
three providers. Aggregate into `meta` (calls, repairs, tokens by kind, duration) and print a
one-line summary under `--verbose`.

## 5. Make requests valid for current models; refresh defaults

**Helps:** Accuracy. **Effort:** Medium. **Not verified with a live call.**

**Today.**

- The default model is `claude-sonnet-4-20250514` (`internal/llm/provider.go:23`), which
  Anthropic lists as deprecated. The OpenAI and Gemini defaults (`gpt-4o`, `gemini-2.0-flash`)
  are similarly dated and worth checking against those providers' current lineups.
- Every request sends `temperature` (`internal/app/check.go:202`, `internal/chunk/executor.go:118`,
  `internal/chunk/synthesis.go:116`, `internal/incremental/executor.go:107`). Per Anthropic's
  reference, Claude Opus 4.7 and later and Claude Sonnet 5 and later reject sampling parameters
  with a 400, so `--llm-model claude-opus-5-5` would fail as the code stands.
- The newest models (Claude Opus 5.5, Claude Sonnet 5.5) think by default, and thinking counts
  against `max_tokens`, which makes the 4096 cap in item 1 tighter still.

**Change.**

- Send `temperature` only when the flag is set explicitly and the model accepts it.
- Default to a current model: `claude-opus-5-5`, or `claude-sonnet-5-5` if cost matters more.
  That trade-off is a product decision.
- Add `--effort`, mapped to `output_config.effort` on Anthropic and reasoning effort on OpenAI.
  Choose the default with the eval in item 6.
- Treat `stop_reason: "refusal"` as a clear provider error instead of "no text content".

**Effect.** Model generation is likely the largest single accuracy lever, and it is currently
out of reach.

## 6. Build an eval set before tuning prompts or chunking

**Helps:** Accuracy, by making it measurable. **Effort:** Medium.

**Today.** `testdata/` holds one good spec, one bad spec and canned mock responses. The tests
prove the plumbing, not the quality of a review. Nothing measures precision, recall, severity
calibration, or whether two runs on the same spec agree.

**Change.** Assemble specs with seeded, labeled defects (category, line range, expected
severity) plus a few clean specs. Add an opt-in `make eval` (it spends real money) that reports:

- precision and recall per category,
- false-CRITICAL rate on the clean specs,
- verdict agreement across N runs of the same input,
- tokens and wall-clock from item 4.

Gate items 9–12 and 16 on it.

## 7. Enforce the output schema at the provider

**Helps:** Speed, Tokens. **Effort:** Medium.

**Today.** Anthropic gets "return JSON only" as an instruction. OpenAI and Gemini use
`json_object` (`internal/llm/openai.go:105`, `internal/llm/gemini.go:58`), which guarantees
syntax but not shape. Enum typos, missing fields and prose preambles all become repair calls.
The schema is taught through a roughly 260-token example in every system prompt
(`internal/llm/prompt.go:50-82`).

**Change.** Use `output_config.format` with a JSON schema on Anthropic and
`response_format: json_schema` with `strict: true` on OpenAI (check what the Gemini
compatibility endpoint accepts). Make severity and category enums. Move field guidance into
schema descriptions and drop the example.

**Notes.** Anthropic's structured outputs do not support numeric bounds, so line-range checks
stay local. The current default model is not on the supported list, which is one more reason
for item 5.

## 8. Verify and re-anchor evidence locally

**Helps:** Accuracy. **Effort:** Medium.

**Today.** Evidence is checked only for line bounds
(`internal/schema/validate/validate.go:197-211`). `quote` is never compared with the cited
lines. An issue with no evidence at all passes validation (`validate.go:144-148`), although the
prompt and the project invariants both require evidence. Convergence fingerprints hash quotes
(`internal/convergence/fingerprint.go:102-114`), so a paraphrased quote also produces false
"new" and "resolved" churn between runs.

**Change.** After parsing, normalize whitespace and look for each quote in the spec:

- found at the cited lines: keep;
- found once elsewhere: move the line range to it;
- not found: tag the finding `evidence-unverified`, and for a CRITICAL either downgrade it or
  send it to verification (item 12).

Then overwrite `quote` with the exact spec text, and drop issues that have no evidence.

**Effect.** The anti-hallucination rule becomes mechanical rather than a request, and
convergence tracking gets stable inputs.

## 9. Raise the auto-chunking threshold and chunk size

**Helps:** Tokens, Accuracy. **Effort:** Small (after item 6).

**Today.** `auto` chunks any spec of 120 lines or more, or any prompt estimated at 4,000 tokens
or more, into chunks of about 180 lines (`internal/chunk/chunk.go:12-17, 199-208`).

- Every SPEC.md in this repo (373–610 lines) is chunked, though each fits in one call many
  times over.
- Each chunk call repeats the system prompt, instructions, table of contents, glossary,
  preflight list and up to 40 overlap lines, and a synthesis call follows. For a 400-line spec
  that is roughly three times the input tokens of a single call and four or five calls instead
  of one (estimate from prompt sizes).
- Because the token test includes context files, a short spec with a context file over about
  3,000 tokens is "chunked" into one chunk plus a synthesis call.
- Each chunk reviewer cannot see the other sections (item 10).

**Change.** Treat chunking as a latency tool for large specs. Start near `--chunk-min-lines 1500`
and `--chunk-lines 600`, apply the token test to the spec alone, and let the eval and usage data
settle the numbers.

**Effect.** Fewer calls and tokens for typical specs and better cross-section accuracy. Latency
for mid-size specs could move either way (one longer generation against parallel chunks plus a
serial synthesis call), so measure it.

## 10. Give chunk reviewers the whole spec, cached; make the cache hit

**Helps:** Accuracy, Tokens. **Effort:** Large.

**Today, accuracy.** A chunk prompt holds its primary lines, 20 lines of overlap on each side,
the table of contents, and sections whose heading contains "glossary" or "definition"
(`internal/chunk/prompt.go:56-63, 103-125`). Anything defined elsewhere is invisible, so
"undefined interface", "missing failure mode" and "assumption required" findings can be false,
and contradictions between sections cannot be seen. Synthesis does not see spec text either,
only the table of contents, findings and 600-character summaries
(`internal/chunk/synthesis.go:51-86`), yet it must cite line numbers.

**Today, caching.**

- Anthropic breakpoints sit on the system prompt and the user prefix
  (`internal/llm/anthropic.go:102-107, 178-190`). The system prompt is roughly 750–800 tokens by
  the tool's own estimate, under the 1,024-token minimum of the default model, so on its own it
  silently does not cache.
- In the single-call path without `--context`, the prefix is one sentence, so nothing is cached.
  The spec, the largest stable block, sits behind no breakpoint, so a repair call re-bills it in
  full.
- In the chunked path the first three requests (`--chunk-concurrency`) leave together. A cache
  entry is readable only once the first response has started, so all three pay the write
  premium and read nothing.

**Change.**

- Put the full numbered spec and the context files in a byte-identical cached prefix shared by
  every chunk call, and name the range to review in the tail.
- Send one chunk first, then fan out the rest so they read the cache.
- Add a breakpoint after the spec block in the single-call path so repairs read it from cache.
- Use the 1-hour TTL when `--context` is large; edit-and-rerun gaps usually exceed five minutes.

**Effect.** Chunk reviewers see the whole spec for input cost comparable to today on providers
with cheap cache reads (estimate; measure on OpenAI and Gemini before making it their default).
Chunk summaries and most of what synthesis does become unnecessary.

## 11. Fix cross-chunk dedupe; let synthesis merge and retract

**Helps:** Accuracy, Tokens. **Effort:** Medium.

**Today.**

- Chunk dedupe needs the same category, an identical normalized title, and overlapping evidence
  lines (`internal/chunk/merge.go:76-98, 217-230`). Chunk findings may cite only their own
  primary range and primary ranges are disjoint, so findings from two chunks can never overlap.
  A spec-wide defect reported by five chunks stays five findings and costs five score
  deductions. The score therefore depends on how the spec happened to be split.
- The synthesis prompt says "you may identify duplicates", but the response schema gives it no
  way to say so. Synthesis can only add findings (`merge.go:36-41`).
- Synthesis receives up to 80 findings as full JSON (`internal/chunk/synthesis.go:72-75`).

**Change.**

- Give synthesis two more output fields: `merge` (groups of issue IDs that are one defect) and
  `retract` (issue IDs answered elsewhere in the spec, with the line that answers them). Apply
  both in `MergeReports`. Retraction is only trustworthy once synthesis can see the spec
  (item 10).
- Send findings to synthesis in the one-line form already used for preflight
  (`internal/app/check.go:785`).

**Effect.** Scores that do not depend on chunking, fewer false positives, and a synthesis prompt
several times smaller (estimate).

## 12. Verify CRITICAL findings before they decide the verdict

**Helps:** Accuracy. **Effort:** Medium.

**Today.** One CRITICAL makes the verdict INVALID and, under `--fail-on`, the exit code 2.
Nothing checks it a second time.

**Change.** One extra call with the spec in the cached prefix. For each CRITICAL issue or
question, the model confirms it, downgrades it, or rejects it with the spec line that answers
it. Record the outcome as a tag and allow `--verify=off`.

**Effect.** Higher precision where an error is most expensive, for one call whose input is
mostly cache reads. It adds output tokens, so judge it with the eval.

## 13. Retry transient provider errors; keep completed chunks

**Helps:** Throughput. **Effort:** Small.

**Today.** Nothing in `internal/llm` retries a 429, a 5xx, an overloaded response or a network
error. The chunk fan-out cancels every worker on the first error
(`internal/chunk/executor.go:68-74`), discarding chunk results that were already paid for.
Parallel fan-out is exactly what makes rate-limit errors likely.

**Change.** Add bounded exponential backoff with jitter that honors `retry-after` in the
provider layer, and re-queue only the chunk that failed. Adopting the official provider SDKs is
an alternative; they retry these errors by default.

## 14. Cache results by content hash

**Helps:** Speed, Tokens. **Effort:** Medium.

**Today.** Every invocation calls the LLM, even when nothing changed. That is common in CI,
pre-commit hooks, and agent loops that re-run the gate.

**Change.** Key a stored report on a hash of the redacted spec, redacted context, profile,
strict flag, provider and model, effort, prompt and schema version, and chunk and preflight
settings. Store it under the user cache directory and add `--no-cache`. Bump the prompt version
whenever a prompt changes.

**Effect.** No tokens and near-zero latency for unchanged input. The same input also gets the
same verdict, which the model alone cannot promise now that current models do not accept
`temperature`.

## 15. Put the model output on a diet

**Helps:** Tokens, Speed. **Effort:** Medium.

Output tokens cost about five times input tokens and set the latency of every call.

- **Fields the tool can fill itself.** `id`, evidence `path`, chunk and range tags,
  `meta.chunk_summary` (unneeded after item 10), and the full `quote`. Ask for a short anchor
  and let item 8 fill in the exact text.
- **Prose.** Cap description, impact and recommendation at a sentence or two each.
- **Patches.** Every review asks for patches (`internal/llm/prompt.go:75-81`), each repeating
  spec text verbatim in `before`. A patch whose `before` is not an exact, unique match is
  silently dropped (`internal/app/check.go:554-560`). Make patch generation skippable
  (`--patches=off`), and express patches as a line range plus replacement text so the tool
  extracts `before` itself.

## 16. Add a severity rubric and worked examples to the prompt

**Helps:** Accuracy. **Effort:** Small (after item 6).

**Today.** Each severity is defined in one line (`internal/llm/prompt.go:27-30`). There are no
examples of what is and is not CRITICAL, and no instruction to check the rest of the spec before
calling something undefined. Severity drives both score and verdict.

**Change.** Add a short rubric with two or three contrasting examples per severity and a rule to
search the whole spec before reporting something as undefined or missing. The text is static, so
it caches. Keep it brief and explain the reasons; tune it against the eval.

## 17. Bring the incremental path to parity with full review

**Helps:** Accuracy. **Effort:** Small.

**Today.** Incremental range prompts omit `--context` files and the known-preflight list
(`internal/incremental/executor.go:14-21`, `internal/incremental/prompt.go:21-61`). The same
text can be judged differently depending on whether the run was incremental or full. The cached
prefix is four fixed sentences, too short to cache.

**Change.** Pass context files and preflight context through, and share the prefix builder with
the chunk path.

## Lower priority

18. **Model tiering.** `llm.Request.Model` exists but nothing sets it
    (`internal/llm/provider.go:41-42`). A faster model could run chunk passes while the main
    model handles synthesis and verification. Try lower effort on a single model first; it keeps
    one cache namespace.
19. **Streaming responses.** Needed before `max_tokens` goes much beyond 16,000 without risking
    HTTP timeouts. It would also give the web UI progress.
20. **Batch mode for CI and bulk runs.** Provider batch APIs trade latency for roughly half the
    cost.
21. **Context ranking.** Include only the context sections relevant to the spec. This matters
    less once context is cached (item 10).
22. **One source for vague-phrase lists.** Profile `ForbiddenPhrases` go to the LLM while
    preflight keeps a different list (`internal/preflight/rules_text.go:21`). Feed the profile
    list to preflight and drop it from the prompt.
