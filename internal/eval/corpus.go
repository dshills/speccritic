// Package eval measures how well SpecCritic reviews specifications whose
// defects are known in advance.
//
// The corpus holds clean specifications. Each comes with mutations: small
// edits that seed one known defect each, labeled with the category and severity
// a reviewer should report and the lines it should point at. A clean spec and
// its seeded twin are reviewed side by side, so the eval sees both whether real
// defects are found and whether sound text is left alone.
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/spec"
)

// Variant names how a case's spec was derived from its base.
type Variant string

const (
	// VariantClean is the base spec unchanged. It should draw no CRITICAL.
	VariantClean Variant = "clean"
	// VariantSeeded is the base spec with every mutation applied.
	VariantSeeded Variant = "seeded"
	// VariantIsolated is the base spec with exactly one mutation applied.
	VariantIsolated Variant = "isolated"
)

// Base is one clean specification and the defects that can be seeded into it.
type Base struct {
	ID string `json:"id"`
	// Profile is the SpecCritic profile the spec is reviewed under.
	Profile string `json:"profile"`
	// Provenance says where the spec and its labels came from, so a reader
	// knows how far to trust them.
	Provenance string     `json:"provenance"`
	Mutations  []Mutation `json:"mutations"`
	// Text is the clean spec.
	Text string `json:"-"`
}

// Mutation seeds one defect into a base spec.
type Mutation struct {
	ID string `json:"id"`
	// Note explains, for a human reader, what is wrong after the mutation.
	Note string `json:"note"`
	// Categories are the defect categories that correctly describe the
	// defect. The first is the primary one, used to group results.
	Categories []schema.Category `json:"categories"`
	// Severity is the severity a reviewer should assign.
	Severity schema.Severity `json:"severity"`
	// Keywords are lowercase fragments, any one of which marks a finding as
	// being about this defect when its category differs from Categories.
	Keywords []string `json:"keywords"`
	Edits    []Edit   `json:"edits"`
	// Evidence names where in the mutated spec a finding about this defect
	// is expected to point.
	Evidence []Anchor `json:"evidence"`
}

// Edit is one change to a spec. Exactly one of Find, DeleteLine, ReplaceLine
// and InsertAfterLine is set, and the text it names must occur exactly once.
type Edit struct {
	// Find is replaced by Replace.
	Find    string `json:"find,omitempty"`
	Replace string `json:"replace,omitempty"`
	// DeleteLine removes the whole line containing this text.
	DeleteLine string `json:"delete_line,omitempty"`
	// ReplaceLine replaces the whole line containing this text with With.
	ReplaceLine string `json:"replace_line,omitempty"`
	// InsertAfterLine adds With as a new line after the line containing this
	// text.
	InsertAfterLine string `json:"insert_after_line,omitempty"`
	With            string `json:"with,omitempty"`
}

// Anchor names lines of a spec by content, so labels survive edits that shift
// line numbers. Exactly one field is set.
type Anchor struct {
	// Text selects the one line containing it.
	Text string `json:"text,omitempty"`
	// Section selects every line under the heading with this exact title, up
	// to the next heading of the same or a higher level.
	Section string `json:"section,omitempty"`
}

// LineRange is an inclusive range of 1-based line numbers.
type LineRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Defect is a seeded defect located in the spec of one case.
type Defect struct {
	ID         string            `json:"id"`
	Note       string            `json:"note"`
	Categories []schema.Category `json:"categories"`
	Severity   schema.Severity   `json:"severity"`
	Keywords   []string          `json:"keywords"`
	// Ranges are the lines a finding about this defect is expected to cite.
	Ranges []LineRange `json:"ranges"`
}

// Case is one spec to review together with what a correct review reports.
type Case struct {
	// ID is unique in a run and safe to use as a file name.
	ID      string  `json:"id"`
	Base    string  `json:"base"`
	Variant Variant `json:"variant"`
	Profile string  `json:"profile"`
	// Spec is the text to review.
	Spec string `json:"-"`
	// Defects are the seeded defects. A clean case has none.
	Defects []Defect `json:"defects"`
}

