package convergence

import (
	"testing"

	"github.com/dshills/speccritic/internal/schema"
)

func TestCompareReportsClassifiesStillOpenNewAndResolved(t *testing.T) {
	prev := reportWithIssues(
		issue("ISSUE-0001", schema.SeverityCritical, "Still open", "same", nil),
		issue("ISSUE-0002", schema.SeverityWarn, "Fixed", "old", nil),
	)
	cur := reportWithIssues(
		issue("ISSUE-0001", schema.SeverityCritical, "Still open", "same", nil),
		issue("ISSUE-0003", schema.SeverityInfo, "New issue", "new", nil),
	)
	result := CompareReports(prev, cur, Config{CurrentReviewCoverage: CoverageFull, SeverityThreshold: "info"}, Compatibility{Status: StatusComplete})
	if result.Summary.Current.StillOpen != 1 || result.Summary.Current.New != 1 || result.Summary.Previous.Resolved != 1 {
		t.Fatalf("summary = %#v", result.Summary)
	}
}

func TestCompareReportsPreflightOnlyDoesNotResolvePriorLLMFinding(t *testing.T) {
	prev := reportWithIssues(issue("ISSUE-0001", schema.SeverityCritical, "LLM issue", "same", nil))
	cur := reportWithIssues()
	result := CompareReports(prev, cur, Config{CurrentReviewCoverage: CoveragePreflightOnly, SeverityThreshold: "info"}, Compatibility{Status: StatusComplete})
	if result.Summary.Previous.Untracked != 1 || result.Status != StatusPartial {
		t.Fatalf("result = %#v", result)
	}
}

func TestCompareReportsPreflightOnlyCanResolvePriorPreflightFinding(t *testing.T) {
	prev := reportWithIssues(issue("ISSUE-0001", schema.SeverityCritical, "Preflight issue", "same", []string{"preflight"}))
	cur := reportWithIssues()
	result := CompareReports(prev, cur, Config{CurrentReviewCoverage: CoveragePreflightOnly, SeverityThreshold: "info"}, Compatibility{Status: StatusComplete})
	if result.Summary.Previous.Resolved != 1 || result.Status != StatusComplete {
		t.Fatalf("result = %#v", result)
	}
}

func TestCompareReportsThresholdDropsPriorFinding(t *testing.T) {
	prev := reportWithIssues(issue("ISSUE-0001", schema.SeverityInfo, "Info issue", "same", nil))
	cur := reportWithIssues()
	result := CompareReports(prev, cur, Config{CurrentReviewCoverage: CoverageFull, SeverityThreshold: "warn"}, Compatibility{Status: StatusComplete})
	if result.Summary.Previous.Dropped != 1 {
		t.Fatalf("summary = %#v", result.Summary)
	}
}

func TestCompareReportsQuestionsByKind(t *testing.T) {
	prev := reportWithQuestions(question("Q-0001", "What happens?", "same"))
	cur := reportWithQuestions(question("Q-0002", "What happens?", "same"))
	result := CompareReports(prev, cur, Config{CurrentReviewCoverage: CoverageFull, SeverityThreshold: "info"}, Compatibility{Status: StatusComplete})
	if result.Summary.ByKind[string(KindQuestion)].StillOpen != 1 {
		t.Fatalf("by kind = %#v", result.Summary.ByKind)
	}
}

func TestCompareReportsMissingPreviousMarksCurrentNew(t *testing.T) {
	cur := reportWithIssues(issue("ISSUE-0001", schema.SeverityCritical, "Current issue", "same", nil))
	result := CompareReports(nil, cur, Config{CurrentReviewCoverage: CoverageFull, SeverityThreshold: "info"}, Compatibility{Status: StatusUnavailable})
	if result.Summary.Current.New != 1 || len(result.Current) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestCompareReportsDuplicateIDsUseSourceIndex(t *testing.T) {
	prev := reportWithIssues(
		issue("ISSUE-0001", schema.SeverityCritical, "A", "a", nil),
		issue("ISSUE-0001", schema.SeverityCritical, "B", "b", nil),
	)
	cur := reportWithIssues(
		issue("ISSUE-0001", schema.SeverityCritical, "A", "a", nil),
		issue("ISSUE-0001", schema.SeverityCritical, "C", "c", nil),
	)
	result := CompareReports(prev, cur, Config{CurrentReviewCoverage: CoverageFull, SeverityThreshold: "info"}, Compatibility{Status: StatusComplete})
	if result.Summary.Current.StillOpen != 1 || result.Summary.Current.New != 1 || result.Summary.Previous.Resolved != 1 {
		t.Fatalf("summary = %#v", result.Summary)
	}
}

func reportWithIssues(issues ...schema.Issue) *schema.Report {
	return &schema.Report{
		Tool:    "speccritic",
		Version: "dev",
		Input:   schema.Input{SpecHash: "sha256:hash", Profile: "general", SeverityThreshold: "info"},
		Issues:  issues,
	}
}

func reportWithQuestions(questions ...schema.Question) *schema.Report {
	report := reportWithIssues()
	report.Questions = questions
	return report
}

func issue(id string, severity schema.Severity, title string, quote string, tags []string) schema.Issue {
	return schema.Issue{
		ID:       id,
		Severity: severity,
		Category: schema.CategoryAmbiguousBehavior,
		Title:    title,
		Evidence: []schema.Evidence{{Quote: quote, LineStart: 1, LineEnd: 1}},
		Tags:     tags,
	}
}

func question(id string, text string, quote string) schema.Question {
	return schema.Question{
		ID:       id,
		Severity: schema.SeverityWarn,
		Question: text,
		Evidence: []schema.Evidence{{Quote: quote, LineStart: 1, LineEnd: 1}},
	}
}
