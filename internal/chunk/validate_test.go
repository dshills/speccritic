package chunk

import (
	"strconv"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/schema"
)

func TestParseChunkResponseValidatesPrimaryEvidenceAndTag(t *testing.T) {
	report, err := ParseChunkResponse(chunkJSON(2, 2, []string{"chunk:CHUNK-0001-L2-L3"}, "summary"), 4, testChunk())
	if err != nil {
		t.Fatalf("ParseChunkResponse: %v", err)
	}
	if report.Issues[0].ID != "ISSUE-0001" {
		t.Fatalf("issue ID = %s", report.Issues[0].ID)
	}
}

func TestParseChunkResponseRejectsContextOnlyEvidence(t *testing.T) {
	_, err := ParseChunkResponse(chunkJSON(1, 1, []string{"chunk:CHUNK-0001-L2-L3"}, "summary"), 4, testChunk())
	if err == nil || !strings.Contains(err.Error(), "outside chunk primary range") {
		t.Fatalf("error = %v, want primary range rejection", err)
	}
}

func TestParseChunkResponseRejectsMissingChunkTag(t *testing.T) {
	_, err := ParseChunkResponse(chunkJSON(2, 2, nil, "summary"), 4, testChunk())
	if err == nil || !strings.Contains(err.Error(), "missing chunk tag") {
		t.Fatalf("error = %v, want missing tag rejection", err)
	}
}

func TestParseChunkResponseAcceptsCaseInsensitiveChunkTag(t *testing.T) {
	_, err := ParseChunkResponse(chunkJSON(2, 2, []string{"Chunk:CHUNK-0001-L2-L3"}, "summary"), 4, testChunk())
	if err != nil {
		t.Fatalf("ParseChunkResponse: %v", err)
	}
}

func TestParseChunkResponseRejectsMissingSummary(t *testing.T) {
	_, err := ParseChunkResponse(chunkJSON(2, 2, []string{"chunk:CHUNK-0001-L2-L3"}, ""), 4, testChunk())
	if err == nil || !strings.Contains(err.Error(), "chunk_summary") {
		t.Fatalf("error = %v, want summary rejection", err)
	}
}

func TestValidateSynthesisReportAllowsAnyOriginalEvidence(t *testing.T) {
	report := &schema.Report{Issues: []schema.Issue{{
		ID:       "ISSUE-0001",
		Severity: schema.SeverityWarn,
		Category: schema.CategoryContradiction,
		Title:    "Contradiction",
		Evidence: []schema.Evidence{{LineStart: 1, LineEnd: 4}},
		Tags:     []string{"synthesis"},
	}}}
	if err := ValidateSynthesisReport(report, 4); err != nil {
		t.Fatalf("ValidateSynthesisReport: %v", err)
	}
}

func testChunk() Chunk {
	return Chunk{ID: "CHUNK-0001-L2-L3", LineStart: 2, LineEnd: 3, ContextFrom: 1, ContextTo: 4}
}

func chunkJSON(lineStart, lineEnd int, tags []string, summary string) string {
	tagJSON := "[]"
	if len(tags) > 0 {
		quoted := make([]string, len(tags))
		for i, tag := range tags {
			quoted[i] = `"` + tag + `"`
		}
		tagJSON = "[" + strings.Join(quoted, ",") + "]"
	}
	return `{
		"issues":[{
			"id":"ISSUE-0001",
			"severity":"WARN",
			"category":"AMBIGUOUS_BEHAVIOR",
			"title":"Ambiguous behavior",
			"description":"desc",
			"evidence":[{"path":"SPEC.md","line_start":` + itoa(lineStart) + `,"line_end":` + itoa(lineEnd) + `,"quote":"q"}],
			"impact":"impact",
			"recommendation":"recommendation",
			"blocking":false,
			"tags":` + tagJSON + `
		}],
		"questions":[],
		"patches":[],
		"meta":{"chunk_summary":` + quote(summary) + `}
	}`
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

func quote(v string) string {
	if v == "" {
		return `""`
	}
	return `"` + v + `"`
}
