package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/dshills/speccritic/internal/chunk"
	"github.com/dshills/speccritic/internal/completion"
	ctxpkg "github.com/dshills/speccritic/internal/context"
	"github.com/dshills/speccritic/internal/convergence"
	"github.com/dshills/speccritic/internal/incremental"
	"github.com/dshills/speccritic/internal/llm"
	"github.com/dshills/speccritic/internal/patch"
	"github.com/dshills/speccritic/internal/preflight"
	"github.com/dshills/speccritic/internal/profile"
	"github.com/dshills/speccritic/internal/redact"
	"github.com/dshills/speccritic/internal/review"
	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/schema/validate"
	"github.com/dshills/speccritic/internal/spec"
	"github.com/dshills/speccritic/internal/verify"
)

type ErrorKind int

const (
	ErrorInput ErrorKind = iota
	ErrorProvider
	ErrorModelOutput
)

type Error struct {
	Kind ErrorKind
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }

func (e *Error) Unwrap() error { return e.Err }

type Source string

const (
	SourceCLI Source = "cli"
	SourceWeb Source = "web"
)

const (
	maxPreflightPromptFindings = 20
)

// maxSharedRequestTokens bounds, in estimated tokens, a chunk call that sends
// the whole spec: the shared prefix (context files and spec) plus the output
// allowance. It leaves a margin for the system prompt and task within a
// 128,000-token context window, the smallest among the default models. Above
// it, each chunk call sees only its own lines.
const maxSharedRequestTokens = 110000

// longCacheContextTokens is the estimated size of the context files above
// which the cached prefix is kept for an hour. Writing the long cache costs
// more, so it is worth it only when a large, unchanging context would
// otherwise be billed in full on every run more than a few minutes apart.
const longCacheContextTokens = 8000

// useLongCache reports whether this run's cached prefix should live for an
// hour rather than a few minutes.
func useLongCache(contextFiles []ctxpkg.ContextFile) bool {
	tokens := 0
	for _, f := range contextFiles {
		tokens += chunk.EstimateTokens(f.Content)
	}
	return tokens >= longCacheContextTokens
}

// Values of CheckRequest.Verify.
const (
	VerifyAuto = "auto"
	VerifyOff  = "off"
)

// Values of CheckRequest.StructuredOutput.
const (
	StructuredOutputAuto = "auto"
	StructuredOutputOff  = "off"
)

// enforceSchema reports whether providers are asked to constrain responses to
// the review schema.
func enforceSchema(req CheckRequest) bool {
	return req.StructuredOutput != StructuredOutputOff
}

type ContextDocument struct {
	Name string
	Text string
}

type CheckRequest struct {
	Version           string
	SpecPath          string
	SpecName          string
	SpecText          string
	ContextPaths      []string
	ContextDocuments  []ContextDocument
	Profile           string
	Strict            bool
	SeverityThreshold string
	LLMProvider       string
	LLMModel          string
	MaxTokens         int
	// Effort asks the model for more or less reasoning. Empty leaves the
	// provider's default in place.
	Effort string
	// Verify is "auto" (the default when empty) to give every CRITICAL
	// finding the model produced a second look before it decides the
	// verdict, or "off" to skip it.
	Verify string
	// StructuredOutput is "auto" (the default when empty) to have the
	// provider constrain responses to the review schema where the model
	// allows it, or "off" to describe the schema in the prompt only.
	StructuredOutput                string
	Offline                         bool
	Debug                           bool
	Verbose                         bool
	Preflight                       bool
	PreflightMode                   string
	PreflightProfile                string
	PreflightIgnore                 []string
	Chunking                        string
	ChunkLines                      int
	ChunkOverlap                    int
	ChunkMinLines                   int
	ChunkTokenThreshold             int
	ChunkConcurrency                int
	SynthesisLineThreshold          int
	IncrementalFrom                 string
	IncrementalFromText             string
	IncrementalBasePath             string
	IncrementalBaseText             string
	IncrementalMode                 string
	IncrementalMaxChangeRatio       float64
	IncrementalMaxRemapFailureRatio float64
	IncrementalContextLines         int
	IncrementalStrictReuse          bool
	IncrementalReport               bool
	ConvergenceFrom                 string
	ConvergenceFromText             string
	ConvergenceMode                 string
	ConvergenceStrict               bool
	ConvergenceReport               bool
	CompletionSuggestions           bool
	CompletionMode                  string
	CompletionTemplate              string
	CompletionMaxPatches            int
	CompletionOpenDecisions         bool
	Source                          Source
	ErrWriter                       io.Writer
}

type CheckResult struct {
	Report       *schema.Report
	PatchDiff    string
	OriginalSpec string
	LineCount    int
	Model        string
}

type ProviderFactory func(providerModel string) (llm.Provider, error)

type Checker struct {
	NewProvider ProviderFactory
}

func NewChecker() *Checker {
	return &Checker{NewProvider: llm.NewProvider}
}

