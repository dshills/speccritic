package chunk

import (
	"strings"
	"testing"

	ctxpkg "github.com/dshills/speccritic/internal/context"
	"github.com/dshills/speccritic/internal/spec"
)

func TestBuildUserPromptIncludesChunkContract(t *testing.T) {
	s, plan, ch := promptFixture(t)
	prefix, tail, err := BuildUserPrompt(PromptInput{Spec: s, Plan: plan, Chunk: ch})
	if err != nil {
		t.Fatalf("BuildUserPrompt: %v", err)
	}
	for _, want := range []string{
		"Review only the primary range",
		`tag "chunk:<CHUNK-ID>"`,
		"meta.chunk_summary",
		"<spec_table_of_contents>",
		"L1-L12 #Spec",
	} {
		if !strings.Contains(prefix, want) {
			t.Fatalf("prefix missing %q:\n%s", want, prefix)
		}
	}
	for _, want := range []string{
		`<chunk id="` + ch.ID + `"`,
		"<chunk_issue_tag>chunk:" + ch.ID + "</chunk_issue_tag>",
		"<context_only_before>",
		"L5: ",
		"<primary_lines>",
		"L6: ## Requirements",
		"<context_only_after>",
		"L9: ",
	} {
		if !strings.Contains(tail, want) {
			t.Fatalf("tail missing %q:\n%s", want, tail)
		}
	}
}

func TestBuildUserPromptIncludesContextPreflightAndGlossary(t *testing.T) {
	s, plan, ch := promptFixture(t)
	prefix, _, err := BuildUserPrompt(PromptInput{
		Spec:  s,
		Plan:  plan,
		Chunk: ch,
		ContextFiles: []ctxpkg.ContextFile{{
			Path:    "glossary.md",
			Content: "external context",
		}},
		PreflightContext: "<known_preflight_findings>\n- PREFLIGHT-TODO-001\n</known_preflight_findings>\n",
	})
	if err != nil {
		t.Fatalf("BuildUserPrompt: %v", err)
	}
	for _, want := range []string{"external context", "PREFLIGHT-TODO-001", "<global_definitions_context>", "UAS means upload service"} {
		if !strings.Contains(prefix, want) {
			t.Fatalf("prefix missing %q:\n%s", want, prefix)
		}
	}
}

func TestBuildUserPromptRejectsInvalidChunk(t *testing.T) {
	s := spec.New("SPEC.md", "# Spec\n")
	_, _, err := BuildUserPrompt(PromptInput{Spec: s, Chunk: Chunk{ID: "bad", LineStart: 3, LineEnd: 3}})
	if err == nil {
		t.Fatal("expected invalid chunk error")
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
