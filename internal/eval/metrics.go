package eval

import (
	"math"
	"sort"

	"github.com/dshills/speccritic/internal/schema"
)

// RunResult is one graded review of one case.
type RunResult struct {
	Case    string  `json:"case"`
	Base    string  `json:"base"`
	Variant Variant `json:"variant"`
	// Rep numbers the repeats of a case from 0.
	Rep int `json:"rep"`
	// Model is the model that answered, as the provider reported it.
	Model   string         `json:"model"`
	Verdict schema.Verdict `json:"verdict"`
	Score   int            `json:"score"`
	Grade   Grade          `json:"grade"`
	// Usage is what the review's LLM calls used.
	Usage           *schema.UsageMeta `json:"usage,omitempty"`
	DroppedFindings int               `json:"dropped_findings"`
	// WallMS is how long the whole review took, preflight included.
	WallMS int64 `json:"wall_ms"`
}

// RunError is a review that produced nothing to grade: a provider failure, a
// timeout, or output that stayed invalid. It is kept apart from the results so
// that a broken run is never read as a review that found nothing.
type RunError struct {
	Case   string `json:"case"`
	Rep    int    `json:"rep"`
	Error  string `json:"error"`
	WallMS int64  `json:"wall_ms"`
}

// Proportion is a count of hits out of a total with its 95% Wilson interval.
// The interval says how far the rate could move on a rerun of the same size.
type Proportion struct {
	Hits  int     `json:"hits"`
	Total int     `json:"total"`
	Rate  float64 `json:"rate"`
	Low   float64 `json:"low"`
	High  float64 `json:"high"`
}

func newProportion(hits, total int) Proportion {
	p := Proportion{Hits: hits, Total: total}
	if total == 0 {
		return p
	}
	const z = 1.96
	n := float64(total)
	rate := float64(hits) / n
	denominator := 1 + z*z/n
	center := (rate + z*z/(2*n)) / denominator
	margin := z * math.Sqrt(rate*(1-rate)/n+z*z/(4*n*n)) / denominator
	p.Rate = rate
	p.Low = math.Max(0, center-margin)
	p.High = math.Min(1, center+margin)
	return p
}

// CategoryStats holds the per-category rates.
type CategoryStats struct {
	Category schema.Category `json:"category"`
	// Recall is seeded defects of this category that were found.
	Recall Proportion `json:"recall"`
	// Precision is model issues reported under this category that matched a
	// seeded defect. An issue matching no label counts against it even if it
	// is a real defect the labels do not list, so it is a lower bound.
	Precision Proportion `json:"precision"`
}

// DefectStats says how often one seeded defect was found across its runs.
type DefectStats struct {
	Base       string          `json:"base"`
	Defect     string          `json:"defect"`
	Category   schema.Category `json:"category"`
	Expected   schema.Severity `json:"expected_severity"`
	Runs       int             `json:"runs"`
	Found      int             `json:"found"`
	ByModel    int             `json:"found_by_model"`
	CategoryOK int             `json:"category_ok"`
	SeverityOK int             `json:"severity_ok"`
}

// CaseVerdicts says how the verdict of one case varied across its runs.
type CaseVerdicts struct {
	Case     string                 `json:"case"`
	Verdicts map[schema.Verdict]int `json:"verdicts"`
	// Agreement is the share of runs that returned the most common verdict.
	Agreement float64 `json:"agreement"`
}

// UnmatchedFinding is a model finding on a seeded or clean spec that matches
// no label. It is either a false positive or a real defect the labels miss;
// only reading it tells which.
type UnmatchedFinding struct {
	Case     string          `json:"case"`
	Rep      int             `json:"rep"`
	Severity schema.Severity `json:"severity"`
	Category schema.Category `json:"category,omitempty"`
	Title    string          `json:"title"`
	Ranges   []LineRange     `json:"ranges"`
}

// UsageStats totals what the graded reviews used.
type UsageStats struct {
	Calls             int   `json:"calls"`
	RepairCalls       int   `json:"repair_calls"`
	ContinuationCalls int   `json:"continuation_calls"`
	InputTokens       int   `json:"input_tokens"`
	OutputTokens      int   `json:"output_tokens"`
	CacheReadTokens   int   `json:"cache_read_tokens"`
	CacheWriteTokens  int   `json:"cache_write_tokens"`
	DroppedFindings   int   `json:"dropped_findings"`
	WallMS            int64 `json:"wall_ms"`
}

