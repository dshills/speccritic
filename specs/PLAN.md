# SpecCritic — Implementation Plan

## Overview

This plan covers the Go implementation of SpecCritic as described in `SPEC.md`. The tool is structured as a pipeline: read → redact → line-number → load profile → build prompt → call LLM → validate → score → emit. Each package in `internal/` owns one stage.

---

## Prerequisites

```sh
go mod init github.com/dshills/speccritic
go get github.com/spf13/cobra
go get github.com/sergi/go-diff/diffmatchpatch
go mod tidy
```

External dependencies:
- `github.com/spf13/cobra` — CLI flags and subcommands
- `github.com/sergi/go-diff/diffmatchpatch` — unified diff for `--patch-out`

LLM providers are implemented using `net/http` directly (no SDK) to avoid import path uncertainty and keep the dependency count minimal. Each provider implementation constructs its own HTTP requests against the provider's REST API.

---

## Spec Gaps and Decisions Made

These items are underspecified in `SPEC.md`. Decisions are recorded here so they are not re-litigated during implementation.

| Gap | Decision |
|-----|----------|
| LLM provider/model selection | `SPECCRITIC_MODEL` env var in `provider:model` format (e.g. `anthropic:claude-sonnet-4-6`). No CLI flag in Phase 1. Default: `anthropic:claude-sonnet-4-6`. |
| API key config | Standard provider env vars: `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`. |
| `--fail-on` accepted values | Accepts verdict strings: `VALID_WITH_GAPS` or `INVALID`. Exit code 2 if actual verdict weight ≥ threshold weight. Verdict ordering: `VALID(0) < VALID_WITH_GAPS(1) < INVALID(2)`. |
| `--fail-on` with CRITICAL questions | CRITICAL questions count toward verdict equivalently to CRITICAL issues. A spec with only CRITICAL questions receives verdict `INVALID`. |
| `--context` flag behavior | Repeatable via cobra `StringArrayVar`: `--context a.md --context b.md`. |
| `--severity-threshold` scope | Score and verdict are computed from ALL issues before filtering. Threshold only affects which issues appear in the emitted `issues` array. `summary.*_count` fields always reflect all issues (pre-filter). |
| `--offline` exit code | When `--offline` is set and no provider env var is found, exit code 3 (input/configuration error). |
| Context file delimiter in prompt | Each context file wrapped in `<context file="NAME.md">…</context>` XML-style tags. |
| Profile format | Go structs embedded in binary. No external YAML in Phase 1. |
| Patch output format | Unified diff format per file, written to `--patch-out` path. |
| Patch array element schema | `{"issue_id": string, "before": string, "after": string}`. Defined in `internal/schema/types.go`. `internal/patch` uses a separate internal type for processing; conversion is explicit. |
| Patch write failure | Log `WARN: patch write failed: <err>` to stderr; continue and emit main report. Patches are advisory per SPEC.md §12. |
| LLM JSON fencing | Strip leading/trailing markdown code fences (` ```json … ``` `) before parsing. This is the most common LLM failure mode. |
| Scoring ownership | Score and verdict are computed by `internal/review`, never trusted from LLM output. |
| INFO-only verdict | INFO-only issues (no WARN, no CRITICAL) produce `VALID_WITH_GAPS`. A zero-issue result produces `VALID`. This means INFO notes prevent a VALID verdict. Documented here as a deliberate choice. |
| "Improves planning quality measurably" | SPEC.md §20 acceptance criterion deferred to Phase 2. Requires baseline measurement methodology not defined in Phase 1. |
| Retry temperature | Retry uses the same temperature as the original request. |
| `--debug` and file paths | `--debug` output includes file paths as-is. Documented: in regulated environments, paths may contain sensitive information. Acceptable for Phase 1. |

---

## Directory Structure

