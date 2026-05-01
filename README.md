# SpecCritic

[![Go Reference](https://pkg.go.dev/badge/github.com/dshills/speccritic.svg)](https://pkg.go.dev/github.com/dshills/speccritic)

SpecCritic evaluates software specifications as formal contracts, identifying defects before implementation begins. It behaves like a hostile contract lawyer—not a collaborator—treating vague language, unverifiable requirements, and missing failure modes as bugs.

```
$ speccritic check SPEC.md --verbose
INFO: Loading spec: SPEC.md
INFO: Calling LLM: anthropic:claude-sonnet-4-20250514
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

Run a fast deterministic preflight check without model credentials:

```bash
speccritic check SPEC.md --preflight-mode only
```

Use this first when iterating on a spec. It catches obvious placeholders, vague language, weak requirements, missing sections, undefined acronyms, and unmeasurable criteria locally before making an LLM call.

Set your model and API key when you are ready for a full review:

```bash
export SPECCRITIC_LLM_PROVIDER=anthropic
export SPECCRITIC_LLM_MODEL=claude-sonnet-4-20250514
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

Gate CI before any LLM call:

```bash
speccritic check SPEC.md --preflight-mode gate --fail-on INVALID
```

Force parallel section chunking for a large spec:

```bash
speccritic check SPEC.md --chunking on --chunk-concurrency 4
```

## Web UI

SpecCritic also includes a local Go web UI for reviewing specs in the browser. It uses the same review pipeline as the CLI, then renders the uploaded spec with line numbers, summary metrics, finding annotations, provider/model metadata, and modal issue details.

The web UI is intended for local review sessions. It does not replace the CLI and it does not change CLI behavior. Large uploaded specs use the same automatic chunked review path as the CLI and still render as one merged result.

Set the same provider configuration used by the CLI:

```bash
export SPECCRITIC_LLM_PROVIDER=anthropic
export SPECCRITIC_LLM_MODEL=claude-sonnet-4-20250514
export ANTHROPIC_API_KEY=sk-ant-...
```

Run the web UI:

```bash
make run-web
```

Then open:

```text
http://127.0.0.1:8080
```

From the browser:

1. Choose a Markdown or text spec file. Manual text entry is intentionally not supported.
2. Select a profile and severity threshold.
3. Optionally enable strict mode or disable the default preflight pass.
4. Click `Check spec`.

The left pane shows the configured provider and model before the review starts. The `Check spec` button is disabled until a file is selected and remains disabled while a check is running. During review, the page shows a running indicator and elapsed timer. When the check completes, findings are shown beside the annotated spec; deterministic findings are labeled `Preflight`, and clicking any finding opens its detail in a modal so the annotated document stays in place.

Use a different address or port with `WEB_ADDR`:

```bash
make run-web WEB_ADDR=127.0.0.1:8081
```

Build, install, or run the web binary directly:

```bash
make build-web
make install-web
go run ./cmd/speccritic-web --addr 127.0.0.1:8080
```

`make build-all` builds both `bin/speccritic` and `bin/speccritic-web`.

For live local development with [Air](https://github.com/air-verse/air), this repository includes `.air.toml` configured for the web server:

```bash
air
```

The Air config builds `./cmd/speccritic-web` into `./tmp/speccritic-web` and runs it on `127.0.0.1:8090`.

## Configuration

### Model Selection

Set `SPECCRITIC_LLM_PROVIDER` and `SPECCRITIC_LLM_MODEL`. If unset, SpecCritic defaults to `SPECCRITIC_LLM_PROVIDER=anthropic` and `SPECCRITIC_LLM_MODEL=claude-sonnet-4-20250514` with a warning to stderr. Preflight-only checks do not require model configuration.

Current builds read the split provider/model variables. If you have old shell or CI snippets that set `SPECCRITIC_MODEL=provider:model`, replace them with the two variables above.

| Provider | API Key Env Var | Model Value Example |
|----------|-----------------|-----------------------|
| `anthropic` | `ANTHROPIC_API_KEY` | `claude-sonnet-4-20250514` |
| `openai` | `OPENAI_API_KEY` | `gpt-4o` |

```bash
export SPECCRITIC_LLM_PROVIDER=openai
export SPECCRITIC_LLM_MODEL=gpt-4o
export OPENAI_API_KEY=sk-...
```

### Preflight

Preflight is a deterministic local pass that runs before the LLM by default. It is designed to reduce review latency, token usage, and repeated model round trips by catching high-signal defects immediately.

Preflight findings use the same issue schema as LLM findings and participate in the same final scoring and verdict calculation. When the LLM confirms a preflight finding, SpecCritic deduplicates the result and tags the LLM issue with `preflight-confirmed` and `preflight-rule:<ID>` instead of showing the same defect twice.

Modes:

| Mode | Behavior |
|------|----------|
| `warn` | Include preflight findings in the final report and continue to the LLM. This is the default. |
| `gate` | Skip the LLM when blocking preflight findings exist. A finding is blocking when its rule or finding marks it blocking, and all CRITICAL preflight findings are blocking by default. |
| `only` | Run only deterministic preflight checks. No model credentials are required. |

Recommended workflow:

```bash
speccritic check SPEC.md --preflight-mode only
# fix deterministic findings
speccritic check SPEC.md --preflight-mode gate --fail-on INVALID
# when preflight is clean enough, run the full review
speccritic check SPEC.md
```

Suppress a known deterministic false positive with `--preflight-ignore`:

```bash
speccritic check SPEC.md --preflight-ignore PREFLIGHT-ACRONYM-001
```

Useful preflight behavior:

- Redaction still runs before any prompt is built.
- `--preflight-mode only` does not require `SPECCRITIC_LLM_PROVIDER`, `SPECCRITIC_LLM_MODEL`, or provider API keys.
- `--preflight-mode gate` is useful in CI when obvious blocking defects should prevent any provider call.
- `--preflight-profile` defaults to `--profile`; override it only when deterministic checks need a different profile than the LLM review.

### Chunked Review

Chunked review is an execution strategy for large specs. It splits the redacted spec by Markdown sections, reviews chunks with bounded parallel LLM calls, validates each chunk against the same schema and evidence rules, optionally runs one cross-section synthesis pass, and merges everything back into one normal report.

Small specs still use the existing single-call path by default.

The final output remains a normal SpecCritic report. Chunk internals are not rendered as separate reports, but chunk-related tags may appear on findings:

| Tag | Meaning |
|-----|---------|
| `chunked-review` | Finding came from chunked review rather than the single-call path. |
| `chunk:<CHUNK-ID>` | Source chunk that emitted the finding. |
| `cross-section` | Chunk reviewer believed the finding depends on another section. |
| `synthesis` | Finding came from the cross-section synthesis pass. |

Modes:

| Mode | Behavior |
|------|----------|
| `auto` | Use chunking when the spec has at least `--chunk-min-lines` lines or the estimated prompt is at least `--chunk-token-threshold` tokens. This is the default. |
| `on` | Force chunking whenever an LLM review is needed. |
| `off` | Always use the original single-call LLM path. |

Examples:

```bash
# Force chunking for a large spec and allow four concurrent chunk calls.
speccritic check SPEC.md --chunking on --chunk-concurrency 4

# Disable chunking while debugging prompt behavior.
speccritic check SPEC.md --chunking off --debug

# Tune for a rate-limited provider.
speccritic check SPEC.md --chunk-concurrency 1 --chunk-lines 140
```

Chunking usually reduces wall-clock latency for large specs, but it may increase the total number of provider calls. Provider rate limits, low concurrency, and cross-section synthesis can reduce the speedup. Cross-section defects are still hard: chunk prompts receive a table of contents and summaries, and synthesis can catch contradictions across sections, but no chunking strategy is a substitute for a well-structured spec.

Implementation details:

- Chunking happens after spec loading, redaction, and preflight.
- `auto` mode uses a deterministic local estimate of one token per four UTF-8 bytes. This is a rough heuristic; code-heavy specs and non-English specs may need a lower `--chunk-token-threshold` or forced `--chunking on`.
- Chunk reviews cite only their primary line range; overlap lines are context only.
- Every chunk response must include `meta.chunk_summary`; summaries are used for synthesis and are not shown as user-facing output.
- Chunk calls run with bounded concurrency.
- If one chunk fails permanently after the built-in repair attempt, the check fails with model-output/provider error rather than returning partial results.
- Synthesis runs when chunked review has findings or when the spec is at least `--synthesis-line-threshold` lines. A no-finding chunked review below that threshold skips synthesis.

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
| `--fail-on` | (none) | Exit 2 if verdict meets or exceeds the threshold; valid values are case-sensitive `VALID_WITH_GAPS` or `INVALID` |
| `--severity-threshold` | `info` | Minimum severity to include in output: `info`, `warn`, `critical` |
| `--patch-out` | (none) | Write suggested patches to file |
| `--temperature` | `0.2` | LLM temperature (0.0–2.0) |
| `--max-tokens` | `4096` | Maximum response tokens |
| `--offline` | `false` | Exit 3 if LLM provider/model env vars are not set (CI enforcement) |
| `--verbose` | `false` | Print processing steps to stderr |
| `--debug` | `false` | Dump full prompt to stderr (use only in trusted environments) |
| `--preflight` | `true` | Run deterministic checks before LLM review |
| `--preflight-mode` | `warn` | Preflight mode: `warn`, `gate`, or `only` |
| `--preflight-profile` | same as `--profile` | Override the preflight rule profile |
| `--preflight-ignore` | (none) | Suppress a preflight rule ID; can be repeated |
| `--chunking` | `auto` | Chunking mode: `auto`, `on`, or `off` |
| `--chunk-lines` | `180` | Target maximum source lines per chunk before overlap |
| `--chunk-overlap` | `20` | Neighboring lines included before and after each chunk for context |
| `--chunk-min-lines` | `120` | Minimum line count before `auto` may use chunking |
| `--chunk-token-threshold` | `4000` | Estimated prompt-token count before `auto` may use chunking |
| `--chunk-concurrency` | `3` | Maximum concurrent chunk LLM calls |
| `--synthesis-line-threshold` | `240` | Minimum total line count before a no-finding chunked review may run synthesis |

Chunking environment defaults are also supported when the matching flag is not provided:

| Env Var | Matching Flag |
|---------|---------------|
| `SPECCRITIC_CHUNKING` | `--chunking` |
| `SPECCRITIC_CHUNK_LINES` | `--chunk-lines` |
| `SPECCRITIC_CHUNK_OVERLAP` | `--chunk-overlap` |
| `SPECCRITIC_CHUNK_MIN_LINES` | `--chunk-min-lines` |
| `SPECCRITIC_CHUNK_TOKEN_THRESHOLD` | `--chunk-token-threshold` |
| `SPECCRITIC_CHUNK_CONCURRENCY` | `--chunk-concurrency` |
| `SPECCRITIC_SYNTHESIS_LINE_THRESHOLD` | `--synthesis-line-threshold` |

Validation rules:

- `--chunk-lines` must be greater than `0`.
- `--chunk-overlap` must be `>= 0` and less than `--chunk-lines`.
- `--chunk-min-lines` must be `>= 0`.
- `--chunk-token-threshold` must be greater than `0`.
- `--chunk-concurrency` must be between `1` and `16`.
- `--synthesis-line-threshold` must be `>= 0`.

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
    "model": "anthropic:claude-sonnet-4-20250514",
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
| `3` | Input error: invalid flags, file not found, or LLM provider/model env vars unset with `--offline` |
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
    SPECCRITIC_LLM_PROVIDER: anthropic
    SPECCRITIC_LLM_MODEL: claude-sonnet-4-20250514
    ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
  run: |
    speccritic check SPEC.md \
      --offline \
      --fail-on INVALID \
      --severity-threshold warn \
      --out spec-review.json
```

The `--offline` flag ensures the run fails immediately (exit 3) if `SPECCRITIC_LLM_PROVIDER` and `SPECCRITIC_LLM_MODEL` are not set, preventing accidental use of the default model in CI.

For a credentials-free deterministic gate:

```yaml
- name: Preflight specification
  run: |
    speccritic check SPEC.md \
      --preflight-mode only \
      --fail-on INVALID
```

## Development

```bash
# Run all tests
make test

# Run a specific test
go test ./cmd/speccritic/... -run TestRunCheck_BadSpec_INVALID -v

# Build CLI and web binaries
make build-all

# Run the local web UI
make run-web

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

### Claude Code Skill

A ready-to-install Claude Code skill lives in [`examples/claude-code-skill/`](examples/claude-code-skill/). It teaches Claude Code when to invoke `speccritic`, how to parse `.speccritic-review.json`, and how to route CRITICAL issues (fix in place) vs. CRITICAL questions (ask the user). Install with:

```bash
mkdir -p ~/.claude/skills/speccritic
cp examples/claude-code-skill/SKILL.md ~/.claude/skills/speccritic/SKILL.md
```

See the [skill README](examples/claude-code-skill/README.md) for project-level install, prerequisites, and customization.

## License

MIT — see [LICENSE](LICENSE)
