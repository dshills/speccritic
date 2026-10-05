package eval

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dshills/speccritic/internal/schema"
)

func runnerCases(t *testing.T) []Case {
	t.Helper()
	bases, err := LoadCorpus(corpusDir)
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	cases, err := BuildCases(bases[:1], false)
	if err != nil {
		t.Fatalf("BuildCases: %v", err)
	}
	return cases
}

// oracle answers every case with a perfect review: each seeded defect cited
// once, at its evidence, with its primary category and labeled severity.
func oracle(_ context.Context, c Case, _ string) (*schema.Report, error) {
	report := &schema.Report{Meta: schema.Meta{Model: "fake:oracle", Usage: &schema.UsageMeta{Calls: 1, InputTokens: 10, OutputTokens: 5}}}
	report.Summary.Verdict = schema.VerdictValid
	for i, d := range c.Defects {
		report.Issues = append(report.Issues, schema.Issue{
			ID: fmt.Sprintf("ISSUE-%04d", i+1), Severity: d.Severity, Category: d.Categories[0], Title: d.Note,
			Evidence: []schema.Evidence{{LineStart: d.Ranges[0].Start, LineEnd: d.Ranges[0].End}},
		})
		report.Summary.Verdict = schema.VerdictInvalid
	}
	return report, nil
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	n := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			n++
		}
	}
	return n
}

