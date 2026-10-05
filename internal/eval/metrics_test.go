package eval

import (
	"math"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/schema"
)

func TestNewProportion(t *testing.T) {
	if p := newProportion(0, 0); p.Rate != 0 || p.Low != 0 || p.High != 0 || p.String() != "no data" {
		t.Errorf("0/0 = %+v (%s), want no data", p, p)
	}
	p := newProportion(5, 10)
	if p.Rate != 0.5 || p.Low >= 0.5 || p.High <= 0.5 || p.Low < 0.2 || p.High > 0.8 {
		t.Errorf("5/10 = %+v, want an interval around 0.5 within 0.2-0.8", p)
	}
	if math.Abs((p.Low+p.High)/2-0.5) > 1e-9 {
		t.Errorf("5/10 interval %v-%v is not centered on 0.5", p.Low, p.High)
	}
	// The interval never leaves 0-1 and narrows as the sample grows.
	if all := newProportion(10, 10); all.High != 1 || all.Low >= 1 {
		t.Errorf("10/10 = %+v", all)
	}
	small, large := newProportion(5, 10), newProportion(500, 1000)
	if large.High-large.Low >= small.High-small.Low {
		t.Errorf("interval did not narrow: n=10 %v, n=1000 %v", small, large)
	}
	if got := newProportion(7, 9).String(); !strings.HasPrefix(got, "7/9 (78%, ") {
		t.Errorf("String() = %q", got)
	}
}

func finding(defect string, severity schema.Severity, category schema.Category, preflight bool) Finding {
	return Finding{Severity: severity, Category: category, Defect: defect, Preflight: preflight, CategoryOK: defect != ""}
}

func TestSummarize(t *testing.T) {
	usage := &schema.UsageMeta{Calls: 2, RepairCalls: 1, InputTokens: 100, OutputTokens: 50, CacheReadTokens: 10, CacheWriteTokens: 5}
	seeded := func(rep int, verdict schema.Verdict, defects []DefectGrade, findings ...Finding) RunResult {
		return RunResult{Case: "demo-seeded", Base: "demo", Variant: VariantSeeded, Rep: rep, Model: "fake:m", Verdict: verdict, Grade: Grade{Defects: defects, Findings: findings}, Usage: usage, WallMS: 2000}
	}
	clean := func(rep int, verdict schema.Verdict, findings ...Finding) RunResult {
		return RunResult{Case: "demo-clean", Base: "demo", Variant: VariantClean, Rep: rep, Model: "fake:m", Verdict: verdict, Grade: Grade{Findings: findings}, WallMS: 1000}
	}
	critical, warn := schema.SeverityCritical, schema.SeverityWarn
	contradiction, missing := schema.CategoryContradiction, schema.CategoryMissingFailureMode

	results := []RunResult{
		// Both defects found by the model, one of them under-rated.
		seeded(0, schema.VerdictInvalid,
			[]DefectGrade{
				{ID: "a", Category: contradiction, Expected: critical, Found: true, FoundByModel: true, CategoryOK: true, Reported: critical},
				{ID: "b", Category: missing, Expected: critical, Found: true, FoundByModel: true, CategoryOK: false, Reported: warn},
			},
			finding("a", critical, contradiction, false),
			finding("b", warn, schema.CategoryAmbiguousBehavior, false),
			finding("", warn, contradiction, false), // matches no label
		),
		// One defect found only by preflight, one missed, and the gate let it through.
		seeded(1, schema.VerdictValidWithGaps,
			[]DefectGrade{
				{ID: "a", Category: contradiction, Expected: critical, Found: true, FoundByModel: false, CategoryOK: true, Reported: warn},
				{ID: "b", Category: missing, Expected: critical},
			},
			finding("a", warn, contradiction, true),
		),
		clean(0, schema.VerdictValid),
		clean(1, schema.VerdictInvalid, finding("", critical, missing, false), finding("", critical, missing, false)),
	}
	s := Summarize(results, []RunError{{Case: "demo-seeded", Rep: 2, Error: "boom"}})

	check := func(name string, got Proportion, hits, total int) {
		t.Helper()
		if got.Hits != hits || got.Total != total {
			t.Errorf("%s = %d/%d, want %d/%d", name, got.Hits, got.Total, hits, total)
		}
	}
	if s.Runs != 4 || s.Errors != 1 {
		t.Errorf("runs=%d errors=%d, want 4 and 1", s.Runs, s.Errors)
	}
	check("recall", s.Recall, 3, 4)
	check("model recall", s.ModelRecall, 2, 4)
	check("critical recall", s.CriticalRecall, 1, 4)
	check("category agreement", s.CategoryAgreement, 2, 3)
	check("severity agreement", s.SeverityAgreement, 1, 3)
	check("precision", s.Precision, 2, 3)
	check("false critical", s.FalseCritical, 1, 2)
	check("verdict expected", s.VerdictExpected, 2, 4)
	if s.CleanCriticalMean != 1 {
		t.Errorf("clean critical mean = %v, want 1", s.CleanCriticalMean)
	}
	// Each case split its two runs between two verdicts.
	if s.VerdictAgreement != 0.5 || len(s.Verdicts) != 2 {
		t.Errorf("verdict agreement = %v over %d cases, want 0.5 over 2", s.VerdictAgreement, len(s.Verdicts))
	}

	byCategory := map[schema.Category]CategoryStats{}
	for _, cs := range s.Categories {
		byCategory[cs.Category] = cs
	}
	check("contradiction recall", byCategory[contradiction].Recall, 2, 2)
	check("missing-failure recall", byCategory[missing].Recall, 1, 2)
	// Preflight findings and clean-spec findings stay out of precision.
	check("contradiction precision", byCategory[contradiction].Precision, 1, 2)
	check("ambiguous precision", byCategory[schema.CategoryAmbiguousBehavior].Precision, 1, 1)

	if s.Usage.Calls != 4 || s.Usage.RepairCalls != 2 || s.Usage.InputTokens != 200 || s.Usage.OutputTokens != 100 || s.Usage.WallMS != 6000 {
		t.Errorf("usage = %+v", s.Usage)
	}
	if len(s.Unmatched) != 3 {
		t.Errorf("unmatched = %d, want the seeded stray and both clean criticals", len(s.Unmatched))
	}
	if len(s.Defects) != 2 || s.Defects[0].Defect != "a" || s.Defects[0].Found != 2 || s.Defects[0].ByModel != 1 || s.Defects[1].Found != 1 {
		t.Errorf("defect stats = %+v", s.Defects)
	}
	if len(s.Models) != 1 || s.Models[0] != "fake:m" {
		t.Errorf("models = %v", s.Models)
	}

	md := s.Markdown()
	for _, want := range []string{"Defects found | 3/4", "Runs that reported a CRITICAL | 1/2", "| demo | b | MISSING_FAILURE_MODE | CRITICAL | 1/2 |", "errors.jsonl", "match no label"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown summary is missing %q:\n%s", want, md)
		}
	}
}

func TestSummarizeNothing(t *testing.T) {
	s := Summarize(nil, nil)
	if s.Runs != 0 || s.Recall.Total != 0 || s.VerdictAgreement != 0 {
		t.Errorf("summary of nothing = %+v", s)
	}
	if md := s.Markdown(); !strings.Contains(md, "no data") {
		t.Errorf("markdown for an empty run should say there is no data:\n%s", md)
	}
}
