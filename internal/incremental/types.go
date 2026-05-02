package incremental

import "github.com/dshills/speccritic/internal/schema"

// Mode controls whether incremental review is disabled, required, or used
// only when the planner can prove reuse is safe.
type Mode string

const (
	ModeAuto Mode = "auto"
	ModeOn   Mode = "on"
	ModeOff  Mode = "off"
)

// Config contains incremental settings shared by CLI, web, and app code.
type Config struct {
	Mode                 Mode
	MaxChangeRatio       float64
	MaxRemapFailureRatio float64
	ContextLines         int
	ChunkTokenThreshold  int
	StrictReuse          bool
	ReportMetadata       bool
	Profile              string
	Strict               bool
	SeverityThreshold    string
	ProfileHash          string
	RedactionConfigHash  string
}

// Plan describes the planner's decision for one current spec.
type Plan struct {
	PreviousHash string
	CurrentHash  string
	Mode         Mode
	Sections     []SectionChange
	ReviewRanges []ReviewRange
	ReuseRanges  []ReuseRange
	Fallback     *FallbackReason
}

type SectionChange struct {
	ID             string
	PreviousRange  LineRange
	CurrentRange   LineRange
	Classification string
	HeadingPath    []string
}

type ReviewRange struct {
	ID      string
	Primary LineRange
	Context LineRange
}

type ReuseRange struct {
	ID       string
	Previous LineRange
	Current  LineRange
}

type LineRange struct {
	Start int
	End   int
}

type FallbackReason struct {
	Code    string
	Message string
}

// Metadata is emitted as meta.incremental when the user explicitly requests
// incremental metadata in JSON output.
type Metadata struct {
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

// PreviousReport wraps a validated previous SpecCritic report and optional
// metadata that does not yet exist in the public schema type.
type PreviousReport struct {
	Report              *schema.Report
	ProfileHash         string
	RedactionConfigHash string
}