func (c *Checker) Check(ctx context.Context, req CheckRequest) (*CheckResult, error) {
	if err := validateRequest(req); err != nil {
		return nil, appError(ErrorInput, err)
	}
	errw := req.ErrWriter
	if errw == nil {
		errw = io.Discard
	}

	logVerbose(errw, req.Verbose, "Loading spec: %s", specLabel(req))
	s, err := loadSpec(req)
	if err != nil {
		return nil, appError(ErrorInput, fmt.Errorf("loading spec: %w", err))
	}
	originalRaw := s.Raw

	s.Numbered = redact.Redact(s.Numbered)
	s.Raw = redact.Redact(s.Raw)
	redactedSpec := s.Raw != originalRaw

	preflightIssues, preflightOnly, err := runPreflight(s, req, errw)
	if err != nil {
		return nil, appError(ErrorInput, err)
	}
	if preflightOnly {
		report := buildReport(req, s, preflightIssues, nil, nil, "preflight")
		if err := c.applyConvergence(req, report, convergence.CoveragePreflightOnly, errw); err != nil {
			return nil, appError(ErrorInput, err)
		}
		if err := c.applyCompletion(req, s, report); err != nil {
			return nil, appError(ErrorInput, err)
		}
		patchDiff := patchDiffForReport(originalRaw, report, redactedSpec, errw)
		return &CheckResult{
			Report:       report,
			PatchDiff:    patchDiff,
			OriginalSpec: originalRaw,
			LineCount:    s.LineCount,
			Model:        "preflight",
		}, nil
	}

	llmProvider, llmModel, err := resolveModel(req, errw)
	if err != nil {
		return nil, appError(ErrorInput, err)
	}
	modelStr := llmProvider + ":" + llmModel

	logVerbose(errw, req.Verbose, "Loading %d context file(s)", len(req.ContextPaths)+len(req.ContextDocuments))
	contextFiles, err := loadContext(req)
	if err != nil {
		return nil, appError(ErrorInput, fmt.Errorf("loading context files: %w", err))
	}

	logVerbose(errw, req.Verbose, "Loading profile: %s", req.Profile)
	prof, err := profile.Get(req.Profile)
	if err != nil {
		return nil, appError(ErrorInput, fmt.Errorf("loading profile: %w", err))
	}

	sysPrompt := llm.BuildSystemPrompt(prof, req.Strict)
	preflightContext, knownPreflightIDs := buildPreflightPromptContext(preflightIssues)
	// Every call in this run starts with the same system prompt and this
	// prefix, so after the first call the spec is read from the prompt cache.
	specPrefix := llm.BuildSpecPrefix(s, contextFiles, preflightContext)
	longCache := useLongCache(contextFiles)

	llmReq := &llm.Request{
		SystemPrompt:           sysPrompt,
		UserPromptCachedPrefix: specPrefix,
		UserPrompt:             llm.ReviewTask,
		LongCache:              longCache,
		MaxTokens:              req.MaxTokens,
		Effort:                 req.Effort,
		Schema:                 llm.ReviewSchema(enforceSchema(req)),
	}

	if req.Debug {
		_, _ = fmt.Fprintf(errw, "=== DEBUG: redacted prompt ===\n")
		_, _ = fmt.Fprintf(errw, "[SYSTEM]\n%s\n\n[USER PREFIX]\n%s\n[USER TASK]\n%s\n", sysPrompt, specPrefix, llm.ReviewTask)
		_, _ = fmt.Fprintf(errw, "=== END DEBUG ===\n")
	}

	newProvider := c.NewProvider
	if newProvider == nil {
		newProvider = llm.NewProvider
	}
	rawProvider, err := newProvider(modelStr)
	if err != nil {
		return nil, appError(ErrorProvider, fmt.Errorf("creating LLM provider: %w", err))
	}
	// Every LLM call below goes through the meter, so the report and the
	// verbose log can say what the review cost, whether or not it succeeded.
	meter := llm.NewMeter(rawProvider)
	var provider llm.Provider = meter
	defer func() { logUsage(errw, req.Verbose, meter.Totals()) }()

	if (req.IncrementalFrom != "" || req.IncrementalFromText != "" || req.IncrementalMode == "on") && req.IncrementalMode != "off" {
		result, handled, err := c.checkIncremental(ctx, provider, req, s, originalRaw, preflightIssues, sysPrompt, errw)
		if err != nil {
			var appErr *Error
			if errors.As(err, &appErr) {
				return nil, err
			}
			return nil, appError(ErrorInput, err)
		}
		if handled {
			c.verifyCriticals(ctx, provider, req, s, result.Report, sysPrompt, specPrefix, longCache, errw)
			if err := c.applyConvergence(req, result.Report, convergence.CoverageIncremental, errw); err != nil {
				return nil, appError(ErrorInput, err)
			}
			if err := c.applyCompletion(req, s, result.Report); err != nil {
				return nil, appError(ErrorInput, err)
			}
			result.PatchDiff = patchDiffForReport(originalRaw, result.Report, redactedSpec, errw)
			applyRunMeta(result.Report, req, meter.Totals())
			return result, nil
		}
		logVerbose(errw, req.Verbose, "Incremental review fell back to full review")
	}

	logVerbose(errw, req.Verbose, "Calling LLM: %s", modelStr)
	chunkCfg := chunkConfigFromRequest(req)
	estimatedSpecTokens := chunk.EstimateTokens(s.Numbered)
	if chunk.ShouldChunk(s.LineCount, estimatedSpecTokens, chunkCfg) {
		logVerbose(errw, req.Verbose, "Using chunked review: %d lines, estimated spec tokens %d", s.LineCount, estimatedSpecTokens)
		report, responseModel, err := c.checkChunked(ctx, provider, req, s, contextFiles, preflightIssues, sysPrompt, specPrefix, preflightContext, longCache, chunkCfg, errw)
		if err != nil {
			return nil, appError(ErrorModelOutput, err)
		}
		c.verifyCriticals(ctx, provider, req, s, report, sysPrompt, specPrefix, longCache, errw)
		if err := c.applyConvergence(req, report, convergence.CoverageFull, errw); err != nil {
			return nil, appError(ErrorInput, err)
		}
		if err := c.applyCompletion(req, s, report); err != nil {
			return nil, appError(ErrorInput, err)
		}
		patchDiff := patchDiffForReport(originalRaw, report, redactedSpec, errw)
		applyRunMeta(report, req, meter.Totals())
		return &CheckResult{
			Report:       report,
			PatchDiff:    patchDiff,
			OriginalSpec: originalRaw,
			LineCount:    s.LineCount,
			Model:        responseModel,
		}, nil
	}

	report, responseModel, err := callWithRetry(ctx, provider, llmReq, s, req.Verbose, errw)
	if err != nil {
		return nil, appError(ErrorModelOutput, err)
	}
	dropped := report.Meta.DroppedFindings
	report.Issues = mergeIssues(preflightIssues, report.Issues, knownPreflightIDs)
	report.Patches = safeReportPatches(s.Raw, report.Issues, report.Patches)

	report = buildReport(req, s, report.Issues, report.Questions, report.Patches, responseModel)
	report.Meta.DroppedFindings = dropped
	c.verifyCriticals(ctx, provider, req, s, report, sysPrompt, specPrefix, longCache, errw)
	if err := c.applyConvergence(req, report, convergence.CoverageFull, errw); err != nil {
		return nil, appError(ErrorInput, err)
	}
	if err := c.applyCompletion(req, s, report); err != nil {
		return nil, appError(ErrorInput, err)
	}
	patchDiff := patchDiffForReport(originalRaw, report, redactedSpec, errw)
	applyRunMeta(report, req, meter.Totals())

	return &CheckResult{
		Report:       report,
		PatchDiff:    patchDiff,
		OriginalSpec: originalRaw,
		LineCount:    s.LineCount,
		Model:        responseModel,
	}, nil
}

