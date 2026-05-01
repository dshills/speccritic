# SpecCritic Web UI Implementation Plan

## Overview

This plan implements the web interface described in `specs/web/SPEC.md`.

The web UI must be a thin Go + HTMX layer over the same SpecCritic engine used by the CLI. The core implementation strategy is:

1. Extract a reusable check orchestration service if the CLI pipeline is not already reusable.
2. Build a local-only Go HTTP server.
3. Render the submitted spec as stable, line-numbered HTML.
4. Map SpecCritic evidence ranges to annotated lines.
5. Use HTMX partials for check submission, result updates, issue details, filtering, and exports.

The first production-quality milestone should be synchronous. Async job handling is valuable, but it should not be allowed to obscure the harder contract: CLI and web must produce the same review result for the same input and options.

## Implementation Principles

- The CLI remains authoritative for domain behavior.
- The web layer must not duplicate redaction, profile loading, prompt construction, LLM retry, schema validation, scoring, verdict calculation, or output rendering when reusable CLI code exists.
- All user-provided content rendered into HTML must pass through Go's contextual HTML escaping.
- Evidence line bounds must be validated before annotation rendering.
- Severity filtering must never change summary counts, score, verdict, or stored canonical JSON.
- The first version assumes local trusted use and binds to `127.0.0.1` by default.

## Spec Gaps and Decisions

| Gap | Decision |
|-----|----------|
| First version execution model | Implement synchronous `POST /checks` first. Add async status endpoints only after the synchronous path is tested. |
| Check ID requirement for synchronous mode | Even synchronous checks receive an in-memory check ID so issue detail and export endpoints have stable URLs. |
| Check ID format | Use 128 bits of cryptographically random data encoded as lowercase hex. |
| Result retention | Keep completed checks in memory only. Default maximum retained checks: 25. Evict oldest checks when the limit is exceeded. |
| Result retention TTL | Default completed-check TTL: 30 minutes. Expire checks by TTL as well as count. |
| Maximum upload size | Default 1 MiB, configurable. |
| Request timeout | Default 120 seconds, configurable. The request context must be passed through `internal/app` into the LLM provider so timeout cancellation stops the provider request. |
| Listen address | Default `127.0.0.1:8080`. |
| Styling | Use plain CSS served by Go. Do not add a CSS framework in the first version. |
| HTMX delivery | Serve a pinned local `htmx.min.js` asset or embed a vendored copy. Do not depend on a CDN for local use. |
| Markdown rendering | The spec text is rendered as escaped plain text lines, not parsed Markdown. This preserves line mapping. Prompt construction and web rendering must use the same canonical line-splitting helper from `internal/spec`. The helper must strip a leading UTF-8 BOM if present and normalize `\r\n` and `\r` to `\n` before hashing, line numbering, and UI rendering. |
| Trailing newlines | The canonical line splitter must treat a trailing newline as terminating the final content line, not as creating an additional empty line. Redaction, prompt construction, validation, and UI rendering must share this behavior. |
| Stored spec text | `CheckResult.OriginalSpec` stores the normalized spec text used for line numbering, not the pre-normalization byte stream. |
| Uploaded filename | Preserve for display and report metadata where appropriate, but never trust it as a filesystem path. |
| Context files | Defer browser support for context-file upload until the base spec flow is complete. Web calls must pass context text through `ContextDocuments`, never through server filesystem paths. |
| Patch application | Display and export patches only. Do not apply patches to local files in the first version. |
| Authentication | None for local-only first version. Binding to non-local interfaces must require explicit configuration and should be treated as a later hardening phase. |
| CSRF protection | Required even for local use. A malicious website can submit forms to a local server and consume provider quota. |
| Rate limiting | Add a simple per-process limit for LLM-backed check submissions per minute. |

## Target Directory Layout

```text
cmd/
  speccritic-web/
    main.go

internal/
  app/
    check.go
    check_test.go

  web/
    server.go
    routes.go
    handlers.go
    handlers_test.go
    config.go
    validation.go
    validation_test.go
    annotations.go
    annotations_test.go
    store.go
    store_test.go
    assets/
      htmx.min.js
      style.css
    templates/
      layout.html
      index.html
      partial_status.html
      partial_result.html
      partial_summary.html
      partial_issue_list.html
      partial_issue_detail.html
      partial_annotated_spec.html
      partial_error.html
```

