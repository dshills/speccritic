# SpecCritic

SpecCritic evaluates software specifications as formal contracts, identifying defects before implementation begins. It behaves like a hostile contract lawyer—not a collaborator—treating vague language, unverifiable requirements, and missing failure modes as bugs.

```
$ speccritic check SPEC.md --verbose
INFO: Loading spec: SPEC.md
INFO: Calling LLM: anthropic:claude-sonnet-4-6
INFO: Rendering output (format: json)

Verdict: INVALID  Score: 60/100  Critical: 2  Warn: 3  Info: 1
```

## Why

A specification is invalid when two competent engineers could implement it differently and both believe they followed it. SpecCritic enforces this contract before a single line of code is written.

**Intended workflow:**

1. Write or update `SPEC.md`
2. Run `speccritic check SPEC.md`
3. Fix issues (revise the spec or answer clarification questions)
4. Repeat until verdict is acceptable
5. Only then begin implementation

## Installation

```bash
go install github.com/dshills/speccritic/cmd/speccritic@latest
```

Or build from source:

```bash
git clone https://github.com/dshills/speccritic.git
cd speccritic
go build -ldflags "-X main.version=$(git describe --tags --always)" -o speccritic ./cmd/speccritic/
```

## Quick Start

Set your model and API key:

```bash
export SPECCRITIC_MODEL=anthropic:claude-sonnet-4-6
export ANTHROPIC_API_KEY=sk-ant-...
```

Run a check:

```bash
speccritic check SPEC.md
```

Output as Markdown:

```bash
speccritic check SPEC.md --format md
```

Fail in CI if the spec is invalid:

```bash
speccritic check SPEC.md --fail-on INVALID
```

## Configuration

### Model Selection

Set `SPECCRITIC_MODEL` to `provider:model`. If unset, defaults to `anthropic:claude-sonnet-4-6` with a warning to stderr.

| Provider | Env Var | Example |
|----------|---------|---------|
| `anthropic` | `ANTHROPIC_API_KEY` | `anthropic:claude-sonnet-4-6` |
| `openai` | `OPENAI_API_KEY` | `openai:gpt-4o` |

```bash
export SPECCRITIC_MODEL=openai:gpt-4o
export OPENAI_API_KEY=sk-...
```

### Flags

```
speccritic check <spec-file> [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `json` | Output format: `json` or `md` |
| `--out` | (stdout) | Write output to file |
| `--profile` | `general` | Evaluation profile (see [Profiles](#profiles)) |
| `--context` | (none) | Context file paths; can be repeated |
| `--strict` | `false` | Treat all unstated behavior as ambiguous |
| `--fail-on` | (none) | Exit 2 if verdict ≥ `VALID_WITH_GAPS` or `INVALID` |
| `--severity-threshold` | `info` | Minimum severity to include in output: `info`, `warn`, `critical` |
| `--patch-out` | (none) | Write suggested patches to file |
| `--temperature` | `0.2` | LLM temperature (0.0–2.0) |
| `--max-tokens` | `4096` | Maximum response tokens |
| `--offline` | `false` | Exit 3 if `SPECCRITIC_MODEL` is not set (CI enforcement) |
| `--verbose` | `false` | Print processing steps to stderr |
| `--debug` | `false` | Dump full prompt to stderr (use only in trusted environments) |

## Profiles

Profiles tune the evaluation for different specification types.

### `general` (default)

Applies to any software specification. Flags vague phrases (`fast`, `quickly`, `as needed`, `TBD`) and enforces that all failure modes and interfaces are defined.

### `backend-api`

Requires sections for Authentication, Error Codes, and Rate Limiting. Every endpoint must define request/response schemas. All error codes must be enumerated. Rate limits must be expressed as numeric values with time windows.

```bash
speccritic check SPEC.md --profile backend-api
```

### `regulated-system`

For specifications subject to compliance requirements. Requires sections for Audit Trail, Data Retention, and Access Control. Data retention periods must be concrete durations (e.g., "7 years", not "a reasonable period"). Every state transition must be enumerable and auditable.

```bash
speccritic check SPEC.md --profile regulated-system
```

### `event-driven`

For event-driven architectures. Requires sections for Event Schema, Delivery Guarantees, and Consumer Failure. Every event type must state delivery semantics (at-least-once vs. exactly-once). Consumer failure modes and retry policies must be specified.

```bash
speccritic check SPEC.md --profile event-driven
```

## Defect Categories

| Category | Description |
|----------|-------------|
| `NON_TESTABLE_REQUIREMENT` | Requirement cannot be verified by a test |
| `AMBIGUOUS_BEHAVIOR` | Two engineers could implement differently |
| `CONTRADICTION` | Two statements cannot both be true |
| `MISSING_FAILURE_MODE` | What happens when X fails is not stated |
| `UNDEFINED_INTERFACE` | A referenced interface has no specification |
| `MISSING_INVARIANT` | A property that must always hold is not stated |
| `SCOPE_LEAK` | Spec describes implementation, not behavior |
| `ORDERING_UNDEFINED` | Sequence of operations is ambiguous |
| `TERMINOLOGY_INCONSISTENT` | Same concept named differently |
| `UNSPECIFIED_CONSTRAINT` | Implicit constraint not made explicit |
| `ASSUMPTION_REQUIRED` | Must assume something unstated to implement |

## Verdicts and Scoring

### Verdicts

| Verdict | Meaning |
|---------|---------|
| `VALID` | No issues found; spec is consistent and testable |
| `VALID_WITH_GAPS` | Has WARN or INFO issues; implementation is possible but risky |
| `INVALID` | Has at least one CRITICAL issue or CRITICAL question; spec cannot be safely implemented |

### Score

Starts at 100, deducted per finding:

| Severity | Deduction |
|----------|-----------|
| CRITICAL | −20 |
| WARN | −7 |
| INFO | −2 |

Score is clamped at 0. Both score and verdict are computed before `--severity-threshold` filtering.

## Output Format

### JSON (default)

```json
{
  "tool": "speccritic",
  "version": "0.1.0",
  "input": {
    "spec_file": "SPEC.md",
    "spec_hash": "sha256:a3f1...",
    "context_files": [],
    "profile": "general",
    "strict": false,
    "severity_threshold": "info"
  },
  "summary": {
    "verdict": "INVALID",
    "score": 60,
    "critical_count": 2,
    "warn_count": 3,
    "info_count": 1
  },
  "issues": [
    {
      "id": "ISSUE-0001",
      "severity": "CRITICAL",
      "category": "NON_TESTABLE_REQUIREMENT",
      "title": "Performance requirement is not measurable",
      "description": "The spec requires the system to be 'fast' without defining metrics.",
      "evidence": [
        {
          "path": "SPEC.md",
          "line_start": 12,
          "line_end": 12,
          "quote": "The system must respond fast."
        }
      ],
      "impact": "No acceptance test can be written.",
      "recommendation": "Define a concrete latency target, e.g. P99 ≤ 200ms under 500 concurrent users.",
      "blocking": true,
      "tags": []
    }
  ],
  "questions": [...],
  "patches": [...],
  "meta": {
    "model": "anthropic:claude-sonnet-4-6",
    "temperature": 0.2
  }
}
```

> **Note:** `summary` counts always reflect all issues regardless of `--severity-threshold`. The `issues` array is filtered. The `input.severity_threshold` field records which filter was applied.

### Patches

When the LLM suggests corrections, they are included in the `patches` array and optionally written to `--patch-out` in diff-match-patch format:

```bash
speccritic check SPEC.md --patch-out spec.patch
```

Patches are advisory—they are minimal textual corrections, never wholesale rewrites.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success; verdict below `--fail-on` threshold (or no threshold set) |
| `2` | Verdict meets or exceeds `--fail-on` threshold |
| `3` | Input error: invalid flags, file not found, or `SPECCRITIC_MODEL` unset with `--offline` |
| `4` | Provider error: failed to create LLM provider (bad format, missing API key) |
| `5` | Model output invalid: LLM response failed schema validation after one retry |

## Context Files

Use `--context` to provide grounding documents that inform the evaluation without adding requirements:

```bash
speccritic check SPEC.md \
  --context glossary.md \
  --context architecture-overview.md \
  --context compliance-notes.md