```
speccritic/
├── cmd/
│   └── speccritic/
│       └── main.go                  # Entry point; wires cobra root command
├── internal/
│   ├── schema/
│   │   ├── types.go                 # All Go types matching the JSON schema v1
│   │   └── validate/
│   │       ├── validate.go          # JSON parse + structural validation
│   │       └── validate_test.go
│   ├── spec/
│   │   ├── loader.go                # File reading, line-numbering, section detection
│   │   └── loader_test.go
│   ├── redact/
│   │   ├── redact.go                # Regex-based secret detection and replacement
│   │   └── redact_test.go
│   ├── context/
│   │   ├── loader.go                # Context file loading + prompt formatting
│   │   └── loader_test.go
│   ├── profile/
│   │   ├── profile.go               # Profile struct + loader
│   │   ├── general.go               # Built-in: general profile rules
│   │   ├── backend_api.go           # Built-in: backend-api rules
│   │   ├── regulated_system.go      # Built-in: regulated-system rules
│   │   ├── event_driven.go          # Built-in: event-driven rules
│   │   └── profile_test.go
│   ├── llm/
│   │   ├── provider.go              # Provider interface + NewProvider factory
│   │   ├── anthropic.go             # Anthropic HTTP implementation
│   │   ├── openai.go                # OpenAI HTTP implementation
│   │   ├── prompt.go                # Prompt construction + strict mode text
│   │   └── llm_test.go
│   ├── review/
│   │   ├── score.go                 # Deterministic scoring + verdict logic
│   │   └── review_test.go
│   ├── render/
│   │   ├── render.go                # Renderer interface
│   │   ├── json.go                  # JSON renderer
│   │   ├── markdown.go              # Markdown renderer
│   │   └── render_test.go
│   └── patch/
│       ├── patch.go                 # Diff generation from patch suggestions
│       └── patch_test.go
└── testdata/
    ├── specs/
    │   ├── good_spec.md             # Golden: should produce VALID
    │   └── bad_spec.md              # Golden: should produce CRITICAL findings
    └── llm/
        ├── mock_response_bad.json   # Canned response with CRITICAL issues (for bad_spec)
        └── mock_response_good.json  # Canned response with zero issues (for good_spec)
```

---

## Phase 1 — Foundation

### 1. `internal/schema` — Data Types

Define all Go structs that match the JSON schema v1. This is the shared vocabulary for every other package.

```go
type Report struct {
    Tool      string     `json:"tool"`
    Version   string     `json:"version"`
    Input     Input      `json:"input"`
    Summary   Summary    `json:"summary"`
    Issues    []Issue    `json:"issues"`
    Questions []Question `json:"questions"`
    Patches   []Patch    `json:"patches"`
    Meta      Meta       `json:"meta"`
}

type Severity string
const (
    SeverityInfo     Severity = "INFO"
    SeverityWarn     Severity = "WARN"
    SeverityCritical Severity = "CRITICAL"
)

type Verdict string
const (
    VerdictValid         Verdict = "VALID"
    VerdictValidWithGaps Verdict = "VALID_WITH_GAPS"
    VerdictInvalid       Verdict = "INVALID"
)

// VerdictWeight defines the ordering used by --fail-on comparison.
// VALID(0) < VALID_WITH_GAPS(1) < INVALID(2)
var VerdictWeight = map[Verdict]int{
    VerdictValid:         0,
    VerdictValidWithGaps: 1,
    VerdictInvalid:       2,
}

type Category string
const (
    CategoryNonTestableRequirement  Category = "NON_TESTABLE_REQUIREMENT"
    CategoryAmbiguousBehavior       Category = "AMBIGUOUS_BEHAVIOR"
    CategoryContradiction           Category = "CONTRADICTION"
    CategoryMissingFailureMode      Category = "MISSING_FAILURE_MODE"
    CategoryUndefinedInterface      Category = "UNDEFINED_INTERFACE"
    CategoryMissingInvariant        Category = "MISSING_INVARIANT"
    CategoryScopeLeak               Category = "SCOPE_LEAK"
    CategoryOrderingUndefined       Category = "ORDERING_UNDEFINED"
    CategoryTerminologyInconsistent Category = "TERMINOLOGY_INCONSISTENT"
    CategoryUnspecifiedConstraint   Category = "UNSPECIFIED_CONSTRAINT"
    CategoryAssumptionRequired      Category = "ASSUMPTION_REQUIRED"
)

// Patch is the JSON-serializable patch type returned by the LLM and written to the report.
// internal/patch uses a separate internal type for diff processing; conversion is explicit.
type Patch struct {
    IssueID string `json:"issue_id"`
    Before  string `json:"before"`
    After   string `json:"after"`
}
```

