# SpecCritic Web UI Specification

## 1. Purpose

SpecCritic Web is a browser-based interface for running SpecCritic against a software specification and viewing the resulting feedback as annotations on the original spec text.

The web UI must make it easier to inspect findings in context. It does not replace the SpecCritic CLI contract. It must use the same review pipeline, scoring rules, issue schema, redaction behavior, and verdict logic as the CLI.

## 2. Goals

- Allow a user to paste or upload a spec document.
- Run a SpecCritic check from a Go HTTP server.
- Render the submitted spec with stable line numbers.
- Annotate spec lines that are referenced by SpecCritic evidence.
- Show issue, question, verdict, score, and patch details without losing the user's place in the spec.
- Use HTMX for interactive server-rendered updates.
- Preserve the existing CLI behavior and internal package boundaries where practical.

## 3. Non-Goals

- No multi-user accounts in the first version.
- No persistent project database in the first version.
- No collaborative editing.
- No browser-side LLM calls.
- No wholesale spec rewriting.
- No replacement of the CLI command.
- No custom JavaScript framework.
- No authentication or authorization unless the server is explicitly configured for remote deployment.

## 4. Users

The primary user is a developer or coding agent who has a draft `SPEC.md` and wants to see whether it is safe to implement.

The first version assumes the server is run locally by a trusted user. Remote, shared, or hosted deployments require additional security controls defined in a later spec.

## 5. Core Workflow

1. User opens the web UI.
2. User pastes spec text into a textarea or uploads a single spec file.
3. User chooses review options.
4. User starts a check.
5. Server validates the request.
6. Server redacts secrets before any LLM call.
7. Server runs the same SpecCritic review pipeline used by the CLI.
8. Server validates model output and evidence line bounds.
9. Server renders the original submitted spec with line numbers and annotations.
10. User clicks an annotated line or issue item to inspect the finding.
11. User may copy or download JSON, Markdown, or patch output.

## 6. HTTP Interface

### 6.1 Pages

`GET /`

Returns the main web UI shell. The shell includes:

- spec input form,
- review option controls,
- empty status region,
- empty verdict region,
- empty annotated spec region,
- empty issue detail region.

### 6.2 Check Submission

`POST /checks`

Accepts either:

- `spec_text`: raw pasted spec text, or
- `spec_file`: one uploaded text or Markdown file.

The request may include:

- `profile`: one of `general`, `backend-api`, `regulated-system`, or `event-driven`.
- `strict`: boolean.
- `severity_threshold`: one of `info`, `warn`, or `critical`.
- `temperature`: decimal value from `0.0` to `2.0`.
- `max_tokens`: integer greater than `0`.
- `csrf_token`: server-issued form token.

Validation rules:

- Exactly one of `spec_text` or `spec_file` must be provided.
- Empty specs are rejected.
- Uploaded files larger than 1 MiB are rejected.
- The server must wrap request bodies with `http.MaxBytesReader` before parsing multipart forms so upload limits are enforced before buffering.
- Uploaded file names are displayed only after HTML escaping.
- Unsupported profile, severity, temperature, or token values are rejected.
- Missing or invalid CSRF tokens are rejected.

For the first version, the server may process the check synchronously. If processing exceeds the configured request timeout, the server must return a recoverable error message instead of a partial review.

### 6.3 Async Check Status

Async checks are optional in the first version. If implemented, the following endpoints must be used:

`POST /checks`

Creates a check and returns a status partial containing the check ID.

`GET /checks/{id}`

Returns the current check status as an HTMX partial. Status values are:

- `queued`
- `running`
- `complete`
- `failed`

`GET /checks/{id}/result`

Returns the annotated spec, summary, and issue list partials for a completed check.

Check IDs must be unguessable if results are retained after the HTTP request finishes.

### 6.4 Issue Details

`GET /checks/{id}/issues/{issue_id}`

Returns an issue detail partial for a single issue. The partial includes:

- severity,
- category,
- title,
- description,
- evidence,
- impact,
- recommendation,
- blocking value,
- tags,
- related questions if any,
- related patches if any.

Unknown check IDs or issue IDs return a user-visible not-found partial and HTTP 404.

`issue_id` values come from the validated canonical SpecCritic issue or question IDs for the stored report. Filtering and rerendering must not renumber or rewrite those IDs.

### 6.5 Export

`GET /checks/{id}/export.json`

Returns canonical SpecCritic JSON for the check.

`GET /checks/{id}/export.md`

Returns Markdown output for the check.

`GET /checks/{id}/patch.diff`

Returns advisory patch output when patches exist. If no patches exist, the endpoint returns HTTP 404 with a user-visible message.

## 7. HTMX Behavior

The web UI must work with server-rendered HTML partials.

Required interactions:

- Submitting the check form updates the status region.
- Completing a check updates the verdict, issue list, and annotated spec regions.
- Clicking an issue in the issue list updates the issue detail region.
- Clicking an annotated spec line updates the issue detail region with the highest-severity issue attached to that line.
- Changing the severity filter updates the issue list and annotated spec without rerunning the LLM review.

The server must not require a client-side JavaScript framework for these interactions.

Small progressive-enhancement JavaScript may be used only for local UI behavior that HTMX does not provide directly, such as preserving scroll position or focusing a selected line.

## 8. Annotation Requirements

The server must render the submitted spec as line-based HTML.

Each rendered line must include:

- 1-based line number,
- original line text,
- zero or more issue markers,
- stable HTML element ID derived from the line number.

For each issue evidence range:

- every line from `line_start` through `line_end` must be marked as annotated,
- severity class must reflect the highest severity affecting that line,
- line annotations must link back to the issue or question that produced them.

If multiple issues affect the same line:

- the line must show the highest severity visibly,
- the detail panel must make all attached issues discoverable,
- clicking the line must show all attached findings in the detail panel, with the highest-severity finding expanded or selected by default,
- issue order must be `CRITICAL`, then `WARN`, then `INFO`, then lexical issue ID.

Evidence with invalid line bounds must be rejected during review validation before rendering. The renderer must not silently clamp invalid evidence.

## 9. Result Summary

The summary region must display:

- verdict,
- score,
- critical count,
- warn count,
- info count,
- selected profile,
- strict mode state,
- severity threshold,
- model name when available.

Summary counts must reflect all findings before severity-threshold filtering, matching CLI behavior.

## 10. Issue List

The issue list must include all issues and questions that pass the selected severity threshold.

Each item must show:

- severity,
- category for issues,
- title or question text,
- evidence line range,
- blocking value when true.

Questions must be visually distinct from issues. Questions are blocking clarification requests, not recommendations.

## 11. Patch Display

When patches are present:

- the UI must show that patches are advisory,
- each patch must reference its related issue,
- patches must be rendered as escaped text or syntax-highlighted diff output,
- the UI must not apply patches to local files in the first version.

## 12. Error Handling

The web UI must render user-visible errors for:

- missing spec input,
- both pasted text and uploaded file provided,
- oversized upload,
- invalid option values,
- missing model configuration when offline behavior requires it,
- LLM provider failure,
- invalid model JSON,
- schema validation failure,
- invalid evidence line bounds,
- request timeout.

Errors must not expose API keys, raw provider credentials, or unredacted prompt content.

## 13. Security and Privacy

The server must HTML-escape all user-provided content before rendering.

Redaction must happen before any LLM call, matching CLI behavior.

The server must not log unredacted spec text, context text, API keys, provider responses, or prompts by default.

The server may log:

- request method and path,
- check status transitions,
- elapsed duration,
- verdict,
- issue counts,
- provider name without credentials.

Debug prompt output must be disabled by default. If exposed in the web UI later, it must require explicit local-only configuration and must display only redacted prompts.

## 14. Persistence

First version persistence is in-memory only.

If async checks are implemented:

- completed checks may be retained for the lifetime of the process,
- retained checks must have a configurable maximum count,
- oldest retained checks must be evicted when the maximum is exceeded.