// verifyCriticals gives the CRITICAL findings in report a second look and
// recomputes the summary from the result. It does nothing when verification
// is off, when no finding needs it, or when the spec is too large to send
// whole in one more call.
func (c *Checker) verifyCriticals(ctx context.Context, provider llm.Provider, req CheckRequest, s *spec.Spec, report *schema.Report, sysPrompt, specPrefix string, longCache bool, errw io.Writer) {
	if req.Verify == VerifyOff || !verify.Candidates(report) {
		return
	}
	if chunk.EstimateTokens(specPrefix)+req.MaxTokens > maxSharedRequestTokens {
		logVerbose(errw, req.Verbose, "Spec is too large to verify CRITICAL findings in one call; they stand unchecked")
		return
	}
	verify.Run(ctx, report, verify.Input{
		Provider:      provider,
		SystemPrompt:  sysPrompt,
		Prefix:        specPrefix,
		LongCache:     longCache,
		MaxTokens:     req.MaxTokens,
		Effort:        req.Effort,
		EnforceSchema: enforceSchema(req),
		Strict:        req.Strict,
		SpecText:      s.Raw,
		SpecPath:      schema.EvidencePath(s.Path),
		Logf:          func(format string, args ...any) { logVerbose(errw, req.Verbose, format, args...) },
	})
	refreshSummary(report)
}

// refreshSummary recomputes the score, verdict and counts after findings
// changed.
func refreshSummary(report *schema.Report) {
	critical, warn, info := review.Counts(report.Issues)
	report.Summary = schema.Summary{
		Verdict:       review.Verdict(report.Issues, report.Questions),
		Score:         review.Score(report.Issues, report.Questions),
		CriticalCount: critical,
		WarnCount:     warn,
		InfoCount:     info,
	}
}

// applyRunMeta records how the LLM was called: what the calls used and the
// effort that was asked for.
func applyRunMeta(report *schema.Report, req CheckRequest, totals llm.Totals) {
	report.Meta.Usage = usageMeta(totals)
	report.Meta.Effort = req.Effort
}

// usageMeta converts call totals to their report form. It returns nil when no
// call was made, so a review served entirely from reuse reports no usage.
func usageMeta(t llm.Totals) *schema.UsageMeta {
	if t.Calls == 0 {
		return nil
	}
	return &schema.UsageMeta{
		Calls:               t.Calls,
		RepairCalls:         t.RepairCalls,
		ContinuationCalls:   t.ContinuationCalls,
		TruncatedResponses:  t.TruncatedResponses,
		SchemaEnforcedCalls: t.SchemaEnforcedCalls,
		RetriedRequests:     t.RetriedRequests,
		InputTokens:         t.InputTokens,
		OutputTokens:        t.OutputTokens,
		CacheReadTokens:     t.CacheReadTokens,
		CacheWriteTokens:    t.CacheWriteTokens,
		CallDurationMS:      t.CallDuration.Milliseconds(),
		WallDurationMS:      t.WallDuration.Milliseconds(),
	}
}

// logUsage prints one line summarizing the LLM calls a review made.
func logUsage(w io.Writer, verbose bool, t llm.Totals) {
	if t.Calls == 0 {
		return
	}
	logVerbose(w, verbose,
		"LLM usage: %d call(s) (%d repair, %d continuation, %d truncated, %d schema-enforced, %d request(s) resent); tokens: %d input, %d cache read, %d cache write, %d output; %s in calls, %s elapsed",
		t.Calls, t.RepairCalls, t.ContinuationCalls, t.TruncatedResponses, t.SchemaEnforcedCalls, t.RetriedRequests,
		t.InputTokens, t.CacheReadTokens, t.CacheWriteTokens, t.OutputTokens,
		t.CallDuration.Round(time.Millisecond), t.WallDuration.Round(time.Millisecond),
	)
}

func (c *Checker) applyConvergence(req CheckRequest, report *schema.Report, coverage convergence.ReviewCoverage, errw io.Writer) error {
	cfg := convergenceConfigFromRequest(req, coverage)
	if cfg.Mode == convergence.ModeOff {
		return nil
	}
	if req.ConvergenceFrom == "" && req.ConvergenceFromText == "" {
		if cfg.Mode == convergence.ModeOn {
			return fmt.Errorf("convergence source is required when convergence mode is enabled")
		}
		return nil
	}
	if !cfg.Report {
		return nil
	}
	var prev *convergence.PreviousReport
	var loadErr error
	if req.ConvergenceFromText != "" {
		prev, loadErr = convergence.ParsePreviousReport([]byte(req.ConvergenceFromText))
	} else {
		prev, loadErr = convergence.LoadPreviousReport(req.ConvergenceFrom)
	}
	if loadErr != nil {
		if cfg.Mode == convergence.ModeOn {
			return fmt.Errorf("loading previous convergence report: %w", loadErr)
		}
		result := convergence.CompareReports(nil, report, cfg, convergence.Compatibility{
			Status: convergence.StatusUnavailable,
			Notes:  []string{fmt.Sprintf("previous convergence report unavailable: %s", loadErr)},
			Err:    loadErr,
		})
		report.Meta.Convergence = convergence.ToSchemaMeta(result, cfg.Mode, "", report.Input.SpecHash)
		logVerbose(errw, req.Verbose, "Convergence unavailable: %s", loadErr)
		return nil
	}
	compat := convergence.CheckCompatibility(prev, cfg)
	if compat.Err != nil && cfg.Mode == convergence.ModeOn {
		return fmt.Errorf("previous convergence report incompatible: %w", compat.Err)
	}
	result := convergence.CompareReports(prev.Report, report, cfg, compat)
	report.Meta.Convergence = convergence.ToSchemaMeta(result, cfg.Mode, prev.Report.Input.SpecHash, report.Input.SpecHash)
	logVerbose(errw, req.Verbose, "Convergence: %d new, %d still open, %d resolved", result.Summary.Current.New, result.Summary.Current.StillOpen, result.Summary.Previous.Resolved)
	return nil
}

