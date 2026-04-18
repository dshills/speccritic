---
name: speccritic
description: Use when the user explicitly asks to validate, check, gate, critique, or review a specification (SPEC.md or any *.spec.md / specs/*.md) before planning or implementation. Trigger phrases include "run speccritic", "check the spec", "gate the spec", "validate SPEC.md", "is this spec ready", or similar. Also trigger when the user says they've just finished writing or revising a SPEC and is about to start planning or coding — speccritic is the first gate in the SPEC → PLAN → CODE pipeline and must pass before plancritic runs. Do NOT trigger on incidental mentions of "spec" in unrelated contexts (e.g. test specs, hardware specs, package specs).
---

# SpecCritic — Spec Validation Gate

SpecCritic is a CLI that evaluates a software specification as a formal contract. It treats the spec like a hostile contract lawyer would: vague language, unverifiable requirements, and missing failure modes are bugs. It is the **first gate** in the pipeline:

```
SPEC.md → speccritic → PLAN.md → plancritic → CODE → realitycheck → prism → clarion
```

Nothing downstream should run until the spec gate passes.

## When to invoke

Run speccritic when:

- The user explicitly asks (`run speccritic`, `gate the spec`, `check SPEC.md`, etc.)
- The user has just finished writing or revising a SPEC and is about to plan or implement
- A SPEC.md file in the working tree has uncommitted changes and the user is about to commit

Do **not** auto-run speccritic on every spec mention. It is a manually triggered gate.

## Invocation

Standard run:

```bash
speccritic check SPEC.md \
  --format json \
  --out .speccritic-review.json \
  --verbose
```

Add a profile when the spec type is known:

| Spec type | Flag |
|---|---|
| General software (default) | `--profile general` |
| REST/HTTP service | `--profile backend-api` |
| Compliance-sensitive (HIPAA, 21 CFR Part 11, SOX, etc.) | `--profile regulated-system` |
| Event-driven / message-based | `--profile event-driven` |

For Medara / clinical-trial work, default to `--profile regulated-system`.

Add `--strict` for final-review gates where any silence must be flagged. Do **not** use `--strict` on early-draft specs — it will drown the loop in noise.

Add context files (read-only grounding, never used to infer requirements):

```bash
speccritic check SPEC.md \
  --context docs/glossary.md \
  --context docs/api-contract.md \
  --profile backend-api \
  --format json --out .speccritic-review.json
```

Required environment:

```bash
SPECCRITIC_MODEL=anthropic:claude-sonnet-4-6   # or openai:gpt-4o
ANTHROPIC_API_KEY=...                          # or OPENAI_API_KEY
```

## Reading the output

Always parse `.speccritic-review.json`. Decide on `summary.verdict`, never on `length(.issues)` (that array is filtered by `--severity-threshold`; the summary counts are not).

```
verdict == "VALID"            → proceed to plancritic
verdict == "VALID_WITH_GAPS"  → document each WARN as a known risk, then proceed
verdict == "INVALID"          → DO NOT plan or code. Refine the spec and re-run.
```

Useful jq one-liners:

```bash
# Gate decision
jq -r '.summary.verdict' .speccritic-review.json

# Critical issues (agent-fixable defects)
jq '[.issues[] | select(.severity == "CRITICAL")]' .speccritic-review.json

# Critical questions (require user decision)
jq '[.questions[] | select(.severity == "CRITICAL")]' .speccritic-review.json

# Suggested patches (advisory only)
jq '.patches' .speccritic-review.json
```

## Acting on findings

Distinguish two output types and route them differently:

| Output | Source | Action |
|---|---|---|
| `issues[]` | Defects in the spec text | Edit SPEC.md using `recommendation` and `evidence.line_start` / `evidence.line_end` |
| `questions[]` | Genuine decision points with no canonical answer | Ask the user verbatim. Do **not** infer an answer. |

Loop:

1. Apply every CRITICAL `issue.recommendation` to the cited lines in SPEC.md (do not edit lines outside the evidence range — that hides the trail).
2. Surface every CRITICAL `question` to the user. Wait for an answer. Fold the answer into SPEC.md.
3. Re-run speccritic.
4. Repeat until `verdict == VALID` (or `VALID_WITH_GAPS` with all WARNs explicitly accepted).
5. Only then hand off to plancritic.

Patches in `.patches` are **advisory** — minimal textual suggestions, never wholesale rewrites. Review them against the underlying requirement before applying. A patch that makes the spec pass but misrepresents intent is worse than a failing spec.

## Defect categories (quick reference)

| Category | What it means |
|---|---|
| `NON_TESTABLE_REQUIREMENT` | No test can verify it |
| `AMBIGUOUS_BEHAVIOR` | Two engineers would implement differently |
| `CONTRADICTION` | Two statements cannot both be true |
| `MISSING_FAILURE_MODE` | What happens when X fails is unstated |
| `UNDEFINED_INTERFACE` | Referenced interface has no spec |
| `MISSING_INVARIANT` | Property that must always hold is unstated |
| `SCOPE_LEAK` | Spec describes implementation, not behavior |
| `ORDERING_UNDEFINED` | Operation sequence is ambiguous |
| `TERMINOLOGY_INCONSISTENT` | Same concept named differently |
| `UNSPECIFIED_CONSTRAINT` | Implicit constraint not made explicit |
| `ASSUMPTION_REQUIRED` | Cannot implement without assuming something unstated |

## Exit codes

| Code | Meaning | Response |
|---|---|---|
| `0` | Verdict below `--fail-on` threshold | Proceed |
| `2` | Verdict meets `--fail-on` threshold | Block; refine spec |
| `3` | Bad input / `SPECCRITIC_MODEL` unset with `--offline` | Fix configuration |
| `4` | LLM provider error | Check API key and model name |
| `5` | Model output failed schema validation after retry | Retry once, then report to user |

For CI or scripted gates, add `--fail-on INVALID` and `--offline`.

## Anti-patterns — avoid these

- **Running speccritic on implementation code.** It evaluates specs, not code. Output will be nonsensical.
- **Inferring answers to CRITICAL questions.** That perpetuates the ambiguity the gate exists to eliminate. Ask the user.
- **Using `--severity-threshold critical` in the loop.** Hides WARNs from the agent. Use the default `info` for the loop; reserve `warn`/`critical` for human-facing reports.
- **Skipping the gate for "small" features.** Small features become large bugs when failure modes are undefined.
- **Editing lines outside the cited evidence range.** Hides the audit trail and makes re-runs noisy.
- **Treating patches as authoritative.** They are minimal suggestions; review intent before applying.

## Pipeline handoff

When the verdict is `VALID` (or `VALID_WITH_GAPS` with documented WARNs), the spec is ready for `plancritic`. State this explicitly to the user before moving on:

> Spec gate passed (verdict: VALID, score: NN/100). Moving to PLAN.md → plancritic.

If the user asks to skip the gate, push back once. The pipeline order is non-negotiable for a reason.
