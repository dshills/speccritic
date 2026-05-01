package chunk

import (
	"testing"

	"github.com/dshills/speccritic/internal/schema"
)

func TestMergeReportsSortsRenumbersAndPreservesTags(t *testing.T) {
	result := MergeReports(MergeInput{ChunkResults: []ChunkResult{
		{Report: &schema.Report{Issues: []schema.Issue{
			testIssue("ISSUE-9999", schema.SeverityInfo, schema.CategoryAmbiguousBehavior, "Info", 10, "chunk:CHUNK-2"),
			testIssue("ISSUE-0001", schema.SeverityCritical, schema.CategoryContradiction, "Critical", 2, "chunk:CHUNK-1"),
		}}},
	}})
	if len(result.Issues) != 2 {
		t.Fatalf("issues = %#v", result.Issues)
	}
	if result.Issues[0].ID != "ISSUE-0001" || result.Issues[0].Title != "Critical" {
		t.Fatalf("first issue = %#v", result.Issues[0])
	}
	if !hasTag(result.Issues[0].Tags, "chunked-review") || !hasTag(result.Issues[0].Tags, "chunk:CHUNK-1") {
		t.Fatalf("tags = %#v", result.Issues[0].Tags)
	}
}

func TestMergeReportsDeduplicatesOverlappingFindings(t *testing.T) {
	result := MergeReports(MergeInput{ChunkResults: []ChunkResult{
		{Report: &schema.Report{Issues: []schema.Issue{
			testIssue("ISSUE-0001", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "Same Title", 3, "chunk:CHUNK-1"),
		}}},
		{Report: &schema.Report{Issues: []schema.Issue{
			testIssue("ISSUE-0002", schema.SeverityCritical, schema.CategoryAmbiguousBehavior, " same   title ", 3, "chunk:CHUNK-2"),
		}}},
	}})
	if len(result.Issues) != 1 {
		t.Fatalf("issues = %#v, want deduped one", result.Issues)
	}
	if result.Issues[0].Severity != schema.SeverityCritical {
		t.Fatalf("severity = %s, want CRITICAL", result.Issues[0].Severity)
	}
	if !hasTag(result.Issues[0].Tags, "chunk:CHUNK-1") || !hasTag(result.Issues[0].Tags, "chunk:CHUNK-2") {
		t.Fatalf("tags = %#v", result.Issues[0].Tags)
	}
	if len(result.Issues[0].Evidence) != 1 {
		t.Fatalf("evidence = %#v, want exact duplicate evidence merged once", result.Issues[0].Evidence)
	}
}

func TestMergeReportsMergesDuplicateEvidence(t *testing.T) {
	left := testIssue("ISSUE-0001", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "Same Title", 3, "chunk:CHUNK-1")
	right := testIssue("ISSUE-0002", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "same title", 4, "chunk:CHUNK-2")
	right.Evidence = []schema.Evidence{{LineStart: 3, LineEnd: 5, Quote: "overlap"}}
	result := MergeReports(MergeInput{ChunkResults: []ChunkResult{
		{Report: &schema.Report{Issues: []schema.Issue{left}}},
		{Report: &schema.Report{Issues: []schema.Issue{right}}},
	}})
	if len(result.Issues) != 1 {
		t.Fatalf("issues = %#v, want deduped one", result.Issues)
	}
	if len(result.Issues[0].Evidence) != 2 {
		t.Fatalf("evidence = %#v, want both evidence ranges", result.Issues[0].Evidence)
	}
	if result.Issues[0].Evidence[0].LineStart != 3 || result.Issues[0].Evidence[1].LineStart != 3 {
		t.Fatalf("evidence order = %#v", result.Issues[0].Evidence)
	}
}

func TestMergeReportsDoesNotDeduplicateDifferentPaths(t *testing.T) {
	left := testIssue("ISSUE-0001", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "Same Title", 3, "chunk:CHUNK-1")
	left.Evidence[0].Path = "SPEC.md"
	right := testIssue("ISSUE-0002", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "same title", 3, "chunk:CHUNK-2")
	right.Evidence[0].Path = "OTHER.md"
	result := MergeReports(MergeInput{ChunkResults: []ChunkResult{
		{Report: &schema.Report{Issues: []schema.Issue{left}}},
		{Report: &schema.Report{Issues: []schema.Issue{right}}},
	}})
	if len(result.Issues) != 2 {
		t.Fatalf("issues = %#v, want separate findings for separate paths", result.Issues)
	}
}