`internal/app` is the shared orchestration boundary. It should depend on existing domain packages. `internal/web` should depend on `internal/app`, schema/review types, and renderers only where needed.

## Phase 0 — Baseline Audit

### Goals

- Confirm the current CLI pipeline shape.
- Identify the smallest extraction needed for shared web/CLI execution.
- Avoid web-specific shortcuts that fork domain behavior.

### Tasks

1. Inspect the current CLI entry point and internal packages.
2. Identify where these steps currently live:
   - spec loading,
   - context loading,
   - redaction,
   - profile loading,
   - prompt construction,
   - provider call,
   - repair retry,
   - JSON/schema validation,
   - evidence bounds validation,
   - scoring and verdict calculation,
   - severity filtering,
   - JSON/Markdown rendering,
   - patch rendering.
3. Record any behavior that differs from `specs/SPEC.md` or `specs/web/SPEC.md`.
4. Add or update package-level tests around existing behavior before extraction if coverage is thin.

### Acceptance Criteria

- There is a written inventory of reusable pipeline stages in code comments, issue notes, or the implementation PR.
- The extraction target for Phase 1 is clear.
- No web handlers exist yet unless the shared engine boundary is already usable.

## Phase 1 — Shared Check Orchestration

### Goals

Create an `internal/app` service that both CLI and web can call.

### Proposed API

```go
package app

type CheckRequest struct {
    SpecPath          string
    SpecName          string
    SpecText          string
    ContextPaths      []string
    ContextDocuments  []ContextDocument
    Profile           string
    Strict            bool
    SeverityThreshold string
    Temperature       float64
    MaxTokens         int
    Offline           bool
    Debug             bool
    Source            Source
}

type Source string

const (
    SourceCLI Source = "cli"
    SourceWeb Source = "web"
)

type ContextDocument struct {
    Name string
    Text string
}

type CheckResult struct {
    Report       schema.Report
    Markdown     string
    PatchDiff    string
    OriginalSpec string // normalized text used for line numbering and UI rendering
    LineCount    int
}

type Checker interface {
    Check(ctx context.Context, req CheckRequest) (*CheckResult, error)
}
```

The exact fields may differ to match the existing code, but the service must support both file-backed CLI checks and text-backed web checks.

When `Source` is `SourceWeb`, `SpecPath` and `ContextPaths` must be rejected. Web handlers must pass uploaded content through `SpecText` and future context uploads through `ContextDocuments`.

The package should expose constructors for CLI and web requests so callers do not assemble ambiguous source combinations by hand.

`SeverityThreshold` is recorded on report input metadata but must not remove findings from `CheckResult.Report`. CLI output filtering and web render filtering must operate on copies so the retained canonical report always contains all validated findings.

### Tasks

1. Add `internal/app`.
2. Move pipeline orchestration out of the CLI command into `internal/app`.
3. Support file-backed specs for CLI behavior.
4. Support text-backed specs for web behavior.
5. Ensure redaction is applied before LLM calls for both paths.
6. Ensure redaction is line-preserving. Redaction may replace characters within a line, but must not add or remove newline characters.
7. Ensure line numbering and evidence validation use the original submitted spec line count.
8. Ensure score and verdict are computed by internal review logic.
9. Ensure severity threshold filtering is applied only to emitted issue lists, not summary counts.
10. Update CLI command to call `internal/app`.
11. Preserve existing CLI flags, stdout/stderr behavior, and exit codes.

### Tests

- File-backed check with mock provider returns expected canonical report.
- Text-backed check with mock provider returns equivalent canonical report.
- Web-source requests reject `SpecPath` and `ContextPaths`.
- Invalid evidence line bounds returns an invalid model output error.
- Severity threshold filters emitted issues without changing summary counts.
- Redaction occurs before prompt construction or provider call.
- CLI command still maps failures to the correct exit codes.

### Acceptance Criteria

- CLI output for existing test cases is unchanged except for intentional bug fixes.
- Web can call one shared service to perform a complete check.
- No web handler needs to know how prompts or providers work.

## Phase 2 — Web Server Skeleton

### Goals

Create a local Go HTTP server that can render the main page and serve local assets.

### Tasks