**Key rule:** Score and verdict fields on `Summary` are never populated from LLM output — they are computed. `summary.*_count` fields always reflect all issues before any `--severity-threshold` filtering.

---

### 2. `internal/spec` — Spec Loader

Responsibilities:
- Read file from disk
- Compute SHA-256 hash (for `input.spec_hash`)
- Inject line numbers: prefix every line with `L{n}: ` before prompt insertion
- Detect presence of headings, numbered requirements (regex scan)
- Expose line count for evidence bounds validation

```go
type Spec struct {
    Path      string
    Hash      string // "sha256:..."
    Raw       string // original content
    Numbered  string // content with "L1: …" prefixes
    LineCount int
}

func Load(path string) (*Spec, error)
```

**Critical:** `LineCount` is used by the validator to reject any evidence where `line_end > LineCount`.

---

### 3. `internal/redact` — Secret Redaction

Apply before any spec or context content is passed to the LLM.

Patterns to match (regex):
- `sk-[a-zA-Z0-9]{20,}` — OpenAI / Anthropic keys
- `AKIA[0-9A-Z]{16}` — AWS access keys
- `[Bb]earer\s+[A-Za-z0-9\-._~+/]+=*` — Bearer tokens
- `[Pp]assword\s*[:=]\s*\S+` — Inline passwords
- `-----BEGIN [A-Z ]+ KEY-----[\s\S]+?-----END [A-Z ]+ KEY-----` — PEM blocks
- `eyJ[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+` — JWTs

Replace matched content with `[REDACTED]`. Preserve surrounding whitespace and line structure (line count must not change after redaction).

Redaction hits are logged to stderr as `WARN: redacted <pattern-type> on line <n>` in verbose mode.

```go
func Redact(input string) string
func RedactFile(path string) (string, error)
```

Unit tests must verify:
- Each pattern is detected
- Non-secret text is unchanged
- Multi-line PEM blocks are fully replaced
- Line count is preserved after redaction

---

### 4. `internal/context` — Context File Loader

Loads optional grounding files and formats them for the prompt.

```go
type ContextFile struct {
    Path    string
    Content string // after redaction
}

func Load(paths []string) ([]ContextFile, error)
func FormatForPrompt(files []ContextFile) string
```

`FormatForPrompt` wraps each file:
```
<context file="glossary.md">
{content}
</context>
```

Context files are redacted before use (same `redact.Redact()` call as spec content).

Unit tests must verify:
- `FormatForPrompt` wraps each file in the correct XML-style tags
- Redaction is applied to content before wrapping
- Multiple files are concatenated in order

---

## Phase 2 — Core Logic

### 5. `internal/profile` — Profile Rules

Each profile is a Go struct with:
- `Name string`
- `RequiredSections []string` — headings the spec must contain
- `ForbiddenPhrases []string` — vague language patterns (e.g., "as needed", "TBD", "fast")
- `DomainInvariants []string` — natural-language rules injected into the LLM prompt
- `ExtraCategories []Category` — additional issue categories to enforce

Built-in profiles:

**general** — baseline; checks all 11 categories; minimal domain invariants.

**backend-api** — adds invariants:
- Every endpoint must define request/response schemas
- All error codes must be enumerated
- Auth requirements must be stated per endpoint
- Rate limits must be numeric

**regulated-system** — adds invariants:
- Audit trail requirements must be explicit
- Data retention periods must be stated as durations
- Every state transition must be enumerable
- Rollback behavior must be defined

**event-driven** — adds invariants:
- Every event must have defined ordering guarantees (or explicitly none)
- At-least-once vs exactly-once delivery must be stated
- Consumer failure modes must be specified
- Schema evolution strategy must be present

```go
type Profile struct {
    Name             string
    RequiredSections []string
    ForbiddenPhrases []string
    DomainInvariants []string
    ExtraCategories  []Category
}

func Get(name string) (*Profile, error)
func (p *Profile) FormatRulesForPrompt() string
```

---

### 6. `internal/schema/validate` — JSON Validation

Validates raw LLM output before it is trusted by any other package. Building this before `internal/review` ensures review tests can use validated fixture data.

