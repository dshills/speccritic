package eval

import (
	"fmt"
	"sort"
	"strings"
)

// maxUnmatchedListed caps the unmatched findings printed in the Markdown
// summary. summary.json holds all of them.
const maxUnmatchedListed = 40

// Markdown renders the summary for a person to read.
func (s Summary) Markdown() string {
	var b strings.Builder
	b.WriteString("# SpecCritic eval\n\n")
	fmt.Fprintf(&b, "Model: %s. Graded reviews: %d. Reviews with nothing to grade: %d.\n\n", strings.Join(s.Models, ", "), s.Runs, s.Errors)
	if s.Errors > 0 {
		b.WriteString("Reviews with nothing to grade are listed in `errors.jsonl` and are not part of any number below.\n\n")
	}
	b.WriteString("Each rate is followed by its 95% interval: how far that rate could move if the same run were repeated. The interval describes one run. It is not a test of the difference between two runs, which needs the two sets of results compared case by case.\n\n")

	b.WriteString("## Seeded defects\n\n")
	b.WriteString("| Measure | Result |\n|---|---|\n")
	row := func(name string, p Proportion) { fmt.Fprintf(&b, "| %s | %s |\n", name, p) }
	row("Defects found", s.Recall)
	row("Defects found by the model, not preflight alone", s.ModelRecall)
	row("CRITICAL defects found and reported as CRITICAL", s.CriticalRecall)
	row("Found defects reported under an accepted category", s.CategoryAgreement)
	row("Found defects reported at the labeled severity", s.SeverityAgreement)
	row("Model findings that match a label (lower bound on precision)", s.Precision)

	b.WriteString("\n## Clean specs\n\n")
	b.WriteString("| Measure | Result |\n|---|---|\n")
	row("Runs that reported a CRITICAL", s.FalseCritical)
	fmt.Fprintf(&b, "| CRITICAL findings per run | %.2f |\n", s.CleanCriticalMean)

	b.WriteString("\n## Verdicts\n\n")
	b.WriteString("| Measure | Result |\n|---|---|\n")
	row("Runs with the verdict the case calls for", s.VerdictExpected)
	fmt.Fprintf(&b, "| Mean share of runs agreeing on a case's verdict | %.0f%% |\n", 100*s.VerdictAgreement)
	b.WriteString("\n| Case | Verdicts | Agreement |\n|---|---|---|\n")
	for _, cv := range s.Verdicts {
		names := make([]string, 0, len(cv.Verdicts))
		for verdict, n := range cv.Verdicts {
			names = append(names, fmt.Sprintf("%s x%d", verdict, n))
		}
		sort.Strings(names)
		fmt.Fprintf(&b, "| %s | %s | %.0f%% |\n", cv.Case, strings.Join(names, ", "), 100*cv.Agreement)
	}

	b.WriteString("\n## By category\n\n")
	b.WriteString("| Category | Recall | Precision (lower bound) |\n|---|---|---|\n")
	for _, cs := range s.Categories {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", cs.Category, cs.Recall, cs.Precision)
	}

	b.WriteString("\n## By defect\n\n")
	b.WriteString("| Spec | Defect | Category | Labeled | Found | By model | Category right | Severity right |\n|---|---|---|---|---|---|---|---|\n")
	for _, d := range s.Defects {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %d/%d | %d/%d | %d/%d | %d/%d |\n",
			d.Base, d.Defect, d.Category, d.Expected, d.Found, d.Runs, d.ByModel, d.Runs, d.CategoryOK, d.Found, d.SeverityOK, d.Found)
	}

	b.WriteString("\n## Cost\n\n")
	b.WriteString("| Measure | Total | Per review |\n|---|---|---|\n")
	mean := func(total float64) float64 {
		if s.Runs == 0 {
			return 0
		}
		return total / float64(s.Runs)
	}
	per := func(name string, total int64) {
		fmt.Fprintf(&b, "| %s | %d | %.1f |\n", name, total, mean(float64(total)))
	}
	per("LLM calls", int64(s.Usage.Calls))
	per("Repair calls", int64(s.Usage.RepairCalls))
	per("Continuation calls", int64(s.Usage.ContinuationCalls))
	per("Input tokens (uncached)", int64(s.Usage.InputTokens))
	per("Cache-read tokens", int64(s.Usage.CacheReadTokens))
	per("Cache-write tokens", int64(s.Usage.CacheWriteTokens))
	per("Output tokens", int64(s.Usage.OutputTokens))
	per("Findings dropped as invalid", int64(s.Usage.DroppedFindings))
	seconds := float64(s.Usage.WallMS) / 1000
	fmt.Fprintf(&b, "| Wall-clock seconds | %.1f | %.1f |\n", seconds, mean(seconds))

	b.WriteString("\n## Model findings that match no label\n\n")
	if len(s.Unmatched) == 0 {
		b.WriteString("None.\n")
		return b.String()
	}
	b.WriteString("Each of these is either a false positive or a real defect the labels do not list. Read them before trusting the precision figures; a real one belongs in the labels or should be fixed in the clean spec.\n\n")
	b.WriteString("| Case | Run | Severity | Category | Lines | Title |\n|---|---|---|---|---|---|\n")
	unmatched := append([]UnmatchedFinding(nil), s.Unmatched...)
	sort.SliceStable(unmatched, func(i, j int) bool { return severityRank(unmatched[i].Severity) > severityRank(unmatched[j].Severity) })
	for i, u := range unmatched {
		if i == maxUnmatchedListed {
			fmt.Fprintf(&b, "\n%d more are in `summary.json`.\n", len(unmatched)-maxUnmatchedListed)
			break
		}
		category := string(u.Category)
		if category == "" {
			category = "(question)"
		}
		fmt.Fprintf(&b, "| %s | %d | %s | %s | %s | %s |\n", u.Case, u.Rep, u.Severity, category, formatRanges(u.Ranges), strings.ReplaceAll(u.Title, "|", "\\|"))
	}
	return b.String()
}

// String renders a proportion as "7/9 (78%, 45-94%)".
func (p Proportion) String() string {
	if p.Total == 0 {
		return "no data"
	}
	return fmt.Sprintf("%d/%d (%.0f%%, %.0f-%.0f%%)", p.Hits, p.Total, 100*p.Rate, 100*p.Low, 100*p.High)
}

func formatRanges(ranges []LineRange) string {
	parts := make([]string, 0, len(ranges))
	for _, r := range ranges {
		if r.End > r.Start {
			parts = append(parts, fmt.Sprintf("L%d-L%d", r.Start, r.End))
		} else {
			parts = append(parts, fmt.Sprintf("L%d", r.Start))
		}
	}
	return strings.Join(parts, ", ")
}
