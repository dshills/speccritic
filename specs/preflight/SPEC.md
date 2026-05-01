# SpecCritic Deterministic Preflight Specification

## 1. Purpose

SpecCritic deterministic preflight is a fast, local validation pass that finds obvious specification defects before any LLM call.

Preflight exists to reduce latency, token usage, and repeated review cycles. It must catch defects that can be identified with deterministic parsing, rule matching, and structural checks. It must not replace the LLM review.

## 2. Goals

- Run before LLM review in CLI and web checks.
- Detect common spec defects without a model call.
- Return findings in the same issue/question model used by normal reviews.
- Allow users to fix obvious problems before spending LLM latency and tokens.
- Support profile-specific rule packs.
- Be deterministic, fast, testable, and safe to run repeatedly.

## 3. Non-Goals

- No semantic reasoning beyond explicit rule definitions.
- No inferred architecture advice.
- No full contradiction detection.
- No wholesale spec rewriting.
- No dependence on network, model providers, embeddings, or external services.
- No hidden mutation of the submitted spec.

## 4. User Workflow

1. User runs `speccritic check SPEC.md` or submits a spec through the web UI.
2. SpecCritic reads, redacts, and line-numbers the spec as it does today.
3. SpecCritic runs deterministic preflight rules.
4. If preflight finds blocking defects and the configured mode is `gate`, SpecCritic returns preflight findings without calling the LLM.
5. If the configured mode is `warn`, SpecCritic includes preflight findings and continues to the LLM review.
6. If preflight passes, SpecCritic continues to the LLM review.

## 5. CLI Interface

Existing `speccritic check` behavior remains the default except that preflight runs before the LLM.

New flags:

| Flag | Default | Description |
|------|---------|-------------|
| `--preflight` | `true` | Enable deterministic preflight checks. |
| `--preflight-mode` | `warn` | `warn` includes preflight findings and continues to LLM; `gate` skips LLM when blocking preflight defects exist; `only` runs preflight and never calls LLM. |
| `--preflight-profile` | same as `--profile` | Optional override for preflight rule pack. |

`--offline` behavior:

- If `--preflight-mode only`, no model configuration is required.
- If `--preflight-mode warn` or `gate` and an LLM call is needed, existing `--offline` behavior applies.

Exit codes:

- Existing exit codes remain unchanged.
- In `--preflight-mode only`, invalid preflight results use the same `--fail-on` and exit code `2` behavior as normal review results.
- Rule engine errors are input errors only when caused by invalid user configuration; built-in rule failures are internal errors.

## 6. Web Interface

The web UI must run preflight before LLM review using the same rule engine as the CLI.

The first version may expose no additional controls. It must use:

- `preflight=true`
- `preflight-mode=warn`
- `preflight-profile=<selected profile>`

If later exposed, web controls must be:

- a simple preflight enabled checkbox,
- a mode selector with `warn`, `gate`, and `only`.

The web result must distinguish preflight findings from LLM findings visually and in accessible text.

## 7. Rule Model

Each rule has:

- stable ID,
- title,
- description,
- default severity,
- category,
- profile applicability,
- matcher type,
- remediation hint,
- optional blocking flag.

Rule IDs use:

```text
PREFLIGHT-<GROUP>-<NNN>
```

Examples:

- `PREFLIGHT-STRUCTURE-001`
- `PREFLIGHT-VAGUE-001`
- `PREFLIGHT-TODO-001`

Rule output must map to existing `schema.Issue` fields:

- `id`
- `severity`
- `category`
- `title`
- `description`
- `evidence`
- `impact`
- `recommendation`
- `blocking`
- `tags`

Preflight findings must include tag `preflight`.

## 8. Required Rule Groups

### 8.1 Structural Completeness

Rules detect missing required sections or headings.

General profile should check for at least:

- purpose or goals,
- non-goals or out-of-scope behavior,
- requirements or functional behavior,
- acceptance criteria or testability section.

Backend API profile should additionally check for:

- authentication or authorization,
- endpoints or routes,
- request/response schemas,
- error responses,
- rate limits or abuse handling.

Regulated-system profile should additionally check for:

- audit trail,
- data retention,
- access control,
- compliance or regulatory constraints.

Event-driven profile should additionally check for:

- event schema,
- delivery guarantees,
- retry or dead-letter behavior,
- consumer failure behavior.

### 8.2 Placeholder and Incomplete Text

Rules detect placeholders such as:

- `TODO`
- `TBD`
- `FIXME`
- `???`
- `[placeholder]`
- `coming soon`
- `to be defined`

