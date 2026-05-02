# SpecCritic Convergence Tracking Specification

## 1. Purpose

SpecCritic convergence tracking compares a current review result against a previous SpecCritic JSON result and classifies findings as `new`, `still_open`, or `resolved`.

The feature exists to make iterative spec improvement measurable. Users should be able to see whether a review cycle reduced open defects, introduced new defects, or left prior defects unresolved. It must preserve SpecCritic's core contract: normal reviews still produce the same verdict, score, evidence validation, redaction behavior, and CLI/web behavior unless convergence tracking is explicitly requested.

## 2. Goals

- Compare two SpecCritic JSON reports for the same evolving specification.
- Classify current findings as `new` or `still_open`.
- Classify previous findings missing from the current report as `resolved`, `dropped`, or `untracked`.
- Track convergence counts by issue/question type and severity.
- Preserve stable finding identity when prior and current reports use incremental rerun.
- Work for full-review, preflight-only, chunked-review, and incremental-review outputs.
- Expose convergence details in CLI JSON output and Markdown output.
- Expose convergence details in the HTMX web UI when a previous report is uploaded.
- Avoid changing score and verdict semantics.

## 3. Non-Goals

- No automatic editing of specs.
- No persistent server-side storage of review history.
- No database requirement.
- No semantic embedding service.
- No attempt to prove a finding is truly resolved when the current review skipped the relevant text.
- No change to the existing issue/question schema fields unless explicitly defined in this spec.
- No replacement for `--fail-on`; convergence status is informational unless a later spec defines convergence gates.

## 4. User Workflow

1. User runs SpecCritic and saves JSON output.
2. User revises the spec.
3. User runs SpecCritic again with the previous JSON result as a convergence baseline.
4. SpecCritic validates the previous result for compatibility.
5. SpecCritic runs the normal current review pipeline.
6. SpecCritic compares previous findings to current findings.
7. SpecCritic emits normal current findings plus convergence metadata.
8. User sees which findings are new, still open, and resolved.

Example:

```bash
speccritic check SPEC.md --format json --out review-1.json
# edit SPEC.md
speccritic check SPEC.md \
  --convergence-from review-1.json \
  --format md
```

## 5. Functional Behavior

Convergence tracking must be a reporting layer over a completed current review.

Required behavior:

- When no convergence flag is present, SpecCritic must produce the same output it produces today.
- When convergence tracking is enabled, SpecCritic must run the current review normally before comparing reports.
- Current score, verdict, issue counts, question counts, patches, and exit code must be computed from the current report only.
- A prior finding marked `resolved` must not affect the current score or verdict.
- A current finding marked `still_open` must affect the current score and verdict exactly like any other current finding.
- A current finding marked `new` must affect the current score and verdict exactly like any other current finding.
- Convergence comparison must never reuse prior findings as current findings. Reuse is owned by incremental rerun.
- Convergence comparison must treat issues and questions separately.
- Convergence metadata must identify when the comparison was partial, unsafe, or unavailable.

## 6. CLI Interface

New flags:

| Flag | Default | Description |
|------|---------|-------------|
| `--convergence-from` | empty | Path to previous SpecCritic JSON report used as the convergence baseline. |
| `--convergence-report` | `true` when `--convergence-from` is set | Include convergence metadata in output. |
| `--convergence-mode` | `auto` | Convergence mode: `auto`, `on`, or `off`. |
| `--convergence-strict` | `false` | Require compatible profile, strict mode, severity threshold, and previous spec hash lineage before comparing. |

Mode behavior:

- `--convergence-mode off` ignores `--convergence-from`.
- `--convergence-mode auto` compares reports when compatibility checks pass and emits a partial/unavailable convergence status when they do not.
- `--convergence-mode on` requires `--convergence-from`; incompatible or invalid previous reports are input errors and exit with code `3`.

Validation:

- `--convergence-from` must point to valid SpecCritic JSON output.
- `--convergence-mode` must be `auto`, `on`, or `off`.
- `--convergence-mode on` requires `--convergence-from`.
- `--convergence-report=false` is allowed only for JSON output when callers want current report data without convergence metadata despite providing a previous report.
- Invalid convergence flags are input errors and exit with code `3`.

Environment defaults:

| Env Var | Matching Flag |
|---------|---------------|
| `SPECCRITIC_CONVERGENCE_FROM` | `--convergence-from` |
| `SPECCRITIC_CONVERGENCE_MODE` | `--convergence-mode` |
| `SPECCRITIC_CONVERGENCE_STRICT` | `--convergence-strict` |
| `SPECCRITIC_CONVERGENCE_REPORT` | `--convergence-report` |

## 7. Web Interface

The web UI must support convergence tracking without storing previous reports server-side.

Required behavior:

- The existing previous JSON upload input may be reused when both incremental and convergence features need the same baseline report.
- The UI must make clear whether the uploaded previous report is being used for incremental rerun, convergence tracking, or both.
- The result summary must show counts for `new`, `still_open`, and `resolved` findings when convergence metadata is available.
- Finding detail modals must show the convergence status for current findings.
- Resolved findings must be visible in a separate resolved list or tab. They must not appear as active annotations on current spec lines unless their old line can be shown as historical context without implying it exists in the current spec.
- The web UI must not persist uploaded previous reports beyond the request lifecycle.

## 8. Previous Result Contract

The previous report must be a SpecCritic JSON report containing:

- `tool = "speccritic"`,
- compatible schema version,
- `input.spec_hash`,
- `input.profile`,
- `input.strict`,
- `input.severity_threshold`,
- `issues`,
- `questions`,
- `summary`,
- `meta.model`.

The previous report may also contain:

- `meta.incremental`,
- `meta.convergence`,
- `meta.redaction_config_hash`,
- issue/question tags from preflight, chunking, incremental review, or provider repair.

Compatibility checks:

- Schema version must be supported.
- Previous and current reports must use the same profile unless `--convergence-strict=false`.
- Previous and current reports must use the same strict mode unless `--convergence-strict=false`.
- Previous and current reports should use compatible severity thresholds. If the previous threshold is more strict than the current threshold, the comparison is partial because previously omitted lower-severity findings cannot be tracked.
- Provider/model differences must not block convergence comparison.
- Different spec file paths must not block convergence comparison.
- Redaction config mismatch makes the comparison partial in `auto` mode and an input error in `on` mode only when both reports expose redaction config hashes.

## 9. Tracked Finding Model

Convergence tracking applies to both issues and questions. The term finding means either an issue or a question.

A tracked finding identity includes:

- kind: `issue` or `question`,
- prior id,
- severity,
- category,
- title or question text,
- evidence line range,
- evidence excerpt hash when available,
- normalized fingerprint.

The normalized fingerprint must be deterministic and stable across runs. It must be computed from:

1. finding kind,
2. normalized severity,
3. normalized category,
4. normalized title or question text,
5. normalized evidence excerpt when available,
6. normalized section path when available.

Normalization rules:

- Trim leading and trailing whitespace.
- Collapse internal whitespace to one ASCII space.
- Lowercase category and severity.
- Remove volatile tags such as `chunk:<ID>`, `range:<ID>`, `incremental-reused`, and provider repair tags from fingerprint input.
- Do not include current issue ids such as `ISSUE-0001` because IDs may be reassigned after filtering, merging, or provider output changes.

## 10. Status Classification

Each current finding must receive exactly one convergence status when convergence comparison is available:

| Status | Meaning |
|--------|---------|
| `new` | No compatible previous finding matches the current finding. |
| `still_open` | A compatible previous finding matches the current finding and the defect/question remains present. |
| `untracked` | Comparison was unavailable or unsafe for this finding. |

Each previous finding that does not match a current finding must receive exactly one historical status:

| Status | Meaning |
|--------|---------|
| `resolved` | The prior finding is absent from the current complete review and the relevant current content was reviewed. |
| `dropped` | The prior finding was tied to deleted content or filtered by current severity threshold. |
| `untracked` | SpecCritic cannot safely determine whether the prior finding was resolved. |

Resolution rules:

- A previous finding can be `resolved` only when the current review covered the relevant current text.
- In a normal full review, all current text is considered reviewed.
- In preflight-only mode, prior LLM findings must be `untracked` unless they match current preflight findings or are tied to deleted content.
- In incremental mode, previous findings attached to unchanged reused ranges may be `still_open` if reused by incremental rerun.
- In incremental mode, previous findings attached to changed ranges may be `resolved` only if the changed range was reviewed by the current LLM or a deterministic preflight rule superseded the prior finding.
- If the current review falls back from incremental to full review, normal full-review resolution rules apply.
- If the previous report used a less strict severity threshold than the current report, prior lower-severity findings filtered out by the current threshold are `dropped`, not `resolved`.
- If the previous report used a more strict severity threshold than the current report, missing lower-severity history makes current lower-severity findings `untracked` unless they clearly match a previous finding.

## 11. Matching Algorithm

Convergence matching must be deterministic.

Matching order:

1. Exact stable id match when both reports expose a stable prior identity tag.
2. Exact normalized fingerprint match.
3. Evidence remap match using unchanged line hash or incremental section mapping.
4. Text similarity match over normalized title/question plus evidence excerpt.
5. No match.

Text similarity requirements:

- Similarity uses normalized Levenshtein similarity.
- Title/question similarity must be at least `0.92`.
- Evidence excerpt similarity must be at least `0.88` when excerpts are available.
- If multiple previous findings match one current finding, choose the highest score only when it is at least `0.05` higher than the next candidate. Otherwise mark the current finding `untracked`.
- One previous finding may match at most one current finding.

Severity/category drift:

- Severity escalation from prior `warn` to current `critical` can still be `still_open` when title and evidence match.
- Severity downgrade from prior `critical` to current `warn` can still be `still_open` when title and evidence match.
- Category changes can still match only through evidence remap or high text similarity.
- A finding with substantially different evidence must be `new` even if its title is similar.

## 12. Output Contract

Convergence metadata is optional in the current schema version. It must be omitted unless convergence tracking is requested and `--convergence-report=true`.

When present in JSON output, convergence metadata must live at `meta.convergence`:

```json
{
  "convergence": {
    "enabled": true,
    "mode": "auto",
    "status": "complete",
    "previous_spec_hash": "sha256:...",
    "current_spec_hash": "sha256:...",
    "current": {
      "new": 2,
      "still_open": 4,
      "untracked": 0
    },
    "previous": {
      "resolved": 3,
      "dropped": 1,
      "untracked": 0
    },
    "by_severity": {
      "critical": {
        "new": 1,
        "still_open": 2,
        "resolved": 2
      }
    },
    "notes": []
  }
}
```

Mandatory fields:

- `enabled`,
- `mode`,
- `status`,
- `previous_spec_hash`,
- `current_spec_hash`,
- `current`,
- `previous`,
- `by_severity`,
- `notes`.

Allowed `status` values:

| Status | Meaning |
|--------|---------|
| `complete` | Comparison is considered complete for the current review mode. |
| `partial` | Comparison succeeded but some findings could not be safely classified. |
| `unavailable` | Comparison was requested but could not be performed in `auto` mode. |

Current active findings may include an optional convergence object:

```json
{
  "convergence": {
    "status": "still_open",
    "previous_id": "ISSUE-0003",
    "confidence": 0.97
  }
}
```

Previous resolved findings must not be inserted into the active `issues` or `questions` arrays. They may appear only in `meta.convergence.resolved_findings` if that optional array is added. If present, `resolved_findings` entries must be clearly historical and must include their previous id, kind, severity, title/question, and previous evidence lines.