// Summary is the outcome of an eval run.
type Summary struct {
	Models []string `json:"models"`
	// Runs counts graded reviews. Errors counts reviews with nothing to grade.
	Runs   int `json:"runs"`
	Errors int `json:"errors"`

	// Recall is seeded defects found, over every seeded and isolated run.
	Recall Proportion `json:"recall"`
	// ModelRecall counts only defects a model finding matched, leaving out
	// those caught solely by the deterministic preflight rules.
	ModelRecall Proportion `json:"model_recall"`
	// CriticalRecall is defects labeled CRITICAL that were found and reported
	// as CRITICAL. A miss here lets a blocking defect through the gate.
	CriticalRecall Proportion `json:"critical_recall"`
	// CategoryAgreement is found defects reported under an accepted category.
	CategoryAgreement Proportion `json:"category_agreement"`
	// SeverityAgreement is found defects reported at the labeled severity.
	SeverityAgreement Proportion `json:"severity_agreement"`
	// Precision is model findings on seeded specs that matched a label. See
	// CategoryStats.Precision for why it is a lower bound.
	Precision  Proportion      `json:"precision"`
	Categories []CategoryStats `json:"categories"`

	// FalseCritical is clean-spec runs that reported at least one CRITICAL.
	FalseCritical Proportion `json:"false_critical"`
	// CleanCriticalMean is the mean number of CRITICAL findings per clean run.
	CleanCriticalMean float64 `json:"clean_critical_mean"`

	// VerdictAgreement is the mean, over cases, of CaseVerdicts.Agreement.
	VerdictAgreement float64 `json:"verdict_agreement"`
	// VerdictExpected is runs whose verdict fits the case: INVALID for a spec
	// seeded with a CRITICAL defect, anything else for a clean one.
	VerdictExpected Proportion     `json:"verdict_expected"`
	Verdicts        []CaseVerdicts `json:"verdicts"`

	Usage     UsageStats         `json:"usage"`
	Defects   []DefectStats      `json:"defects"`
	Unmatched []UnmatchedFinding `json:"unmatched"`
}

