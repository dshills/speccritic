package schema

// Report is the top-level output structure matching the JSON schema v1.
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

// Input captures the parameters used for this run.
type Input struct {
	SpecFile          string   `json:"spec_file"`
	SpecHash          string   `json:"spec_hash"` // SHA-256 of the original file, computed before redaction
	ContextFiles      []string `json:"context_files"`
	Profile           string   `json:"profile"`
	Strict            bool     `json:"strict"`
	SeverityThreshold string   `json:"severity_threshold"`
}

// Summary holds the computed verdict and issue counts.
// Counts always reflect all issues before any --severity-threshold filtering.
type Summary struct {
	Verdict       Verdict `json:"verdict"`
	Score         int     `json:"score"`
	CriticalCount int     `json:"critical_count"`
	WarnCount     int     `json:"warn_count"`
	InfoCount     int     `json:"info_count"`
}

// Meta holds runtime metadata about the LLM call.
type Meta struct {
	Model string `json:"model"`
	// Temperature appears only in reports written by older versions. Current
	// models do not accept a sampling temperature, so none is sent or recorded.
	//
	// Deprecated: never set.
	Temperature float64 `json:"temperature,omitempty"`
	// Effort is the reasoning effort requested with --effort. It is omitted
	// when the provider's default was used.
	Effort       string `json:"effort,omitempty"`
	ChunkSummary string `json:"chunk_summary,omitempty"`
	// DroppedFindings counts model findings that failed local validation and
	// were left out of the report.
	DroppedFindings int `json:"dropped_findings,omitempty"`
	// Usage totals the LLM calls behind this report. It is omitted when the
	// review made none.
	Usage       *UsageMeta       `json:"usage,omitempty"`
	Incremental *IncrementalMeta `json:"incremental,omitempty"`
	Convergence *ConvergenceMeta `json:"convergence,omitempty"`
	Completion  *CompletionMeta  `json:"completion,omitempty"`
}

// UsageMeta totals the LLM calls a review made. The three input token counts
// do not overlap: input_tokens excludes anything read from or written to the
// provider's prompt cache.
type UsageMeta struct {
	// Calls counts every request sent, including repair and continuation calls.
	Calls              int `json:"calls"`
	RepairCalls        int `json:"repair_calls"`
	ContinuationCalls  int `json:"continuation_calls"`
	TruncatedResponses int `json:"truncated_responses"`
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	CacheReadTokens    int `json:"cache_read_tokens"`
	CacheWriteTokens   int `json:"cache_write_tokens"`
	// CallDurationMS adds up the time spent inside calls; with concurrent
	// chunk calls it exceeds WallDurationMS, which runs from the start of the
	// first call to the end of the last.
	CallDurationMS int64 `json:"call_duration_ms"`
	WallDurationMS int64 `json:"wall_duration_ms"`
}

// CompletionMeta describes optional profile-specific completion generation.
type CompletionMeta struct {
	Enabled            bool   `json:"enabled"`
	Mode               string `json:"mode"`
	Template           string `json:"template"`
	GeneratedPatches   int    `json:"generated_patches"`
	SkippedSuggestions int    `json:"skipped_suggestions"`
	OpenDecisions      int    `json:"open_decisions"`
}

const (
	CompletionModeAuto = "auto"
	CompletionModeOn   = "on"
	CompletionModeOff  = "off"

	CompletionTemplateProfile         = "profile"
	CompletionTemplateGeneral         = "general"
	CompletionTemplateBackendAPI      = "backend-api"
	CompletionTemplateRegulatedSystem = "regulated-system"
	CompletionTemplateEventDriven     = "event-driven"
)

var completionTemplateNames = []string{
	CompletionTemplateGeneral,
	CompletionTemplateBackendAPI,
	CompletionTemplateRegulatedSystem,
	CompletionTemplateEventDriven,
}

// CompletionTemplateNames returns the concrete template names accepted in report metadata.
func CompletionTemplateNames() []string {
	return append([]string(nil), completionTemplateNames...)
}

// CompletionInputTemplateNames returns template names accepted from user configuration.
func CompletionInputTemplateNames() []string {
	return append([]string{CompletionTemplateProfile}, completionTemplateNames...)
}

// IsCompletionTemplateName reports whether name is a concrete completion template name.
func IsCompletionTemplateName(name string) bool {
	for _, valid := range completionTemplateNames {
		if name == valid {
			return true
		}
	}
	return false
}

// IsCompletionInputTemplateName reports whether name is accepted from user configuration.
func IsCompletionInputTemplateName(name string) bool {
	if name == CompletionTemplateProfile {
		return true
	}
	return IsCompletionTemplateName(name)
}

