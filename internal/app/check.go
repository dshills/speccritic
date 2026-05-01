package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	ctxpkg "github.com/dshills/speccritic/internal/context"
	"github.com/dshills/speccritic/internal/llm"
	"github.com/dshills/speccritic/internal/patch"
	"github.com/dshills/speccritic/internal/preflight"
	"github.com/dshills/speccritic/internal/profile"
	"github.com/dshills/speccritic/internal/redact"
	"github.com/dshills/speccritic/internal/review"
	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/schema/validate"
	"github.com/dshills/speccritic/internal/spec"
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
	Temperature       float64
	MaxTokens         int
	Offline           bool
	Debug             bool
	Verbose           bool
	Preflight         bool
	PreflightMode     string
	PreflightProfile  string
	PreflightIgnore   []string
	Source            Source
	ErrWriter         io.Writer
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

	s.Numbered = redact.Redact(s.Numbered)
	s.Raw = redact.Redact(s.Raw)

	preflightIssues, preflightOnly, err := runPreflight(s, req, errw)
	if err != nil {
		return nil, appError(ErrorInput, err)
	}
	if preflightOnly {
		report := buildReport(req, s, preflightIssues, nil, nil, "preflight")
		return &CheckResult{
			Report:       report,
			OriginalSpec: s.Raw,
			LineCount:    s.LineCount,
			Model:        "preflight",
		}, nil
	}

	llmProvider, llmModel, err := resolveModel(req.Offline, errw)
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
	userPrefix, userSpec := llm.BuildUserPrompt(s, contextFiles)

	llmReq := &llm.Request{
		SystemPrompt:           sysPrompt,
		UserPromptCachedPrefix: userPrefix,
		UserPrompt:             userSpec,
		Temperature:            &req.Temperature,
		MaxTokens:              req.MaxTokens,
	}

	if req.Debug {
		fmt.Fprintf(errw, "=== DEBUG: redacted prompt ===\n")
		fmt.Fprintf(errw, "[SYSTEM]\n%s\n\n[USER PREFIX]\n%s\n[USER SPEC]\n%s\n", sysPrompt, userPrefix, userSpec)
		fmt.Fprintf(errw, "=== END DEBUG ===\n")
	}

	newProvider := c.NewProvider
	if newProvider == nil {
		newProvider = llm.NewProvider
	}
	provider, err := newProvider(modelStr)
	if err != nil {
		return nil, appError(ErrorProvider, fmt.Errorf("creating LLM provider: %w", err))
	}

	logVerbose(errw, req.Verbose, "Calling LLM: %s", modelStr)
	report, responseModel, err := callWithRetry(ctx, provider, llmReq, s.LineCount, req.Verbose, errw)
	if err != nil {
		return nil, appError(ErrorModelOutput, err)
	}
	report.Issues = mergeIssues(preflightIssues, report.Issues)

	report = buildReport(req, s, report.Issues, report.Questions, report.Patches, responseModel)

	return &CheckResult{
		Report:       report,
		PatchDiff:    patch.GenerateDiff(s.Raw, report.Patches, errw),
		OriginalSpec: s.Raw,
		LineCount:    s.LineCount,
		Model:        responseModel,
	}, nil
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

func mergeIssues(preflightIssues, llmIssues []schema.Issue) []schema.Issue {
	if len(preflightIssues) == 0 {
		return llmIssues
	}
	merged := make([]schema.Issue, 0, len(preflightIssues)+len(llmIssues))
	merged = append(merged, preflightIssues...)
	merged = append(merged, llmIssues...)
	return merged
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
			Model:       model,
			Temperature: req.Temperature,
		},
	}
}

func validateRequest(req CheckRequest) error {
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

func resolveModel(offline bool, errw io.Writer) (string, string, error) {
	llmProvider := os.Getenv("SPECCRITIC_LLM_PROVIDER")
	llmModel := os.Getenv("SPECCRITIC_LLM_MODEL")
	if offline && (llmProvider == "" || llmModel == "") {
		return "", "", fmt.Errorf("SPECCRITIC_LLM_PROVIDER and SPECCRITIC_LLM_MODEL environment variables must be set (required with --offline)")
	}
	providerSet := llmProvider != ""
	modelSet := llmModel != ""
	if providerSet != modelSet {
		return "", "", fmt.Errorf("SPECCRITIC_LLM_PROVIDER and SPECCRITIC_LLM_MODEL must both be set or both be unset")
	}
	if llmProvider == "" {
		llmProvider = "anthropic"
		llmModel = "claude-sonnet-4-6"
		fmt.Fprintf(errw, "WARN: SPECCRITIC_LLM_PROVIDER/SPECCRITIC_LLM_MODEL not set, using default %s:%s\n", llmProvider, llmModel)
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

func callWithRetry(ctx context.Context, provider llm.Provider, req *llm.Request, lineCount int, verbose bool, errw io.Writer) (*schema.Report, string, error) {
	resp, err := provider.Complete(ctx, req)
	if err != nil {
		return nil, "", fmt.Errorf("LLM call failed: %w", err)
	}

	report, parseErr := validate.Parse(resp.Content, lineCount)
	if parseErr == nil {
		return report, resp.Model, nil
	}

	logVerbose(errw, verbose, "Validation failed, retrying: %s", parseErr)

	repairReq := *req
	repairReq.UserPrompt = req.UserPrompt + fmt.Sprintf(
		"\n\nYour previous response failed schema validation (error category: %q). Return only valid JSON matching the schema above.",
		sanitizeErrForPrompt(parseErr),
	)

	resp2, err := provider.Complete(ctx, &repairReq)
	if err != nil {
		return nil, "", fmt.Errorf("LLM retry call failed: %w", err)
	}

	report, parseErr = validate.Parse(resp2.Content, lineCount)
	if parseErr != nil {
		return nil, "", fmt.Errorf("invalid model output after retry: %w", parseErr)
	}

	return report, resp2.Model, nil
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
		fmt.Fprintf(w, "INFO: "+format+"\n", args...)
	}
}