func (c *Checker) applyCompletion(req CheckRequest, s *spec.Spec, report *schema.Report) error {
	cfg := completionConfigFromRequest(req)
	if cfg.Mode == completion.ModeOff || (cfg.Mode == completion.ModeAuto && !cfg.Suggestions) {
		return nil
	}
	tmpl, err := completion.GetTemplate(cfg.Template, req.Profile)
	if err != nil {
		return err
	}
	candidates := completion.GenerateCandidates(completion.Input{
		SpecText:  s.Raw,
		Profile:   req.Profile,
		Template:  tmpl,
		Issues:    report.Issues,
		Questions: report.Questions,
		Config:    cfg,
	})
	result, issues, err := completion.BuildResult(completion.BuildInput{
		OriginalSpec:    s.Raw,
		Candidates:      candidates,
		ExistingPatches: report.Patches,
		Issues:          report.Issues,
		Config:          cfg,
		Template:        tmpl.Name,
	})
	if err != nil {
		return err
	}
	report.Issues = issues
	report.Patches = result.Patches
	meta := report.Meta
	meta.Completion = &result.Meta
	report.Meta = meta
	return nil
}

func (c *Checker) checkIncremental(ctx context.Context, provider llm.Provider, req CheckRequest, s *spec.Spec, originalRaw string, preflightIssues []schema.Issue, sysPrompt string, errw io.Writer) (*CheckResult, bool, error) {
	cfg := incrementalConfigFromRequest(req)
	if cfg.Mode == incremental.ModeOff {
		return nil, false, nil
	}
	if req.IncrementalFrom == "" && req.IncrementalFromText == "" {
		return nil, false, fmt.Errorf("incremental source is required when incremental mode is enabled")
	}
	prev, err := loadPreviousIncrementalReport(req)
	if err != nil {
		return nil, false, fmt.Errorf("loading previous incremental report: %w", err)
	}
	if err := incremental.ValidatePreviousCompatibility(prev, cfg); err != nil {
		if cfg.Mode == incremental.ModeAuto {
			logVerbose(errw, req.Verbose, "Incremental compatibility failed, falling back: %s", err)
			return nil, false, nil
		}
		return nil, false, err
	}
	previousRaw, ok, err := loadIncrementalBase(req, s, prev)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		if cfg.Mode == incremental.ModeAuto {
			logVerbose(errw, req.Verbose, "Incremental base spec unavailable, falling back")
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("--incremental-base is required when previous report hash differs from the current spec hash")
	}
	previousRedacted := redact.Redact(previousRaw)
	currentRedacted := redact.Redact(s.Raw)
	plan, err := incremental.PlanChanges(previousRedacted, currentRedacted, cfg)
	if err != nil {
		return nil, false, err
	}
	reuse, err := incremental.ReuseFindings(incremental.ReuseInput{
		Plan:            plan,
		Previous:        prev.Report,
		SpecPath:        s.Path,
		CurrentRaw:      s.Raw,
		CurrentRedacted: currentRedacted,
		Config:          cfg,
		PreflightIssues: preflightIssues,
	})
	if err != nil {
		return nil, false, err
	}
	if reuse.Fallback != nil {
		if cfg.Mode == incremental.ModeAuto {
			logVerbose(errw, req.Verbose, "Incremental reuse unsafe, falling back: %s", reuse.Fallback.Message)
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("%s", reuse.Fallback.Message)
	}
	var rangeResults []incremental.RangeResult
	model := "incremental-reuse"
	if len(plan.ReviewRanges) > 0 {
		logVerbose(errw, req.Verbose, "Incremental review: %d range(s), %d reused issue(s)", len(plan.ReviewRanges), len(reuse.Issues))
		rangeResults, err = incremental.ReviewRanges(ctx, provider, s, plan, incremental.ExecutorConfig{
			SystemPrompt:  sysPrompt,
			MaxTokens:     req.MaxTokens,
			Effort:        req.Effort,
			EnforceSchema: enforceSchema(req),
			Concurrency:   req.ChunkConcurrency,
			Issues:        reuse.Issues,
			Questions:     reuse.Questions,
		})
		if err != nil {
			return nil, false, appError(ErrorModelOutput, err)
		}
		model = firstIncrementalModel(rangeResults)
	}
	meta := &schema.IncrementalMeta{
		Enabled:          true,
		PreviousSpecHash: prev.Report.Input.SpecHash,
		Mode:             string(cfg.Mode),
		Fallback:         false,
		ReviewedSections: len(plan.ReviewRanges),
		ReusedSections:   len(plan.ReuseRanges),
		ReusedIssues:     len(reuse.Issues),
		ReusedQuestions:  len(reuse.Questions),
		DroppedFindings:  len(reuse.Dropped),
		ChangedLineRatio: changedLineRatio(plan, s.LineCount),
	}
	report, err := incremental.MergeReport(incremental.MergeInput{
		Spec:                s,
		PreflightIssues:     preflightIssues,
		ReusedIssues:        reuse.Issues,
		ReusedQuestions:     reuse.Questions,
		RangeResults:        rangeResults,
		Model:               model,
		Profile:             req.Profile,
		Strict:              req.Strict,
		SeverityThreshold:   req.SeverityThreshold,
		ContextFiles:        req.ContextPaths,
		IncrementalMetadata: meta,
		IncludeMetadata:     req.IncrementalReport,
	})
	if err != nil {
		return nil, false, err
	}
	redactedSpec := s.Raw != originalRaw
	return &CheckResult{
		Report:       report,
		PatchDiff:    patchDiffForReport(originalRaw, report, redactedSpec, errw),
		OriginalSpec: originalRaw,
		LineCount:    s.LineCount,
		Model:        model,
	}, true, nil
}

func loadIncrementalBase(req CheckRequest, current *spec.Spec, prev *incremental.PreviousReport) (string, bool, error) {
	baseText := req.IncrementalBaseText
	if baseText != "" {
		return baseText, true, nil
	}
	if req.IncrementalBasePath != "" {
		raw, err := os.ReadFile(req.IncrementalBasePath)
		if err != nil {
			return "", false, fmt.Errorf("reading incremental base spec: %w", err)
		}
		return string(raw), true, nil
	}
	if prev.Report.Input.SpecHash == current.Hash {
		return current.Raw, true, nil
	}
	return "", false, nil
}

func loadPreviousIncrementalReport(req CheckRequest) (*incremental.PreviousReport, error) {
	if req.IncrementalFromText != "" {
		return incremental.ParsePreviousReport([]byte(req.IncrementalFromText))
	}
	return incremental.LoadPreviousReport(req.IncrementalFrom)
}

func firstIncrementalModel(results []incremental.RangeResult) string {
	for _, result := range results {
		if result.Model != "" {
			return result.Model
		}
	}
	return "incremental-reuse"
}

func changedLineRatio(plan incremental.Plan, lineCount int) float64 {
	if lineCount <= 0 {
		return 0
	}
	changed := 0
	for _, rr := range plan.ReviewRanges {
		if rr.Primary.End < rr.Primary.Start {
			continue
		}
		changed += rr.Primary.End - rr.Primary.Start + 1
	}
	ratio := float64(changed) / float64(lineCount)
	if ratio > 1 {
		return 1
	}
	return ratio
}

func patchDiffForReport(originalRaw string, report *schema.Report, redactedSpec bool, errw io.Writer) string {
	if redactedSpec {
		return ""
	}
	return patch.GenerateDiffWithIssues(originalRaw, report.Patches, report.Issues, errw)
}

func safeReportPatches(raw string, issues []schema.Issue, patches []schema.Patch) []schema.Patch {
	if len(patches) == 0 {
		return nil
	}
	issueIDs := make(map[string]bool, len(issues))
	for _, issue := range issues {
		issueIDs[issue.ID] = true
	}
	out := make([]schema.Patch, 0, len(patches))
	occupied := make([]patchRange, 0, len(patches))
	for _, patch := range patches {
		if !issueIDs[patch.IssueID] || strings.TrimSpace(patch.Before) == "" || patch.After == "" {
			continue
		}
		rng, ok := exactUniquePatchRange(raw, patch.Before)
		if !ok || overlapsPatchRange(rng, occupied) {
			continue
		}
		occupied = append(occupied, rng)
		out = append(out, patch)
	}
	return out
}

type patchRange struct {
	start int
	end   int
}

func exactUniquePatchRange(raw string, before string) (patchRange, bool) {
	start := strings.Index(raw, before)
	if start < 0 {
		return patchRange{}, false
	}
	next := start + 1
	if next < len(raw) && strings.Contains(raw[next:], before) {
		return patchRange{}, false
	}
	return patchRange{start: start, end: start + len(before)}, true
}

func overlapsPatchRange(rng patchRange, existing []patchRange) bool {
	for _, candidate := range existing {
		if rng.start < candidate.end && candidate.start < rng.end {
			return true
		}
	}
	return false
}

func (c *Checker) checkChunked(ctx context.Context, provider llm.Provider, req CheckRequest, s *spec.Spec, contextFiles []ctxpkg.ContextFile, preflightIssues []schema.Issue, sysPrompt, specPrefix, preflightContext string, longCache bool, cfg chunk.Config, errw io.Writer) (*schema.Report, string, error) {
	if errw == nil {
		errw = io.Discard
	}
	plan, err := chunk.PlanSpec(s, cfg)
	if err != nil {
		return nil, "", err
	}
	withLines := chunk.EstimateTokens(specPrefix)+req.MaxTokens > maxSharedRequestTokens
	if withLines {
		logVerbose(errw, req.Verbose, "Spec is too large to send whole with every chunk call; each chunk call gets only its own lines")
		specPrefix = llm.BuildSpecPrefix(nil, contextFiles, preflightContext)
	}
	if req.Debug {
		_, _ = fmt.Fprintf(errw, "=== DEBUG: chunk prompt components ===\n")
		_, _ = fmt.Fprintf(errw, "[SYSTEM]\n%s\n\n[SHARED USER PREFIX]\n%s\n", sysPrompt, specPrefix)
		for _, ch := range plan.Chunks {
			task, err := chunk.BuildChunkTask(ch, s.LineCount, withLines)
			if err != nil {
				_, _ = fmt.Fprintf(errw, "WARN: building debug prompt for chunk %s failed: %s\n", ch.ID, err)
				continue
			}
			_, _ = fmt.Fprintf(errw, "[CHUNK %s TASK]\n%s\n", ch.ID, task)
		}
		_, _ = fmt.Fprintf(errw, "=== END DEBUG ===\n")
	}
	results, err := chunk.ReviewChunks(ctx, provider, s, plan, chunk.ExecutorConfig{
		SystemPrompt:  sysPrompt,
		Prefix:        specPrefix,
		LongCache:     longCache,
		WithLines:     withLines,
		MaxTokens:     req.MaxTokens,
		Effort:        req.Effort,
		EnforceSchema: enforceSchema(req),
		Concurrency:   cfg.ChunkConcurrency,
		Verbose:       req.Verbose,
		ErrWriter:     errw,
	})
	if err != nil {
		return nil, "", err
	}
	merged := chunk.MergeReports(chunk.MergeInput{
		ChunkResults: results,
		Preflight:    preflightIssues,
		OriginalSpec: s.Raw,
	})
	model := firstChunkModel(results)
	synthesis, synthesisModel, err := chunk.RunSynthesis(ctx, provider, s, preflightIssues, merged, chunk.SynthesisConfig{
		SystemPrompt:  sysPrompt,
		SpecShown:     !withLines,
		Prefix:        specPrefix,
		LongCache:     longCache,
		MaxTokens:     req.MaxTokens,
		Effort:        req.Effort,
		EnforceSchema: enforceSchema(req),
		LineThreshold: cfg.SynthesisLineThreshold,
		Enabled:       true,
	})
	if err != nil {
		return nil, "", err
	}
	var synthesisMeta *schema.SynthesisMeta
	if synthesis != nil {
		merged, synthesisMeta = chunk.ApplySynthesis(merged, synthesis, s.Raw, schema.EvidencePath(s.Path))
		logVerbose(errw, req.Verbose, "Synthesis: %d finding(s) merged, %d retracted, %d retraction(s) ignored", synthesisMeta.MergedFindings, len(synthesisMeta.Retracted), synthesisMeta.IgnoredRetractions)
		if synthesisModel != "" {
			model = synthesisModel
		}
	}
	report := buildReport(req, s, merged.Issues, merged.Questions, merged.Patches, model)
	report.Meta.DroppedFindings = merged.DroppedFindings
	report.Meta.Synthesis = synthesisMeta
	return report, model, nil
}

func runPreflight(s *spec.Spec, req CheckRequest, errw io.Writer) ([]schema.Issue, bool, error) {
	if !req.Preflight {
		return nil, false, nil
	}
	mode := preflight.Mode(req.PreflightMode)
	if mode == "" {
		mode = preflight.ModeWarn
	}
	profileName := req.PreflightProfile
	if profileName == "" {
		profileName = req.Profile
	}
	logVerbose(errw, req.Verbose, "Running preflight: %s", mode)
	result, err := preflight.Run(s, preflight.Config{
		Enabled:   true,
		Mode:      mode,
		Profile:   profileName,
		Strict:    req.Strict,
		IgnoreIDs: req.PreflightIgnore,
	})
	if err != nil {
		return nil, false, err
	}
	switch mode {
	case preflight.ModeOnly:
		return result.Issues, true, nil
	case preflight.ModeGate:
		return result.Issues, hasBlockingIssue(result.Issues), nil
	case preflight.ModeWarn:
		return result.Issues, false, nil
	default:
		return nil, false, fmt.Errorf("invalid preflight mode %q", req.PreflightMode)
	}
}

func hasBlockingIssue(issues []schema.Issue) bool {
	for _, issue := range issues {
		if issue.Blocking {
			return true
		}
	}
	return false
}

func chunkConfigFromRequest(req CheckRequest) chunk.Config {
	return chunk.WithDefaults(chunk.Config{
		Mode:                   chunk.Mode(req.Chunking),
		ChunkLines:             req.ChunkLines,
		ChunkOverlap:           req.ChunkOverlap,
		ChunkMinLines:          req.ChunkMinLines,
		ChunkTokenThreshold:    req.ChunkTokenThreshold,
		ChunkConcurrency:       req.ChunkConcurrency,
		SynthesisLineThreshold: req.SynthesisLineThreshold,
	})
}

func firstChunkModel(results []chunk.ChunkResult) string {
	for _, result := range results {
		if result.Model != "" {
			return result.Model
		}
	}
	return ""
}

func mergeIssues(preflightIssues, llmIssues []schema.Issue, knownPreflightIDs map[string]bool) []schema.Issue {
	if len(preflightIssues) == 0 {
		return cleanDuplicateTags(llmIssues, knownPreflightIDs)
	}
	llmIssues = cleanDuplicateTags(llmIssues, knownPreflightIDs)
	consumed := make(map[int]bool)
	confirmedLLM := make([]schema.Issue, 0, len(llmIssues))
	for _, issue := range llmIssues {
		if idx := duplicatePreflightIndex(issue, preflightIssues, knownPreflightIDs, consumed); idx >= 0 {
			consumed[idx] = true
			issue = markPreflightConfirmed(issue, preflightIssues[idx])
		}
		confirmedLLM = append(confirmedLLM, issue)
	}
	merged := make([]schema.Issue, 0, len(preflightIssues)+len(llmIssues))
	for i, issue := range preflightIssues {
		if !consumed[i] {
			merged = append(merged, issue)
		}
	}
	merged = append(merged, confirmedLLM...)
	return merged
}

func buildPreflightPromptContext(issues []schema.Issue) (string, map[string]bool) {
	if len(issues) == 0 {
		return "", nil
	}
	sorted := append([]schema.Issue(nil), issues...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if severityRank(sorted[i].Severity) != severityRank(sorted[j].Severity) {
			return severityRank(sorted[i].Severity) > severityRank(sorted[j].Severity)
		}
		if issueLineStart(sorted[i]) != issueLineStart(sorted[j]) {
			return issueLineStart(sorted[i]) < issueLineStart(sorted[j])
		}
		return sorted[i].ID < sorted[j].ID
	})
	limit := len(sorted)
	if limit > maxPreflightPromptFindings {
		limit = maxPreflightPromptFindings
	}
	known := make(map[string]bool, limit)
	var sb strings.Builder
	sb.WriteString("<known_preflight_findings>\n")
	sb.WriteString("These deterministic findings were found locally before the LLM call. Do not repeat them unless you can add materially new information. If your issue duplicates one, add tag duplicates:<PREFLIGHT-ID> using only an ID listed below.\n")
	for _, issue := range sorted[:limit] {
		known[issue.ID] = true
		fmt.Fprintf(&sb, "- %s %s %s L%d: %s\n", issue.ID, issue.Severity, issue.Category, issueLineStart(issue), issue.Title)
	}
	if len(sorted) > limit {
		counts := make(map[string]int)
		for _, issue := range sorted[limit:] {
			counts[preflightGroup(issue)]++
		}
		groups := make([]string, 0, len(counts))
		for group := range counts {
			groups = append(groups, group)
		}
		sort.Strings(groups)
		sb.WriteString("Additional deterministic findings omitted from this prompt:\n")
		for _, group := range groups {
			fmt.Fprintf(&sb, "- %s: %d\n", group, counts[group])
		}
	}
	sb.WriteString("</known_preflight_findings>\n")
	return sb.String(), known
}

