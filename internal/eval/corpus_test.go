package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/schema"
)

const corpusDir = "../../eval/corpus"

// The committed corpus must load, every mutation must apply on its own and
// together with the others, and every evidence anchor must resolve. A label
// that drifts from its spec fails here rather than skewing a paid run.
func TestCommittedCorpusIsConsistent(t *testing.T) {
	bases, err := LoadCorpus(corpusDir)
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	cases, err := BuildCases(bases, true)
	if err != nil {
		t.Fatalf("BuildCases: %v", err)
	}

	mutations := 0
	covered := map[schema.Category]bool{}
	for _, base := range bases {
		mutations += len(base.Mutations)
		for _, m := range base.Mutations {
			covered[m.Categories[0]] = true
		}
	}
	if want := 2*len(bases) + mutations; len(cases) != want {
		t.Fatalf("cases = %d, want %d (a clean and a seeded case per spec, plus one per mutation)", len(cases), want)
	}
	for _, category := range []schema.Category{
		schema.CategoryNonTestableRequirement, schema.CategoryAmbiguousBehavior, schema.CategoryContradiction,
		schema.CategoryMissingFailureMode, schema.CategoryUndefinedInterface, schema.CategoryMissingInvariant,
		schema.CategoryScopeLeak, schema.CategoryOrderingUndefined, schema.CategoryTerminologyInconsistent,
		schema.CategoryUnspecifiedConstraint,
	} {
		if !covered[category] {
			t.Errorf("no mutation has %s as its primary category", category)
		}
	}

	baseText := map[string]string{}
	for _, base := range bases {
		baseText[base.ID] = base.Text
	}
	seen := map[string]bool{}
	for _, c := range cases {
		if seen[c.ID] {
			t.Errorf("case id %q is repeated", c.ID)
		}
		seen[c.ID] = true
		if strings.ContainsAny(c.ID, `/\ `) {
			t.Errorf("case id %q is not safe as a file name", c.ID)
		}
		switch c.Variant {
		case VariantClean:
			if len(c.Defects) != 0 || c.Spec != baseText[c.Base] {
				t.Errorf("%s: a clean case must be the base spec with no defects", c.ID)
			}
		case VariantIsolated:
			if len(c.Defects) != 1 {
				t.Errorf("%s: defects = %d, want 1", c.ID, len(c.Defects))
			}
			fallthrough
		default:
			if c.Spec == baseText[c.Base] {
				t.Errorf("%s: seeding left the spec unchanged", c.ID)
			}
		}
		lines := strings.Count(strings.TrimSuffix(c.Spec, "\n"), "\n") + 1
		for _, d := range c.Defects {
			for _, r := range d.Ranges {
				if r.Start < 1 || r.End < r.Start || r.End > lines {
					t.Errorf("%s: defect %s has range %v outside the %d-line spec", c.ID, d.ID, r, lines)
				}
			}
		}
	}
}