// IncrementalMeta describes optional incremental rerun execution details.
type IncrementalMeta struct {
	Enabled          bool    `json:"enabled"`
	PreviousSpecHash string  `json:"previous_spec_hash"`
	Mode             string  `json:"mode"`
	Fallback         bool    `json:"fallback"`
	ReviewedSections int     `json:"reviewed_sections"`
	ReusedSections   int     `json:"reused_sections"`
	ReusedIssues     int     `json:"reused_issues"`
	ReusedQuestions  int     `json:"reused_questions"`
	DroppedFindings  int     `json:"dropped_findings"`
	ChangedLineRatio float64 `json:"changed_line_ratio"`
}

// ConvergenceMeta describes optional report-to-report progress tracking.
type ConvergenceMeta struct {
	Enabled          bool                         `json:"enabled"`
	Mode             string                       `json:"mode"`
	Status           string                       `json:"status"`
	PreviousSpecHash string                       `json:"previous_spec_hash"`
	CurrentSpecHash  string                       `json:"current_spec_hash"`
	Current          ConvergenceCurrentCounts     `json:"current"`
	Previous         ConvergenceHistoricalCounts  `json:"previous"`
	BySeverity       map[string]ConvergenceCounts `json:"by_severity"`
	ByKind           map[string]ConvergenceCounts `json:"by_kind"`
	Notes            []string                     `json:"notes"`
}

const (
	ConvergenceModeAuto = "auto"
	ConvergenceModeOn   = "on"
	ConvergenceModeOff  = "off"

	ConvergenceStatusComplete    = "complete"
	ConvergenceStatusPartial     = "partial"
	ConvergenceStatusUnavailable = "unavailable"
)

type ConvergenceCurrentCounts struct {
	New       int `json:"new"`
	StillOpen int `json:"still_open"`
	Untracked int `json:"untracked"`
}

type ConvergenceHistoricalCounts struct {
	Resolved  int `json:"resolved"`
	Dropped   int `json:"dropped"`
	Untracked int `json:"untracked"`
}

type ConvergenceCounts struct {
	New       int `json:"new"`
	StillOpen int `json:"still_open"`
	Resolved  int `json:"resolved"`
	Dropped   int `json:"dropped"`
	Untracked int `json:"untracked"`
}

// Severity levels for issues and questions.
type Severity string

const (
	SeverityInfo     Severity = "INFO"
	SeverityWarn     Severity = "WARN"
	SeverityCritical Severity = "CRITICAL"
)

// Verdict represents the overall assessment of the specification.
type Verdict string

const (
	VerdictValid         Verdict = "VALID"
	VerdictValidWithGaps Verdict = "VALID_WITH_GAPS"
	VerdictInvalid       Verdict = "INVALID"
)

// VerdictOrdinal returns the numeric ordering for a verdict, used by --fail-on
// comparison. VALID(0) < VALID_WITH_GAPS(1) < INVALID(2).
// Returns -1 for an unrecognised verdict.
func VerdictOrdinal(v Verdict) int {
	switch v {
	case VerdictValid:
		return 0
	case VerdictValidWithGaps:
		return 1
	case VerdictInvalid:
		return 2
	default:
		return -1
	}
}

// Category classifies the type of spec defect.
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

// IsValidCategory reports whether c is one of the 11 defined defect categories.
func IsValidCategory(c Category) bool {
	switch c {
	case CategoryNonTestableRequirement,
		CategoryAmbiguousBehavior,
		CategoryContradiction,
		CategoryMissingFailureMode,
		CategoryUndefinedInterface,
		CategoryMissingInvariant,
		CategoryScopeLeak,
		CategoryOrderingUndefined,
		CategoryTerminologyInconsistent,
		CategoryUnspecifiedConstraint,
		CategoryAssumptionRequired:
		return true
	}
	return false
}

// Issue represents a single defect found in the specification.
type Issue struct {
	ID             string     `json:"id"`
	Severity       Severity   `json:"severity"`
	Category       Category   `json:"category"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	Evidence       []Evidence `json:"evidence"`
	Impact         string     `json:"impact"`
	Recommendation string     `json:"recommendation"`
	Blocking       bool       `json:"blocking"`
	Tags           []string   `json:"tags"`
}

// Question represents a blocking clarification request.
type Question struct {
	ID        string     `json:"id"`
	Severity  Severity   `json:"severity"`
	Question  string     `json:"question"`
	WhyNeeded string     `json:"why_needed"`
	Blocks    []string   `json:"blocks"`
	Evidence  []Evidence `json:"evidence"`
}

// Evidence links a finding to a specific location in the spec.
type Evidence struct {
	Path      string `json:"path"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	Quote     string `json:"quote"`
}

// Patch is the JSON-serializable patch type returned by the LLM.
// internal/patch uses a separate internal type for diff processing.
type Patch struct {
	IssueID string `json:"issue_id"`
	Before  string `json:"before"`
	After   string `json:"after"`
}
