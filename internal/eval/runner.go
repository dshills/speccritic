package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/dshills/speccritic/internal/schema"
)

// Reviewer reviews the spec of one case, stored at specPath, and returns the
// report. An error means the review produced nothing to grade.
type Reviewer func(ctx context.Context, c Case, specPath string) (*schema.Report, error)

// Config controls an eval run.
type Config struct {
	// Runs is how many times each case is reviewed. Verdict agreement needs
	// at least two.
	Runs int
	// Concurrency is how many reviews run at once.
	Concurrency int
	// Tolerance is how many lines a finding may be off a defect's evidence
	// and still be counted as pointing at it.
	Tolerance int
	// Timeout bounds one review. A review that exceeds it is recorded as an
	// error, never as a review that found nothing.
	Timeout time.Duration
	// OutDir receives the specs reviewed, every report, and the results.
	OutDir string
	// Logf, when set, receives one line per finished review.
	Logf func(format string, args ...any)
}

// Run reviews every case cfg.Runs times, grades the reports, and writes to
// cfg.OutDir:
//
//	specs/CASE.md            the spec each case reviewed
//	reports/CASE_repN.json   every report, for tracing a surprising grade
//	results.jsonl            one graded review per line
//	errors.jsonl             reviews that produced nothing to grade
//	summary.json, summary.md the metrics
func Run(ctx context.Context, cases []Case, review Reviewer, cfg Config) (Summary, error) {
	if cfg.Runs < 1 || cfg.Concurrency < 1 {
		return Summary{}, fmt.Errorf("runs and concurrency must be at least 1")
	}
	specDir := filepath.Join(cfg.OutDir, "specs")
	reportDir := filepath.Join(cfg.OutDir, "reports")
	for _, dir := range []string{specDir, reportDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Summary{}, err
		}
	}
	for _, c := range cases {
		if err := os.WriteFile(filepath.Join(specDir, c.ID+".md"), []byte(c.Spec), 0o644); err != nil {
			return Summary{}, err
		}
	}

	type job struct {
		c   Case
		rep int
	}
	jobs := make(chan job)
	var (
		mu      sync.Mutex
		results []RunResult
		errs    []RunError
		wg      sync.WaitGroup
	)
	for range cfg.Concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				result, runErr := runOne(ctx, j.c, j.rep, review, cfg, specDir, reportDir)
				mu.Lock()
				if runErr != nil {
					errs = append(errs, *runErr)
					logf(cfg, "%s run %d: error: %s", j.c.ID, j.rep, runErr.Error)
				} else {
					results = append(results, result)
					logf(cfg, "%s run %d: %s, %d finding(s), %.1fs", j.c.ID, j.rep, result.Verdict, len(result.Grade.Findings), float64(result.WallMS)/1000)
				}
				mu.Unlock()
			}
		}()
	}
	// Stop handing out reviews once the run is canceled. Reviews already in
	// flight finish or fail on their own, and what was graded is still saved.
dispatch:
	for _, c := range cases {
		for rep := range cfg.Runs {
			// Checked first because select picks at random when a worker is
			// also ready, which would let a canceled run hand out more work.
			if ctx.Err() != nil {
				break dispatch
			}
			select {
			case <-ctx.Done():
				break dispatch
			case jobs <- job{c: c, rep: rep}:
			}
		}
	}
	close(jobs)
	wg.Wait()

	sort.Slice(results, func(i, j int) bool {
		if results[i].Case != results[j].Case {
			return results[i].Case < results[j].Case
		}
		return results[i].Rep < results[j].Rep
	})
	sort.Slice(errs, func(i, j int) bool {
		if errs[i].Case != errs[j].Case {
			return errs[i].Case < errs[j].Case
		}
		return errs[i].Rep < errs[j].Rep
	})

	summary := Summarize(results, errs)
	if err := writeJSONLines(filepath.Join(cfg.OutDir, "results.jsonl"), results); err != nil {
		return summary, err
	}
	if err := writeJSONLines(filepath.Join(cfg.OutDir, "errors.jsonl"), errs); err != nil {
		return summary, err
	}
	summaryJSON, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return summary, err
	}
	if err := os.WriteFile(filepath.Join(cfg.OutDir, "summary.json"), append(summaryJSON, '\n'), 0o644); err != nil {
		return summary, err
	}
	if err := os.WriteFile(filepath.Join(cfg.OutDir, "summary.md"), []byte(summary.Markdown()), 0o644); err != nil {
		return summary, err
	}
	// A canceled run is incomplete. Its partial results are on disk, but the
	// caller must not mistake them for a full run.
	return summary, ctx.Err()
}

func runOne(ctx context.Context, c Case, rep int, review Reviewer, cfg Config, specDir, reportDir string) (RunResult, *RunError) {
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}
	start := time.Now()
	// The review runs on its own goroutine so that the time limit holds even
	// if a reviewer does not watch its context. Such a reviewer is abandoned,
	// not stopped; the channel is buffered so it can still finish and exit.
	type outcome struct {
		report *schema.Report
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		report, err := review(ctx, c, filepath.Join(specDir, c.ID+".md"))
		done <- outcome{report, err}
	}()
	var report *schema.Report
	var err error
	select {
	case o := <-done:
		report, err = o.report, o.err
	case <-ctx.Done():
	}
	wall := time.Since(start).Milliseconds()
	switch {
	case err != nil:
	case ctx.Err() != nil:
		// The time limit is a ceiling on the review, whatever it returned.
		err = fmt.Errorf("review outlived its time limit: %w", ctx.Err())
	case report == nil:
		err = fmt.Errorf("reviewer returned no report")
	}
	if err != nil {
		return RunResult{}, &RunError{Case: c.ID, Rep: rep, Error: err.Error(), WallMS: wall}
	}

	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err == nil {
		err = os.WriteFile(filepath.Join(reportDir, fmt.Sprintf("%s_rep%d.json", c.ID, rep)), append(reportJSON, '\n'), 0o644)
	}
	if err != nil {
		return RunResult{}, &RunError{Case: c.ID, Rep: rep, Error: "saving report: " + err.Error(), WallMS: wall}
	}

	return RunResult{
		Case:            c.ID,
		Base:            c.Base,
		Variant:         c.Variant,
		Rep:             rep,
		Model:           report.Meta.Model,
		Verdict:         report.Summary.Verdict,
		Score:           report.Summary.Score,
		Grade:           GradeReport(c, report, cfg.Tolerance),
		Usage:           report.Meta.Usage,
		DroppedFindings: report.Meta.DroppedFindings,
		WallMS:          wall,
	}, nil
}

func writeJSONLines[T any](path string, rows []T) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, row := range rows {
		if err := enc.Encode(row); err != nil {
			_ = f.Close()
			return err
		}
	}
	return f.Close()
}

func logf(cfg Config, format string, args ...any) {
	if cfg.Logf != nil {
		cfg.Logf(format, args...)
	}
}