// LoadCorpus reads every base in dir: a spec NAME.md with its labels in
// NAME.defects.json.
func LoadCorpus(dir string) ([]Base, error) {
	labelFiles, err := filepath.Glob(filepath.Join(dir, "*.defects.json"))
	if err != nil {
		return nil, err
	}
	if len(labelFiles) == 0 {
		return nil, fmt.Errorf("no *.defects.json files in %s", dir)
	}
	sort.Strings(labelFiles)
	bases := make([]Base, 0, len(labelFiles))
	for _, labelFile := range labelFiles {
		name := strings.TrimSuffix(filepath.Base(labelFile), ".defects.json")
		raw, err := os.ReadFile(labelFile)
		if err != nil {
			return nil, err
		}
		var base Base
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&base); err != nil {
			return nil, fmt.Errorf("%s: %w", labelFile, err)
		}
		if base.ID != name {
			return nil, fmt.Errorf("%s: id %q does not match the file name", labelFile, base.ID)
		}
		text, err := os.ReadFile(filepath.Join(dir, name+".md"))
		if err != nil {
			return nil, fmt.Errorf("%s: reading its spec: %w", labelFile, err)
		}
		base.Text = string(text)
		if err := base.validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", labelFile, err)
		}
		bases = append(bases, base)
	}
	return bases, nil
}

func (b Base) validate() error {
	if b.Profile == "" || b.Provenance == "" {
		return fmt.Errorf("profile and provenance are required")
	}
	if len(b.Mutations) == 0 {
		return fmt.Errorf("at least one mutation is required")
	}
	seen := make(map[string]bool, len(b.Mutations))
	for _, m := range b.Mutations {
		switch {
		case m.ID == "" || seen[m.ID]:
			return fmt.Errorf("mutation id %q is empty or repeated", m.ID)
		case len(m.Categories) == 0:
			return fmt.Errorf("mutation %s: at least one category is required", m.ID)
		case len(m.Edits) == 0 || len(m.Evidence) == 0:
			return fmt.Errorf("mutation %s: edits and evidence are required", m.ID)
		}
		seen[m.ID] = true
		for _, category := range m.Categories {
			if !schema.IsValidCategory(category) {
				return fmt.Errorf("mutation %s: unknown category %q", m.ID, category)
			}
		}
		switch m.Severity {
		case schema.SeverityInfo, schema.SeverityWarn, schema.SeverityCritical:
		default:
			return fmt.Errorf("mutation %s: unknown severity %q", m.ID, m.Severity)
		}
		for _, keyword := range m.Keywords {
			if keyword != strings.ToLower(keyword) {
				return fmt.Errorf("mutation %s: keyword %q must be lowercase", m.ID, keyword)
			}
		}
	}
	return nil
}

// BuildCases derives the cases to review from bases. Every base yields a clean
// case and a seeded case with all of its mutations. With isolated set, it also
// yields one case per mutation, which shows which single defect was missed at
// the cost of that many more reviews.
func BuildCases(bases []Base, isolated bool) ([]Case, error) {
	var cases []Case
	for _, base := range bases {
		cases = append(cases, Case{
			ID:      base.ID + "-" + string(VariantClean),
			Base:    base.ID,
			Variant: VariantClean,
			Profile: base.Profile,
			Spec:    base.Text,
		})
		seeded, err := base.seed(base.ID+"-"+string(VariantSeeded), VariantSeeded, base.Mutations)
		if err != nil {
			return nil, err
		}
		cases = append(cases, seeded)
		if !isolated {
			continue
		}
		for _, m := range base.Mutations {
			single, err := base.seed(base.ID+"-"+string(VariantIsolated)+"-"+m.ID, VariantIsolated, []Mutation{m})
			if err != nil {
				return nil, err
			}
			cases = append(cases, single)
		}
	}
	return cases, nil
}

// seed applies mutations to the base spec and locates each defect's evidence
// in the result.
func (b Base) seed(id string, variant Variant, mutations []Mutation) (Case, error) {
	text := b.Text
	for _, m := range mutations {
		for i, edit := range m.Edits {
			edited, err := edit.apply(text)
			if err != nil {
				return Case{}, fmt.Errorf("%s: mutation %s edit %d: %w", id, m.ID, i, err)
			}
			text = edited
		}
	}
	lines := spec.Lines(text)
	c := Case{ID: id, Base: b.ID, Variant: variant, Profile: b.Profile, Spec: text}
	for _, m := range mutations {
		defect := Defect{ID: m.ID, Note: m.Note, Categories: m.Categories, Severity: m.Severity, Keywords: m.Keywords}
		for _, anchor := range m.Evidence {
			r, err := anchor.resolve(lines)
			if err != nil {
				return Case{}, fmt.Errorf("%s: mutation %s evidence: %w", id, m.ID, err)
			}
			defect.Ranges = append(defect.Ranges, r)
		}
		c.Defects = append(c.Defects, defect)
	}
	return c, nil
}

