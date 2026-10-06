# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**SpecCritic** is a Go CLI tool that evaluates software specifications (SPEC.md files) as formal contracts, determining whether they are complete, internally consistent, unambiguous, testable, and safe to implement. It behaves like a "hostile contract lawyer" — not a collaborator.

The authoritative specification is in `specs/initial/SPEC.md`; each later feature has its own `specs/<feature>/SPEC.md`.

## Code Quality

After writing or modifying code, run `prism review staged` before committing.
If findings are severity high, fix them before proceeding.
For security-sensitive changes, use compare mode:
```sh
prism review staged --compare openai:gpt-5.2,gemini:gemini-3-flash-preview
```

## Build, Test, and Lint Commands

```sh
make build        # CLI binary
make build-all    # CLI and web binaries
make install      # install the CLI
make test         # go test ./...
make race         # tests with the race detector
make lint         # golangci-lint run ./...
make eval-plan    # what the accuracy eval would review (free)
make eval         # run the accuracy eval against the configured LLM (spends real money)
```

For a single test:
```sh
go test ./internal/<package> -run TestName
```

Run `make eval` before and after changing a prompt, the output schema, chunking, the model or effort, and compare the two `summary.md` files. See `eval/README.md`.

## Architecture

**Language:** Go. Feature specs live in `specs/<feature>/SPEC.md`: initial, preflight, chunking, incremental, convergence, completion, web.

**Package layout:**
```
cmd/speccritic/       # CLI entry point (cobra)
cmd/speccritic-web/   # Local web UI
cmd/speccritic-eval/  # Accuracy eval over eval/corpus
pkg/speccritic/       # Library facade over internal/app
internal/app/         # Check orchestration: the whole data flow below
internal/spec/        # Spec file reading, line-numbering, section parsing
internal/context/     # Optional grounding document loading
internal/redact/      # Secret redaction (keys, tokens, passwords → [REDACTED])
internal/profile/     # Profile rule loading (general, backend-api, regulated-system, event-driven)
internal/preflight/   # Deterministic checks run before any LLM call
internal/llm/         # Providers (Anthropic, OpenAI, Gemini), prompts, output schema, transient-error retries, usage meter
internal/chunk/       # Chunked review of large specs, merge and synthesis
internal/incremental/ # Reruns that review only changed sections
internal/verify/      # Second look at CRITICAL findings before the verdict
internal/evidence/    # Checks and re-anchors evidence quotes against the spec
internal/schema/      # Report types; schema/validate parses model output finding by finding
internal/review/      # Scoring and verdict
internal/cache/       # On-disk review cache keyed by a content hash
internal/convergence/ # Comparison with a previous report
internal/completion/  # Advisory completion patches from templates
internal/render/      # Output formatting (JSON / Markdown)
internal/patch/       # Patch diff generation
internal/eval/        # Eval corpus, grading and summaries
internal/web/         # Web UI handlers
```

**Data flow:**
1. Read spec + optional context files
2. Redact secrets (always before any LLM call)
3. Add line numbers to spec content
4. Run deterministic preflight; `--preflight-mode only` or a `gate` failure stops here
5. Build the system prompt (rules, severity rubric, profile rules) and the shared user prefix (context files, numbered spec, known preflight findings). Every call in a run starts with both, so providers serve them from the prompt cache
6. Look up the review cache; an unchanged review is returned with no LLM call
7. Review: one call, chunked calls plus synthesis for large specs, or changed ranges only for an incremental rerun. Output is JSON constrained by the provider where the model allows it. A cut-off response is continued; a response with nothing usable gets one repair call. Transient provider errors are retried
8. Parse finding by finding: invalid findings are dropped and counted, evidence quotes are checked and re-anchored, patch `before` text is copied from the spec
9. Verify CRITICAL findings with one more call (`--verify off` skips it)
10. Calculate score (start 100, −20 per CRITICAL, −7 per WARN, −2 per INFO, clamped at 0) and determine verdict; store the review in the cache
11. Apply convergence and completion if requested, then emit formatted output (stdout or `--out` file)

## CLI Interface

```
speccritic check SPEC.md [flags]
```

Key flags: `--format`, `--out`, `--context`, `--profile`, `--strict`, `--fail-on`, `--severity-threshold`, `--patch-out`, `--patches`, `--verify`, `--no-cache`, `--effort`, `--structured-output`, `--max-tokens`, `--chunking`, `--incremental-from`, `--preflight-mode`, `--offline`, `--verbose`, `--debug`. The README lists every flag.

Model selection: `SPECCRITIC_LLM_PROVIDER` and `SPECCRITIC_LLM_MODEL`, or `--llm-provider` and `--llm-model`. No temperature is sent to any provider.

Exit codes: `0` = acceptable, `2` = invalid per `--fail-on`, `3` = input error, `4` = LLM/provider error, `5` = invalid model output

## Core Invariants

- Verdict is deterministic: any CRITICAL issue forces verdict ≥ INVALID
- Redaction is always applied before any LLM call
- LLM output must be JSON-only matching the v1 schema — no prose
- Evidence references must include valid line bounds into the actual spec file
- Patches are advisory and minimal (never wholesale rewrites)
- Strict mode (`--strict`): silence = ambiguity; any required assumption → CRITICAL
- A cached review is served only when everything that shapes it is unchanged, including the speccritic build

## Testing Strategy

- **Unit tests:** redaction correctness, line-number integrity, schema validation failures, deterministic scoring, patch diff correctness, cache keys
- **Golden tests:** known-bad spec → expected CRITICAL findings; known-good spec → VALID verdict
- **Integration tests:** mock LLM provider, validate end-to-end CLI behavior including exit codes. CLI tests point `SPECCRITIC_CACHE_DIR` at a temp dir so they never read or write the user's cache
- **Accuracy eval:** `make eval` grades recall, severity and false alarms on clean specs against seeded defects

## Code Search Protocol

Use this decision tree — in order — before reading any source file:

### Structural questions → atlas (always first)
- "Where is X defined?" → `atlas find symbol X --agent`
- "What calls X?" → `atlas who-calls X --agent`
- "What does X call?" → `atlas calls X --agent`
- "What implements interface X?" → `atlas implementations X --agent`
- "Which tests cover X?" → `atlas tests-for X --agent`
- "What routes exist?" → `atlas list routes --agent`
- "What changed?" → `atlas index --since HEAD~1 && atlas stale --agent`

### Before reading a large file → summarize first
`atlas summarize file <path> --agent`
Only read the file directly if the summary is insufficient.

### Content/pattern questions → rg
- Error strings, log messages, string literals
- Comments, TODOs, inline notes
- Non-Go/TS files (YAML, SQL, Markdown)
- Unstaged files not yet indexed

### Never read source files to answer these questions
If atlas has the answer, do not use Read or Bash(cat).
Atlas is authoritative — its index is maintained by a PostToolUse hook on Write/Edit/MultiEdit.
