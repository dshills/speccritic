# SpecCritic Profile Completion Templates and Patch Suggestions Specification

## 1. Purpose

Profile completion templates help users converge on implementable specifications faster by turning profile gaps into concrete, auditable draft additions.

A profile gap is one of these current-review findings:

- a missing-section preflight issue whose rule applies to the selected profile,
- a current issue with category `MISSING_FAILURE_MODE`, `UNDEFINED_INTERFACE`, `MISSING_INVARIANT`, `UNSPECIFIED_CONSTRAINT`, `ASSUMPTION_REQUIRED`, `ORDERING_UNDEFINED`, or `NON_TESTABLE_REQUIREMENT`,
- a current question whose `blocks` list references a current issue ID,
- a current issue tagged by chunking or incremental rerun after merge and evidence validation.

This feature adds an optional completion layer that uses the selected profile to propose section skeletons and advisory patch suggestions for those gaps. It must preserve SpecCritic's core review contract across CLI, JSON, Markdown, web, patch export, and metadata outputs: hostile critique remains separate from draft completion, score and verdict are computed from findings only, and suggested text is never applied automatically.

Terminology:

- API means application programming interface.
- CLI means command-line interface.
- DLQ means dead-letter queue.
- LLM means large language model.
- OPEN DECISION means an explicit placeholder requiring user judgment before the spec is implementation-ready.
- SLA means service-level agreement.
- UI means user interface.
- ATX means the Markdown heading form that starts a line with one or more `#` characters.
- ISSUE means a SpecCritic issue identifier prefix.
- REDACTED means the literal replacement marker inserted by SpecCritic redaction.

## 2. Goals

- Provide profile-specific completion templates for `general`, `backend-api`, `regulated-system`, and `event-driven`.
- Generate advisory patch suggestions for missing or incomplete spec sections.
- Reduce repeated review loops by giving users a concrete next draft to edit before rerunning SpecCritic.
- Use current issues as the primary driver for patch suggestions.
- Use current questions only when they reference a current issue through `blocks`.
- Insert `OPEN DECISION` placeholders when the missing behavior requires user, product, security, compliance, operational, or business judgment.
- Preserve the existing `patches` output contract for machine-readable patch export.
- Expose the same generated completion patch set in CLI JSON, Markdown, and web results. Renderers are allowed to format the same data differently; renderers must not add, remove, or reorder completion patches.
- Keep review findings hostile and evidence-based; keep completion suggestions explicitly labeled `draft/advisory`.

## 3. Non-Goals

- No automatic application of patches to source files.
- No replacement for findings, questions, scoring, verdict, preflight, chunking, incremental rerun, or convergence tracking.
- No generation of complete production-ready specifications.
- No invention of business policy, security posture, regulatory interpretation, service-level agreement values, data retention period values, or API semantics.
- No new LLM provider requirement in the first implementation.
- No persistent template database.
- No user-defined templates loaded from disk in the first implementation.
- No user-specific template customization beyond `--profile`, `--completion-suggestions`, `--completion-mode`, `--completion-template`, `--completion-max-patches`, and `--completion-open-decisions`.
- No first-class `completion_suggestions` top-level array in the first implementation.
- No direct-copy web UI for completion sections in the first implementation.

## 4. User Workflow

1. User runs SpecCritic with completion suggestions enabled.
2. SpecCritic performs the normal review pipeline.
3. SpecCritic identifies profile gaps from the final current report.
4. SpecCritic maps current issues and linked questions to profile completion templates.
5. SpecCritic emits the normal report plus advisory completion patches and `meta.completion`.
6. User reviews the suggested patches.
7. User resolves every `OPEN DECISION` placeholder before treating the patched spec as implementation-ready.
8. User applies or copies text manually.
9. User reruns SpecCritic, optionally using incremental rerun and convergence tracking.

Example:

```bash
speccritic check SPEC.md \
  --profile backend-api \
  --completion-suggestions \
  --patch-out completion.patch
```

## 5. Global Invariants