func cleanDuplicateTags(issues []schema.Issue, knownPreflightIDs map[string]bool) []schema.Issue {
	if len(knownPreflightIDs) == 0 {
		return issues
	}
	out := append([]schema.Issue(nil), issues...)
	for i := range out {
		tags := make([]string, 0, len(out[i].Tags))
		for _, tag := range out[i].Tags {
			if id, ok := duplicateTagID(tag); ok && !knownPreflightIDs[id] {
				continue
			}
			tags = append(tags, tag)
		}
		out[i].Tags = tags
	}
	return out
}

func duplicatePreflightIndex(issue schema.Issue, preflightIssues []schema.Issue, knownPreflightIDs map[string]bool, consumed map[int]bool) int {
	for _, tag := range issue.Tags {
		id, ok := duplicateTagID(tag)
		if !ok || !knownPreflightIDs[id] {
			continue
		}
		if idx := findPreflightByIDAndEvidence(id, issue, preflightIssues, consumed); idx >= 0 {
			return idx
		}
	}
	for i, preflightIssue := range preflightIssues {
		if consumed[i] {
			continue
		}
		if deterministicDuplicate(preflightIssue, issue) {
			return i
		}
	}
	return -1
}

func findPreflightByIDAndEvidence(id string, issue schema.Issue, preflightIssues []schema.Issue, consumed map[int]bool) int {
	fallback := -1
	for i, preflightIssue := range preflightIssues {
		if consumed[i] || preflightIssue.ID != id {
			continue
		}
		if fallback < 0 {
			fallback = i
		}
		if evidenceOverlaps(preflightIssue, issue) {
			return i
		}
	}
	return fallback
}

