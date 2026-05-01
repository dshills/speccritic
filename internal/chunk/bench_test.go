package chunk

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/spec"
)

func BenchmarkPlanTenThousandLineSpec(b *testing.B) {
	s := spec.New("SPEC.md", syntheticChunkSpec(10000))
	cfg := Config{ChunkLines: 180, ChunkOverlap: 20, ChunkConcurrency: 3}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		plan, err := PlanSpec(s, cfg)
		if err != nil {
			b.Fatalf("PlanSpec: %v", err)
		}
		if len(plan.Chunks) == 0 {
			b.Fatal("expected chunks")
		}
	}
}

func BenchmarkMergeThousandFindings(b *testing.B) {
	results := make([]ChunkResult, 0, 1000)
	for i := 0; i < 1000; i++ {
		line := i + 1
		chunkID := fmt.Sprintf("CHUNK-%04d-L%d-L%d", i+1, line, line)
		issue := schema.Issue{
			ID:             fmt.Sprintf("ISSUE-%04d", i+1),
			Severity:       schema.SeverityWarn,
			Category:       schema.CategoryAmbiguousBehavior,
			Title:          fmt.Sprintf("Finding %d", i),
			Description:    "description",
			Evidence:       []schema.Evidence{{Path: "SPEC.md", LineStart: line, LineEnd: line, Quote: "q"}},
			Impact:         "impact",
			Recommendation: "recommendation",
			Tags:           []string{"chunk:" + chunkID},
		}
		results = append(results, ChunkResult{
			Chunk:  Chunk{ID: chunkID, LineStart: line, LineEnd: line},
			Report: &schema.Report{Issues: []schema.Issue{issue}},
		})
	}
	original := syntheticChunkSpec(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cp := append([]ChunkResult(nil), results...)
		merged := MergeReports(MergeInput{ChunkResults: cp, OriginalSpec: original})
		if len(merged.Issues) != 1000 {
			b.Fatalf("issues = %d, want 1000", len(merged.Issues))
		}
	}
}

func syntheticChunkSpec(lines int) string {
	var b strings.Builder
	b.Grow(lines * 24)
	b.WriteString("# Synthetic Spec\n")
	for i := 1; i < lines; i++ {
		if i%100 == 0 {
			fmt.Fprintf(&b, "\n## Section %d\n", i/100)
			continue
		}
		fmt.Fprintf(&b, "Requirement line %d.\n", i)
	}
	return b.String()
}
