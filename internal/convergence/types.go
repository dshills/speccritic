package convergence

import "github.com/dshills/speccritic/internal/schema"

// Mode controls whether convergence comparison is disabled, required, or
// best-effort.
type Mode string

const (
	ModeAuto Mode = "auto"
	ModeOn   Mode = "on"
	ModeOff  Mode = "off"
)

// Status describes the overall convergence comparison quality.
type Status string

const (
	StatusComplete    Status = "complete"
	StatusPartial     Status = "partial"
	StatusUnavailable Status = "unavailable"
)

// FindingKind distinguishes issues from clarification questions.
type FindingKind string

const (
	KindIssue    FindingKind = "issue"
	KindQuestion FindingKind = "question"
)

// FindingStatus is assigned to active current-report findings.
type FindingStatus string

const (
	FindingNew       FindingStatus = "new"
	FindingStillOpen FindingStatus = "still_open"
	FindingUntracked FindingStatus = "untracked"
)

// HistoricalStatus is assigned to previous-report findings not present as
// active current findings.
type HistoricalStatus string

const (
	HistoricalResolved  HistoricalStatus = "resolved"
	HistoricalDropped   HistoricalStatus = "dropped"
	HistoricalUntracked HistoricalStatus = "untracked"
)

// Config contains convergence settings shared by CLI, web, and app code.
type Config struct {
	Mode                  Mode
	Report                bool
	StrictCompatibility   bool
	Profile               string
	ReviewStrict          bool
	SeverityThreshold     string
	RedactionConfigHash   string
	CurrentSpecHash       string
	CurrentReviewCoverage ReviewCoverage
}

// ReviewCoverage describes how completely the current report reviewed the
// current spec.
type ReviewCoverage string

const (
	CoverageFull          ReviewCoverage = "full"
	CoveragePreflightOnly ReviewCoverage = "preflight_only"
	CoverageIncremental   ReviewCoverage = "incremental"
	CoverageUnknown       ReviewCoverage = "unknown"
)

// Compatibility describes whether a previous report is usable for convergence
// comparison and whether the comparison is complete or partial.
type Compatibility struct {
	Status Status
	Notes  []string
	Err    error
}

// PreviousReport wraps a validated previous SpecCritic report and optional
// metadata not represented directly in the public schema type.
type PreviousReport struct {
	Report              *schema.Report
	RedactionConfigHash string
}

// TrackedFinding is the normalized representation used by matching phases.
type TrackedFinding struct {
	Kind        FindingKind
	ID          string
	Severity    schema.Severity
	Category    string
	Text        string
	SectionPath []string
	Evidence    []schema.Evidence
	Tags        []string
	SourceIndex int
	Fingerprint string
}

// Match links one previous finding to one current finding.
type Match struct {
	Previous TrackedFinding
	Current  TrackedFinding
	Score    float64
	Method   string
}

// Result is the convergence comparison output before schema rendering.
type Result struct {
	Status   Status
	Current  []CurrentFinding
	Previous []HistoricalFinding
	Summary  Summary
	Notes    []string
}

type CurrentFinding struct {
	Finding    TrackedFinding
	Status     FindingStatus
	PreviousID string
	Confidence float64
}

type HistoricalFinding struct {
	Finding TrackedFinding
	Status  HistoricalStatus
}

type Summary struct {
	Current    CountSet
	Previous   HistoricalCountSet
	BySeverity map[string]CountSet
	ByKind     map[string]CountSet
}

type CountSet struct {
	New       int
	StillOpen int
	Resolved  int
	Dropped   int
	Untracked int
}

type HistoricalCountSet struct {
	Resolved  int
	Dropped   int
	Untracked int
}