1. Add `cmd/speccritic-web/main.go`.
2. Add `internal/web.Config`.
3. Parse configuration from flags and environment:
   - `--addr`, default `127.0.0.1:8080`,
   - `--request-timeout`, default `120s`,
   - `--max-upload-bytes`, default `1048576`,
   - `--max-retained-checks`, default `25`,
   - `--retained-check-ttl`, default `30m`.
4. Add `internal/web.Server` with an `http.Handler`.
5. Add routes:
   - `GET /`,
   - `GET /assets/style.css`,
   - `GET /assets/htmx.min.js`.
6. Add base templates:
   - layout,
   - index page,
   - empty status/result/detail regions.
7. Add graceful server startup and shutdown.

### Tests

- Config defaults are correct.
- Invalid config values are rejected.
- `GET /` returns HTTP 200 and contains the check form.
- Asset routes return HTTP 200 and correct content types.

### Acceptance Criteria

- `go run ./cmd/speccritic-web` starts a server on `127.0.0.1:8080`.
- The browser shows the main form without requiring network assets.
- No LLM call is possible yet from the UI.

## Phase 3 — Request Validation and Synchronous Checks

### Goals

Implement `POST /checks` for uploaded spec files.

### Tasks

1. Add form fields:
   - spec file input,
   - profile select,
   - strict checkbox,
   - severity threshold select,
   - temperature input,
   - max tokens input,
   - hidden CSRF token.
2. Implement request parsing for `multipart/form-data`.
3. Require `spec_file`.
4. Reject empty specs.
5. Reject uploads larger than configured maximum.
6. Reject unsupported profile, severity, temperature, and token values.
7. Validate CSRF token before reading or processing uploaded content.
8. Ensure web calls to `internal/app.Checker` use `SpecText`, not `SpecPath`.
9. Create a context with request timeout.
10. Call `internal/app.Checker`.
11. Store successful results in the in-memory check store.
12. Return a result partial for HTMX requests.
13. Return a full page fallback for non-HTMX requests.
14. Return user-visible error partials for validation or check errors.

### Error Mapping

| Error | HTTP Status |
|-------|-------------|
| Missing spec upload | 400 |
| Empty spec | 400 |
| Oversized upload | 413 |
| Invalid option | 400 |
| Missing or invalid CSRF token | 403 |
| LLM/provider failure | 502 |
| Invalid model output | 502 |
| Request timeout | 504 |
| Unexpected internal error | 500 |

### Tests

- Uploaded spec succeeds with mock checker.
- Missing input returns 400.
- Oversized file returns 413.
- Invalid profile returns 400.
- Invalid severity threshold returns 400.
- Missing CSRF token returns 403.
- Invalid CSRF token returns 403.
- Timeout returns 504.
- Provider failure returns escaped error partial.

### Acceptance Criteria

- A user can upload one Markdown/text file and receive a rendered result.
- Validation failures are visible and do not crash the server.

CSRF implementation uses cryptographically strong random tokens stored in in-memory session state keyed by a `SameSite=Strict`, `HttpOnly` session cookie. `POST /checks` validates the submitted token against the session token before processing the request body.

## Phase 4 — In-Memory Check Store

### Goals

Provide stable check IDs for detail and export endpoints.

### Data Model

```go
type StoredCheck struct {
    ID          string
    CreatedAt   time.Time
    ExpiresAt   time.Time
    SpecName    string
    Result      *app.CheckResult
    Annotations AnnotatedSpec
}
```

`StoredCheck` must not duplicate the original spec string if `app.CheckResult` already retains it. The store should keep one string reference per retained check.

### Tasks

1. Implement a concurrency-safe store using `sync.RWMutex`.
2. Generate unguessable IDs using `crypto/rand`.
3. Store completed synchronous checks.
4. Evict oldest checks when the configured maximum is exceeded.
5. Expire checks older than the configured TTL.
6. Verify generated IDs do not already exist before insertion; retry generation on collision.
7. Run a background janitor goroutine that periodically removes expired checks, including during idle periods. The janitor must accept a `context.Context` or stop channel, listen for cancellation on every loop, and be cancelled by `internal/web.Server` shutdown and test cleanup.
8. Return not-found errors for unknown IDs.

### Tests