- Completion suggestions are always advisory.
- Completion suggestions are never applied automatically.
- Completion suggestions never affect score, verdict, counts, convergence status, or `--fail-on` exit behavior.
- Every emitted completion patch must trace to exactly one current issue ID.
- A question influences a completion patch only through a current issue listed in that question's `blocks`.
- Completion generation must be deterministic for the same normalized input set.
- Completion generation must not inspect unredacted secret values.
- Completion generation must omit unsafe patches instead of emitting best-effort patches.
- Completion output must use the exact label `draft/advisory` in Markdown and web rendering.

## 6. Functional Behavior

Completion suggestions are a post-review advisory layer.

Required behavior:

- When completion suggestions are disabled, SpecCritic must behave exactly as it does today and must emit no completion patches or `meta.completion`.
- When completion suggestions are enabled, SpecCritic must run the normal review pipeline before generating completion suggestions.
- Completion generation must use the effective template selected by Section 8.
- Completion generation must use only the redacted review input set defined in Section 7 and the validated current findings defined in Section 7.
- Completion suggestions must not change issue counts, question counts, score, verdict, fail-on behavior, or exit code, except invalid completion configuration exits with code `3`.
- Completion suggestions must add entries to the existing `patches` array only when each patch satisfies the traceability rules in Section 10.
- Completion patches must be deterministic for the same inputs listed in Section 7.
- Completion patches must be omitted when the target insertion or replacement point fails the safety rules in Section 11.
- A completion patch must never delete unrelated spec text.
- A completion patch must never rewrite the whole spec.
- A completion patch must use `OPEN DECISION` placeholders for every unstated behavior listed in Section 12.
- Completion suggestions must add missing structure before changing existing normative requirements.
- When a missing section has a stable insertion location under Section 11, the patch must insert a section skeleton.
- When an existing section is materially incomplete under Section 11, the patch must append missing subsection placeholders inside that section.
- When no stable patch is generated, SpecCritic must record a skipped suggestion in `meta.completion.skipped_suggestions`, must omit the patch from `patches`, must include a skipped completion note in Markdown and web output, and must still write any other safe patches to `--patch-out`.

## 7. Completion Input Set

Completion generation input is exactly:

- current redacted spec path,
- current redacted spec raw text,
- current redacted line-numbered spec text,
- current parsed Markdown section tree,
- current selected profile,
- effective completion template,
- current strict-mode value,
- current severity threshold,
- current completion options,
- final current issues after preflight, chunk merge, incremental merge, schema validation, evidence validation, severity filtering for output, and convergence annotation,
- final current questions after the same validation and merge steps,
- final current report metadata needed to determine whether a finding was preflight, chunked, incremental, or reused.

Completion generation must not use:

- unredacted context file contents,
- unredacted spec text for drafting suggestion content,
- prior reports except through findings present in the final current report,
- model prompts that were not already part of the review result.

Validated current findings are the final `issues` and `questions` arrays in the current report after schema and evidence validation. Findings suppressed by severity threshold must not source completion suggestions because they are not visible in the current output.

The deterministic input comparison includes:

- exact redacted spec text bytes,
- profile name,
- strict value,
- severity threshold,
- completion mode,
- effective template name,
- completion maximum patch count,
- completion open-decision setting,
- sorted current issue IDs, severities, categories, tags, and evidence ranges,
- sorted current question IDs, severities, blocks, and evidence ranges.

## 8. CLI Interface

New flags:

| Flag | Default | Description |
|------|---------|-------------|
| `--completion-suggestions` | `false` | Generate profile-specific advisory completion patches after review. |
| `--completion-mode` | `auto` | Completion mode: `auto`, `on`, or `off`. |
| `--completion-template` | `profile` | Template set to use: `profile`, `general`, `backend-api`, `regulated-system`, or `event-driven`. |
| `--completion-max-patches` | `8` | Maximum completion patches to emit. |
| `--completion-open-decisions` | `true` | Insert `OPEN DECISION` placeholders instead of inventing unstated behavior. |

