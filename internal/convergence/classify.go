package convergence

import (
	"strconv"

	"github.com/dshills/speccritic/internal/schema"
)

// CompareReports classifies current and previous findings and returns
// convergence summary data. It does not mutate either report.
func CompareReports(previous, current *schema.Report, cfg Config, compat Compatibility) Result {
	result := Result{
		Status: compat.Status,
		Notes:  append([]string(nil), compat.Notes...),
		Summary: Summary{
			BySeverity: make(map[string]CountSet),
			ByKind:     make(map[string]CountSet),
		},
	}
	if result.Status == "" {
		result.Status = StatusComplete
	}
	if current == nil {
		if result.Status == "" || result.Status == StatusComplete {
			result.Status = StatusUnavailable
		}
		return result
	}

	curFindings := reportFindings(current)
	if previous == nil || result.Status == StatusUnavailable {
		result.Status = StatusUnavailable
		for _, cur := range curFindings {
			result.Current = append(result.Current, CurrentFinding{Finding: cur, Status: FindingNew})
			addCurrentCount(&result.Summary, cur, FindingNew)
		}
		return result
	}

	prevFindings := reportFindings(previous)
	matches := MatchFindings(prevFindings, curFindings)
	matchedPrev := make(map[string]Match, len(matches))
	matchedCur := make(map[string]Match, len(matches))
	for _, match := range matches {
		matchedPrev[findingKey(match.Previous)] = match
		matchedCur[findingKey(match.Current)] = match
	}

	for _, cur := range curFindings {
		key := findingKey(cur)
		if match, ok := matchedCur[key]; ok {
			result.Current = append(result.Current, CurrentFinding{
				Finding:    cur,
				Status:     FindingStillOpen,
				PreviousID: match.Previous.ID,
				Confidence: match.Score,
			})
			addCurrentCount(&result.Summary, cur, FindingStillOpen)
			continue
		}
		result.Current = append(result.Current, CurrentFinding{Finding: cur, Status: FindingNew})
		addCurrentCount(&result.Summary, cur, FindingNew)
	}

	for _, prev := range prevFindings {
		if _, ok := matchedPrev[findingKey(prev)]; ok {
			continue
		}
		status := classifyHistorical(prev, cfg)
		result.Previous = append(result.Previous, HistoricalFinding{Finding: prev, Status: status})
		addHistoricalCount(&result.Summary, prev, status)
		if status == HistoricalUntracked && result.Status == StatusComplete {
			result.Status = StatusPartial
		}
	}

	return result
}

func reportFindings(report *schema.Report) []TrackedFinding {
	issues := TrackIssues(report.Issues)
	questions := TrackQuestions(report.Questions)
	findings := make([]TrackedFinding, 0, len(issues)+len(questions))
	for i := range issues {
		issues[i].SourceIndex = i
	}
	findings = append(findings, issues...)
	offset := len(issues)
	for i := range questions {
		questions[i].SourceIndex = offset + i
	}
	findings = append(findings, questions...)
	return findings
}

func classifyHistorical(prev TrackedFinding, cfg Config) HistoricalStatus {
	if thresholdDrops(prev.Severity, cfg.SeverityThreshold) {
		return HistoricalDropped
	}
	switch cfg.CurrentReviewCoverage {
	case CoverageFull:
		return HistoricalResolved
	case CoveragePreflightOnly:
		if hasTag(prev.Tags, "preflight") {
			return HistoricalResolved
		}
		return HistoricalUntracked
	case CoverageIncremental:
		return HistoricalUntracked
	default:
		return HistoricalUntracked
	}
}

func thresholdDrops(severity schema.Severity, threshold string) bool {
	if threshold == "" {
		return false
	}
	sevRank, sevOK := severityRank(string(severity))
	thresholdRank, thresholdOK := severityRank(threshold)
	return sevOK && thresholdOK && sevRank < thresholdRank
}

func addCurrentCount(summary *Summary, finding TrackedFinding, status FindingStatus) {
	switch status {
	case FindingNew:
		summary.Current.New++
	case FindingStillOpen:
		summary.Current.StillOpen++
	case FindingUntracked:
		summary.Current.Untracked++
	}
	sev := string(finding.Severity)
	counts := summary.BySeverity[sev]
	switch status {
	case FindingNew:
		counts.New++
	case FindingStillOpen:
		counts.StillOpen++
	case FindingUntracked:
		counts.Untracked++
	}
	summary.BySeverity[sev] = counts
	kind := string(finding.Kind)
	kindCounts := summary.ByKind[kind]
	switch status {
	case FindingNew:
		kindCounts.New++
	case FindingStillOpen:
		kindCounts.StillOpen++
	case FindingUntracked:
		kindCounts.Untracked++
	}
	summary.ByKind[kind] = kindCounts
}

func addHistoricalCount(summary *Summary, finding TrackedFinding, status HistoricalStatus) {
	switch status {
	case HistoricalResolved:
		summary.Previous.Resolved++
	case HistoricalDropped:
		summary.Previous.Dropped++
	case HistoricalUntracked:
		summary.Previous.Untracked++
	}
	sev := string(finding.Severity)
	counts := summary.BySeverity[sev]
	switch status {
	case HistoricalResolved:
		counts.Resolved++
	case HistoricalDropped:
		counts.Dropped++
	case HistoricalUntracked:
		counts.Untracked++
	}
	summary.BySeverity[sev] = counts
	kind := string(finding.Kind)
	kindCounts := summary.ByKind[kind]
	switch status {
	case HistoricalResolved:
		kindCounts.Resolved++
	case HistoricalDropped:
		kindCounts.Dropped++
	case HistoricalUntracked:
		kindCounts.Untracked++
	}
	summary.ByKind[kind] = kindCounts
}

func findingKey(f TrackedFinding) string {
	return string(f.Kind) + ":" + strconv.Itoa(f.SourceIndex)
}

func hasTag(tags []string, want string) bool {
	for _, tag := range tags {
		if normalizeToken(tag) == normalizeToken(want) {
			return true
		}
	}
	return false
}