- IDs are non-empty and unique across a large sample.
- Store returns saved checks.
- Unknown ID returns not found.
- Eviction removes oldest checks.
- TTL expiration removes old checks.
- ID generation retries if a collision is detected.
- Janitor sweep removes expired checks without requiring new insertions.
- Concurrent reads and writes are race-safe under `go test -race`.

### Acceptance Criteria

- Result, issue detail, and export routes can retrieve checks by ID.
- Store has deterministic eviction behavior.

## Phase 5 — Annotation Engine

### Goals

Convert a check result into a render-ready annotated spec model.

### Proposed Types

```go
type AnnotatedSpec struct {
    Lines []AnnotatedLine
}

type AnnotatedLine struct {
    Number          int
    Text            string
    HighestSeverity schema.Severity
    IssueRefs       []FindingRef
    ElementID       string
}

type FindingRef struct {
    ID       string
    Kind     string // issue or question
    Severity schema.Severity
    Title    string
}
```

### Tasks

1. Use a shared line-splitting helper from `internal/spec` so prompt line numbering and UI rendering agree on trailing-newline behavior.
2. Create one `AnnotatedLine` per original line.
3. Map each issue evidence range to all affected lines.
4. Map each question evidence range to all affected lines.
5. Sort refs per line by severity, then lexical ID.
6. Compute the highest severity per line.
7. Apply severity threshold filtering at render-model construction time.
8. Ensure filtering does not mutate the stored canonical report.
9. Return a specific error if evidence bounds are invalid, even though app validation should already prevent this.

### Tests

- Single-line issue annotates one line.
- Multi-line issue annotates every line in the range.
- Multiple issues on one line are sorted correctly.
- Questions are included and marked as questions.
- Severity threshold hides lower-severity refs.
- Summary counts are not changed by filtering.
- Invalid evidence bounds return an error.
- HTML-sensitive spec text survives as plain text in the render model and is escaped by templates.

### Acceptance Criteria

- The renderer can show line-based annotations without inspecting raw report internals.
- Annotation behavior is deterministic.

## Phase 6 — Result Rendering and HTMX Partials

### Goals

Render summary, issue list, issue details, and annotated spec as server-side HTML partials.

### Tasks

1. Add summary partial:
   - verdict,
   - score,
   - severity counts,
   - profile,
   - strict mode,
   - severity threshold,
   - model.
2. Add issue list partial:
   - issues and questions above threshold,
   - severity marker,
   - category for issues,
   - line range,
   - blocking marker.
3. Add annotated spec partial:
   - line number,
   - escaped line text,
   - issue/question markers,
   - stable line IDs.
4. Add issue detail partial:
   - issue or question detail,
   - evidence,
   - impact/recommendation for issues,
   - why-needed/blocks for questions,
   - related patches.
   - a finding navigation list when the selected line has multiple attached findings.
5. Wire HTMX attributes:
   - form submission updates result region,
   - issue click updates detail region,
   - line click updates detail region,
   - severity filter updates result views without rerunning check.
6. Add a non-HTMX fallback path for direct navigation where practical.

### Tests

- Summary partial includes all required fields.
- Issue list includes questions and issues distinctly.
- Issue detail escapes user/model content.
- Annotated spec escapes raw spec text.
- Line IDs are stable and predictable.
- Multiple issue line click selects highest-severity issue by default.
- Severity filter rerender does not call checker.

### Acceptance Criteria

- The user can review a result entirely through server-rendered HTML and HTMX partial swaps.
- Clicking issue list items and annotated lines updates the detail panel.

## Phase 7 — Export Endpoints

### Goals

Expose canonical JSON, Markdown, and patch output for retained checks.

### Tasks

1. Implement `GET /checks/{id}/export.json`.
2. Implement `GET /checks/{id}/export.md`.
3. Implement `GET /checks/{id}/patch.diff`.
4. Set appropriate content types:
   - `application/json`,
   - `text/markdown; charset=utf-8`,
   - `text/x-diff; charset=utf-8`.
5. Set download-friendly `Content-Disposition` filenames using sanitized spec/check names.
6. Return 404 for missing checks.
7. Return 404 for patch export when no patches exist.

Filename sanitization must use a strict ASCII whitelist: letters, digits, underscore, hyphen, and dot. All other characters, including quotes and path separators, are replaced with underscore. `Content-Disposition` must include a quoted `filename` value built only from the sanitized name.

### Tests

