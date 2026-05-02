package incremental

import (
	"testing"

	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/spec"
)

func TestMergeReportPreservesReusedIDsAndAllocatesAfterMax(t *testing.T) {
	s := spec.New("SPEC.md", "# Spec\n## Behavior\none\ntwo\n")
	reused := issueAt("ISSUE-0007", 3, "one")
	newIssue := issueAt("ISSUE-0001", 4, "two")
	newIssue.Tags = []string{TagIncrementalReview, "range:RANGE-1"}
	report, err := MergeReport(MergeInput{
		Spec:         s,
		ReusedIssues: []schema.Issue{reused},
		RangeResults: []RangeResult{{Range: ReviewRange{ID: "RANGE-1"}, Report: &schema.Report{
			Issues: []schema.Issue{newIssue},
		}}},
		Profile:           "general",
		SeverityThreshold: "info",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Issues) != 2 {
		t.Fatalf("issues = %#v", report.Issues)
	}
	if report.Issues[0].ID != "ISSUE-0007" || report.Issues[1].ID != "ISSUE-0008" {
		t.Fatalf("issue IDs = %s, %s", report.Issues[0].ID, report.Issues[1].ID)
	}
}

func TestMergeReportDeduplicatesNewAgainstReused(t *testing.T) {
	s := spec.New("SPEC.md", "# Spec\n## Behavior\none\n")
	reused := issueAt("ISSUE-0003", 3, "one")
	reused.Tags = []string{TagReused}
	newIssue := issueAt("ISSUE-0001", 3, "one")
	newIssue.Description = "longer current description"
	newIssue.Evidence = append(newIssue.Evidence, schema.Evidence{Path: "SPEC.md", LineStart: 2, LineEnd: 3, Quote: "Behavior\none"})
	newIssue.Tags = []string{TagIncrementalReview, "range:RANGE-1"}
	report, err := MergeReport(MergeInput{
		Spec:         s,
		ReusedIssues: []schema.Issue{reused},
		RangeResults: []RangeResult{{Range: ReviewRange{ID: "RANGE-1"}, Report: &schema.Report{
			Issues: []schema.Issue{newIssue},
		}}},
		Profile:           "general",
		SeverityThreshold: "info",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Issues) != 1 || report.Issues[0].ID != "ISSUE-0003" {
		t.Fatalf("issues = %#v", report.Issues)
	}
	if report.Issues[0].Description != "longer current description" {
		t.Fatalf("description = %q", report.Issues[0].Description)
	}
	if len(report.Issues[0].Evidence) != 2 {
		t.Fatalf("evidence = %#v", report.Issues[0].Evidence)
	}
	if !hasTag(report.Issues[0].Tags, TagReused) || !hasTag(report.Issues[0].Tags, TagIncrementalReview) {
		t.Fatalf("tags = %#v", report.Issues[0].Tags)
	}
}

func TestMergeReportAllocatesAfterFiveDigitIDs(t *testing.T) {
	s := spec.New("SPEC.md", "# Spec\n## Behavior\none\ntwo\n")
	reused := issueAt("ISSUE-10000", 3, "one")
	newIssue := issueAt("ISSUE-0001", 4, "two")
	report, err := MergeReport(MergeInput{
		Spec:         s,
		ReusedIssues: []schema.Issue{reused},
		RangeResults: []RangeResult{{Range: ReviewRange{ID: "RANGE-1"}, Report: &schema.Report{
			Issues: []schema.Issue{newIssue},
		}}},
		Profile:           "general",
		SeverityThreshold: "info",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Issues[1].ID != "ISSUE-10001" {
		t.Fatalf("new ID = %s", report.Issues[1].ID)
	}
}

func TestMergeReportRecomputesSummaryAndMetadata(t *testing.T) {
	s := spec.New("SPEC.md", "# Spec\n## Behavior\none\n")
	meta := &schema.IncrementalMeta{
		Enabled:          true,
		PreviousSpecHash: "sha256:old",
		Mode:             "auto",
		ReviewedSections: 1,
		ReusedSections:   2,
	}
	report, err := MergeReport(MergeInput{
		Spec:                s,
		ReusedIssues:        []schema.Issue{issueAt("ISSUE-0001", 3, "one")},
		Profile:             "general",
		SeverityThreshold:   "info",
		IncrementalMetadata: meta,
		IncludeMetadata:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.WarnCount != 1 || report.Summary.Score != 93 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	if report.Meta.Incremental == nil || report.Meta.Incremental.PreviousSpecHash != "sha256:old" {
		t.Fatalf("meta = %#v", report.Meta)
	}
}

func TestMergeReportDeduplicatesInitialPreflightAndReused(t *testing.T) {
	s := spec.New("SPEC.md", "# Spec\n## Behavior\none\n")
	preflight := issueAt("ISSUE-0001", 3, "one")
	reused := issueAt("ISSUE-0002", 3, "one")
	report, err := MergeReport(MergeInput{
		Spec:              s,
		PreflightIssues:   []schema.Issue{preflight},
		ReusedIssues:      []schema.Issue{reused},
		Profile:           "general",
		SeverityThreshold: "info",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Issues) != 1 {
		t.Fatalf("issues = %#v", report.Issues)
	}
}

func TestMergeReportOmitsMetadataByDefaultAndFiltersInvalidPatches(t *testing.T) {
	s := spec.New("SPEC.md", "# Spec\nold text\n")
	report, err := MergeReport(MergeInput{
		Spec:              s,
		Profile:           "general",
		SeverityThreshold: "info",
		Patches: []schema.Patch{
			{IssueID: "ISSUE-0001", Before: "old text", After: "new text"},
			{IssueID: "ISSUE-0002", Before: "missing", After: "new text"},
		},
		IncrementalMetadata: &schema.IncrementalMeta{Enabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Meta.Incremental != nil {
		t.Fatalf("metadata should be omitted by default: %#v", report.Meta)
	}
	if len(report.Patches) != 1 || report.Patches[0].IssueID != "ISSUE-0001" {
		t.Fatalf("patches = %#v", report.Patches)
	}
}