Precedence:

1. Explicit CLI flags override environment variables.
2. Environment variables override built-in defaults.
3. `--completion-mode off` disables completion regardless of `--completion-suggestions`.
4. `--completion-suggestions=false` disables completion when `--completion-mode` is `auto`.
5. `--completion-mode on` enables completion even when `--completion-suggestions=false`; this combination exists for automation that wants mode to be the single control.
6. `--completion-template profile` resolves to the selected `--profile`.
7. A named `--completion-template` overrides `--profile` for template selection only. Review still uses `--profile`.

Mode behavior:

- `off`: completion does not run and no completion metadata is emitted.
- `auto`: completion runs only when requested by `--completion-suggestions=true` or its environment equivalent; unsafe suggestions are skipped and reported.
- `on`: completion runs; every blocking missing-section issue must either produce a safe patch or cause exit code `3`.

Validation:

- Invalid completion flag values are input errors and exit with code `3`.
- Invalid environment values are validated exactly like the matching CLI flag and exit with code `3` unless an explicit CLI flag overrides the invalid environment value.
- `--completion-mode` must be one of `auto`, `on`, or `off`.
- `--completion-template` must be `profile`, `general`, `backend-api`, `regulated-system`, or `event-driven`.
- `--completion-max-patches` must be `>= 0`.
- `--completion-open-decisions=false` is valid only when every generated suggestion is based entirely on explicit existing spec text. If a candidate suggestion requires an unstated decision and open decisions are disabled, that candidate must be skipped in `auto` mode and must cause exit code `3` in `on` mode when it is required for a blocking missing-section issue.

Environment defaults:

| Env Var | Matching Flag |
|---------|---------------|
| `SPECCRITIC_COMPLETION_SUGGESTIONS` | `--completion-suggestions` |
| `SPECCRITIC_COMPLETION_MODE` | `--completion-mode` |
| `SPECCRITIC_COMPLETION_TEMPLATE` | `--completion-template` |
| `SPECCRITIC_COMPLETION_MAX_PATCHES` | `--completion-max-patches` |
| `SPECCRITIC_COMPLETION_OPEN_DECISIONS` | `--completion-open-decisions` |

## 9. Template Sets

Each template set defines:

- exact canonical section headings,
- subsection headings,
- placeholder bullets,
- related finding categories,
- related preflight rule IDs when available,
- insertion ordering.

Templates must be stored as code-owned built-ins. They must be represented by typed Go structs accessed through this API:

```go
type Template struct {
    Name     string
    Sections []TemplateSection
}

type TemplateSection struct {
    Heading       string
    Subheadings   []string
    Placeholders  []Placeholder
    Categories    []schema.Category
    PreflightRuleIDs []string
    Order         int
}

type Placeholder struct {
    Text              string
    RequiresDecision  bool
    RelevanceKeywords []string
}

func GetTemplate(name string, selectedProfile string) (*Template, error)
```

`GetTemplate("profile", selectedProfile)` must resolve to the selected profile. Unknown template names and unsupported selected profiles must return an input error that exits with code `3` in the CLI.

### 9.1 General Template

The general template must use these exact canonical headings in this order:

1. `Purpose`
2. `Non-Goals`
3. `Functional Requirements`
4. `Acceptance Criteria`
5. `Failure Modes`
6. `Open Decisions`

Required placeholder behavior:

- Missing functional behavior must produce `OPEN DECISION` placeholders, not invented requirements.
- Acceptance criteria placeholders must ask for observable inputs, actions, and expected outcomes.
- Failure-mode placeholders must ask about the exact concept named by the current finding when the category is `MISSING_FAILURE_MODE`, `ASSUMPTION_REQUIRED`, or `UNSPECIFIED_CONSTRAINT`. The supported concepts are timeout, invalid input, unavailable dependency, and permission denial.

### 9.2 Backend API Template

The backend API template must use these exact canonical headings in this order:

1. `Endpoints`
2. `Authentication and Authorization`
3. `Request and Response Schemas`
4. `Error Responses`
5. `Rate Limits and Abuse Handling`
6. `Idempotency and Repeat Submission Behavior`
7. `Observability`
8. `Acceptance Tests`

Required placeholder behavior:

- Endpoint placeholders must not invent paths, methods, status codes, or JSON schemas.
- Error response placeholders must request exact HTTP status codes, response body shape, and retryability.
- Auth placeholders must request the auth mechanism and per-endpoint authorization rules.
- Rate limit placeholders must request the exact request count and time window.

### 9.3 Regulated-System Template

The regulated-system template must use these exact canonical headings in this order:

1. `Compliance Scope`
2. `Data Classification`
3. `Access Control`
4. `Audit Trail`
5. `Data Lifecycle and Deletion`
6. `Approval and Review Workflow`
7. `Incident and Exception Handling`
8. `Validation Evidence`

Required placeholder behavior:

- Compliance placeholders must not name legal obligations unless present in the source spec or context.
- Data lifecycle placeholders must request exact retention periods, deletion triggers, and legal-hold behavior.
- Audit placeholders must request actors, actions, timestamps, immutable fields, and access controls.

### 9.4 Event-Driven Template

The event-driven template must use these exact canonical headings in this order:

1. `Event Producers and Consumers`
2. `Event Schema`
3. `Delivery Guarantees`
4. `Ordering and Idempotency`
5. `Retry and Failed-Event Queue Behavior`
6. `Consumer Failure Behavior`
7. `Backfill and Replay`
8. `Observability`

Required placeholder behavior:

- Event schema placeholders must not invent fields.
- Delivery placeholders must request at-most-once, at-least-once, or exactly-once expectations.
- Ordering placeholders must ask for partitioning/key semantics when the current finding category is `ORDERING_UNDEFINED` or when the spec contains the exact words `order`, `ordered`, `ordering`, `sequence`, or `partition`.
- Failed-event queue placeholders must request retention period, alerting behavior, replay rules, and poison-message handling.

## 10. Traceability Model

Every completion candidate has:

- `source_issue_id`: one current issue ID,
- `source_question_ids`: zero or more current question IDs whose `blocks` contain `source_issue_id`,
- `template`: effective template name,
- `section`: canonical target section heading,
- `target_line`: insertion or replacement line in the current spec when known,
- `status`: `patch_generated`, `skipped_no_safe_location`, `skipped_overlap`, `skipped_redaction`, `skipped_limit`, or `skipped_open_decisions_disabled`.

Patch traceability rules:

- `schema.Patch.issue_id` must equal `source_issue_id`.
- A patch must not be emitted for a question unless a current issue is also the source.
- Multiple questions support the same patch only through `source_question_ids`.
- The source issue must receive the tag `completion-suggested` when at least one completion candidate for that issue has status `patch_generated`.
- If the source issue cannot be mutated because the report is immutable at that stage, the implementation must build completion before final report freezing. It must not emit an untagged generated completion patch.
- Markdown and web rendering must use the same `source_issue_id` and `source_question_ids`.

## 11. Patch Suggestion Contract

Completion patches use the existing `schema.Patch` shape:

```json
{
  "issue_id": "ISSUE-0001",
  "before": "exact text from spec to be replaced",
  "after": "corrected minimal replacement text"
}
```

Field contract:

- `issue_id` is a non-empty string equal to a current issue ID.
- `before` is a non-empty exact substring of the original current spec text.
- `before` is unique only when `strings.Count(originalSpec, before) == 1`.
- `after` is a non-empty replacement string.
- For insertion-after operations, `before` must be the complete existing line or block after which content is inserted, and `after` must equal `before + "\n\n" + insertedContent`.
- For replacement operations, `after` must be the complete replacement for `before`.

Stable insertion rules:

