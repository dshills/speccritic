package chunk

import (
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/spec"
)

func TestBuildChunkTask(t *testing.T) {
	s, _, ch := promptFixture(t)
	task, err := BuildChunkTask(ch, s.LineCount, false)
	if err != nil {
		t.Fatalf("BuildChunkTask: %v", err)
	}
	for _, want := range []string{
		"Review lines L6-L9 (Spec > Requirements) of the specification above",
		"report only defects in these lines",
		"Cite only lines L6-L9",
		`"cross-section"`,
	} {
		if !strings.Contains(task, want) {
			t.Fatalf("task missing %q:\n%s", want, task)
		}
	}
	// The spec lives in the shared prefix, so the task must not repeat it,
	// and the chunk tag is added locally, so the model is not asked for it.
	for _, unwanted := range []string{"L6: ", "UAS SHALL", "chunk:", "chunk_summary"} {
		if strings.Contains(task, unwanted) {
			t.Fatalf("task contains %q:\n%s", unwanted, task)
		}
	}
}

func TestBuildChunkTaskRejectsInvalidChunk(t *testing.T) {
	for _, ch := range []Chunk{{}, {ID: "bad", LineStart: 3, LineEnd: 3}, {ID: "bad", LineStart: 0, LineEnd: 1}, {ID: "bad", LineStart: 2, LineEnd: 1}} {
		if _, err := BuildChunkTask(ch, 2, false); err == nil {
			t.Errorf("BuildChunkTask(%+v) should fail", ch)
		}
	}
}

func promptFixture(t *testing.T) (*spec.Spec, Plan, Chunk) {
	t.Helper()
	s := spec.New("SPEC.md", strings.Join([]string{
		"# Spec",
		"",
		"## Glossary",
		"UAS means upload service.",
		"",
		"## Requirements",
		"The UAS SHALL accept a file.",
		"The UAS SHALL return status 200 within 100 ms.",
		"",
		"## Acceptance Criteria",
		"A test uploads a file.",
		"A test receives status 200.",
	}, "\n"))
	plan, err := PlanSpec(s, Config{ChunkLines: 3, ChunkOverlap: 1, ChunkConcurrency: 1})
	if err != nil {
		t.Fatalf("PlanSpec: %v", err)
	}
	for _, ch := range plan.Chunks {
		if ch.LineStart == 6 {
			return s, plan, ch
		}
	}
	t.Fatalf("requirements chunk not found: %#v", plan.Chunks)
	return nil, Plan{}, Chunk{}
}

func TestBuildChunkTaskWithLines(t *testing.T) {
	s, _, ch := promptFixture(t)
	task, err := BuildChunkTask(ch, s.LineCount, true)
	if err != nil {
		t.Fatalf("BuildChunkTask: %v", err)
	}
	for _, want := range []string{"too large to show in full", `<spec_lines file="SPEC.md">`, "L6: ## Requirements", "L8: The UAS SHALL return status 200", "Cite only lines L6-L9"} {
		if !strings.Contains(task, want) {
			t.Fatalf("task missing %q:\n%s", want, task)
		}
	}
	if strings.Contains(task, "L1: # Spec") {
		t.Fatalf("task carries lines outside the chunk:\n%s", task)
	}
}
