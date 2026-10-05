package eval

import (
	"testing"

	"github.com/dshills/speccritic/internal/schema"
)

// gradeCase has two defects whose evidence overlaps: a contradiction on line
// 12 sits inside the section (lines 10-15) of a missing failure mode.
func gradeCase() Case {
	return Case{
		ID: "demo-seeded", Base: "demo", Variant: VariantSeeded,
		Defects: []Defect{
			{
				ID:         "store-failure",
				Categories: []schema.Category{schema.CategoryMissingFailureMode, schema.CategoryUnspecifiedConstraint},
				Severity:   schema.SeverityCritical,
				Keywords:   []string{"unavailable", "unreachable"},
				Ranges:     []LineRange{{10, 15}},
			},
			{
				ID:         "counter-contradiction",
				Categories: []schema.Category{schema.CategoryContradiction},
				Severity:   schema.SeverityCritical,
				Keywords:   []string{"contradict"},
				Ranges:     []LineRange{{12, 12}, {30, 30}},
			},
		},
	}
}

func issue(id string, severity schema.Severity, category schema.Category, title string, line int, tags ...string) schema.Issue {
	return schema.Issue{
		ID: id, Severity: severity, Category: category, Title: title, Tags: tags,
		Evidence: []schema.Evidence{{LineStart: line, LineEnd: line}},
	}
}

func defectGrade(t *testing.T, g Grade, id string) DefectGrade {
	t.Helper()
	for _, d := range g.Defects {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("no grade for defect %q", id)
	return DefectGrade{}
}

// A report that cites every defect with its primary category and labeled
// severity must earn full marks. If it does not, the grader is broken.
func TestGradeReport_OracleEarnsFullMarks(t *testing.T) {
	c := gradeCase()
	report := &schema.Report{}
	for i, d := range c.Defects {
		report.Issues = append(report.Issues, issue("ISSUE-000"+string(rune('1'+i)), d.Severity, d.Categories[0], "t", d.Ranges[0].Start))
	}
	g := GradeReport(c, report, 0)
	for _, d := range g.Defects {
		if !d.Found || !d.FoundByModel || !d.CategoryOK || d.Reported != d.Expected {
			t.Errorf("defect %s = %+v, want found with the right category and severity", d.ID, d)
		}
	}
	for _, f := range g.Findings {
		if f.Defect == "" {
			t.Errorf("finding %s matched no defect", f.ID)
		}
	}
}

// A report with nothing in it must earn nothing.
func TestGradeReport_EmptyReportFindsNothing(t *testing.T) {
	for _, report := range []*schema.Report{nil, {}} {
		g := GradeReport(gradeCase(), report, 2)
		for _, d := range g.Defects {
			if d.Found || d.CategoryOK || d.Reported != "" {
				t.Errorf("defect %s = %+v, want not found", d.ID, d)
			}
		}
		if len(g.Findings) != 0 {
			t.Errorf("findings = %v, want none", g.Findings)
		}
	}
}

func TestGradeReport_Matching(t *testing.T) {
	cases := map[string]struct {
		report       *schema.Report
		tolerance    int
		defect       string
		wantFound    bool
		wantCategory bool
		wantByModel  bool
		wantReported schema.Severity
	}{
		"right place but about something else": {
			report: &schema.Report{Issues: []schema.Issue{issue("ISSUE-0001", schema.SeverityInfo, schema.CategoryScopeLeak, "Names a database", 11)}},
			defect: "store-failure",
		},
		"right place, other category, keyword in the text": {
			report: &schema.Report{Issues: []schema.Issue{issue("ISSUE-0001", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "Behavior when the store is unreachable", 11)}},
			defect: "store-failure", wantFound: true, wantByModel: true, wantReported: schema.SeverityWarn,
		},
		"accepted secondary category": {
			report: &schema.Report{Issues: []schema.Issue{issue("ISSUE-0001", schema.SeverityCritical, schema.CategoryUnspecifiedConstraint, "t", 14)}},
			defect: "store-failure", wantFound: true, wantCategory: true, wantByModel: true, wantReported: schema.SeverityCritical,
		},
		"right category, wrong place": {
			report: &schema.Report{Issues: []schema.Issue{issue("ISSUE-0001", schema.SeverityCritical, schema.CategoryMissingFailureMode, "t", 40)}},
			defect: "store-failure",
		},
		"two lines off with tolerance": {
			report:    &schema.Report{Issues: []schema.Issue{issue("ISSUE-0001", schema.SeverityCritical, schema.CategoryMissingFailureMode, "t", 17)}},
			tolerance: 2, defect: "store-failure", wantFound: true, wantCategory: true, wantByModel: true, wantReported: schema.SeverityCritical,
		},
		"two lines off without tolerance": {
			report: &schema.Report{Issues: []schema.Issue{issue("ISSUE-0001", schema.SeverityCritical, schema.CategoryMissingFailureMode, "t", 17)}},
			defect: "store-failure",
		},
		"question with a keyword": {
			report: &schema.Report{Questions: []schema.Question{{ID: "Q-0001", Severity: schema.SeverityCritical, Question: "What happens when the store is unavailable?", Evidence: []schema.Evidence{{LineStart: 10, LineEnd: 10}}}}},
			defect: "store-failure", wantFound: true, wantByModel: true, wantReported: schema.SeverityCritical,
		},
		"question without a keyword": {
			report: &schema.Report{Questions: []schema.Question{{ID: "Q-0001", Severity: schema.SeverityCritical, Question: "Who owns this?", Evidence: []schema.Evidence{{LineStart: 10, LineEnd: 10}}}}},
			defect: "store-failure",
		},
		"found only by preflight": {
			report: &schema.Report{Issues: []schema.Issue{issue("PREFLIGHT-X", schema.SeverityWarn, schema.CategoryMissingFailureMode, "t", 11, "preflight")}},
			defect: "store-failure", wantFound: true, wantCategory: true, wantReported: schema.SeverityWarn,
		},
		"highest severity among several matches is reported": {
			report: &schema.Report{Issues: []schema.Issue{
				issue("ISSUE-0001", schema.SeverityInfo, schema.CategoryMissingFailureMode, "t", 11),
				issue("ISSUE-0002", schema.SeverityCritical, schema.CategoryMissingFailureMode, "t", 13),
			}},
			defect: "store-failure", wantFound: true, wantCategory: true, wantByModel: true, wantReported: schema.SeverityCritical,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			d := defectGrade(t, GradeReport(gradeCase(), tc.report, tc.tolerance), tc.defect)
			if d.Found != tc.wantFound || d.CategoryOK != tc.wantCategory || d.FoundByModel != tc.wantByModel || d.Reported != tc.wantReported {
				t.Errorf("grade = %+v, want found=%v category=%v byModel=%v reported=%q", d, tc.wantFound, tc.wantCategory, tc.wantByModel, tc.wantReported)
			}
		})
	}
}

