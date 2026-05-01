# SpecCritic Web Phase 0 Audit

## Current State

The repository already contains a working Go CLI implementation in `cmd/speccritic/main.go` and supporting packages under `internal/`.

The current CLI command owns orchestration directly in `runCheck`. It performs the full pipeline:

1. validate CLI flags,
2. resolve model configuration from `SPECCRITIC_LLM_PROVIDER` and `SPECCRITIC_LLM_MODEL`,
3. load the spec from disk with `internal/spec`,
4. redact raw and numbered spec content with `internal/redact`,
5. load context files with `internal/context`,
6. load profile rules with `internal/profile`,
7. build prompts with `internal/llm`,
8. construct an LLM provider with `internal/llm.NewProvider`,
9. call the provider and retry once on schema validation failure,
10. parse and validate model JSON with `internal/schema/validate`,
11. compute score and verdict with `internal/review`,
12. populate canonical report metadata,
13. filter emitted issues by severity threshold,
14. optionally generate patch output with `internal/patch`,
15. render JSON or Markdown with `internal/render`,
16. write stdout or `--out`,
17. apply `--fail-on` exit behavior.

## Existing Reusable Packages

The following packages are already reusable by a web layer:

| Package | Current Responsibility |
|---------|------------------------|
| `internal/spec` | File loading, hashing, line numbering, line count. |
| `internal/context` | Context file loading and prompt formatting. |
| `internal/redact` | Secret redaction. |
| `internal/profile` | Built-in profile lookup. |
| `internal/llm` | Provider abstraction, provider implementations, prompt construction. |
| `internal/schema` | Canonical report types and enums. |
| `internal/schema/validate` | JSON parsing, schema validation, evidence line bounds validation. |
| `internal/review` | Deterministic score, verdict, counts, severity filtering. |
| `internal/render` | JSON and Markdown report rendering. |
| `internal/patch` | Advisory patch diff generation. |

## Required Extraction

The web implementation should not duplicate `runCheck`. Phase 1 must extract the orchestration and retry logic into `internal/app`.

The CLI should keep its public behavior and call the shared app service. The web server should call the same app service with text-backed specs.

## Behavior That Must Stay Stable

- CLI command remains `speccritic check <spec-file>`.
- Existing CLI flags remain available.
- Existing environment-variable model configuration remains available.
- Default model warning behavior remains visible on stderr.
- `--offline` still fails with exit code 3 when provider/model configuration is missing.
- Provider creation failures still map to exit code 4.
- Invalid model output still maps to exit code 5.
- Input, render, and output failures still map to exit code 3.
- `--fail-on` still maps matching verdict thresholds to exit code 2.
- Severity filtering affects emitted issues only, not score, verdict, or summary counts.
- Patch write failures remain warnings and do not fail the main review.

## Phase 1 Target

Add `internal/app.Checker` with support for:

- file-backed specs for CLI use,
- text-backed specs for web use,
- injected providers for tests and web integration,
- shared retry and validation behavior,
- reusable rendered JSON, Markdown, and patch diff artifacts.

After Phase 1, `cmd/speccritic/main.go` should mostly own Cobra wiring, environment defaults, exit-code mapping, and output writes. Domain review behavior should live in `internal/app`.