```go
func Parse(raw string, lineCount int) (*schema.Report, error)
```

`Parse` performs:
1. Strip markdown code fences (` ```json … ``` `) from raw string
2. Strict JSON unmarshal into `schema.Report`
3. Structural validation:
   - Required fields present (`tool`, `version`, `issues`, `questions`)
   - All `severity` values are valid (`INFO`, `WARN`, `CRITICAL`)
   - All `category` values match a defined constant
   - Issue IDs match `ISSUE-\d{4}` format
   - Question IDs match `Q-\d{4}` format
4. Evidence bounds validation: every `line_start` and `line_end` must satisfy `1 ≤ line_start ≤ line_end ≤ lineCount`

Returns a structured error describing the first validation failure, for use in the repair prompt.

Unit tests:
- Valid report passes all checks
- Missing required field returns error
- Invalid severity string returns error
- Evidence `line_end` > `lineCount` returns error
- Markdown fences are stripped before parse

---

### 7. `internal/review` — Scoring and Verdict

This package owns the authoritative scoring and verdict logic. It never trusts these values from the LLM.

```go
// Score computes the deterministic score from ALL issues (before any threshold filtering).
func Score(issues []schema.Issue) int {
    score := 100
    for _, issue := range issues {
        switch issue.Severity {
        case schema.SeverityCritical:
            score -= 20
        case schema.SeverityWarn:
            score -= 7
        case schema.SeverityInfo:
            score -= 2
        }
    }
    if score < 0 {
        score = 0
    }
    return score
}

// Verdict computes the deterministic verdict from ALL issues and questions.
// CRITICAL questions are treated equivalently to CRITICAL issues:
// a spec with only CRITICAL questions (and no issues) receives INVALID.
func Verdict(issues []schema.Issue, questions []schema.Question) schema.Verdict {
    for _, issue := range issues {
        if issue.Severity == schema.SeverityCritical {
            return schema.VerdictInvalid
        }
    }
    for _, q := range questions {
        if q.Severity == schema.SeverityCritical {
            return schema.VerdictInvalid
        }
    }
    for _, issue := range issues {
        if issue.Severity == schema.SeverityWarn {
            return schema.VerdictValidWithGaps
        }
    }
    if len(issues) > 0 { // INFO only
        return schema.VerdictValidWithGaps
    }
    return schema.VerdictValid
}
```

Unit tests:
- 3 CRITICAL issues → score 40, verdict INVALID
- Score clamps at 0 (not negative)
- WARN-only issues → VALID_WITH_GAPS
- INFO-only issues → VALID_WITH_GAPS
- Zero issues, zero questions → VALID
- Zero issues, one CRITICAL question → INVALID
- Mixed CRITICAL question + WARN issue → INVALID

---

### 8. `internal/patch` — Diff Generation

Converts `schema.Patch` entries from the LLM into a unified diff written to `--patch-out`.

**Internal type** (separate from `schema.Patch`):
```go
type diffPatch struct {
    IssueID   string
    Before    string
    After     string
    LineStart int // resolved during matching
}
```

**Matching strategy:** Before falling back to skip, attempt normalized matching:
1. Trim trailing whitespace from each line of the "before" text
2. Normalize line endings (CRLF → LF)
3. Attempt substring match against the normalized spec
4. If still not found: log `WARN: patch for ISSUE-XXXX could not be located in spec (before text not matched)` to stderr and skip

The report is never failed because of a patch miss.

```go
func GenerateDiff(specPath string, patches []schema.Patch) (string, error)
```

Unit tests:
- Exact match generates correct unified diff hunk
- Whitespace-normalized match succeeds where exact match would fail
- Unmatched "before" text is skipped (no error returned, warning logged)

---

## Phase 3 — LLM Integration

### 9. `internal/llm` — Provider Interface and Prompt

#### Provider Interface

```go
type Request struct {
    SystemPrompt string
    UserPrompt   string
    Temperature  float64
    MaxTokens    int
    Model        string
}

type Response struct {
    Content string
    Model   string // echoed back for meta
}

type Provider interface {
    Complete(ctx context.Context, req *Request) (*Response, error)
}

func NewProvider(model string) (Provider, error)
// Parses "anthropic:claude-sonnet-4-6" → AnthropicProvider
// Parses "openai:gpt-4o" → OpenAIProvider
// Returns error for unknown provider prefix
```