- The parser must build a Markdown heading tree from ATX headings (`#`, `##`, etc.).
- A missing top-level template section has a stable location when the spec has at least one top-level heading and no duplicate top-level heading with the target canonical name.
- The target insertion point is after the nearest existing top-level template section with a lower template order.
- If no lower-order template section exists, the target insertion point is before the nearest existing higher-order template section.
- If neither lower-order nor higher-order template sections exist, the target insertion point is the end of the document.
- If the selected insertion point's `before` text appears more than once, the candidate must be skipped with status `skipped_no_safe_location`.
- Duplicate existing target headings make the location ambiguous and must skip the candidate.

Materially incomplete section rules:

- A template section is materially incomplete when it exists and contains no non-heading line with at least one of these markers: `must`, `shall`, `OPEN DECISION`, a numbered requirement ID, or a bullet containing a question mark.
- Missing subsections are appended as placeholders when the parent section exists and passes the unique-location rule.
- Subsection placeholders are preferred over checklist items. Checklist items are allowed only for `Acceptance Criteria` and `Acceptance Tests`.

Ordering and limits:

- Candidate sort order is target line ascending, severity order `CRITICAL`, `WARN`, `INFO`, source issue ID ascending, section order ascending, then generated text lexical order.
- Overlapping candidates are resolved after sorting; the first candidate is kept, later overlapping candidates receive `skipped_overlap`.
- The patch limit is applied after overlap and redaction checks.
- When more safe candidates exist than `--completion-max-patches`, the first N candidates by sort order are emitted and the rest receive `skipped_limit`.
- `--completion-max-patches=0` emits no patches and marks every otherwise-safe candidate `skipped_limit`.

Safety checks:

- A patch must be skipped when `before` is absent or non-unique.
- A patch must be skipped when its edit range overlaps an earlier emitted patch.
- A patch must be skipped when `before` contains `[REDACTED]`.
- A patch must be skipped when `after` contains text matching the redaction detector's secret patterns.
- A patch must be skipped when generated from a finding whose evidence line range is invalid after merge.
- Skipped patches are not written to `patches` or `--patch-out`; skipped candidates are counted in `meta.completion.skipped_suggestions` and listed in Markdown/web output.

Patch export:

- `--patch-out` must include completion patches and normal advisory patches in the same diff-match-patch format.
- Existing patch comments of the form `# patch for ISSUE-XXXX` must remain.
- Completion patch comments must use `# completion patch for ISSUE-XXXX`.
- When the patch renderer cannot locate `before`, it must preserve the existing warning format: `WARN: patch for ISSUE-XXXX could not be located in spec (before text not matched)`.

## 12. OPEN DECISION Rules

A placeholder must use this exact format:

```text
OPEN DECISION: <specific decision needed before implementation>.
```

An `OPEN DECISION` placeholder is required when the suggestion would otherwise choose any of these unstated values:

- endpoint path, method, request field, response field, status code, or retryability,
- auth mechanism or authorization rule,
- rate limit number or time window,
- retention period, deletion trigger, legal hold behavior, or audit event field,
- event delivery guarantee, ordering key, replay rule, dead-letter queue retention, or poison-message behavior,
- timeout duration, error handling behavior, dependency fallback, permission behavior, or acceptance threshold,
- product behavior, security posture, compliance interpretation, operational policy, or business rule.

When `--completion-open-decisions=false`, candidates requiring any value in this list must be skipped in `auto` mode. In `on` mode, skipping such a candidate for a blocking missing-section issue must exit with code `3`.

Strict mode:

- Strict mode does not change template text.
- Strict mode increases OPEN DECISION usage only because more current findings become eligible sources.
- A strict-mode candidate must still satisfy the same source, safety, and traceability rules.

## 13. Prompt and LLM Behavior

The first implementation must use deterministic template generation from validated findings and current spec structure. It must not make a second LLM call for completion.

LLM-assisted completion is out of scope for this implementation. A later specification must define the exact flag, JSON schema, redaction behavior, validation rules, and conflict behavior before any implementation adds LLM-assisted completion.

The existing review prompt must not be changed by this feature except to preserve current patch behavior. Completion generation is local and deterministic.

## 14. Web Interface

The web UI must expose completion suggestions without applying them to local files.

