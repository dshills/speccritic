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
	Model       string  `json:"model"`
	Temperature float64 `json:"temperature"`
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