func (e Edit) apply(text string) (string, error) {
	set := 0
	for _, field := range []string{e.Find, e.DeleteLine, e.ReplaceLine, e.InsertAfterLine} {
		if field != "" {
			set++
		}
	}
	if set != 1 {
		return "", fmt.Errorf("exactly one of find, delete_line, replace_line and insert_after_line must be set")
	}
	if e.Find != "" {
		if n := strings.Count(text, e.Find); n != 1 {
			return "", fmt.Errorf("find text occurs %d times, want 1: %q", n, e.Find)
		}
		return strings.Replace(text, e.Find, e.Replace, 1), nil
	}

	lines := strings.Split(text, "\n")
	needle := e.DeleteLine + e.ReplaceLine + e.InsertAfterLine
	idx, err := lineContaining(lines, needle)
	if err != nil {
		return "", err
	}
	switch {
	case e.DeleteLine != "":
		lines = append(lines[:idx], lines[idx+1:]...)
	case e.ReplaceLine != "":
		lines[idx] = e.With
	default:
		lines = append(lines[:idx+1], append([]string{e.With}, lines[idx+1:]...)...)
	}
	return strings.Join(lines, "\n"), nil
}

func (a Anchor) resolve(lines []string) (LineRange, error) {
	switch {
	case a.Text != "" && a.Section == "":
		idx, err := lineContaining(lines, a.Text)
		if err != nil {
			return LineRange{}, err
		}
		return LineRange{Start: idx + 1, End: idx + 1}, nil
	case a.Section != "" && a.Text == "":
		return sectionRange(lines, a.Section)
	}
	return LineRange{}, fmt.Errorf("exactly one of text and section must be set")
}

// lineContaining returns the index of the one line that contains needle.
func lineContaining(lines []string, needle string) (int, error) {
	found := -1
	for i, line := range lines {
		if !strings.Contains(line, needle) {
			continue
		}
		if found >= 0 {
			return 0, fmt.Errorf("text is on more than one line: %q", needle)
		}
		found = i
	}
	if found < 0 {
		return 0, fmt.Errorf("text is on no line: %q", needle)
	}
	return found, nil
}

// sectionRange returns the lines from the heading titled title to the line
// before the next heading of the same or a higher level. Lines inside a fenced
// code block are never headings, whatever they start with.
func sectionRange(lines []string, title string) (LineRange, error) {
	start, level := -1, 0
	var open fence
	for i, line := range lines {
		if open.length > 0 {
			if open.closedBy(line) {
				open = fence{}
			}
			continue
		}
		if open = openingFence(line); open.length > 0 {
			continue
		}
		l, text := heading(line)
		if l == 0 {
			continue
		}
		if start >= 0 && l <= level {
			return LineRange{Start: start + 1, End: i}, nil
		}
		if start < 0 && text == title {
			start, level = i, l
		}
	}
	if start < 0 {
		return LineRange{}, fmt.Errorf("no section titled %q", title)
	}
	return LineRange{Start: start + 1, End: len(lines)}, nil
}

// fence is an open fenced code block: the character it was opened with and
// how many of them. The zero value means no block is open.
type fence struct {
	char   byte
	length int
}

// openingFence returns the fence a line opens: three or more backticks or
// tildes at its start. Any other line returns the zero value.
func openingFence(line string) fence {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || (trimmed[0] != '`' && trimmed[0] != '~') {
		return fence{}
	}
	run := len(trimmed) - len(strings.TrimLeft(trimmed, trimmed[:1]))
	if run < 3 {
		return fence{}
	}
	return fence{char: trimmed[0], length: run}
}

// closedBy reports whether line closes the block: the same character, at least
// as many of them as opened it, and nothing else on the line.
func (f fence) closedBy(line string) bool {
	trimmed := strings.TrimSpace(line)
	return len(trimmed) >= f.length && strings.Trim(trimmed, string(f.char)) == ""
}

// heading returns the level and title of a Markdown heading line, or level 0.
func heading(line string) (int, string) {
	trimmed := strings.TrimLeft(line, "#")
	level := len(line) - len(trimmed)
	if level == 0 || level > 6 || !strings.HasPrefix(trimmed, " ") {
		return 0, ""
	}
	return level, strings.TrimSpace(trimmed)
}