func TestEditApply(t *testing.T) {
	const text = "alpha\nbeta one\ngamma\n"
	cases := map[string]struct {
		edit    Edit
		want    string
		wantErr string
	}{
		"replace text":         {edit: Edit{Find: "beta one", Replace: "beta two"}, want: "alpha\nbeta two\ngamma\n"},
		"delete text":          {edit: Edit{Find: " one"}, want: "alpha\nbeta\ngamma\n"},
		"delete line":          {edit: Edit{DeleteLine: "beta"}, want: "alpha\ngamma\n"},
		"replace line":         {edit: Edit{ReplaceLine: "beta", With: "delta"}, want: "alpha\ndelta\ngamma\n"},
		"insert after line":    {edit: Edit{InsertAfterLine: "beta", With: "delta"}, want: "alpha\nbeta one\ndelta\ngamma\n"},
		"text not found":       {edit: Edit{Find: "omega"}, wantErr: "occurs 0 times"},
		"text found twice":     {edit: Edit{Find: "a\n"}, wantErr: "occurs 2 times"},
		"line not found":       {edit: Edit{DeleteLine: "omega"}, wantErr: "on no line"},
		"line found twice":     {edit: Edit{DeleteLine: "a"}, wantErr: "more than one line"},
		"no operation":         {edit: Edit{With: "x"}, wantErr: "exactly one of"},
		"two operations given": {edit: Edit{Find: "alpha", DeleteLine: "beta"}, wantErr: "exactly one of"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := tc.edit.apply(text)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestAnchorResolve(t *testing.T) {
	lines := strings.Split("# Title\n\n## One\na\n### Inner\nb\n## Two\nc", "\n")
	cases := map[string]struct {
		anchor  Anchor
		want    LineRange
		wantErr string
	}{
		"text":                         {anchor: Anchor{Text: "b"}, want: LineRange{6, 6}},
		"section includes subsections": {anchor: Anchor{Section: "One"}, want: LineRange{3, 6}},
		"subsection ends at its parent's sibling": {anchor: Anchor{Section: "Inner"}, want: LineRange{5, 6}},
		"last section runs to the end":            {anchor: Anchor{Section: "Two"}, want: LineRange{7, 8}},
		"unknown section":                         {anchor: Anchor{Section: "Three"}, wantErr: "no section titled"},
		"unknown text":                            {anchor: Anchor{Text: "zzz"}, wantErr: "on no line"},
		"neither set":                             {anchor: Anchor{}, wantErr: "exactly one of"},
		"both set":                                {anchor: Anchor{Text: "a", Section: "One"}, wantErr: "exactly one of"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := tc.anchor.resolve(lines)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestLoadCorpusRejectsBadLabels(t *testing.T) {
	const good = `{"id":"demo","profile":"general","provenance":"test","mutations":[{"id":"m","note":"n","categories":["CONTRADICTION"],"severity":"WARN","keywords":["x"],"edits":[{"find":"a","replace":"b"}],"evidence":[{"text":"b"}]}]}`
	cases := map[string]struct {
		labels  string
		wantErr string
	}{
		"id differs from file name": {strings.Replace(good, `"id":"demo"`, `"id":"other"`, 1), "does not match the file name"},
		"unknown category":          {strings.Replace(good, "CONTRADICTION", "VIBES", 1), "unknown category"},
		"unknown severity":          {strings.Replace(good, `"WARN"`, `"HIGH"`, 1), "unknown severity"},
		"uppercase keyword":         {strings.Replace(good, `["x"]`, `["X"]`, 1), "must be lowercase"},
		"misspelled field":          {strings.Replace(good, `"note"`, `"notes"`, 1), "unknown field"},
		"no evidence":               {strings.Replace(good, `"evidence":[{"text":"b"}]`, `"evidence":[]`, 1), "edits and evidence are required"},
		"no provenance":             {strings.Replace(good, `"provenance":"test"`, `"provenance":""`, 1), "provenance are required"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "demo.md"), "a\n")
			writeFile(t, filepath.Join(dir, "demo.defects.json"), tc.labels)
			if _, err := LoadCorpus(dir); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "demo.md"), "a\n")
	writeFile(t, filepath.Join(dir, "demo.defects.json"), good)
	if _, err := LoadCorpus(dir); err != nil {
		t.Fatalf("the unmodified labels should load: %v", err)
	}
	if _, err := LoadCorpus(t.TempDir()); err == nil {
		t.Fatal("an empty corpus directory should be an error")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSectionRangeIgnoresHeadingsInsideCodeFences(t *testing.T) {
	text := strings.Join([]string{
		"## One", // 1
		"```sh",  // 2
		"# not a heading",
		"## Two",
		"```", // 5
		"~~~", // 6
		"## Two",
		"```", // 8: backticks do not close a tilde fence
		"## Two",
		"~~~",  // 10
		"````", // 11
		"```",  // 12: three do not close a fence opened with four
		"## Two",
		"```` trailing", // 14: nor does a fence with text after it
		"## Two",
		"`````",       // 16: more than four does
		"last of one", // 17
		"## Two",      // 18
		"real",        // 19
	}, "\n")
	lines := strings.Split(text, "\n")
	one, err := sectionRange(lines, "One")
	if err != nil || one != (LineRange{1, 17}) {
		t.Fatalf("section One = %v, %v; want lines 1-17, running past every fenced block", one, err)
	}
	two, err := sectionRange(lines, "Two")
	if err != nil || two != (LineRange{18, 19}) {
		t.Fatalf("section Two = %v, %v; want lines 18-19, the real heading", two, err)
	}
}