```

Context is used for reference only—it is never used to infer requirements. Each file is redacted independently before being sent to the LLM.

## Strict Mode

In strict mode, all silence is treated as ambiguity:

```bash
speccritic check SPEC.md --strict
```

Any behavior not explicitly stated is flagged. Any assumption required to implement is filed as CRITICAL and tagged `assumption`. Use this for specifications that must be complete before any ambiguity is acceptable.

## Security and Privacy

- **Redaction** is always applied before the LLM call. The following patterns are replaced with `[REDACTED]` (line structure is preserved for accurate evidence citations):
  - PEM key blocks
  - AWS access key IDs (`AKIA...`)
  - API secret keys (`sk-...`)
  - JWT tokens
  - Bearer tokens (≥ 20 characters)
  - Inline password assignments
- **No telemetry.** Nothing is logged or transmitted beyond the LLM call.
- **`--debug`** dumps the full redacted prompt to stderr. Do not use in environments where stderr is captured in shared logs.

## CI Integration

```yaml
# GitHub Actions example
- name: Check specification
  env:
    SPECCRITIC_MODEL: anthropic:claude-sonnet-4-6
    ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
  run: |
    speccritic check SPEC.md \
      --offline \
      --fail-on INVALID \
      --severity-threshold warn \
      --out spec-review.json
```

The `--offline` flag ensures the run fails immediately (exit 3) if `SPECCRITIC_MODEL` is not set, preventing accidental use of the default model in CI.

## Development

```bash
# Run all tests
go test ./...

# Run a specific test
go test ./cmd/speccritic/... -run TestRunCheck_BadSpec_INVALID -v

# Build with version
go build -ldflags "-X main.version=0.1.0" -o speccritic ./cmd/speccritic/

# Code review (staged changes)
prism review staged
```

## Agentic Integration

See [WORKFLOW.md](WORKFLOW.md) for a detailed guide on integrating SpecCritic into an agentic coding system (Claude Code, Cursor, or any LLM-based agent), including:

- The canonical spec → plan → implement gate order
- How to parse JSON output and route on verdict
- Handling questions (user decisions) vs. issues (agent-fixable)
- `CLAUDE.md` snippet, pre-commit hook, and GitHub Actions CI job
- Anti-patterns and a full example agent session

## License

MIT — see [LICENSE](LICENSE)