func deterministicDuplicate(preflightIssue, llmIssue schema.Issue) bool {
	return preflightIssue.Category == llmIssue.Category &&
		preflightIssue.Title == llmIssue.Title &&
		evidenceOverlaps(preflightIssue, llmIssue)
}

func evidenceOverlaps(a, b schema.Issue) bool {
	for _, left := range a.Evidence {
		for _, right := range b.Evidence {
			if left.LineStart <= right.LineEnd && right.LineStart <= left.LineEnd {
				return true
			}
		}
	}
	return false
}

func markPreflightConfirmed(issue, preflightIssue schema.Issue) schema.Issue {
	issue.Tags = appendUnique(append([]string(nil), issue.Tags...), "preflight-confirmed", "preflight-rule:"+preflightIssue.ID)
	return issue
}

func duplicateTagID(tag string) (string, bool) {
	id, ok := strings.CutPrefix(tag, "duplicates:")
	return id, ok && id != ""
}

func preflightGroup(issue schema.Issue) string {
	for _, tag := range issue.Tags {
		if group, ok := strings.CutPrefix(tag, "preflight-rule:"); ok {
			return group
		}
	}
	return "unknown"
}

func issueLineStart(issue schema.Issue) int {
	if len(issue.Evidence) == 0 {
		return 0
	}
	return issue.Evidence[0].LineStart
}

