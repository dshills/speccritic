# SpecCritic Agentic Workflow Guide

This document describes how to integrate SpecCritic into an agentic coding system such as Claude Code, Cursor, or any LLM-based coding agent.

## Core Principle

**SpecCritic runs before planning. Planning runs before coding.**

The gate order is non-negotiable:

```
SPEC.md → speccritic check → plan → implement
```

A coding agent that skips spec validation is making silent assumptions. Those assumptions surface as bugs, rework, and incorrect implementations. SpecCritic makes them explicit before any code is written.

---

## Placement in the Agent Loop

### The Canonical Loop

```
1. User describes a feature or change
2. Agent writes (or updates) SPEC.md
3. Agent runs: speccritic check SPEC.md
4. Agent reads the JSON output
5. If INVALID or VALID_WITH_GAPS:
   a. For each CRITICAL issue → revise SPEC.md
   b. For each CRITICAL question → ask the user before proceeding
   c. For each WARN issue → revise or accept with documented risk
6. Repeat from step 3 until verdict is VALID (or VALID_WITH_GAPS with all WARNs accepted)
7. Agent produces an implementation plan
8. Agent implements
```

The spec loop (steps 3–6) runs entirely before the agent touches any code. This is the invariant.

### Minimum Viable Integration

Add this to your `CLAUDE.md` (or equivalent agent instruction file):

```markdown
## Specification Gate

Before writing any implementation plan or code for a new feature:

1. Create or update `SPEC.md` describing the feature as a formal contract
2. Run `speccritic check SPEC.md --format json --out .speccritic-review.json`
3. If verdict is not VALID:
   - Fix CRITICAL issues in SPEC.md directly
   - For CRITICAL questions, ask the user before proceeding
   - Re-run until verdict is VALID or all remaining issues are accepted WARNs
4. Only then produce a plan and begin implementation
```

---

## Reading the Output

SpecCritic always writes JSON (use `--format json`). Parse `.summary.verdict` first, then route based on its value.

### Decision Tree

```
verdict == "VALID"
  └─ Proceed to planning

verdict == "VALID_WITH_GAPS"
  └─ Check .summary.critical_count
       == 0: Proceed, but document each WARN issue as a known risk
       > 0:  (shouldn't happen; treat as INVALID)

verdict == "INVALID"
  └─ Do NOT plan or implement
  └─ Process .issues where severity == "CRITICAL"
       └─ Each issue has .recommendation → apply to SPEC.md
  └─ Process .questions where severity == "CRITICAL"
       └─ Each question has .why_needed → ask the user
  └─ Re-run speccritic
```

### Extracting Actionable Items

Given the JSON output at `.speccritic-review.json`:

**Critical issues to fix autonomously:**
```bash
jq '[.issues[] | select(.severity == "CRITICAL")]' .speccritic-review.json
```

**Questions that require user input:**
```bash
jq '[.questions[] | select(.severity == "CRITICAL")]' .speccritic-review.json
```

**Suggested patches:**
```bash
jq '.patches' .speccritic-review.json
```

**Overall gate check (for scripting):**
```bash
verdict=$(jq -r '.summary.verdict' .speccritic-review.json)
if [[ "$verdict" == "INVALID" ]]; then
  echo "Spec not ready. Fix CRITICAL issues before proceeding."
  exit 1
fi
```

### Evidence Blocks

Every issue includes an `evidence` array with exact line numbers and quoted text:

```json
{
  "id": "ISSUE-0003",
  "severity": "CRITICAL",
  "category": "MISSING_FAILURE_MODE",
  "title": "No error behavior defined for auth service timeout",
  "evidence": [
    {
      "path": "SPEC.md",
      "line_start": 47,
      "line_end": 47,
      "quote": "The API shall validate tokens via the auth service."
    }
  ],
  "recommendation": "Specify the fallback behavior when the auth service is unreachable."
}
```

Use `line_start`/`line_end` to locate the exact text in SPEC.md that needs revision. Do not guess—edit only the cited lines.

---

## Handling Questions vs. Issues

SpecCritic distinguishes between issues (things the agent can fix autonomously) and questions (things that require a human decision):

| Type | What it means | Agent action |
|------|---------------|--------------|
| **Issue** | Defect in the spec text | Revise SPEC.md using `.recommendation` |
| **Question** | Decision point with no right answer | Ask the user; do not proceed until answered |

**Example question requiring user input:**

```json
{
  "id": "Q-0002",
  "severity": "CRITICAL",
  "question": "Should the API return HTTP 401 or HTTP 403 when a valid token lacks permission?",
  "why_needed": "Two engineers will implement this differently without a canonical answer.",
  "blocks": ["Authorization"]
}
```

