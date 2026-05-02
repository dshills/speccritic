package completion

import (
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/schema"
)

func TestAnalyzeSections(t *testing.T) {
	doc := AnalyzeSections("# Spec\n\n## Purpose\nText\n\n### Detail\nMore\n\n## Acceptance Criteria\n- must work\n")
	if len(doc.Sections) != 4 {
		t.Fatalf("sections = %#v", doc.Sections)
	}
	if doc.Sections[0].Heading != "Spec" || doc.Sections[0].Level != 1 || doc.Sections[0].StartLine != 1 {
		t.Fatalf("top section = %#v", doc.Sections[0])
	}
	if doc.Sections[1].Heading != "Purpose" || doc.Sections[1].EndLine != 8 {
		t.Fatalf("purpose section = %#v", doc.Sections[1])
	}
	if doc.Sections[2].Parent != 1 {
		t.Fatalf("detail parent = %d, want 1", doc.Sections[2].Parent)
	}
	if doc.PrimaryLevel != 2 {
		t.Fatalf("primary level = %d, want 2", doc.PrimaryLevel)
	}
}

func TestMissingSections(t *testing.T) {
	tmpl, err := GetTemplate(schema.CompletionTemplateGeneral, "general")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	doc := AnalyzeSections("# Spec\n\n## Purpose\nThe system must work.\n")
	missing := MissingSections(doc, tmpl)
	if len(missing) != len(tmpl.Sections)-1 {
		t.Fatalf("missing = %#v", missing)
	}
	if missing[0].Heading != "Non-Goals" {
		t.Fatalf("first missing = %q", missing[0].Heading)
	}
}

func TestMissingSectionsWithLevelOneStructure(t *testing.T) {
	tmpl, err := GetTemplate(schema.CompletionTemplateGeneral, "general")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	doc := AnalyzeSections("# Purpose\nThe system must work.\n")
	missing := MissingSections(doc, tmpl)
	if len(missing) != len(tmpl.Sections)-1 {
		t.Fatalf("missing = %#v", missing)
	}
}

func TestMissingSectionsWithLevelTwoTitle(t *testing.T) {
	tmpl, err := GetTemplate(schema.CompletionTemplateGeneral, "general")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	doc := AnalyzeSections("## Spec\n\n### Purpose\nThe system must work.\n")
	if doc.PrimaryLevel != 3 {
		t.Fatalf("primary level = %d, want 3", doc.PrimaryLevel)
	}
	missing := MissingSections(doc, tmpl)
	if len(missing) != len(tmpl.Sections)-1 {
		t.Fatalf("missing = %#v", missing)
	}
}

func TestAnalyzeSectionsNormalizesCRLF(t *testing.T) {
	doc := AnalyzeSections("# Spec\r\n\r\n## Purpose\r\nThe system must work.\r\n")
	if len(doc.Lines) != 4 || strings.Contains(doc.Lines[0], "\r") {
		t.Fatalf("lines = %#v", doc.Lines)
	}
	if len(doc.Sections) != 2 || doc.Sections[1].Heading != "Purpose" {
		t.Fatalf("sections = %#v", doc.Sections)
	}
}

func TestAnalyzeSectionsIgnoresIndentedCodeHeadings(t *testing.T) {
	doc := AnalyzeSections("# Spec\n\n    ## Not A Heading\n\n   ## Purpose\nThe system must work.\n")
	if len(doc.Sections) != 2 {
		t.Fatalf("sections = %#v", doc.Sections)
	}
	if doc.Sections[1].Heading != "Purpose" {
		t.Fatalf("heading = %q", doc.Sections[1].Heading)
	}
}

func TestAnalyzeSectionsIgnoresFencedCodeHeadings(t *testing.T) {
	doc := AnalyzeSections("# Spec\n\n````markdown\n## Not A Heading\n```\nStill code\n`````   \n\n## Purpose\nThe system must work.\n")
	if len(doc.Sections) != 2 {
		t.Fatalf("sections = %#v", doc.Sections)
	}
	if doc.Sections[1].Heading != "Purpose" {
		t.Fatalf("heading = %q", doc.Sections[1].Heading)
	}
}

func TestIncompleteSections(t *testing.T) {
	tmpl, err := GetTemplate(schema.CompletionTemplateGeneral, "general")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	doc := AnalyzeSections("# Spec\n\n## Purpose\nSome context.\n\n## Acceptance Criteria\n- must return 200\n")
	incomplete := IncompleteSections(doc, tmpl)
	if len(incomplete) != 1 || incomplete[0].Heading != "Purpose" {
		t.Fatalf("incomplete = %#v", incomplete)
	}
}