func severityRank(severity schema.Severity) int {
	switch severity {
	case schema.SeverityCritical:
		return 2
	case schema.SeverityWarn:
		return 1
	case schema.SeverityInfo:
		return 0
	default:
		return -1
	}
}

func appendUnique(tags []string, values ...string) []string {
	for _, value := range values {
		exists := false
		for _, tag := range tags {
			if tag == value {
				exists = true
				break
			}
		}
		if !exists {
			tags = append(tags, value)
		}
	}
	return tags
}

func buildReport(req CheckRequest, s *spec.Spec, issues []schema.Issue, questions []schema.Question, patches []schema.Patch, model string) *schema.Report {
	score := review.Score(issues, questions)
	verdict := review.Verdict(issues, questions)
	critical, warn, info := review.Counts(issues)
	return &schema.Report{
		Tool:    "speccritic",
		Version: req.Version,
		Input: schema.Input{
			SpecFile:          s.Path,
			SpecHash:          s.Hash,
			ContextFiles:      req.ContextPaths,
			Profile:           req.Profile,
			Strict:            req.Strict,
			SeverityThreshold: req.SeverityThreshold,
		},
		Summary: schema.Summary{
			Verdict:       verdict,
			Score:         score,
			CriticalCount: critical,
			WarnCount:     warn,
			InfoCount:     info,
		},
		Issues:    issues,
		Questions: questions,
		Patches:   patches,
		Meta: schema.Meta{
			Model: model,
		},
	}
}

func validateRequest(req CheckRequest) error {
	if err := chunk.ValidateConfig(chunkConfigFromRequest(req)); err != nil {
		return err
	}
	if req.IncrementalFrom != "" || req.IncrementalFromText != "" || req.IncrementalMode != "" {
		if err := incremental.ValidateConfig(incrementalConfigFromRequest(req)); err != nil {
			return err
		}
	}
	if req.ConvergenceFrom != "" || req.ConvergenceFromText != "" || req.ConvergenceMode != "" {
		if err := convergence.ValidateConfig(convergenceConfigFromRequest(req, convergence.CoverageUnknown)); err != nil {
			return err
		}
	}
	if err := validateCompletionRequest(req); err != nil {
		return err
	}
	switch req.Verify {
	case "", VerifyAuto, VerifyOff:
	default:
		return fmt.Errorf("verify %q must be %s or %s", req.Verify, VerifyAuto, VerifyOff)
	}
	switch req.StructuredOutput {
	case "", StructuredOutputAuto, StructuredOutputOff:
	default:
		return fmt.Errorf("structured output %q must be %s or %s", req.StructuredOutput, StructuredOutputAuto, StructuredOutputOff)
	}
	if !llm.IsValidEffort(req.Effort) {
		return fmt.Errorf("effort %q must be one of %s", req.Effort, strings.Join(llm.EffortLevels(), ", "))
	}
	if req.Source == SourceWeb {
		if req.SpecPath != "" {
			return fmt.Errorf("web checks must not use SpecPath")
		}
		if len(req.ContextPaths) > 0 {
			return fmt.Errorf("web checks must not use ContextPaths")
		}
	}
	if req.SpecPath == "" && req.SpecText == "" {
		return fmt.Errorf("spec path or spec text is required")
	}
	if req.SpecPath != "" && req.SpecText != "" {
		return fmt.Errorf("spec path and spec text are mutually exclusive")
	}
	return nil
}

func validateCompletionRequest(req CheckRequest) error {
	switch req.CompletionMode {
	case "", schema.CompletionModeAuto, schema.CompletionModeOn, schema.CompletionModeOff:
	default:
		return fmt.Errorf("completion mode %q must be auto, on, or off", req.CompletionMode)
	}
	if req.CompletionTemplate != "" && !schema.IsCompletionInputTemplateName(req.CompletionTemplate) {
		return fmt.Errorf("completion template %q must be one of %s", req.CompletionTemplate, strings.Join(schema.CompletionInputTemplateNames(), ", "))
	}
	if req.CompletionMaxPatches < 0 {
		return fmt.Errorf("completion max patches must be >= 0, got %d", req.CompletionMaxPatches)
	}
	return nil
}

