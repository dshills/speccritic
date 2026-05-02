package incremental

import (
	"strings"
	"testing"
)

func TestBuildSectionsDuplicateSiblingHeadings(t *testing.T) {
	raw := "# API\n## Examples\none\n## Examples\ntwo\n"
	sections := buildSections(raw)
	if len(sections) != 3 {
		t.Fatalf("sections = %d, want 3", len(sections))
	}
	if sections[1].IdentityPath == sections[2].IdentityPath {
		t.Fatalf("duplicate heading identity should be unique: %q", sections[1].IdentityPath)
	}
}

func TestNormalizeHeadingStripsAttributes(t *testing.T) {
	if got := normalizeHeading("Behavior   {#behavior}"); got != "Behavior" {
		t.Fatalf("normalizeHeading = %q", got)
	}
}

func TestPlanChangesOneChangedSection(t *testing.T) {
	previous := "# Spec\nintro\n## A\nsame\n## B\nold\n"
	current := "# Spec\nintro\n## A\nsame\n## B\nnew\n"
	plan, err := PlanChanges(previous, current, testPlanConfig())
	if err != nil {
		t.Fatalf("PlanChanges: %v", err)
	}
	assertClass(t, plan, ClassChanged, 5, 6)
	if len(plan.ReviewRanges) != 1 {
		t.Fatalf("review ranges = %#v, want one changed section", plan.ReviewRanges)
	}
	if plan.ReviewRanges[0].Primary.Start != 5 || plan.ReviewRanges[0].Primary.End != 6 {
		t.Fatalf("primary = %#v", plan.ReviewRanges[0].Primary)
	}
}

func TestPlanChangesMovedSection(t *testing.T) {
	previous := "# Spec\n## A\nsame\n## B\nmove me\n"
	current := "# Spec\n## B\nmove me\n## A\nsame\n"
	plan, err := PlanChanges(previous, current, testPlanConfig())
	if err != nil {
		t.Fatalf("PlanChanges: %v", err)
	}
	if len(plan.ReuseRanges) != 3 {
		t.Fatalf("reuse ranges = %#v, want moved sections reusable", plan.ReuseRanges)
	}
	assertClass(t, plan, ClassMoved, 2, 3)
}

func TestPlanChangesAddedAndDeletedSections(t *testing.T) {
	previous := "# Spec\n## Old\nbody\n"
	current := "# Spec\n## New\nbody\n"
	plan, err := PlanChanges(previous, current, testPlanConfig())
	if err != nil {
		t.Fatalf("PlanChanges: %v", err)
	}
	assertHasClass(t, plan, ClassAdded)
	assertHasClass(t, plan, ClassDeleted)
}

func TestPlanChangesRenamedSection(t *testing.T) {
	previous := "# Spec\n## Old Name\nThe API must return JSON.\n"
	current := "# Spec\n## New Name\nThe API must return JSON.\n"
	plan, err := PlanChanges(previous, current, testPlanConfig())
	if err != nil {
		t.Fatalf("PlanChanges: %v", err)
	}
	assertHasClass(t, plan, ClassRenamed)
}

func TestPlanChangesCoalescesOverlappingContext(t *testing.T) {
	previous := "# Spec\n## A\nold\n## B\nold\n"
	current := "# Spec\n## A\nnew\n## B\nnew\n"
	cfg := testPlanConfig()
	cfg.ContextLines = 2
	plan, err := PlanChanges(previous, current, cfg)
	if err != nil {
		t.Fatalf("PlanChanges: %v", err)
	}
	if len(plan.ReviewRanges) != 1 {
		t.Fatalf("review ranges = %#v, want coalesced", plan.ReviewRanges)
	}
}

func TestPlanChangesDoesNotCoalesceOverTokenBudget(t *testing.T) {
	previous := "# Spec\n## A\nold\n## B\nold\n"
	current := "# Spec\n## A\n" + strings.Repeat("new ", 30) + "\n## B\n" + strings.Repeat("new ", 30) + "\n"
	cfg := testPlanConfig()
	cfg.ContextLines = 2
	cfg.ChunkTokenThreshold = 10
	plan, err := PlanChanges(previous, current, cfg)
	if err != nil {
		t.Fatalf("PlanChanges: %v", err)
	}
	if len(plan.ReviewRanges) != 2 {
		t.Fatalf("review ranges = %#v, want separate ranges", plan.ReviewRanges)
	}
}

func testPlanConfig() Config {
	cfg := DefaultConfig()
	cfg.ContextLines = 0
	cfg.ChunkTokenThreshold = 4000
	return cfg
}

func assertClass(t *testing.T, plan Plan, class string, start, end int) {
	t.Helper()
	for _, ch := range plan.Sections {
		if ch.Classification == class && ch.CurrentRange.Start == start && ch.CurrentRange.End == end {
			return
		}
	}
	t.Fatalf("missing class %s at %d-%d in %#v", class, start, end, plan.Sections)
}

func assertHasClass(t *testing.T, plan Plan, class string) {
	t.Helper()
	for _, ch := range plan.Sections {
		if ch.Classification == class {
			return
		}
	}
	t.Fatalf("missing class %s in %#v", class, plan.Sections)
}
