---
name: speccritic
description: Use when the user explicitly asks to validate, check, gate, critique, or review a specification (SPEC.md or any *.spec.md / specs/*.md) before planning or implementation. Trigger phrases include "run speccritic", "check the spec", "gate the spec", "validate SPEC.md", "is this spec ready", or similar. Also trigger when the user says they've just finished writing or revising a SPEC and is about to start planning or coding — speccritic is the first gate in the SPEC → PLAN → CODE pipeline and must pass before plancritic runs. Do NOT trigger on incidental mentions of "spec" in unrelated contexts (e.g. test specs, hardware specs, package specs).
---

# SpecCritic — Spec Validation Gate

SpecCritic evaluates a software specification as a formal contract. It treats the spec like a hostile contract lawyer would: vague language, unverifiable requirements, contradictions, and missing failure modes are bugs.

It is the **first gate** in the pipeline:

```text
SPEC.md → speccritic → PLAN.md → plancritic → CODE → realitycheck → prism → clarion
```

Nothing downstream should run until the spec gate passes.

## When To Invoke

Run speccritic when:

- The user explicitly asks: `run speccritic`, `gate the spec`, `check SPEC.md`, etc.
- The user has just finished writing or revising a SPEC and is about to plan or implement.
- A `SPEC.md` file in the working tree has uncommitted changes and the user is about to commit.

Do **not** auto-run speccritic on every incidental mention of “spec”. It is a manually triggered gate.

## Provider Configuration

Default model: **OpenAI `gpt-6.1-sol`**. Use it unless the user asks for another model.

```bash
export SPECCRITIC_LLM_PROVIDER=openai
export SPECCRITIC_LLM_MODEL=gpt-6.1-sol
export OPENAI_API_KEY=...
```

If these variables are not already set in the environment, pass the model on the command line instead:

```bash
speccritic check SPEC.md --llm-provider openai --llm-model gpt-6.1-sol ...
```

Without either, SpecCritic warns and falls back to its built-in default, `anthropic:claude-opus-5-5`, which is not the model to use here.

Other providers, only when the user asks:

```bash
export SPECCRITIC_LLM_PROVIDER=anthropic
export SPECCRITIC_LLM_MODEL=claude-opus-5-5
export ANTHROPIC_API_KEY=...

export SPECCRITIC_LLM_PROVIDER=gemini
export SPECCRITIC_LLM_MODEL=gemini-3.8-flash
export GEMINI_API_KEY=...
```

Do **not** use the old `SPECCRITIC_MODEL=provider:model` form. Current builds ignore it and fall back to the built-in default; with `--offline` they exit with code 3.

SpecCritic sends no temperature. Use `--effort` to trade depth against cost.

## Standard Invocation

```bash
speccritic check SPEC.md \
  --format json \
  --out .speccritic-review.json \
  --verbose
```

Add a profile when the spec type is known:

| Spec type | Flag |
|---|---|
| General software | `--profile general` |
| REST/HTTP service | `--profile backend-api` |
| Compliance-sensitive, clinical, regulated | `--profile regulated-system` |
| Event-driven / message-based | `--profile event-driven` |

For Medara / clinical-trial work, default to:

```bash
--profile regulated-system
```

Add `--strict` only for final-review gates where any silence must be flagged. Do **not** use `--strict` on early-draft specs unless the user explicitly wants maximum scrutiny.

Add context files as read-only grounding:

```bash
speccritic check SPEC.md \
  --context docs/glossary.md \
  --context docs/api-contract.md \
  --profile backend-api \
  --format json \
  --out .speccritic-review.json
```

Context files help interpretation. They must not be used to invent requirements missing from the spec.

Add `--patches off` when the loop applies `recommendation` text and never reads `.patches`. The model then writes no patches, which saves output tokens and time on every run. Completion patches are not affected.

## Fast Preflight

Use preflight first when iterating. It is deterministic, local, and does not require model credentials.

```bash
speccritic check SPEC.md \
  --preflight-mode only \
  --format json \
  --out .speccritic-review.json
```

Preflight catches obvious placeholders, vague language, missing required sections, undefined acronyms, and unmeasurable requirements before any LLM call.

Modes:

| Mode | Behavior |
|---|---|
| `warn` | Default. Include preflight findings and continue to the LLM. |
| `gate` | Skip the LLM if blocking preflight findings exist. |
| `only` | Run only deterministic preflight checks. No model call. |

Recommended loop:

```bash
speccritic check SPEC.md --preflight-mode only --format json --out .speccritic-review.json
# fix deterministic issues
speccritic check SPEC.md --preflight-mode gate --fail-on INVALID
# then run full review
speccritic check SPEC.md --format json --out .speccritic-review.json
```

Use `--preflight-ignore RULE_ID` only for known false positives.

## Chunked Review For Large Specs

SpecCritic can split large specs into Markdown-section chunks and review them with bounded parallel LLM calls.

Default mode is automatic:

```bash
speccritic check SPEC.md \
  --chunking auto \
  --format json \
  --out .speccritic-review.json
```

Useful controls:

```bash
# Force chunking
speccritic check SPEC.md --chunking on --chunk-concurrency 4

# Disable chunking while debugging prompt behavior
speccritic check SPEC.md --chunking off --debug

# Tune for rate-limited providers
speccritic check SPEC.md --chunk-concurrency 1 --chunk-lines 140
```

Chunking behavior:

- Final output is still one normal SpecCritic report.
- Findings may include tags such as `chunked-review`, `chunk:<ID>`, `cross-section`, or `synthesis`.
- Chunking can reduce wall-clock time for large specs but may increase total provider calls.
- Rate limits, overloaded providers and dropped connections are retried automatically, and a chunk that still fails on such an error is tried once more after the others. A chunk that fails for any other reason fails the run rather than returning a partial report.
- Gemini may need low concurrency, often `--chunk-concurrency 1`.

## Incremental Rerun

Use incremental rerun when the user is iterating on a large spec and already has a previous JSON result.

Workflow:

```bash
# Save baseline result
speccritic check SPEC.md \
  --format json \
  --out previous.json

# Preserve exact previous spec text
cp SPEC.md SPEC.previous.md

# Edit SPEC.md, then rerun incrementally
speccritic check SPEC.md \
  --incremental-from previous.json \
  --incremental-base SPEC.previous.md \
  --incremental-report \
  --format json \
  --out .speccritic-review.json
```

Modes:

| Mode | Behavior |
|---|---|
| `auto` | Default. Fall back to full review when safe reuse cannot be proven. |
| `on` | Require incremental reuse; unsafe conditions fail instead of falling back. |
| `off` | Disable incremental behavior. |

Important rules:

- Use `--incremental-base` when the current spec differs from the previous report hash.
- Preflight still runs against the full current spec.
- Reused findings are tagged `incremental-reused`.
- New findings from changed ranges are tagged `incremental-review`.
- `meta.incremental` is included only when requested with `--incremental-report`.
- Changed ranges are reviewed with the same context files, whole spec and preflight findings as a full review, so the same text gets the same judgment either way.
- Incremental reruns do not use the review cache.

## Convergence Tracking

Use convergence tracking to show what changed between review iterations: resolved, still-open, new, dropped, and untracked findings.

```bash
speccritic check SPEC.md \
  --convergence-from previous.json \
  --convergence-report \
  --format json \
  --out .speccritic-review.json
```

Modes:

| Mode | Behavior |
|---|---|
| `auto` | Default. Compare when possible; do not fail on partial comparison. |
| `on` | Require convergence comparison; missing/invalid/incompatible baseline exits 3. |
| `off` | Ignore convergence baseline. |

Important rules:

- Convergence does not change current score or verdict.
- Current score/verdict are based only on current findings.
- Resolved historical findings do not reduce current score.
- Preflight-only runs cannot prove prior LLM findings are resolved, so they may be marked `untracked`.
- Convergence matching is local and does not send previous report contents to a provider.

Useful jq:

```bash
jq '.meta.convergence' .speccritic-review.json
```

## Completion Suggestions

Completion suggestions generate draft/advisory patch text for common profile-specific gaps. They are useful when the user wants help converging faster after findings come back.

```bash
speccritic check SPEC.md \
  --completion-suggestions \
  --format json \
  --out .speccritic-review.json \
  --patch-out .speccritic-completion.patch
```

For deterministic-only completion:

```bash
speccritic check SPEC.md \
  --preflight-mode only \
  --completion-suggestions \
  --format json \
  --out .speccritic-review.json \
  --patch-out .speccritic-completion.patch
```

Templates:

| Template | Use |
|---|---|
| `profile` | Default. Use selected `--profile`. |
| `general` | General software specs. |
| `backend-api` | Auth, endpoints, schemas, errors, rate limits. |
| `regulated-system` | Audit, retention, access control, compliance evidence. |
| `event-driven` | Event schemas, delivery, ordering, retry, failed-event queues. |

Controls:

```bash
--completion-mode auto              # default; only runs when --completion-suggestions is true
--completion-mode on                # force completion and fail if required safe patches cannot be generated
--completion-mode off               # disable completion even if requested
--completion-template backend-api
--completion-max-patches 8
--completion-open-decisions=true
```

Rules for agents:

- Completion patches are **advisory only**.
- Never treat completion text as authoritative.
- Never apply completion patches blindly.
- `OPEN DECISION` means the user or business owner must decide; do not invent the answer.
- Completion suggestions do not affect score, verdict, or `--fail-on`.
- `meta.completion` records `enabled`, `mode`, `template`, `generated_patches`, `skipped_suggestions`, and `open_decisions`.

Useful jq:

```bash
jq '.meta.completion' .speccritic-review.json
jq '.patches' .speccritic-review.json
```

## Combined Iteration Workflow

For active spec-writing loops, prefer this order:

```bash
# 1. Fast deterministic pass
speccritic check SPEC.md \
  --preflight-mode only \
  --completion-suggestions \
  --format json \
  --out .speccritic-review.json \
  --patch-out .speccritic.patch

# 2. Fix deterministic issues and user decisions

# 3. Full review, chunking auto-enabled for large specs
speccritic check SPEC.md \
  --format json \
  --out .speccritic-review.json \
  --completion-suggestions

# 4. Save baseline before another edit loop
cp .speccritic-review.json previous.json
cp SPEC.md SPEC.previous.md

# 5. Rerun incrementally after edits
speccritic check SPEC.md \
  --incremental-from previous.json \
  --incremental-base SPEC.previous.md \
  --convergence-from previous.json \
  --incremental-report \
  --convergence-report \
  --completion-suggestions \
  --format json \
  --out .speccritic-review.json
```

## Re-runs And The Review Cache

A finished review is cached on disk, keyed by the spec, the context files, the model and every review flag. Re-running an unchanged spec with the same flags returns the stored review: the same verdict and findings, no model call, `meta.cache.hit` set to `true` and no `meta.usage`. Any edit to SPEC.md or a context file gets a fresh review.

```bash
jq -r '.meta.cache.hit // false' .speccritic-review.json
```

Use `--no-cache` only when the user asks for a second opinion on an unchanged spec. Re-running unchanged input in the hope of a better verdict is not a fix: change the spec.

## Reading The Output

Always parse `.speccritic-review.json`. Decide on `summary.verdict`, never on `length(.issues)`, because `issues[]` may be filtered by `--severity-threshold`; summary counts are not.

```text
verdict == "VALID"           → proceed to plancritic
verdict == "VALID_WITH_GAPS" → document each WARN as a known risk, then proceed only if accepted
verdict == "INVALID"         → DO NOT plan or code. Refine the spec and re-run.
```

Useful jq:

```bash
# Gate decision
jq -r '.summary.verdict' .speccritic-review.json

# Critical issues, agent-fixable defects
jq '[.issues[] | select(.severity == "CRITICAL")]' .speccritic-review.json

# Critical questions, require user decision
jq '[.questions[] | select(.severity == "CRITICAL")]' .speccritic-review.json

# Suggested patches, advisory only
jq '.patches' .speccritic-review.json

# Runtime metadata
jq '.meta' .speccritic-review.json

# Cost of the run (absent when served from the cache)
jq '.meta.usage' .speccritic-review.json

# Incremental metadata
jq '.meta.incremental' .speccritic-review.json

# Convergence metadata
jq '.meta.convergence' .speccritic-review.json

# Completion metadata
jq '.meta.completion' .speccritic-review.json
```

## Acting On Findings

Route output types differently:

| Output | Source | Action |
|---|---|---|
| `issues[]` | Defects in spec text | Edit SPEC.md using `recommendation` and evidence lines. |
| `questions[]` | Genuine decision points | Ask the user verbatim. Do not infer an answer. |
| `patches[]` | Advisory suggestions | Review intent, then optionally apply. |
| `meta.incremental` | Reuse/fallback info | Explain whether the rerun was partial or full. |
| `meta.convergence` | Iteration progress | Report new/still-open/resolved findings. |
| `meta.completion` | Completion patch stats | Explain generated/skipped suggestions and open decisions. |

Loop:

1. Apply every CRITICAL `issue.recommendation` to the relevant spec area.
2. Surface every CRITICAL `question` to the user. Wait for an answer.
3. Fold user answers into `SPEC.md`.
4. Review advisory patches before applying them.
5. Re-run speccritic.
6. Repeat until `VALID`, or `VALID_WITH_GAPS` with all WARNs explicitly accepted.
7. Only then hand off to plancritic.

Avoid editing unrelated sections just to quiet a finding. Preserve the audit trail.

## Defect Categories

| Category | Meaning |
|---|---|
| `NON_TESTABLE_REQUIREMENT` | No test can verify it |
| `AMBIGUOUS_BEHAVIOR` | Two engineers could implement differently |
| `CONTRADICTION` | Two statements cannot both be true |
| `MISSING_FAILURE_MODE` | What happens when X fails is unstated |
| `UNDEFINED_INTERFACE` | Referenced interface has no spec |
| `MISSING_INVARIANT` | Property that must always hold is unstated |
| `SCOPE_LEAK` | Spec describes implementation, not behavior |
| `ORDERING_UNDEFINED` | Operation sequence is ambiguous |
| `TERMINOLOGY_INCONSISTENT` | Same concept named differently |
| `UNSPECIFIED_CONSTRAINT` | Implicit constraint not made explicit |
| `ASSUMPTION_REQUIRED` | Cannot implement without assuming something unstated |

## Exit Codes

| Code | Meaning | Response |
|---|---|---|
| `0` | Command succeeded and verdict is below `--fail-on` threshold | Proceed according to verdict |
| `2` | Verdict meets `--fail-on` threshold | Block; refine spec |
| `3` | Input/configuration error, required convergence failure, or required completion patch failure | Fix configuration/spec loop |
| `4` | Provider error. Rate limits, overloads and dropped connections were already retried | Check API key, provider, model, network; after a rate limit, wait before retrying |
| `5` | Model output failed schema validation after retry | Retry once, then report to user |

For CI or scripted gates:

```bash
speccritic check SPEC.md \
  --offline \
  --fail-on INVALID \
  --severity-threshold warn \
  --format json \
  --out .speccritic-review.json
```

For credentials-free deterministic CI:

```bash
speccritic check SPEC.md \
  --preflight-mode only \
  --fail-on INVALID \
  --format json \
  --out .speccritic-review.json
```

## Anti-Patterns

Avoid these:

- Running speccritic on implementation code. It evaluates specs, not code.
- Inferring answers to CRITICAL questions. Ask the user.
- Treating completion patches as authoritative. They are advisory drafts.
- Applying `OPEN DECISION` placeholders as if they were final requirements.
- Using `--severity-threshold critical` during the agent loop. It hides WARNs.
- Skipping the gate for “small” features.
- Treating `VALID_WITH_GAPS` as clean without documenting accepted WARNs.
- Ignoring `meta.incremental.Fallback`; if fallback occurred, say so.
- Claiming convergence resolved an issue unless `meta.convergence` says it did.
- Overusing `--strict` on early drafts.
- Re-running an unchanged spec with `--no-cache` to fish for a different verdict.

## Pipeline Handoff

When the verdict is `VALID`, or `VALID_WITH_GAPS` with documented accepted WARNs, state the handoff clearly:

```text
Spec gate passed (verdict: VALID, score: NN/100). Moving to PLAN.md → plancritic.
```

If the user asks to skip the gate, push back once. The pipeline order is non-negotiable unless the user explicitly overrides it after being warned.