Both implementations use `net/http` directly against the provider's REST API. No SDK dependencies.

#### Prompt Construction

Defined in `prompt.go`. The prompt is the most quality-sensitive component in the system.

**System prompt:**
```
You are a specification auditor. Your job is to identify defects in software specifications.

Defect categories you must check:
- NON_TESTABLE_REQUIREMENT: requirement cannot be verified by a test
- AMBIGUOUS_BEHAVIOR: two engineers could implement differently
- CONTRADICTION: two statements cannot both be true
- MISSING_FAILURE_MODE: what happens when X fails is not stated
- UNDEFINED_INTERFACE: a referenced interface has no specification
- MISSING_INVARIANT: a property that must always hold is not stated
- SCOPE_LEAK: spec describes implementation, not behavior
- ORDERING_UNDEFINED: sequence of operations is ambiguous
- TERMINOLOGY_INCONSISTENT: same concept named differently
- UNSPECIFIED_CONSTRAINT: implicit constraint not made explicit
- ASSUMPTION_REQUIRED: must assume something unstated to implement

Severity rules:
- CRITICAL: must be resolved before implementation can begin
- WARN: should be resolved; implementation possible but risky
- INFO: note for clarity; does not block implementation

Anti-hallucination rules:
- Only cite lines that exist in the provided spec (lines are prefixed L1:, L2:, etc.)
- Do not invent requirements not present in the spec
- Do not suggest architectural solutions
- Every issue must have at least one evidence block with valid line numbers
- Issue IDs must follow format ISSUE-XXXX (four digits, zero-padded)
- Question IDs must follow format Q-XXXX

Output rules:
- Return JSON only — no prose, no markdown fences, no explanation
- JSON must match the provided schema exactly
- Do not include score or verdict — those are computed externally

{profile_rules}
```

**Strict mode injection** (appended to system prompt when `--strict` is set):
```
STRICT MODE ENABLED: Treat all silence as ambiguity. Any behavior not explicitly
stated must be flagged. Any assumption required to implement must be filed as CRITICAL.
Label all uncertain findings with tag "assumption".
```

**User prompt:**
```
Analyze the following specification for defects.

<spec file="{spec_path}">
{numbered_spec_content}
</spec>

{context_files_if_any}

Return your findings as JSON with this structure:
{
  "issues": [
    {
      "id": "ISSUE-0001",
      "severity": "CRITICAL",
      "category": "NON_TESTABLE_REQUIREMENT",
      "title": "...",
      "description": "...",
      "evidence": [{"path": "SPEC.md", "line_start": 10, "line_end": 12, "quote": "..."}],
      "impact": "...",
      "recommendation": "...",
      "blocking": true,
      "tags": []
    }
  ],
  "questions": [
    {
      "id": "Q-0001",
      "severity": "CRITICAL",
      "question": "...",
      "why_needed": "...",
      "blocks": [],
      "evidence": [{"path": "SPEC.md", "line_start": 10, "line_end": 12, "quote": "..."}]
    }
  ],
  "patches": [
    {
      "issue_id": "ISSUE-0001",
      "before": "exact text from spec",
      "after": "corrected text"
    }
  ]
}
```

#### Retry Logic

**`attempt(ctx, provider, req) → (*schema.Report, error)`:**
1. Call `provider.Complete(ctx, req)` → get raw string
2. Strip markdown code fences from raw string
3. `validate.Parse(raw, lineCount)` → on error, return error with description

**Orchestration in `runCheck`:**
1. `report, err = attempt(ctx, provider, fullReq)`; if `err == nil` → proceed to scoring
2. Build repair request: same `req` with `UserPrompt` appended: `"\n\nYour previous response failed validation with error: {err}. Return only valid JSON matching the schema above."`
3. `report, err = attempt(ctx, provider, repairReq)`; if `err == nil` → proceed to scoring
4. If `err != nil` → exit code 5

Retry uses the same temperature as the original request. The repair appends to the existing user prompt rather than replacing it, so the LLM retains full context.