func TestMergeReportsRenumbersQuestions(t *testing.T) {
	result := MergeReports(MergeInput{ChunkResults: []ChunkResult{
		{Report: &schema.Report{Questions: []schema.Question{
			{ID: "Q-9999", Severity: schema.SeverityWarn, Question: "Later?", Evidence: []schema.Evidence{{LineStart: 9, LineEnd: 9}}},
			{ID: "Q-0001", Severity: schema.SeverityCritical, Question: "Sooner?", Evidence: []schema.Evidence{{LineStart: 2, LineEnd: 2}}},
		}}},
	}})
	if result.Questions[0].ID != "Q-0001" || result.Questions[0].Question != "Sooner?" {
		t.Fatalf("questions = %#v", result.Questions)
	}
}

func TestMergeReportsValidatesPatchBeforeText(t *testing.T) {
	result := MergeReports(MergeInput{
		OriginalSpec: "replace me\nleave me\n",
		ChunkResults: []ChunkResult{{Report: &schema.Report{
			Issues: []schema.Issue{testIssue("ISSUE-0001", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "Title", 1)},
			Patches: []schema.Patch{
				{IssueID: "ISSUE-0001", Before: "replace me", After: "done"},
				{IssueID: "ISSUE-0002", Before: "missing", After: "done"},
				{IssueID: "ISSUE-0003", Before: "me", After: "done"},
			},
		}}},
	})
	if len(result.Patches) != 1 || result.Patches[0].Before != "replace me" {
		t.Fatalf("patches = %#v, want only exact unique before text", result.Patches)
	}
}

func TestMergeReportsRemapsPatchIssueIDs(t *testing.T) {
	result := MergeReports(MergeInput{
		OriginalSpec: "critical text\nwarn text\n",
		ChunkResults: []ChunkResult{{Report: &schema.Report{
			Issues: []schema.Issue{
				testIssue("ISSUE-9999", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "Warn", 2),
				testIssue("ISSUE-0007", schema.SeverityCritical, schema.CategoryContradiction, "Critical", 1),
			},
			Patches: []schema.Patch{
				{IssueID: "ISSUE-9999", Before: "warn text", After: "better text"},
				{IssueID: "ISSUE-0007", Before: "critical text", After: "resolved text"},
				{IssueID: "ISSUE-4040", Before: "critical text", After: "unlinked text"},
			},
		}}},
	})
	if len(result.Patches) != 2 {
		t.Fatalf("patches = %#v, want linked patches only", result.Patches)
	}
	if result.Patches[0].IssueID != "ISSUE-0002" || result.Patches[1].IssueID != "ISSUE-0001" {
		t.Fatalf("patch issue IDs = %#v, want remapped IDs", result.Patches)
	}
}

func TestMergeReportsRemapsPatchIDsThroughDeduplication(t *testing.T) {
	result := MergeReports(MergeInput{
		OriginalSpec: "replace me\n",
		ChunkResults: []ChunkResult{{Report: &schema.Report{
			Issues: []schema.Issue{
				testIssue("ISSUE-0001", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "Same", 1),
				testIssue("ISSUE-0002", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "same", 1),
			},
			Patches: []schema.Patch{
				{IssueID: "ISSUE-0002", Before: "replace me", After: "done"},
			},
		}}},
	})
	if len(result.Patches) != 1 || result.Patches[0].IssueID != "ISSUE-0001" {
		t.Fatalf("patches = %#v, want deduped issue ID", result.Patches)
	}
	if hasTag(result.Issues[0].Tags, "merged-issue-id:ISSUE-0002") {
		t.Fatalf("internal alias tag leaked: %#v", result.Issues[0].Tags)
	}
}

func TestMergeReportsDropsPatchForUnknownIssue(t *testing.T) {
	result := MergeReports(MergeInput{
		OriginalSpec: "replace me\n",
		ChunkResults: []ChunkResult{{Report: &schema.Report{Patches: []schema.Patch{
			{IssueID: "ISSUE-4040", Before: "replace me", After: "done"},
			{IssueID: "ISSUE-0002", Before: "missing", After: "done"},
		}}}},
	})
	if len(result.Patches) != 0 {
		t.Fatalf("patches = %#v, want unknown issue patch dropped", result.Patches)
	}
}

func TestMergeReportsDoesNotAliasInputTags(t *testing.T) {
	issue := testIssue("ISSUE-0001", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "Title", 1, "chunk:CHUNK-1")
	result := MergeReports(MergeInput{ChunkResults: []ChunkResult{{Report: &schema.Report{Issues: []schema.Issue{issue}}}}})
	result.Issues[0].Tags[0] = "changed"
	if issue.Tags[0] != "chunk:CHUNK-1" {
		t.Fatalf("input tags mutated: %#v", issue.Tags)
	}
}

func testIssue(id string, severity schema.Severity, category schema.Category, title string, line int, tags ...string) schema.Issue {
	return schema.Issue{
		ID:             id,
		Severity:       severity,
		Category:       category,
		Title:          title,
		Description:    "description",
		Evidence:       []schema.Evidence{{LineStart: line, LineEnd: line}},
		Impact:         "impact",
		Recommendation: "recommendation",
		Tags:           tags,
	}
}