func TestIncompleteSectionsUsesSubsectionContent(t *testing.T) {
	tmpl, err := GetTemplate(schema.CompletionTemplateGeneral, "general")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	doc := AnalyzeSections("# Spec\n\n## Purpose\n\n### Behavior\nThe system must validate requests.\n")
	incomplete := IncompleteSections(doc, tmpl)
	for _, section := range incomplete {
		if section.Heading == "Purpose" {
			t.Fatalf("purpose should be complete from subsection content: %#v", incomplete)
		}
	}
}

func TestStableInsertionTargetAfterLowerSection(t *testing.T) {
	tmpl, err := GetTemplate(schema.CompletionTemplateGeneral, "general")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	doc := AnalyzeSections("# Spec\n\n## Purpose\nThe system must work.\n\n## Functional Requirements\nREQ-1 must be true.\n")
	target := findTemplateSection(t, tmpl, "Non-Goals")
	patch, status := StableInsertionTarget(doc, tmpl, target, "## Non-Goals\nOPEN DECISION: define exclusions.")
	if status != StatusPatchGenerated {
		t.Fatalf("status = %s", status)
	}
	if patch.Line != 6 || !strings.Contains(patch.After, "## Non-Goals") || !strings.HasPrefix(patch.After, patch.Before) {
		t.Fatalf("patch = %#v", patch)
	}
}

func TestStableInsertionTargetBeforeHigherSection(t *testing.T) {
	tmpl, err := GetTemplate(schema.CompletionTemplateGeneral, "general")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	doc := AnalyzeSections("# Spec\n\n## Acceptance Criteria\n- must pass\n")
	target := findTemplateSection(t, tmpl, "Functional Requirements")
	patch, status := StableInsertionTarget(doc, tmpl, target, "## Functional Requirements\nOPEN DECISION: define behavior.")
	if status != StatusPatchGenerated {
		t.Fatalf("status = %s", status)
	}
	if patch.Line != 3 || !strings.HasPrefix(patch.After, "## Functional Requirements") || !strings.Contains(patch.After, patch.Before) {
		t.Fatalf("patch = %#v", patch)
	}
}

func TestStableInsertionTargetEndOfDocument(t *testing.T) {
	tmpl, err := GetTemplate(schema.CompletionTemplateGeneral, "general")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	doc := AnalyzeSections("# Spec\n\nIntro\n\n")
	target := findTemplateSection(t, tmpl, "Purpose")
	patch, status := StableInsertionTarget(doc, tmpl, target, "## Purpose\nOPEN DECISION: define purpose.")
	if status != StatusPatchGenerated {
		t.Fatalf("status = %s", status)
	}
	if patch.Line != 3 || !strings.HasSuffix(patch.After, "OPEN DECISION: define purpose.") {
		t.Fatalf("patch = %#v", patch)
	}
}

func TestStableInsertionTargetDuplicateHeadingSkips(t *testing.T) {
	tmpl, err := GetTemplate(schema.CompletionTemplateGeneral, "general")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	doc := AnalyzeSections("# Spec\n\n## Purpose\nA\n\n## Purpose\nB\n")
	target := findTemplateSection(t, tmpl, "Purpose")
	_, status := StableInsertionTarget(doc, tmpl, target, "## Purpose\nOPEN DECISION: define purpose.")
	if status != StatusSkippedNoSafeLocation {
		t.Fatalf("status = %s", status)
	}
}

func TestAppendSubsectionTarget(t *testing.T) {
	doc := AnalyzeSections("# Spec\n\n## Purpose\nSome context.\n")
	patch, status := AppendSubsectionTarget(doc, doc.Sections[1], "### Details\nOPEN DECISION: define detail.")
	if status != StatusPatchGenerated {
		t.Fatalf("status = %s", status)
	}
	if !strings.Contains(patch.After, "### Details") || !strings.HasPrefix(patch.After, patch.Before) {
		t.Fatalf("patch = %#v", patch)
	}
}

func findTemplateSection(t *testing.T, tmpl *Template, heading string) TemplateSection {
	t.Helper()
	for _, section := range tmpl.Sections {
		if section.Heading == heading {
			return section
		}
	}
	t.Fatalf("missing template section %q", heading)
	return TemplateSection{}
}
