// Command speccritic-eval reviews a corpus of specifications with known,
// seeded defects and reports how well the reviews did.
//
// Without --run it only prints what it would do. With --run it calls the
// configured LLM once per case per run, which costs real money.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/dshills/speccritic/internal/eval"
	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/pkg/speccritic"
)

type options struct {
	corpus      string
	out         string
	runs        int
	concurrency int
	tolerance   int
	timeout     time.Duration
	isolated    bool
	cases       string
	run         bool

	provider      string
	model         string
	effort        string
	maxTokens     int
	chunking      string
	chunkLines    int
	chunkMinLines int
}

func main() {
	var o options
	flag.StringVar(&o.corpus, "corpus", "eval/corpus", "Directory holding the specs and their defect labels")
	flag.StringVar(&o.out, "out", "", "Directory for results (default eval/results/<UTC time>)")
	flag.IntVar(&o.runs, "runs", 3, "Reviews per case; verdict agreement needs at least 2")
	flag.IntVar(&o.concurrency, "concurrency", 2, "Reviews running at once")
	flag.IntVar(&o.tolerance, "line-tolerance", 2, "Lines a finding may be off a defect's evidence and still count")
	flag.DurationVar(&o.timeout, "timeout", 10*time.Minute, "Time limit for one review")
	flag.BoolVar(&o.isolated, "isolated", false, "Also review one spec per single defect; shows which defect is missed, at several times the cost")
	flag.StringVar(&o.cases, "cases", "", "Only run cases whose ID contains one of these comma-separated fragments")
	flag.BoolVar(&o.run, "run", false, "Call the LLM. Without this flag the plan is printed and nothing is spent")

	flag.StringVar(&o.provider, "llm-provider", "", "LLM provider (default: SPECCRITIC_LLM_PROVIDER, then the built-in default)")
	flag.StringVar(&o.model, "llm-model", "", "LLM model (default: SPECCRITIC_LLM_MODEL, then the provider's default)")
	flag.StringVar(&o.effort, "effort", "", "Reasoning effort passed to the model (default: the provider's)")
	flag.IntVar(&o.maxTokens, "max-tokens", 0, "Response cap per call (default: the CLI's default)")
	flag.StringVar(&o.chunking, "chunking", "", "Chunking mode: auto, on or off (default: the CLI's default)")
	flag.IntVar(&o.chunkLines, "chunk-lines", 0, "Target lines per chunk (default: the CLI's default)")
	flag.IntVar(&o.chunkMinLines, "chunk-min-lines", 0, "Line count at which auto chunking starts (default: the CLI's default)")
	flag.Parse()

	if err := execute(o); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func execute(o options) error {
	bases, err := eval.LoadCorpus(o.corpus)
	if err != nil {
		return err
	}
	cases, err := eval.BuildCases(bases, o.isolated)
	if err != nil {
		return err
	}
	cases = filterCases(cases, o.cases)
	if len(cases) == 0 {
		return fmt.Errorf("no cases match --cases %q", o.cases)
	}

	checkOptions := o.checkOptions()
	reviews := len(cases) * o.runs
	fmt.Printf("Corpus: %s (%d specs)\n", o.corpus, len(bases))
	fmt.Printf("Model:  %s\n", describeModel(checkOptions))
	fmt.Printf("Plan:   %d case(s) x %d run(s) = %d review(s), %d at a time\n", len(cases), o.runs, reviews, o.concurrency)
	for _, c := range cases {
		fmt.Printf("  %-52s %d seeded defect(s)\n", c.ID, len(c.Defects))
	}
	if !o.run {
		fmt.Printf("\nNothing was run. Add --run (or use `make eval`) to call the LLM %d time(s) or more; that spends real money.\n", reviews)
		return nil
	}

	out := o.out
	if out == "" {
		out = filepath.Join("eval", "results", time.Now().UTC().Format("20060102T150405Z"))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	summary, err := eval.Run(ctx, cases, reviewer(checkOptions), eval.Config{
		Runs:        o.runs,
		Concurrency: o.concurrency,
		Tolerance:   o.tolerance,
		Timeout:     o.timeout,
		OutDir:      out,
		Logf:        func(format string, args ...any) { fmt.Printf("  "+format+"\n", args...) },
	})
	if err != nil {
		return err
	}
	fmt.Printf("\nDefects found: %s. Clean runs with a CRITICAL: %s. Verdict as expected: %s.\n", summary.Recall, summary.FalseCritical, summary.VerdictExpected)
	fmt.Printf("Results: %s\n", filepath.Join(out, "summary.md"))
	if summary.Errors > 0 {
		return fmt.Errorf("%d review(s) produced nothing to grade; see %s", summary.Errors, filepath.Join(out, "errors.jsonl"))
	}
	return nil
}

// checkOptions starts from the same defaults the CLI uses, so the eval
// measures the tool as it ships, and applies only the overrides given.
func (o options) checkOptions() speccritic.CheckOptions {
	opts := speccritic.DefaultCheckOptions()
	opts.Version = "eval"
	opts.LLMProvider = o.provider
	opts.LLMModel = o.model
	opts.Effort = o.effort
	if o.maxTokens > 0 {
		opts.MaxTokens = o.maxTokens
	}
	if o.chunking != "" {
		opts.Chunking = o.chunking
	}
	if o.chunkLines > 0 {
		opts.ChunkLines = o.chunkLines
	}
	if o.chunkMinLines > 0 {
		opts.ChunkMinLines = o.chunkMinLines
	}
	return opts
}

// reviewer runs a case through the same entry point library callers use.
func reviewer(base speccritic.CheckOptions) eval.Reviewer {
	return func(ctx context.Context, c eval.Case, specPath string) (*schema.Report, error) {
		opts := base
		opts.SpecPath = specPath
		opts.Profile = c.Profile
		result, err := speccritic.Check(ctx, opts)
		if err != nil {
			return nil, err
		}
		return result.Report, nil
	}
}

func filterCases(cases []eval.Case, fragments string) []eval.Case {
	if strings.TrimSpace(fragments) == "" {
		return cases
	}
	var kept []eval.Case
	for _, c := range cases {
		for _, fragment := range strings.Split(fragments, ",") {
			if fragment = strings.TrimSpace(fragment); fragment != "" && strings.Contains(c.ID, fragment) {
				kept = append(kept, c)
				break
			}
		}
	}
	return kept
}

// describeModel says which model a run will use, following the same order of
// precedence as the checker: flags, then environment, then defaults.
func describeModel(opts speccritic.CheckOptions) string {
	provider, model := opts.LLMProvider, opts.LLMModel
	if provider == "" {
		provider = os.Getenv("SPECCRITIC_LLM_PROVIDER")
	}
	if model == "" {
		model = os.Getenv("SPECCRITIC_LLM_MODEL")
	}
	if provider == "" {
		provider = speccritic.ProviderForModel(model)
	}
	if provider == "" {
		provider = "anthropic"
	}
	if model == "" {
		model = speccritic.DefaultModelForProvider(provider)
	}
	description := provider + ":" + model
	if opts.Effort != "" {
		description += ", effort " + opts.Effort
	}
	return description
}