// Line 12 is evidence for both defects. A contradiction reported there belongs
// to the contradiction, not to the failure mode whose section contains it.
func TestGradeReport_OverlappingEvidenceGoesToTheDefectTheFindingNames(t *testing.T) {
	report := &schema.Report{Issues: []schema.Issue{
		issue("ISSUE-0001", schema.SeverityCritical, schema.CategoryContradiction, "The store being unreachable is contradicted here", 12),
	}}
	g := GradeReport(gradeCase(), report, 0)
	if g.Findings[0].Defect != "counter-contradiction" || !g.Findings[0].CategoryOK {
		t.Fatalf("finding matched %q (category ok %v), want counter-contradiction", g.Findings[0].Defect, g.Findings[0].CategoryOK)
	}
	if defectGrade(t, g, "store-failure").Found {
		t.Fatal("the failure-mode defect was credited with a finding about the contradiction")
	}
}

func TestGradeReport_RepeatsOfOneDefectAreNotUnmatched(t *testing.T) {
	report := &schema.Report{Issues: []schema.Issue{
		issue("ISSUE-0001", schema.SeverityCritical, schema.CategoryMissingFailureMode, "t", 10),
		issue("ISSUE-0002", schema.SeverityWarn, schema.CategoryMissingFailureMode, "t", 15),
	}}
	g := GradeReport(gradeCase(), report, 0)
	for _, f := range g.Findings {
		if f.Defect != "store-failure" {
			t.Errorf("finding %s matched %q, want store-failure", f.ID, f.Defect)
		}
	}
}