// Summarize computes the eval metrics from graded runs.
func Summarize(results []RunResult, errs []RunError) Summary {
	s := Summary{Runs: len(results), Errors: len(errs)}

	type tally struct{ hits, total int }
	recall, precision := map[schema.Category]*tally{}, map[schema.Category]*tally{}
	bump := func(m map[schema.Category]*tally, category schema.Category, hit bool) {
		t := m[category]
		if t == nil {
			t = &tally{}
			m[category] = t
		}
		t.total++
		if hit {
			t.hits++
		}
	}

	defects := map[string]*DefectStats{}
	verdicts := map[string]*CaseVerdicts{}
	models := map[string]bool{}
	var found, foundByModel, total int
	var criticalKept, criticalTotal, categoryOK, severityOK int
	var matched, modelFindings int
	var cleanRuns, cleanWithCritical, cleanCriticals int
	var verdictOK int

	for _, r := range results {
		models[r.Model] = true
		cv := verdicts[r.Case]
		if cv == nil {
			cv = &CaseVerdicts{Case: r.Case, Verdicts: map[schema.Verdict]int{}}
			verdicts[r.Case] = cv
		}
		cv.Verdicts[r.Verdict]++

		if r.Usage != nil {
			s.Usage.Calls += r.Usage.Calls
			s.Usage.RepairCalls += r.Usage.RepairCalls
			s.Usage.ContinuationCalls += r.Usage.ContinuationCalls
			s.Usage.InputTokens += r.Usage.InputTokens
			s.Usage.OutputTokens += r.Usage.OutputTokens
			s.Usage.CacheReadTokens += r.Usage.CacheReadTokens
			s.Usage.CacheWriteTokens += r.Usage.CacheWriteTokens
		}
		s.Usage.DroppedFindings += r.DroppedFindings
		s.Usage.WallMS += r.WallMS

		criticals := 0
		for _, f := range r.Grade.Findings {
			if f.Severity == schema.SeverityCritical {
				criticals++
			}
		}

		if r.Variant == VariantClean {
			cleanRuns++
			cleanCriticals += criticals
			if criticals > 0 {
				cleanWithCritical++
			}
			if r.Verdict != schema.VerdictInvalid {
				verdictOK++
			}
		} else {
			expectInvalid := false
			for _, d := range r.Grade.Defects {
				expectInvalid = expectInvalid || d.Expected == schema.SeverityCritical
			}
			if (r.Verdict == schema.VerdictInvalid) == expectInvalid {
				verdictOK++
			}
		}

		for _, d := range r.Grade.Defects {
			total++
			key := r.Base + "\x00" + d.ID
			ds := defects[key]
			if ds == nil {
				ds = &DefectStats{Base: r.Base, Defect: d.ID, Category: d.Category, Expected: d.Expected}
				defects[key] = ds
			}
			ds.Runs++
			bump(recall, d.Category, d.Found)
			if d.Expected == schema.SeverityCritical {
				criticalTotal++
				if d.Found && d.Reported == schema.SeverityCritical {
					criticalKept++
				}
			}
			if !d.Found {
				continue
			}
			found++
			ds.Found++
			if d.FoundByModel {
				foundByModel++
				ds.ByModel++
			}
			if d.CategoryOK {
				categoryOK++
				ds.CategoryOK++
			}
			if d.Reported == d.Expected {
				severityOK++
				ds.SeverityOK++
			}
		}

		for _, f := range r.Grade.Findings {
			if f.Preflight {
				continue
			}
			if f.Defect == "" {
				s.Unmatched = append(s.Unmatched, UnmatchedFinding{
					Case: r.Case, Rep: r.Rep, Severity: f.Severity, Category: f.Category, Title: f.Title, Ranges: f.Ranges,
				})
			}
			if r.Variant == VariantClean {
				continue
			}
			modelFindings++
			if f.Defect != "" {
				matched++
			}
			if !f.Question {
				bump(precision, f.Category, f.Defect != "")
			}
		}
	}

	s.Recall = newProportion(found, total)
	s.ModelRecall = newProportion(foundByModel, total)
	s.CriticalRecall = newProportion(criticalKept, criticalTotal)
	s.CategoryAgreement = newProportion(categoryOK, found)
	s.SeverityAgreement = newProportion(severityOK, found)
	s.Precision = newProportion(matched, modelFindings)
	s.FalseCritical = newProportion(cleanWithCritical, cleanRuns)
	if cleanRuns > 0 {
		s.CleanCriticalMean = float64(cleanCriticals) / float64(cleanRuns)
	}
	s.VerdictExpected = newProportion(verdictOK, len(results))

	categories := map[schema.Category]bool{}
	for category := range recall {
		categories[category] = true
	}
	for category := range precision {
		categories[category] = true
	}
	for category := range categories {
		cs := CategoryStats{Category: category}
		if t := recall[category]; t != nil {
			cs.Recall = newProportion(t.hits, t.total)
		}
		if t := precision[category]; t != nil {
			cs.Precision = newProportion(t.hits, t.total)
		}
		s.Categories = append(s.Categories, cs)
	}
	sort.Slice(s.Categories, func(i, j int) bool { return s.Categories[i].Category < s.Categories[j].Category })

	agreement := 0.0
	for _, cv := range verdicts {
		runs, most := 0, 0
		for _, n := range cv.Verdicts {
			runs += n
			most = max(most, n)
		}
		cv.Agreement = float64(most) / float64(runs)
		agreement += cv.Agreement
		s.Verdicts = append(s.Verdicts, *cv)
	}
	if len(verdicts) > 0 {
		s.VerdictAgreement = agreement / float64(len(verdicts))
	}
	sort.Slice(s.Verdicts, func(i, j int) bool { return s.Verdicts[i].Case < s.Verdicts[j].Case })

	for _, ds := range defects {
		s.Defects = append(s.Defects, *ds)
	}
	sort.Slice(s.Defects, func(i, j int) bool {
		if s.Defects[i].Base != s.Defects[j].Base {
			return s.Defects[i].Base < s.Defects[j].Base
		}
		return s.Defects[i].Defect < s.Defects[j].Defect
	})
	for model := range models {
		s.Models = append(s.Models, model)
	}
	sort.Strings(s.Models)
	return s
}