Required behavior:

- Add one completion suggestions toggle named `completion_suggestions` near profile controls.
- Add one mode selector named `completion_mode` with `auto`, `on`, and `off`.
- Add one template selector named `completion_template` with `profile`, `general`, `backend-api`, `regulated-system`, and `event-driven`.
- Add one numeric input named `completion_max_patches`.
- Disable those four controls from check submission until the check completes or fails.
- Show `generated_patches` from `meta.completion` in the result summary when `meta.completion.enabled=true`.
- In finding detail modals, show related completion suggestions whose `source_issue_id` matches the finding.
- Patch export must include completion patches together with normal advisory patches using the comment format in Section 11.
- The UI must display the exact label `draft/advisory` next to each completion suggestion.
- The UI must not overwrite the uploaded spec or write suggested patches to disk.
- The UI must HTML-escape all generated suggestion text through Go `html/template`.
- The UI must render generated Markdown as untrusted text, not trusted HTML.

## 15. Rendering

JSON output:

- Must include completion patches in the existing `patches` array.
- Must include `meta.completion` whenever completion mode is `auto` or `on`, completion is enabled by Section 8, and review reaches report construction.
- Must omit `meta.completion` when completion mode is `off` or completion is not requested in `auto` mode.

Markdown output:

- Must include a `Completion Suggestions` section when `meta.completion.enabled=true`.
- Must omit the `Completion Suggestions` section when no completion metadata exists.
- Must list `source_issue_id` for each suggestion.
- Must list `source_question_ids` when non-empty.
- Must display the exact label `draft/advisory` next to each suggestion.
- Must distinguish `patch_generated` from skipped statuses.

Patch output:

- Must remain valid diff-match-patch output.
- Must use the comment and warning behavior defined in Section 11.

Web output:

- Must follow Section 14.

## 16. Compatibility With Existing Modes

Preflight:

- Missing-section preflight findings are valid sources for completion suggestions.
- Completion suggestions must not hide, downgrade, or suppress preflight findings.
- Completion suggestions are available in `--preflight-mode only` and `--preflight-mode gate` when completion is enabled and current findings exist.

Chunking:

- Completion generation runs after chunk merge.
- Completion generation uses the merged current report and full current spec.
- Chunk-originated findings must pass the same traceability, non-overlap, exact-match, and redaction checks.
- A chunk-originated candidate that fails a check must be skipped with the matching status from Section 10.

Incremental rerun:

- Completion suggestions run after incremental merge.
- Reused prior findings source completion suggestions only when they are present in the final current report and their evidence was remapped by the incremental rerun exact line-hash or unchanged-section remap path.
- Prior findings remapped by approximate text search, fuzzy matching, or fallback full-review recovery must not source completion suggestions.
- Unsafe remaps must skip the candidate with status `skipped_no_safe_location`.

Convergence:

- Completion suggestions must not affect convergence classification.
- Resolved historical findings must not source new completion patches.

Strict mode:

- Strict mode follows Section 12.

## 17. Data Model

The implementation must extend `schema.Meta` with:

```json
"completion": {
  "enabled": true,
  "mode": "auto",
  "template": "backend-api",
  "generated_patches": 3,
  "skipped_suggestions": 2,
  "open_decisions": 5
}
```

Metadata rules:

- `enabled` is true when completion generation ran.
- `enabled` is false only when mode is `off`; omitted metadata is used when completion is not requested in `auto` mode.
- `mode` must be `auto`, `on`, or `off`.
- `template` must be `general`, `backend-api`, `regulated-system`, or `event-driven`.
- `generated_patches` counts emitted completion `schema.Patch` entries only.
- `skipped_suggestions` counts completion candidates with a skipped status.
- `open_decisions` counts `OPEN DECISION:` placeholders in emitted patches and skipped candidates.
- `generated_patches + skipped_suggestions` equals the number of completion candidates considered.
- `open_decisions` is not disjoint from the other counters.

Mode truth table:

| Requested | Mode | Completion runs | `meta.completion` |
|-----------|------|-----------------|-------------------|
| false | auto | no | omitted |
| true | auto | yes | emitted with `enabled=true` |
| false | on | yes | emitted with `enabled=true` |
| true | on | yes | emitted with `enabled=true` |
| false | off | no | omitted |
| true | off | no | omitted |

## 18. Safety and Quality Rules

Completion suggestions must follow these rules:

- Do not invent facts.
- Do not silently choose between alternatives.
- Do not propose architecture unless the spec already requires that architecture.
- Do not expand scope beyond the selected profile and current findings.
- Do not weaken existing requirements.
- Do not remove constraints unless the finding explicitly identifies a contradiction and the replacement preserves both sides as an `OPEN DECISION`.
- Do not turn examples into normative requirements unless the spec already labels them as requirements.
- Keep each emitted patch under 80 added lines.

Quality gates:

- Suggestions must include the traceability fields in Section 10.
- Suggestions must follow the deterministic input and ordering rules in Sections 7 and 11.
- Suggestions must include `target_line` or a skipped status.
- Suggestions must preserve Markdown heading levels.
- Suggestions must avoid duplicate headings.
- Suggestions must not emit more than `--completion-max-patches` patch entries.

## 19. Testing Requirements

Unit tests:

- Built-in template lookup for `general`, `backend-api`, `regulated-system`, and `event-driven`.
- Unknown template rejection exits with code `3` through CLI validation.
- Missing-section finding maps to expected template section.
- Generated patch uses exact `before` text and `strings.Count(originalSpec, before) == 1`.
- Ambiguous insertion point skips patch generation and increments `skipped_suggestions`.
- Overlapping suggestions keep the first sorted candidate and mark later candidates `skipped_overlap`.
- `OPEN DECISION` placeholders are emitted for unstated behavior.
- Completion disabled path produces no completion patches and no `meta.completion`.
- Invalid environment defaults fail like invalid CLI flags.
- Patch limit overflow emits the first N sorted candidates and marks the rest `skipped_limit`.

Golden tests:

- Backend API spec missing error responses produces an advisory `Error Responses` section patch.
- Regulated-system spec missing data lifecycle behavior produces an `OPEN DECISION` data lifecycle patch.
- Event-driven spec missing dead-letter queue behavior produces dead-letter queue placeholders.
- General spec missing acceptance criteria produces an `Acceptance Criteria` section patch.

Integration tests:

- CLI `--completion-suggestions --patch-out` writes patch output without changing verdict semantics.
- JSON output includes normal findings, completion patches, and `meta.completion`.
- Markdown output includes a `Completion Suggestions` section with `draft/advisory` labels.
- Web check enables completion suggestions and renders escaped suggestion text.
- Completion suggestions work after preflight-only blocking findings.
- Completion suggestions work after chunked and incremental runs.

Regression tests:

- Redacted secrets never appear in completion patches.
- Patches are not generated when `before` appears multiple times.
- Existing manual patch behavior is unchanged when completion suggestions are disabled.
- `--completion-mode on` exits with code `3` when required completion patches cannot be safely generated and must not emit partial JSON, Markdown, or patch output.

## 20. Acceptance Criteria

The feature is complete when all of the following are true:

- The four built-in profiles `general`, `backend-api`, `regulated-system`, and `event-driven` each expose a completion template.
- The CLI provides the five flags in Section 8 and validates them with the specified precedence.
- For the same deterministic input set in Section 7, completion generation returns the same patches, skipped statuses, ordering, and metadata.
- Any missing behavior listed in Section 12 is represented as `OPEN DECISION`.
- Review score, verdict, counts, convergence classification, and fail-on behavior remain unchanged except when completion configuration is invalid.
- Patch export includes completion suggestions only when the patch satisfies Section 11 safety criteria.
- Markdown and web renderers display the exact label `draft/advisory` next to each completion suggestion.
- Tests verify template lookup, patch generation safety, CLI behavior, metadata, renderer output, and disabled-mode behavior.