Unit tests for `internal/llm`:
- `BuildPrompt` output contains line-numbered spec content
- `BuildPrompt` output contains profile rules when profile has invariants
- `BuildPrompt` output contains context file XML tags when context files provided
- `BuildPrompt` output contains strict mode text when `--strict` is set
- `BuildPrompt` output does NOT contain strict mode text when `--strict` is not set
- `NewProvider` returns error for unknown provider prefix

---

## Phase 4 — Output and CLI

### 10. `internal/render` — Formatters

```go
type Renderer interface {
    Render(report *schema.Report) ([]byte, error)
}

func NewRenderer(format string) (Renderer, error)
```

**JSON renderer** (`json.go`): `json.MarshalIndent` with 2-space indent.

**Markdown renderer** (`markdown.go`): Text template producing:
```
# SpecCritic Report

**Verdict:** INVALID
**Score:** 61/100
**Critical:** 3 | **Warn:** 6 | **Info:** 4
> Note: counts reflect all findings; --severity-threshold may hide some from this output.

---

## Issues

### ISSUE-0001 · CRITICAL · NON_TESTABLE_REQUIREMENT
**Performance requirement is not measurable**

> SPEC.md L47–48: "The system must provide fast responses."

**Impact:** Implementations cannot be verified or compared.
**Recommendation:** Define concrete latency targets and measurement conditions.

---

## Clarification Questions

### Q-0001 · CRITICAL
What are the acceptable latency bounds for this endpoint?
…
```

---

### 11. `cmd/speccritic` — CLI Orchestration

Uses `cobra`. Single subcommand: `check`.

```go
var checkCmd = &cobra.Command{
    Use:   "check <spec-file>",
    Short: "Evaluate a specification for defects",
    Args:  cobra.ExactArgs(1),
    RunE:  runCheck,
}
```

Registered flags (all on `checkCmd`):
- `--format string` (default `"json"`) — `"json"` or `"md"`
- `--out string` — write output to file instead of stdout
- `--context stringArray` — repeatable via `StringArrayVar`; e.g. `--context a.md --context b.md`
- `--profile string` (default `"general"`)
- `--strict bool`
- `--fail-on string` — `"VALID_WITH_GAPS"` or `"INVALID"`
- `--severity-threshold string` (default `"info"`) — `"info"`, `"warn"`, or `"critical"`
- `--patch-out string`
- `--temperature float64` (default `0.2`)
- `--max-tokens int` (default `4096`)
- `--offline bool`
- `--verbose bool`
- `--debug bool`

`runCheck` orchestrates the pipeline:
1. Parse and validate flags: `--format`, `--profile`, `--fail-on`, `--severity-threshold`; capture `--temperature` (default 0.2) and `--max-tokens`
2. If `--offline`: check `SPECCRITIC_MODEL` env var is set; if not, exit 3
3. Load spec via `spec.Load()` — exit 3 on error
4. Apply `redact.Redact()` to spec content
5. Load context files via `context.Load(contextPaths)` — exit 3 on error
6. Redact context file contents
7. Load profile via `profile.Get()` — exit 3 on unknown profile
8. Construct `llm.Request` from `llm.BuildPrompt(spec, contextFiles, profile, strict)`, setting `Temperature` and `MaxTokens` from parsed flags; set `Model` from `SPECCRITIC_MODEL` env var
9. If `--debug`: dump redacted prompt to stderr (note: output includes file paths as-is)
10. Call LLM with retry logic — exit 4 on provider error, exit 5 on persistent validation failure
11. Compute score via `review.Score(report.Issues)` and verdict via `review.Verdict(report.Issues, report.Questions)`
12. Populate `report.Summary` (counts from ALL issues, pre-filter); populate `report.Meta.Temperature` from flag value
13. Apply `--severity-threshold` filter to `report.Issues` for output only (score and verdict already computed)
14. If `--patch-out`: call `patch.GenerateDiff()`; write to file; on write failure log `WARN: patch write failed: <err>` to stderr and continue
15. Render output via `render.Renderer.Render(report)`
16. Write to stdout or `--out` file — exit 3 on write error
17. Evaluate `--fail-on`: if `schema.VerdictWeight[actualVerdict] >= schema.VerdictWeight[threshold]`, exit 2; otherwise exit 0

