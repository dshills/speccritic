package chunk

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseChunkResponseKeepsValidFinding(t *testing.T) {
	report, err := ParseChunkResponse(chunkJSON(2, 2, []string{"chunk:CHUNK-0001-L2-L3"}, "summary"), 4, testChunk())
	if err != nil {
		t.Fatalf("ParseChunkResponse: %v", err)
	}
	if report.Issues[0].ID != "ISSUE-0001" {
		t.Fatalf("issue ID = %s", report.Issues[0].ID)
	}
	if report.Meta.DroppedFindings != 0 {
		t.Fatalf("dropped = %d, want 0", report.Meta.DroppedFindings)
	}
}

func TestParseChunkResponseDropsOnlyTheFindingOutsidePrimaryRange(t *testing.T) {
	raw := chunkReportJSON("summary",
		chunkIssueJSON("In range", 2, 3, nil),
		chunkIssueJSON("Context only", 1, 1, nil),
	)
	report, err := ParseChunkResponse(raw, 4, testChunk())
	if err != nil {
		t.Fatalf("ParseChunkResponse: %v", err)
	}
	if len(report.Issues) != 1 || report.Issues[0].Title != "In range" {
		t.Fatalf("issues = %#v, want only the in-range finding", report.Issues)
	}
	if report.Meta.DroppedFindings != 1 {
		t.Fatalf("dropped = %d, want 1", report.Meta.DroppedFindings)
	}
}

func TestParseChunkResponseFailsWhenNoFindingIsUsable(t *testing.T) {
	_, err := ParseChunkResponse(chunkJSON(1, 1, []string{"chunk:CHUNK-0001-L2-L3"}, "summary"), 4, testChunk())
	if err == nil || !strings.Contains(err.Error(), "outside chunk primary range") {
		t.Fatalf("error = %v, want primary range rejection", err)
	}
}

func TestParseChunkResponseSetsChunkTag(t *testing.T) {
	want := "chunk:CHUNK-0001-L2-L3"
	cases := map[string][]string{
		"missing":             nil,
		"already present":     {want},
		"different case":      {"Chunk:CHUNK-0001-L2-L3"},
		"names another chunk": {"chunk:CHUNK-0009-L90-L99", "cross-section"},
	}
	for name, tags := range cases {
		t.Run(name, func(t *testing.T) {
			report, err := ParseChunkResponse(chunkJSON(2, 2, tags, "summary"), 4, testChunk())
			if err != nil {
				t.Fatalf("ParseChunkResponse: %v", err)
			}
			var chunkTags []string
			for _, tag := range report.Issues[0].Tags {
				if strings.HasPrefix(strings.ToLower(tag), "chunk:") {
					chunkTags = append(chunkTags, tag)
				}
			}
			if len(chunkTags) != 1 || chunkTags[0] != want {
				t.Fatalf("chunk tags = %v, want exactly [%s]", chunkTags, want)
			}
			if name == "names another chunk" && !hasTag(report.Issues[0].Tags, "cross-section") {
				t.Fatalf("tags = %v, want unrelated tags kept", report.Issues[0].Tags)
			}
		})
	}
}

func TestParseChunkResponseAllowsMissingSummary(t *testing.T) {
	report, err := ParseChunkResponse(chunkJSON(2, 2, nil, ""), 4, testChunk())
	if err != nil {
		t.Fatalf("ParseChunkResponse: %v", err)
	}
	if report.Meta.ChunkSummary != "" || len(report.Issues) != 1 {
		t.Fatalf("summary = %q issues = %d", report.Meta.ChunkSummary, len(report.Issues))
	}
}

func TestParseChunkResponseShortensLongSummary(t *testing.T) {
	long := strings.Repeat("é", maxChunkSummaryRunes+50)
	report, err := ParseChunkResponse(chunkJSON(2, 2, nil, long), 4, testChunk())
	if err != nil {
		t.Fatalf("ParseChunkResponse: %v", err)
	}
	if got := len([]rune(report.Meta.ChunkSummary)); got != maxChunkSummaryRunes {
		t.Fatalf("summary length = %d runes, want %d", got, maxChunkSummaryRunes)
	}
}

func TestParseChunkResponseSetsEvidencePath(t *testing.T) {
	ch := testChunk()
	ch.Path = "specs/api/SPEC.md"
	raw := strings.Replace(chunkJSON(2, 2, nil, "summary"), `"path":"SPEC.md"`, `"path":"../../etc/passwd"`, 1)
	report, err := ParseChunkResponse(raw, 4, ch)
	if err != nil {
		t.Fatalf("ParseChunkResponse: %v", err)
	}
	if got := report.Issues[0].Evidence[0].Path; got != ch.Path {
		t.Fatalf("evidence path = %q, want the reviewed spec %q", got, ch.Path)
	}
}

func TestParseSynthesisResponseAllowsAnyOriginalEvidenceAndAddsTag(t *testing.T) {
	raw := chunkReportJSON("", chunkIssueJSON("Contradiction", 1, 4, nil))
	report, err := ParseSynthesisResponse(raw, 4)
	if err != nil {
		t.Fatalf("ParseSynthesisResponse: %v", err)
	}
	if len(report.Issues) != 1 || !hasTag(report.Issues[0].Tags, TagSynthesis) {
		t.Fatalf("issues = %#v, want one issue tagged %s", report.Issues, TagSynthesis)
	}
	if _, err := ParseSynthesisResponse(chunkReportJSON("", chunkIssueJSON("Beyond the spec", 1, 9, nil)), 4); err == nil {
		t.Fatal("expected error for evidence beyond the spec")
	}
}

func testChunk() Chunk {
	return Chunk{ID: "CHUNK-0001-L2-L3", Path: "SPEC.md", LineStart: 2, LineEnd: 3, ContextFrom: 1, ContextTo: 4}
}

func chunkJSON(lineStart, lineEnd int, tags []string, summary string) string {
	return chunkReportJSON(summary, chunkIssueJSON("Ambiguous behavior", lineStart, lineEnd, tags))
}

func chunkIssueJSON(title string, lineStart, lineEnd int, tags []string) string {
	quoted := make([]string, len(tags))
	for i, tag := range tags {
		quoted[i] = fmt.Sprintf("%q", tag)
	}
	return fmt.Sprintf(`{
		"id":"ISSUE-0001",
		"severity":"WARN",
		"category":"AMBIGUOUS_BEHAVIOR",
		"title":%q,
		"description":"desc",
		"evidence":[{"path":"SPEC.md","line_start":%d,"line_end":%d,"quote":"q"}],
		"impact":"impact",
		"recommendation":"recommendation",
		"blocking":false,
		"tags":[%s]
	}`, title, lineStart, lineEnd, strings.Join(quoted, ","))
}

func chunkReportJSON(summary string, issues ...string) string {
	return fmt.Sprintf(`{"issues":[%s],"questions":[],"patches":[],"meta":{"chunk_summary":%q}}`, strings.Join(issues, ","), summary)
}