- JSON export matches canonical report.
- Markdown export matches shared renderer output.
- Patch export returns patch diff when present.
- Patch export returns 404 when absent.
- Unknown check ID returns 404.
- Export filenames are sanitized.

### Acceptance Criteria

- A user can download or inspect the same review artifacts produced by the CLI.

## Phase 8 — Frontend Layout, Accessibility, and Usability

### Goals

Make the first version usable without introducing frontend complexity.

### Layout Requirements

- Desktop: two-column layout with annotated spec as the primary pane and issue/detail panels as secondary panes.
- Narrow viewport: stack form, summary, issue list, annotated spec, and detail panel.
- Keep line numbers visible.
- Prevent issue markers from overlapping spec text.
- Use severity labels in addition to color.

### Tasks

1. Add CSS for:
   - form controls,
   - summary region,
   - issue list,
   - annotated line table/list,
   - severity states,
   - detail panel,
   - responsive breakpoints.
2. Add accessible labels for all form inputs.
3. Add ARIA live region for check status and errors.
4. Ensure buttons and issue links are keyboard focusable.
5. Add focus management only if needed, using minimal progressive-enhancement JavaScript.
6. Test at viewport widths:
   - 390 px,
   - 768 px,
   - 1024 px,
   - 1440 px.

### Tests

- Template tests verify labels and ARIA live region exist.
- Manual browser check confirms no overlapping text at target widths.
- Keyboard-only flow can submit a check and open issue details.

### Acceptance Criteria

- The UI is readable and operable on mobile and desktop widths.
- Severity is not conveyed by color alone.

## Phase 9 — Optional Async Checks

### Goals

Add async processing for slow LLM calls after the synchronous path is stable.

### Routes

- `POST /checks`
- `GET /checks/{id}`
- `GET /checks/{id}/result`

### Tasks

1. Extend store to support statuses:
   - `queued`,
   - `running`,
   - `complete`,
   - `failed`.
2. Add background worker execution with context cancellation.
3. Return status partial from `POST /checks`.
4. Use HTMX polling against `GET /checks/{id}`.
5. Stop polling once status is `complete` or `failed`.
6. Render result via `GET /checks/{id}/result`.
7. Store sanitized error information for failed checks.
8. Ensure max retained checks applies to terminal checks first. Active queued or running checks must not be evicted unless they have timed out and transitioned to `failed`.
9. If capacity is reached and all retained checks are active, reject new submissions with HTTP 503 and a retryable user-visible message.

### Tests

- Submitted check enters queued/running state.
- Completed check returns result.
- Failed check returns error partial.
- Polling endpoint returns expected status fragments.
- Context timeout transitions check to failed.
- Eviction handles queued/running/terminal checks predictably.
- Active queued or running checks are not evicted while still within their timeout.
- New async submissions return 503 when the store is full of active checks.

### Acceptance Criteria

- Long-running checks no longer depend on a single HTTP request staying open.
- Synchronous implementation can remain available for tests and simple local use.

## Phase 10 — Security Hardening

### Goals

Make local use safe by default and prepare for future remote deployment.

### Tasks

1. Confirm server binds to `127.0.0.1` by default.
2. Add a startup warning if binding to `0.0.0.0`, `::`, or a non-loopback address.
3. Ensure logs never include:
   - raw spec text,
   - raw context text,
   - API keys,
   - prompts,
   - provider responses.
4. Ensure debug prompt display is unavailable in web UI unless explicitly configured.
5. Add security headers:
   - `X-Content-Type-Options: nosniff`,
   - `Referrer-Policy: no-referrer`,
   - `Content-Security-Policy: default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'`.
   Inline scripts and `hx-on` attributes must not be used unless they are replaced with CSP-compatible nonce handling.
6. Add Host-header validation middleware. By default it must accept only exact `host:port` matches for the configured listen port: `127.0.0.1:<port>`, `[::1]:<port>`, and `localhost:<port>`. If the server is explicitly configured with another listen host, requests must exactly match that configured host and port or an explicit allowed-host entry with port. Reject every other Host header before routing.
7. Validate `Origin` and `Referer` on state-changing requests when present. They must match the accepted local origin or an explicit allowed origin.
8. Ensure uploads are never written to disk.
9. Ensure filenames are never used as paths.

### Tests

