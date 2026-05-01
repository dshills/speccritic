package chunk

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/spec"
)

func TestExtractHeadings(t *testing.T) {
	lines := []string{"# Title", "body", "## Child", "### Grandchild", "not # heading"}
	headings := ExtractHeadings(lines)
	if len(headings) != 3 {
		t.Fatalf("headings = %d, want 3", len(headings))
	}
	if headings[1].Level != 2 || headings[1].Text != "Child" || headings[1].Line != 3 {
		t.Fatalf("heading[1] = %#v", headings[1])
	}
}

func TestExtractHeadingsIgnoresIndentedCodeBlock(t *testing.T) {
	lines := []string{"# Title", "    # Not a heading", "   ## Still heading"}
	headings := ExtractHeadings(lines)
	if len(headings) != 2 {
		t.Fatalf("headings = %#v, want title and three-space heading only", headings)
	}
	if headings[1].Text != "Still heading" {
		t.Fatalf("second heading = %#v", headings[1])
	}
}

func TestBuildSectionsHeadingPaths(t *testing.T) {
	lines := spec.Lines("# Title\nintro\n## A\nbody\n## B\nbody")
	sections := BuildSections(lines, ExtractHeadings(lines))
	if len(sections) != 3 {
		t.Fatalf("sections = %d, want 3", len(sections))
	}
	if sections[0].LineStart != 1 || sections[0].LineEnd != 6 {
		t.Fatalf("top section = %#v", sections[0])
	}
	if got := strings.Join(sections[1].HeadingPath, " > "); got != "Title > A" {
		t.Fatalf("heading path = %q", got)
	}
}

func TestPlanSpecKeepsIntroBeforeFirstHeading(t *testing.T) {
	s := spec.New("SPEC.md", "intro\n\n# Title\nbody\n")
	plan, err := PlanSpec(s, Config{ChunkLines: 10, ChunkOverlap: 0, ChunkConcurrency: 1})
	if err != nil {
		t.Fatalf("PlanSpec: %v", err)
	}
	if len(plan.Chunks) < 2 {
		t.Fatalf("chunks = %#v, want intro and heading chunks", plan.Chunks)
	}
	if plan.Chunks[0].LineStart != 1 || plan.Chunks[0].LineEnd != 2 {
		t.Fatalf("intro chunk = %#v", plan.Chunks[0])
	}
}

func TestPlanSpecKeepsOrphanHeadingBeforeLowerLevelHeading(t *testing.T) {
	s := spec.New("SPEC.md", "## Orphan\norphan body\n# Title\ntitle body\n")
	plan, err := PlanSpec(s, Config{ChunkLines: 10, ChunkOverlap: 0, ChunkConcurrency: 1})
	if err != nil {
		t.Fatalf("PlanSpec: %v", err)
	}
	if len(plan.Chunks) != 2 {
		t.Fatalf("chunks = %#v, want orphan and title chunks", plan.Chunks)
	}
	if plan.Chunks[0].LineStart != 1 || plan.Chunks[0].LineEnd != 2 {
		t.Fatalf("orphan chunk = %#v", plan.Chunks[0])
	}
	if plan.Chunks[1].LineStart != 3 || plan.Chunks[1].LineEnd != 4 {
		t.Fatalf("title chunk = %#v", plan.Chunks[1])
	}
}

func TestPlanSpecCreatesStableChunksWithOverlap(t *testing.T) {
	s := spec.New("SPEC.md", "# Title\n\n## A\none\n\n## B\ntwo\n")
	plan, err := PlanSpec(s, Config{ChunkLines: 3, ChunkOverlap: 1, ChunkMinLines: 0, ChunkTokenThreshold: 1, ChunkConcurrency: 1})
	if err != nil {
		t.Fatalf("PlanSpec: %v", err)
	}
	if len(plan.Chunks) != 3 {
		t.Fatalf("chunks = %d, want 3: %#v", len(plan.Chunks), plan.Chunks)
	}
	chunk := plan.Chunks[1]
	if chunk.ID != "CHUNK-0002-L3-L5" {
		t.Fatalf("chunk ID = %q", chunk.ID)
	}
	if chunk.ContextFrom != 2 || chunk.ContextTo != 6 {
		t.Fatalf("context = %d-%d, want 2-6", chunk.ContextFrom, chunk.ContextTo)
	}
	if !strings.Contains(chunk.Numbered, "L3: ## A") || strings.Contains(chunk.Numbered, "L6: ## B") {
		t.Fatalf("numbered primary range = %q", chunk.Numbered)
	}
}

func TestPlanSpecSplitsOversizedSection(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Title\n")
	for i := 0; i < 9; i++ {
		fmt.Fprintf(&b, "- item %d\n", i)
	}
	s := spec.New("SPEC.md", b.String())
	plan, err := PlanSpec(s, Config{ChunkLines: 3, ChunkOverlap: 0, ChunkMinLines: 0, ChunkTokenThreshold: 1, ChunkConcurrency: 1})
	if err != nil {
		t.Fatalf("PlanSpec: %v", err)
	}
	if len(plan.Chunks) < 2 {
		t.Fatalf("chunks = %d, want split", len(plan.Chunks))
	}
	for _, chunk := range plan.Chunks {
		if chunk.LineEnd < chunk.LineStart {
			t.Fatalf("invalid chunk = %#v", chunk)
		}
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []Config{
		{ChunkLines: 0, ChunkOverlap: 0, ChunkMinLines: 0, ChunkTokenThreshold: 0, ChunkConcurrency: 0},
		WithDefaults(Config{ChunkOverlap: 180}),
		WithDefaults(Config{ChunkConcurrency: 17}),
		WithDefaults(Config{ChunkTokenThreshold: -1}),
		WithDefaults(Config{SynthesisLineThreshold: -1}),
	}
	if err := ValidateConfig(WithDefaults(tests[0])); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
	for i, cfg := range tests[1:] {
		if err := ValidateConfig(cfg); err == nil {
			t.Fatalf("test %d expected invalid config %#v", i, cfg)
		}
	}
}

func TestShouldChunk(t *testing.T) {
	cfg := WithDefaults(Config{ChunkMinLines: 10, ChunkTokenThreshold: 100})
	if ShouldChunk(9, 99, cfg) {
		t.Fatal("should not chunk below line and token thresholds")
	}
	if !ShouldChunk(10, 1, cfg) {
		t.Fatal("should chunk at line threshold")
	}
	if !ShouldChunk(1, 100, cfg) {
		t.Fatal("should chunk at token threshold")
	}
	if ShouldChunk(100, 1000, WithDefaults(Config{Mode: ModeOff})) {
		t.Fatal("off mode should not chunk")
	}
}