// The whole pipeline, fed perfect reviews, must report perfect scores. If it
// does not, the harness is miscounting.
func TestRun_OracleScoresPerfectly(t *testing.T) {
	cases := runnerCases(t)
	out := t.TempDir()
	summary, err := Run(context.Background(), cases, oracle, Config{Runs: 2, Concurrency: 3, OutDir: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	defects := len(cases[1].Defects)
	if summary.Runs != 4 || summary.Errors != 0 {
		t.Fatalf("runs=%d errors=%d, want 4 and 0", summary.Runs, summary.Errors)
	}
	for name, p := range map[string]Proportion{
		"recall": summary.Recall, "model recall": summary.ModelRecall, "category agreement": summary.CategoryAgreement,
		"severity agreement": summary.SeverityAgreement, "precision": summary.Precision,
	} {
		if p.Hits != 2*defects || p.Total != 2*defects {
			t.Errorf("%s = %s, want %d/%d", name, p, 2*defects, 2*defects)
		}
	}
	if summary.FalseCritical.Hits != 0 || summary.FalseCritical.Total != 2 {
		t.Errorf("false critical = %s, want 0/2", summary.FalseCritical)
	}
	if summary.VerdictExpected.Hits != 4 || summary.VerdictAgreement != 1 {
		t.Errorf("verdict expected = %s agreement = %v, want 4/4 and 1", summary.VerdictExpected, summary.VerdictAgreement)
	}
	if summary.Usage.Calls != 4 || summary.Usage.InputTokens != 40 {
		t.Errorf("usage = %+v", summary.Usage)
	}

	if got := countLines(t, filepath.Join(out, "results.jsonl")); got != 4 {
		t.Errorf("results.jsonl has %d rows, want 4", got)
	}
	if got := countLines(t, filepath.Join(out, "errors.jsonl")); got != 0 {
		t.Errorf("errors.jsonl has %d rows, want 0", got)
	}
	for _, name := range []string{"summary.json", "summary.md", "specs/" + cases[0].ID + ".md", "specs/" + cases[1].ID + ".md", "reports/" + cases[1].ID + "_rep1.json"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("missing output %s: %v", name, err)
		}
	}
	var saved Summary
	raw, err := os.ReadFile(filepath.Join(out, "summary.json"))
	if err != nil || json.Unmarshal(raw, &saved) != nil || saved.Recall != summary.Recall {
		t.Errorf("summary.json does not round-trip the summary (err=%v)", err)
	}
}

// Reviews that say nothing must score nothing: the opposite end from the
// oracle.
func TestRun_EmptyReviewsScoreNothing(t *testing.T) {
	silent := func(context.Context, Case, string) (*schema.Report, error) {
		report := &schema.Report{}
		report.Summary.Verdict = schema.VerdictValid
		return report, nil
	}
	summary, err := Run(context.Background(), runnerCases(t), silent, Config{Runs: 1, Concurrency: 1, OutDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Recall.Hits != 0 || summary.Recall.Total == 0 || summary.CriticalRecall.Hits != 0 {
		t.Errorf("recall = %s critical = %s, want nothing found", summary.Recall, summary.CriticalRecall)
	}
	// The seeded spec was passed as VALID, which is the wrong verdict for it.
	if summary.VerdictExpected.Hits != 1 || summary.VerdictExpected.Total != 2 {
		t.Errorf("verdict expected = %s, want 1/2", summary.VerdictExpected)
	}
}

// A review that fails is an error row. It must never be graded as a review
// that found nothing.
func TestRun_FailedReviewsAreErrorsNotZeroes(t *testing.T) {
	cases := runnerCases(t)
	var calls atomic.Int32
	flaky := func(ctx context.Context, c Case, path string) (*schema.Report, error) {
		if calls.Add(1)%2 == 0 {
			return nil, errors.New("provider down")
		}
		return oracle(ctx, c, path)
	}
	out := t.TempDir()
	summary, err := Run(context.Background(), cases, flaky, Config{Runs: 2, Concurrency: 1, OutDir: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Runs != 2 || summary.Errors != 2 {
		t.Fatalf("runs=%d errors=%d, want 2 and 2", summary.Runs, summary.Errors)
	}
	if summary.Recall.Hits != summary.Recall.Total {
		t.Errorf("recall = %s, want the failed reviews left out rather than counted as misses", summary.Recall)
	}
	if results, errs := countLines(t, filepath.Join(out, "results.jsonl")), countLines(t, filepath.Join(out, "errors.jsonl")); results != 2 || errs != 2 {
		t.Errorf("results.jsonl=%d errors.jsonl=%d rows, want 2 and 2", results, errs)
	}
}

func TestRun_TimeoutIsAnError(t *testing.T) {
	stuck := func(ctx context.Context, _ Case, _ string) (*schema.Report, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	summary, err := Run(context.Background(), runnerCases(t)[:1], stuck, Config{Runs: 1, Concurrency: 1, Timeout: 20 * time.Millisecond, OutDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Runs != 0 || summary.Errors != 1 {
		t.Fatalf("runs=%d errors=%d, want 0 and 1", summary.Runs, summary.Errors)
	}
}

func TestRun_RejectsBadConfig(t *testing.T) {
	for _, cfg := range []Config{{Runs: 0, Concurrency: 1}, {Runs: 1, Concurrency: 0}} {
		cfg.OutDir = t.TempDir()
		if _, err := Run(context.Background(), runnerCases(t), oracle, cfg); err == nil {
			t.Errorf("config %+v should be rejected", cfg)
		}
	}
}

// The time limit is a ceiling: a reviewer that ignores it and hands back a
// report afterwards has still failed.
func TestRun_ReportReturnedAfterTheDeadlineIsAnError(t *testing.T) {
	late := func(ctx context.Context, c Case, path string) (*schema.Report, error) {
		<-ctx.Done()
		return oracle(context.Background(), c, path)
	}
	summary, err := Run(context.Background(), runnerCases(t)[:1], late, Config{Runs: 1, Concurrency: 1, Timeout: 20 * time.Millisecond, OutDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Runs != 0 || summary.Errors != 1 {
		t.Fatalf("runs=%d errors=%d, want the late report recorded as an error", summary.Runs, summary.Errors)
	}
}

func TestRun_CancelStopsDispatchAndKeepsWhatWasGraded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	cancelAfterFirst := func(ctx context.Context, c Case, path string) (*schema.Report, error) {
		if calls.Add(1) == 1 {
			defer cancel()
			return oracle(context.Background(), c, path)
		}
		return nil, ctx.Err()
	}
	out := t.TempDir()
	summary, err := Run(ctx, runnerCases(t), cancelAfterFirst, Config{Runs: 5, Concurrency: 1, OutDir: out})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled so a partial run is not taken for a full one", err)
	}
	if got := int(calls.Load()); got > 2 {
		t.Errorf("reviewer was called %d times after the cancel, want dispatch to stop", got)
	}
	if summary.Runs+summary.Errors >= 10 {
		t.Errorf("runs=%d errors=%d, want fewer than the 10 planned", summary.Runs, summary.Errors)
	}
	if got := countLines(t, filepath.Join(out, "results.jsonl")); got != summary.Runs {
		t.Errorf("results.jsonl has %d rows, want the %d graded before the cancel", got, summary.Runs)
	}
}

// A reviewer that never looks at its context must not hold the run past the
// time limit.
func TestRun_TimeoutBoundsAReviewerThatIgnoresItsContext(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	deaf := func(context.Context, Case, string) (*schema.Report, error) {
		<-release
		return nil, errors.New("released")
	}
	start := time.Now()
	summary, err := Run(context.Background(), runnerCases(t)[:1], deaf, Config{Runs: 1, Concurrency: 1, Timeout: 30 * time.Millisecond, OutDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Run took %s, want it bounded by the 30ms time limit", elapsed)
	}
	if summary.Runs != 0 || summary.Errors != 1 {
		t.Fatalf("runs=%d errors=%d, want 0 and 1", summary.Runs, summary.Errors)
	}
}

func TestRun_AlreadyCanceledRunsNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	counting := func(ctx context.Context, c Case, path string) (*schema.Report, error) {
		calls.Add(1)
		return oracle(ctx, c, path)
	}
	if _, err := Run(ctx, runnerCases(t), counting, Config{Runs: 3, Concurrency: 2, OutDir: t.TempDir()}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("reviewer was called %d times on a run canceled before it began", got)
	}
}