- Security headers are present.
- Uploaded filename containing HTML or path separators is escaped/sanitized.
- Error rendering does not include sensitive test strings.
- Non-loopback bind warning is emitted.
- Invalid Host headers are rejected.
- Host headers with unconfigured hosts or ports are rejected.
- Cross-origin `Origin` and `Referer` headers are rejected for `POST /checks`.

### Acceptance Criteria

- The first version is safe for local use and does not accidentally expose sensitive text through logs or HTML.

## Phase 11 — End-to-End Integration

### Goals

Verify the complete browser-facing workflow using a mock LLM provider.

### Tasks

1. Add test fixtures:
   - known good spec,
   - known bad spec,
   - model response with issues,
   - model response with questions,
   - model response with patches,
   - invalid evidence response.
2. Add integration tests using `httptest.Server`.
3. Verify uploaded spec flow.
5. Verify issue detail flow.
6. Verify severity filter flow.
7. Verify export flow.
8. Verify invalid model output flow.
9. Verify a spec containing a sentinel secret reaches the mock LLM provider only in redacted form.

### Acceptance Criteria

- `go test ./...` passes.
- `go test -race ./internal/web ./internal/app` passes.
- Manual local run can complete a check using real provider configuration.

## Phase 12 — Documentation

### Goals

Document how to run, configure, and test the web UI.

### Tasks

1. Update `README.md` with a web UI section.
2. Document local run command:

```sh
go run ./cmd/speccritic-web
```

3. Document model environment variables shared with CLI.
4. Document web flags and defaults.
5. Document local-only security assumption.
6. Add screenshots only after UI stabilizes.

### Acceptance Criteria

- A new contributor can run the web UI locally using README instructions.
- The docs clearly state that the web UI shares the CLI review engine.

## Release Milestones

### Milestone A — Shared Engine Ready

Includes:

- Phase 0,
- Phase 1.

Outcome:

- CLI uses `internal/app`.
- Web implementation can start without duplicating review logic.

### Milestone B — Synchronous Web MVP

Includes:

- Phase 2,
- Phase 3,
- Phase 4,
- Phase 5,
- Phase 6,
- Phase 7.

Outcome:

- Paste/upload a spec.
- Run a check.
- View annotated lines.
- Click issues and lines for details.
- Export JSON/Markdown/patches.

### Milestone C — Usable Local Tool

Includes:

- Phase 8,
- Phase 10,
- Phase 11,
- Phase 12.

Outcome:

- The web UI is usable, tested, documented, and safe for local use.

### Milestone D — Async Upgrade

Includes:

- Phase 9.

Outcome:

- Slow LLM checks run in the background with HTMX polling.

## Validation Commands

Run after implementation changes:

```sh
go test ./...
go vet ./...
```

Run targeted web tests during development:

```sh
go test ./internal/web
go test ./internal/app
go test -race ./internal/web ./internal/app
```

Before committing code changes, run the repository-required review command:

```sh
prism review staged
```

For security-sensitive changes, run:

```sh
prism review staged --compare openai:gpt-5.2,gemini:gemini-3-flash-preview
```

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| CLI and web behavior diverge | Route both through `internal/app`; test equivalent CLI/web results with the same mock provider. |
| LLM calls exceed HTTP timeout | Implement synchronous first with clear timeout errors; add async Phase 9 once MVP is stable. |
| Annotation line mapping drifts | Render escaped plain text by line; do not parse Markdown for the annotated spec pane. |
| Severity filtering mutates canonical data | Store canonical report once; build filtered render models per request. |
| User-provided HTML executes in browser | Use `html/template`; never mark spec/model content as trusted HTML. |
| Sensitive data leaks into logs | Log metadata only; add tests with sentinel secrets. |
| Uploaded files are mishandled as paths | Treat uploads as streams; sanitize names for display only. |
| UI becomes JavaScript-heavy | Keep HTMX as the interaction layer; use small JS only for progressive enhancement. |

## First Implementation Slice

The smallest useful implementation slice is:

1. Extract `internal/app.Checker`.
2. Add `cmd/speccritic-web`.
3. Render `GET /`.
4. Implement upload-only `POST /checks` with synchronous mock-provider tests.
5. Render summary, issue list, and annotated spec.
6. Add issue detail partials.
7. Add JSON export.

File upload, patch export, async polling, and accessibility refinements can follow immediately after this slice without reworking the architecture.