Markdown output must include a short convergence section before active findings when convergence metadata is present:

```text
Convergence: 2 new, 4 still open, 3 resolved, 1 dropped, 0 untracked
```

## 13. Relationship To Incremental Rerun

Incremental rerun and convergence tracking are separate features.

- Incremental rerun optimizes execution by reusing prior findings and reviewing changed ranges.
- Convergence tracking reports progress between previous and current results.
- A user may enable convergence tracking without incremental rerun.
- A user may enable incremental rerun without convergence tracking.
- When both are enabled, they may use the same previous JSON report.
- Incremental metadata must remain under `meta.incremental`.
- Convergence metadata must remain under `meta.convergence`.
- A finding reused by incremental rerun is normally `still_open` for convergence.
- A finding newly emitted by an incremental changed-range review is `new` unless it matches a previous finding.

## 14. Failure And Fallback Behavior

In `auto` mode:

- Invalid previous JSON makes convergence status `unavailable` and adds a note.
- Incompatible schema makes convergence status `unavailable`.
- Partial threshold/profile/strict compatibility makes convergence status `partial` unless strict mode requires unavailable.
- Ambiguous finding matches mark only the affected findings `untracked`.
- The review itself must still succeed when the current review pipeline succeeds.

In `on` mode:

- Missing `--convergence-from` exits `3`.
- Invalid previous JSON exits `3`.
- Unsupported schema exits `3`.
- Incompatible strict compatibility checks exit `3`.
- Ambiguous individual matches do not exit unless they make the entire comparison unsafe; they produce `partial` status with `untracked` counts.

Provider or model failures during the current review are handled by the existing provider error behavior. Convergence tracking must not mask review failures.

## 15. Security And Privacy

- Previous reports may contain spec excerpts, issue text, and model metadata. Treat them as user-provided input.
- Previous report content must never be sent to an LLM solely for convergence comparison.
- Convergence matching must run locally.
- Web uploads must respect the existing maximum upload size and UTF-8 validation.
- The web server must not persist previous reports or convergence metadata beyond the request lifecycle.
- Debug output must not print full previous report contents unless existing debug behavior already prints equivalent current report content.

## 16. Performance Constraints

- Matching must be O(n²) or better for n findings, where n is total previous plus current findings.
- The implementation must handle at least 1,000 prior findings and 1,000 current findings without provider calls.
- Fingerprint computation must avoid repeated expensive normalization for the same finding.
- Convergence comparison must not materially increase LLM latency because it runs after provider calls complete and uses only local data.

## 17. Testing Requirements

Unit tests:

- Exact fingerprint match produces `still_open`.
- Missing previous finding becomes `new`.
- Missing current finding becomes `resolved` after full review.
- Missing current finding becomes `untracked` after preflight-only review for prior LLM findings.
- Deleted-content prior finding becomes `dropped`.
- Severity drift still matches when evidence matches.
- Ambiguous match produces `untracked`.
- Invalid previous report behavior differs between `auto` and `on`.
- `meta.convergence` is omitted when not requested.

Integration tests:

- CLI full-review-to-full-review convergence.
- CLI incremental-plus-convergence using the same previous report.
- CLI `--convergence-mode on` exits `3` for invalid previous report.
- Markdown output includes convergence summary.
- Web upload renders convergence counts and resolved findings.

Regression tests:

- Current score and verdict are unchanged with convergence enabled.
- Existing incremental metadata remains unchanged when convergence metadata is added.
- Existing web review without previous upload behaves unchanged.

## 18. Acceptance Criteria

- Default SpecCritic behavior is unchanged when convergence flags are absent.
- Users can pass a previous JSON report and see `new`, `still_open`, and `resolved` counts.
- Current findings have deterministic convergence classification when comparison is available.
- Historical resolved findings do not affect current score or verdict.
- The web UI displays convergence information without storing history.
- Invalid or incompatible previous reports have deterministic `auto` and `on` behavior.
- All new behavior is covered by focused tests.