The first version must not write uploaded specs, review results, prompts, or patches to disk unless the user explicitly uses an export endpoint.

Retained checks must expire after a configurable TTL as well as a configurable maximum count.

## 15. Shared Engine Requirements

The web server must call the same internal review pipeline as the CLI.

Web handlers must pass pasted or uploaded spec content as text, not as server filesystem paths. File-backed spec and context paths are reserved for trusted CLI use.

The implementation must avoid duplicating:

- redaction logic,
- line numbering logic,
- profile loading,
- prompt construction,
- LLM retry behavior,
- schema validation,
- evidence validation,
- scoring,
- verdict calculation,
- Markdown and JSON rendering where reusable.

If existing CLI code is not structured for reuse, the implementation must first extract a shared application service that both CLI and web handlers call.

## 16. Suggested Go Package Layout

The web implementation should use this layout unless existing code establishes a better pattern:

```text
cmd/speccritic-web/       HTTP server entry point
internal/app/             Shared check orchestration used by CLI and web
internal/web/             HTTP handlers, routing, middleware
internal/web/templates/   HTML templates and HTMX partials
internal/web/session/     In-memory check storage if async mode exists
```

Existing packages from the CLI spec remain authoritative for domain behavior:

```text
internal/spec/
internal/context/
internal/redact/
internal/profile/
internal/llm/
internal/schema/
internal/review/
internal/render/
internal/patch/
```

## 17. Rendering Requirements

HTML templates must use Go's `html/template` package or an equivalent escaping template engine.

Templates must be organized so full-page rendering and HTMX partial rendering share the same underlying components where practical.

The annotated spec view must remain readable for specs up to 2,000 lines.

The annotated spec HTML must use a lightweight line structure, such as a simple table or list, and must not attach per-line JavaScript handlers when event delegation or standard links can provide the interaction.

The page must remain usable at viewport widths from 390 px to 1440 px.

At narrow widths:

- the issue list and detail panel may stack below the spec,
- line numbers must remain visible,
- issue markers must not overlap spec text.

## 18. Configuration

The web server must support configuration through environment variables and flags.

Required configuration:

- listen address,
- request timeout,
- maximum upload size,
- maximum retained checks for async mode,
- retained check TTL,
- model configuration using the same environment variables as the CLI.

Default listen address for local development is `127.0.0.1:8080`.

The server must not listen on a public interface by default.

## 19. Accessibility

The UI must support keyboard navigation for:

- submitting a check,
- moving through the issue list,
- opening issue details,
- returning focus to the annotated spec line.

Severity must not be communicated by color alone. Each marker must include text or an accessible label indicating severity.

Form inputs must have labels. Dynamic status updates must be exposed through an ARIA live region.

## 20. Testing Requirements

Unit tests must cover:

- request validation,
- option parsing,
- HTML escaping of spec text and issue content,
- mapping issue evidence ranges to spec lines,
- severity filtering without changing summary counts,
- issue ordering for lines with multiple findings,
- not-found behavior for unknown check IDs and issue IDs.

Handler tests must cover:

- `GET /`,
- successful `POST /checks`,
- invalid `POST /checks`,
- issue detail partial rendering,
- export endpoints.

Integration tests must use a mock LLM provider and verify:

- pasted spec review flow,
- uploaded spec review flow,
- invalid model output path,
- evidence bounds validation failure,
- rendered annotations for known line ranges.

## 21. Acceptance Criteria

The first version is complete when:

- a user can paste a spec and run a check from the browser,
- the server returns the same verdict, score, issue counts, and issue data that the CLI would return for the same input and options,
- the submitted spec is rendered with line numbers,
- lines referenced by issue evidence are visibly annotated,
- clicking an issue or annotated line displays the relevant details,
- severity filtering updates rendered results without rerunning the LLM,
- JSON export matches the canonical SpecCritic output schema,
- all user-provided content is HTML-escaped,
- tests pass with a mock LLM provider.