**Logging convention (all to stderr):**
- Normal operation: no output (silent on success)
- Warnings (patch skips, redaction hits in verbose mode): `WARN: <message>`
- `--verbose`: each pipeline step logs `INFO: <stage> (<duration>)`
- `--debug`: dumps full redacted prompt after step 9

---

## Phase 5 — Testing

### Unit Tests (per package)

| Package | Tests |
|---------|-------|
| `internal/redact` | Each regex pattern matches; non-secret text unchanged; line count preserved after redaction |
| `internal/spec` | Line numbering correct (`L1:` prefix); SHA-256 hash stable; `LineCount` accurate |
| `internal/context` | `FormatForPrompt` wraps files in correct XML tags; redaction applied to content; multiple files concatenated in order |
| `internal/profile` | Each named profile loads without error; unknown name returns error |
| `internal/schema/validate` | Valid report passes; missing required field fails; invalid severity fails; line bounds outside `LineCount` fail; markdown fences stripped before parse |
| `internal/review` | 3 CRITICAL → score 40; score clamps at 0; CRITICAL issue → INVALID; WARN-only → VALID_WITH_GAPS; INFO-only → VALID_WITH_GAPS; zero issues + zero questions → VALID; zero issues + CRITICAL question → INVALID |
| `internal/patch` | Exact match generates correct hunk; whitespace-normalized match succeeds; unmatched "before" skipped with no error |
| `internal/llm` | Prompt contains line-numbered spec; profile rules injected when present; context XML tags present; strict mode text injected when `--strict`; strict mode text absent when not `--strict`; `NewProvider` errors on unknown prefix |

### Golden Tests

Located in `testdata/specs/`:

**`bad_spec.md`** — contains:
- "The system must be fast." (NON_TESTABLE_REQUIREMENT → CRITICAL)
- A requirement that contradicts another (CONTRADICTION → CRITICAL)
- "Handle errors appropriately." (AMBIGUOUS_BEHAVIOR → WARN)
- Missing failure mode for a stated operation (MISSING_FAILURE_MODE → WARN)

Mock: `testdata/llm/mock_response_bad.json` returns ≥2 CRITICAL findings.
Expected: verdict = INVALID, at least 2 CRITICAL findings, score ≤ 60.

**`good_spec.md`** — a well-formed spec with concrete measurable requirements, explicit error handling, defined interfaces, no contradictions.

Mock: `testdata/llm/mock_response_good.json` returns zero issues and zero questions.
Expected: verdict = VALID, score = 100.

### Integration Tests

Use an `internal/llm` mock that implements `Provider` and returns fixtures keyed by spec path. Test:
- Full pipeline produces non-error output for `good_spec.md` (exit 0)
- Exit code 2 returned when `--fail-on INVALID` and spec produces INVALID verdict
- Exit code 2 returned when `--fail-on VALID_WITH_GAPS` and spec produces VALID_WITH_GAPS verdict
- Exit code 0 returned when `--fail-on INVALID` and spec produces VALID_WITH_GAPS verdict
- Exit code 3 returned for missing spec file
- `--format md` produces non-empty markdown output
- `--patch-out` creates a file with valid unified diff syntax
- `--debug` writes prompt to stderr (capture stderr in test)
- `--severity-threshold critical` omits WARN and INFO from `issues` array but summary counts still reflect all findings
- `--offline` with no `SPECCRITIC_MODEL` env var exits with code 3

---

## Implementation Order

```
1.  internal/schema            — types + constants (no logic)
2.  internal/spec              — file I/O + line numbering
3.  internal/redact            — string manipulation
4.  internal/context           — file loading + prompt formatting
5.  internal/profile           — rules data, no external deps
6.  internal/schema/validate   — JSON parse + structural validation
7.  internal/review            — scoring/verdict math (uses validated fixtures in tests)
8.  internal/llm               — provider interface + prompt builder
9.  internal/patch             — diff generation
10. internal/render            — output formatters
11. cmd/speccritic             — CLI wiring
12. testdata fixtures           — golden specs + separate mock LLM responses
13. All tests                  — written alongside or immediately after each package
```

Note: `internal/schema/validate` (step 6) is built before `internal/review` (step 7) so that review unit tests can use pre-validated fixture data.