func convergenceConfigFromRequest(req CheckRequest, coverage convergence.ReviewCoverage) convergence.Config {
	cfg := convergence.DefaultConfig()
	if req.ConvergenceMode != "" {
		cfg.Mode = convergence.Mode(req.ConvergenceMode)
	}
	cfg.Report = req.ConvergenceReport || req.ConvergenceFrom != "" || req.ConvergenceFromText != ""
	cfg.StrictCompatibility = req.ConvergenceStrict
	cfg.Profile = req.Profile
	cfg.ReviewStrict = req.Strict
	cfg.SeverityThreshold = req.SeverityThreshold
	cfg.CurrentSpecHash = ""
	cfg.CurrentReviewCoverage = coverage
	return cfg
}

func incrementalConfigFromRequest(req CheckRequest) incremental.Config {
	cfg := incremental.DefaultConfig()
	if req.IncrementalMode != "" {
		cfg.Mode = incremental.Mode(req.IncrementalMode)
	}
	cfg.MaxChangeRatio = req.IncrementalMaxChangeRatio
	cfg.MaxRemapFailureRatio = req.IncrementalMaxRemapFailureRatio
	cfg.ContextLines = req.IncrementalContextLines
	cfg.StrictReuse = req.IncrementalStrictReuse
	cfg.ReportMetadata = req.IncrementalReport
	cfg.Profile = req.Profile
	cfg.Strict = req.Strict
	cfg.SeverityThreshold = req.SeverityThreshold
	return cfg
}

func completionConfigFromRequest(req CheckRequest) completion.Config {
	cfg := completion.Config{
		Suggestions:   req.CompletionSuggestions,
		Mode:          completion.ModeAuto,
		Template:      req.CompletionTemplate,
		MaxPatches:    req.CompletionMaxPatches,
		OpenDecisions: req.CompletionOpenDecisions,
	}
	if cfg.Template == "" {
		cfg.Template = schema.CompletionTemplateProfile
	}
	if req.CompletionMode != "" {
		cfg.Mode = completion.Mode(req.CompletionMode)
	}
	return cfg
}

func resolveModel(req CheckRequest, errw io.Writer) (string, string, error) {
	llmProvider := strings.TrimSpace(req.LLMProvider)
	llmModel := strings.TrimSpace(req.LLMModel)
	if llmProvider == "" {
		llmProvider = strings.TrimSpace(os.Getenv("SPECCRITIC_LLM_PROVIDER"))
	}
	if llmModel == "" {
		llmModel = strings.TrimSpace(os.Getenv("SPECCRITIC_LLM_MODEL"))
	}
	configured := llmProvider != "" || llmModel != ""
	if req.Offline && !configured {
		return "", "", fmt.Errorf("LLM provider or model must be configured when --offline is set")
	}
	if llmProvider == "" {
		if inferred := llm.ProviderForModel(llmModel); inferred != "" {
			llmProvider = inferred
		} else {
			llmProvider = llm.DefaultProvider
		}
	}
	if llmModel == "" {
		llmModel = llm.DefaultModelForProvider(llmProvider)
	}
	if !configured {
		_, _ = fmt.Fprintf(errw, "WARN: SPECCRITIC_LLM_PROVIDER/SPECCRITIC_LLM_MODEL not set, using default %s:%s\n", llmProvider, llmModel)
	}
	return llmProvider, llmModel, nil
}

func loadSpec(req CheckRequest) (*spec.Spec, error) {
	if req.SpecPath != "" {
		return spec.Load(req.SpecPath)
	}
	name := req.SpecName
	if name == "" {
		name = "SPEC.md"
	}
	return spec.LoadText(name, req.SpecText)
}

func loadContext(req CheckRequest) ([]ctxpkg.ContextFile, error) {
	files, err := ctxpkg.Load(req.ContextPaths)
	if err != nil {
		return nil, err
	}
	for i := range files {
		files[i].Content = redact.Redact(files[i].Content)
	}
	for _, doc := range req.ContextDocuments {
		name := doc.Name
		if name == "" {
			name = "context.md"
		}
		files = append(files, ctxpkg.ContextFile{
			Path:    name,
			Content: redact.Redact(doc.Text),
		})
	}
	return files, nil
}

func specLabel(req CheckRequest) string {
	if req.SpecPath != "" {
		return req.SpecPath
	}
	if req.SpecName != "" {
		return req.SpecName
	}
	return "<text>"
}

func callWithRetry(ctx context.Context, provider llm.Provider, req *llm.Request, s *spec.Spec, verbose bool, errw io.Writer) (*schema.Report, string, error) {
	return llm.CompleteReport(ctx, provider, llm.ReportCall{
		Request: req,
		Parse: func(raw string) (llm.Parsed, error) {
			res, err := validate.ParseResponse(raw, validate.Options{LineCount: s.LineCount, SpecPath: s.Path, SpecText: s.Raw})
			if err != nil {
				return llm.Parsed{}, err
			}
			return llm.Parsed{Report: res.Report, Incomplete: res.Incomplete, Dropped: res.Dropped}, nil
		},
		RepairPrompt: func(reason error, _ string) string {
			return fmt.Sprintf(
				"\n\nYour previous response failed schema validation (error category: %q). Return only valid JSON matching the schema above.",
				sanitizeErrForPrompt(reason),
			)
		},
		Logf: func(format string, args ...any) { logVerbose(errw, verbose, format, args...) },
	})
}

func sanitizeErrForPrompt(err error) string {
	msg := err.Error()
	switch {
	case strings.HasPrefix(msg, "JSON parse failed"):
		return "JSON syntax error"
	case strings.Contains(msg, "invalid severity"):
		return "invalid enum value (severity must be INFO, WARN, or CRITICAL)"
	case strings.Contains(msg, "unknown category"):
		return "invalid enum value (unknown defect category)"
	case strings.Contains(msg, "title is required"), strings.Contains(msg, "question text is required"):
		return "missing required field"
	case strings.Contains(msg, "does not match ISSUE-"), strings.Contains(msg, "does not match Q-"):
		return "invalid ID format"
	case strings.Contains(msg, "line_start"), strings.Contains(msg, "line_end"):
		return "invalid line range in evidence"
	default:
		return "schema validation error"
	}
}

func appError(kind ErrorKind, err error) error {
	return &Error{Kind: kind, Err: err}
}

func logVerbose(w io.Writer, verbose bool, format string, args ...any) {
	if verbose {
		_, _ = fmt.Fprintf(w, "INFO: "+format+"\n", args...)
	}
}