Present this to the user verbatim. After receiving an answer, incorporate it into SPEC.md and re-run SpecCritic.

---

## Using Context Files

Context files provide grounding for the LLM without adding requirements. Use them to anchor the spec evaluation against existing reality.

### When to Use Context

- **Existing API contracts**: Provide an OpenAPI spec so SpecCritic knows what interfaces already exist
- **Glossary**: Prevent `TERMINOLOGY_INCONSISTENT` false positives by defining domain terms
- **Architecture overview**: Clarify component boundaries for `UNDEFINED_INTERFACE` checks
- **Regulatory constraints**: Inform `MISSING_INVARIANT` checks for compliance-sensitive specs

### Example: New feature on an existing API

```bash
speccritic check SPEC.md \
  --context docs/api-contract.md \
  --context docs/glossary.md \
  --profile backend-api \
  --format json \
  --out .speccritic-review.json
```

Context is redacted before being sent to the LLM. It is used for reference only—never to infer new requirements.

---

## Profiles

Choose a profile that matches the type of specification being evaluated:

```bash
# General software specification (default)
speccritic check SPEC.md --profile general

# REST API or service specification
speccritic check SPEC.md --profile backend-api

# Compliance-sensitive system (healthcare, finance, legal)
speccritic check SPEC.md --profile regulated-system

# Message-driven or event-sourced architecture
speccritic check SPEC.md --profile event-driven
```

The profile determines which sections are required, which phrases are forbidden, and which defect categories are emphasized. Pass the profile that matches the system being specified—mismatched profiles will produce noisy or missed findings.

---

## Strict Mode

Use `--strict` when a spec must be complete before any ambiguity is acceptable—typically for compliance-sensitive or safety-critical systems:

```bash
speccritic check SPEC.md --strict --profile regulated-system
```

In strict mode, any behavior not explicitly stated is flagged as `AMBIGUOUS_BEHAVIOR` or `ASSUMPTION_REQUIRED` at CRITICAL severity. Silence is never permitted. Do not use strict mode on early-draft specs; it is for final-review gates.

---

## Suggested Patches

When SpecCritic returns patches, they represent the LLM's minimal corrections. They are advisory—review before applying.

### Using `--patch-out`

```bash
speccritic check SPEC.md --patch-out spec.patch
```

The patch file uses diff-match-patch format. To apply patches as a starting point:

1. Read each patch's `issue_id` to map it back to the corresponding issue
2. Review the `before`/`after` fields in the JSON output for a human-readable diff
3. Apply changes to SPEC.md manually or use the patch file as a reference

Patches are intentionally minimal (additive or substitutive only, never rewrites). If a patch feels like it's solving the wrong problem, the underlying requirement may need user clarification rather than a text substitution.

---

## Severity Threshold in Agentic Loops

In the spec-refinement loop, always run with `--severity-threshold info` (the default) so the agent sees everything. Reserve `--severity-threshold warn` or `--severity-threshold critical` for reporting, not for the gate loop:

```bash
# Gate loop (agent sees all issues)
speccritic check SPEC.md --severity-threshold info --format json

# Reporting (suppress noise for human review)
speccritic check SPEC.md --severity-threshold warn --format md
```

Note: `--severity-threshold` filters only the `issues` array in the output. The `summary.critical_count`, `summary.warn_count`, `summary.score`, and `summary.verdict` always reflect all findings. Use `.summary.verdict` and `.summary.critical_count` for gate decisions, not `length(.issues)`.

---

## Exit Codes in Scripts and Hooks

| Code | Meaning | Agent action |
|------|---------|--------------|
| `0` | Spec acceptable | Proceed |
| `2` | Verdict ≥ `--fail-on` threshold | Block; fix spec |
| `3` | Bad input (invalid flags, missing file, missing `SPECCRITIC_MODEL`) | Fix configuration |
| `4` | LLM provider error | Check API key and model name |
| `5` | Model output invalid after retry | Retry or report |

Use `--fail-on` to create a hard gate:

```bash
speccritic check SPEC.md --fail-on INVALID && echo "Spec ready"
# exit 0: proceed
# exit 2: spec not ready; do not proceed

speccritic check SPEC.md --fail-on VALID_WITH_GAPS && echo "Spec clean"
# exit 2: any issue at all blocks proceeding
```

---

## Claude Code Integration

### CLAUDE.md Snippet

Add the following to your project's `CLAUDE.md`:

```markdown
## Specification Gate (required before any implementation)

For any new feature or significant change:

1. Write SPEC.md (or update the relevant section) describing the feature as a
   formal contract: what the system must do, not how to do it.

2. Run the spec gate:
   ```
   speccritic check SPEC.md \
     --format json \
     --out .speccritic-review.json \
     --verbose
   ```

3. Read `.speccritic-review.json`:
   - If `summary.verdict` is `INVALID`: fix every CRITICAL issue using
     `issues[].recommendation` before writing any code.
   - If `summary.questions` contains CRITICAL items: ask the user each
     question. Do not infer answers.
   - If `summary.verdict` is `VALID_WITH_GAPS`: document the WARN issues
     as known risks and proceed only with user approval.
   - If `summary.verdict` is `VALID`: proceed to planning.

4. Never begin an implementation plan until the spec gate passes.

Environment:
  SPECCRITIC_MODEL=anthropic:claude-sonnet-4-6
  ANTHROPIC_API_KEY (must be set in environment)
```

### Pre-commit Hook

To prevent committing a spec that hasn't passed validation:

```bash
prism hook install   # if using prism
```

Or manually in `.git/hooks/pre-commit`:

```bash
#!/bin/sh
if git diff --cached --name-only | grep -q "SPEC.md"; then
  echo "SPEC.md changed — running SpecCritic gate..."
  speccritic check SPEC.md --fail-on INVALID --offline
  if [ $? -ne 0 ]; then
    echo "Spec gate failed. Fix CRITICAL issues before committing."
    exit 1
  fi
fi
```

---

## CI Integration

### GitHub Actions

```yaml
name: Spec Gate

on:
  pull_request:
    paths:
      - 'SPEC.md'
      - 'specs/**/*.md'

jobs:
  spec-check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      - name: Install speccritic
        run: go install github.com/dshills/speccritic/cmd/speccritic@latest

      - name: Run spec gate
        env:
          SPECCRITIC_MODEL: anthropic:claude-sonnet-4-6
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
        run: |
          speccritic check SPEC.md \
            --offline \
            --fail-on INVALID \
            --severity-threshold warn \
            --out spec-review.json \
            --verbose

      - name: Upload review artifact
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: spec-review
          path: spec-review.json
```

The `--offline` flag ensures the job fails immediately (exit 3) rather than silently falling back to the default model if `ANTHROPIC_API_KEY` or `SPECCRITIC_MODEL` is misconfigured.

---

## Anti-Patterns

**Do not run SpecCritic on implementation code.** It evaluates specifications, not code. Running it on code will produce irrelevant or nonsensical output.

**Do not use `--severity-threshold critical` as a gate.** This hides WARN issues from the agent. Use `info` (default) for the loop, `warn` for human-facing reports.

**Do not proceed past CRITICAL questions by inferring answers.** Questions in the output represent genuine decision points where two engineers would make different choices. Inferring an answer perpetuates the ambiguity SpecCritic was designed to eliminate.

**Do not apply patches wholesale.** Patches are minimal suggestions, not authoritative corrections. Review each patch against the original requirement before applying. A patch that makes the spec pass SpecCritic but misrepresents the intent is worse than a failing spec.

**Do not skip the spec gate for "small" features.** Small features become large bugs when their failure modes are undefined. The gate is fast (typically 10–30 seconds) and the cost of skipping it is paid in debugging time.

---

## Example: Full Agent Session

```
User: Add rate limiting to the API

Agent:
  1. Writes SPEC.md §Rate Limiting with requirements

  2. Runs:
     speccritic check SPEC.md --format json --out .speccritic-review.json

  3. Reads output:
     verdict: INVALID
     ISSUE-0001 (CRITICAL, MISSING_FAILURE_MODE):
       "No behavior defined when rate limit is exceeded"
       recommendation: "Specify the HTTP status code and response body"
     ISSUE-0002 (CRITICAL, NON_TESTABLE_REQUIREMENT):
       "Rate limit of 'reasonable number of requests' is not measurable"
       recommendation: "Define a numeric limit with a time window"
     Q-0001 (CRITICAL):
       "Should rate limits be per-user, per-IP, or per-API-key?"
       why_needed: "Different scopes require different implementation strategies"

  4. Agent fixes ISSUE-0001 and ISSUE-0002 directly in SPEC.md

  5. Agent asks user:
     "SpecCritic requires clarification before I can proceed:
      Should rate limits apply per-user, per-IP, or per-API-key?"

  User: Per API key

  6. Agent updates SPEC.md to specify per-API-key rate limiting

  7. Runs speccritic again:
     verdict: VALID_WITH_GAPS
     ISSUE-0003 (WARN): "No retry-after header specified in 429 response"

  8. Agent documents the WARN as a known gap and proceeds to planning
     (or fixes it if time permits)
```

---

## Reference

| Resource | Path |
|----------|------|
| Specification | `specs/SPEC.md` |
| Implementation plan | `specs/PLAN.md` |
| Usage guide | `README.md` |
| Project guidance | `CLAUDE.md` |