Each finding must point to the exact line containing the placeholder.

### 8.3 Vague or Non-Testable Language

Rules detect terms that often indicate unverifiable requirements:

- `fast`
- `quick`
- `reasonable`
- `as needed`
- `where appropriate`
- `user-friendly`
- `robust`
- `scalable`
- `secure`
- `intuitive`

The rule must avoid firing on quoted examples that explicitly describe bad wording if the line is clearly in an examples/anti-pattern section.

### 8.4 Undefined References

Rules detect likely undefined acronyms and terms:

- all-caps tokens with at least two letters,
- quoted domain terms introduced without a nearby definition,
- references like `the policy`, `the adapter`, or `the service` when no matching named entity appears earlier.

The first version may limit this group to all-caps acronyms.

### 8.5 Weak Requirements

Rules detect requirements that use non-binding language:

- `should`
- `may`
- `might`
- `can`
- `try to`
- `best effort`

These findings default to WARN unless strict mode is enabled. In strict mode, weak requirement findings default to CRITICAL.

### 8.6 Missing Measurable Criteria

Rules detect numeric-free requirements for common measurable domains:

- latency,
- throughput,
- availability,
- retry timing,
- timeout behavior,
- retention period,
- rate limit,
- file size limit.

If a line mentions one of these domains but contains no number, duration, percentage, size, or explicit enum, emit a finding.

## 9. Evidence Rules

Every preflight finding must include evidence with:

- path,
- line_start,
- line_end,
- quote.

Line bounds must be valid for the submitted spec.

For missing-section findings, evidence must point to:

- line 1 when no better location exists, or
- the nearest parent heading when the missing section belongs under an existing heading.

## 10. Scoring and Verdict

Preflight findings use the same scoring deductions as normal findings:

- CRITICAL: -20
- WARN: -7
- INFO: -2

Verdict calculation remains deterministic:

- any CRITICAL preflight issue forces verdict at least `INVALID`,
- WARN/INFO-only preflight findings produce `VALID_WITH_GAPS` unless LLM findings make the verdict worse.

When preflight and LLM both run, duplicate findings must be deduplicated before final scoring.

## 11. Deduplication

The first version must deduplicate exact same-line findings with the same category and equivalent title.

Later versions may deduplicate semantically similar preflight and LLM findings.

If a preflight finding and an LLM finding overlap:

- retain the LLM finding as canonical,
- add tag `preflight-confirmed` to the retained finding,
- do not double-count score deductions.

## 12. Performance Requirements

Preflight must be fast enough to run on every check.

Targets:

- specs up to 10,000 lines: under 250 ms on a typical laptop,
- no network calls,
- no model calls,
- memory usage proportional to spec size.

## 13. Package Design

Add:

```text
internal/preflight/
```

Responsibilities:

- rule definitions,
- matcher execution,
- profile filtering,
- strict-mode severity adjustment,
- finding generation,
- evidence bounds validation,
- deduplication helpers.

The package must not import provider or LLM packages.

Suggested public API:

```go
type Config struct {
    Enabled bool
    Mode string
    Profile string
    Strict bool
}

type Result struct {
    Issues []schema.Issue
}

func Run(spec spec.Document, cfg Config) (Result, error)
```

Exact types may differ to match existing code.

## 14. Rendering

JSON output must include preflight findings in the existing `issues` array with tag `preflight`.

Markdown output must include a `Preflight Findings` subsection when any preflight finding exists.

Web output must visually distinguish preflight findings using the existing severity presentation plus a `Preflight` label.

## 15. Testing Requirements

Unit tests must cover:

- placeholder detection,
- vague-language detection,
- weak requirement detection,
- missing-section detection by profile,
- acronym detection,
- measurable-criteria detection,
- strict-mode severity escalation,
- evidence line bounds,
- no false positive for anti-pattern examples,
- deterministic ordering.

Integration tests must cover:

- `--preflight-mode only` never calls the LLM,
- `--preflight-mode gate` skips LLM when blocking findings exist,
- `--preflight-mode warn` still calls LLM,
- web checks include preflight findings,
- scoring includes preflight findings exactly once.

## 16. Acceptance Criteria

The feature is complete when:

- CLI checks run preflight before LLM review by default.
- Users can run preflight-only checks without model configuration.
- Obvious TODO/vague/weak/missing-section defects are reported with valid line evidence.
- Web checks display preflight findings alongside LLM findings.
- Existing CLI behavior remains compatible for users who do not change flags.
- Preflight tests are deterministic and do not require network or LLM credentials.
