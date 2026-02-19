package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	ctxpkg "github.com/dshills/speccritic/internal/context"
	"github.com/dshills/speccritic/internal/llm"
	"github.com/dshills/speccritic/internal/patch"
	"github.com/dshills/speccritic/internal/profile"
	"github.com/dshills/speccritic/internal/redact"
	"github.com/dshills/speccritic/internal/render"
	"github.com/dshills/speccritic/internal/review"
	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/schema/validate"
	"github.com/dshills/speccritic/internal/spec"
)

// version is set at build time via -ldflags "-X main.version=x.y.z".
var version = "dev"

// exitErr carries a numeric exit code through the cobra error path.
type exitErr struct {
	code int
	msg  string
}

func (e *exitErr) Error() string { return e.msg }

// codeError returns an exitErr for the given code.
func codeError(code int, format string, args ...any) error {
	return &exitErr{code: code, msg: fmt.Sprintf(format, args...)}
}

// checkFlags holds the parsed flags for the check command.
type checkFlags struct {
	format            string
	out               string
	contextFiles      []string
	profileName       string
	strict            bool
	failOn            string
	severityThreshold string
	patchOut          string
	temperature       float64
	maxTokens         int
	offline           bool
	verbose           bool
	debug             bool
}

func main() {
	root := &cobra.Command{
		Use:   "speccritic",
		Short: "Evaluate software specifications for defects",
		Long:  "SpecCritic evaluates SPEC.md files as formal contracts, identifying defects before implementation begins.",
	}

	var flags checkFlags
	checkCmd := &cobra.Command{
		Use:   "check <spec-file>",
		Short: "Analyze a specification and produce a review",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(args[0], flags)
		},
	}

	f := checkCmd.Flags()
	f.StringVar(&flags.format, "format", "json", "Output format: json or md")
	f.StringVar(&flags.out, "out", "", "Write output to file instead of stdout")
	f.StringArrayVar(&flags.contextFiles, "context", nil, "Context file paths (may be repeated)")
	f.StringVar(&flags.profileName, "profile", "general", "Specification profile")
	f.BoolVar(&flags.strict, "strict", false, "Enable strict mode (silence = ambiguity)")
	f.StringVar(&flags.failOn, "fail-on", "", "Exit 2 if verdict >= this level (VALID_WITH_GAPS or INVALID)")
	f.StringVar(&flags.severityThreshold, "severity-threshold", "info", "Minimum severity to emit: info, warn, or critical")
	f.StringVar(&flags.patchOut, "patch-out", "", "Write suggested patches in diff-match-patch format to this file")
	f.Float64Var(&flags.temperature, "temperature", 0.2, "LLM temperature")
	f.IntVar(&flags.maxTokens, "max-tokens", 4096, "Maximum response tokens")
	f.BoolVar(&flags.offline, "offline", false, "Exit 3 if SPECCRITIC_MODEL env var is not set; use to enforce explicit model config in CI")
	f.BoolVar(&flags.verbose, "verbose", false, "Print processing steps to stderr")
	f.BoolVar(&flags.debug, "debug", false, "Dump full prompt (including spec and context file contents) to stderr; use only in trusted environments")

	root.AddCommand(checkCmd)

	if err := root.Execute(); err != nil {
		var ee *exitErr
		if errors.As(err, &ee) {
			fmt.Fprintln(os.Stderr, "Error:", ee.msg)
			os.Exit(ee.code)
		}
		// cobra already printed the error
		os.Exit(1)
	}
}

func runCheck(specPath string, flags checkFlags) error {
	// --- Step 1: Validate flags ---
	if err := validateFlags(flags); err != nil {
		return codeError(3, "invalid flags: %s", err)
	}

	// --- Step 2: Resolve model; offline check uses raw env var ---
	rawModel := os.Getenv("SPECCRITIC_MODEL")
	if flags.offline && rawModel == "" {
		return codeError(3, "SPECCRITIC_MODEL environment variable not set (required with --offline)")
	}
	modelStr := rawModel
	if modelStr == "" {
		modelStr = "anthropic:claude-sonnet-4-6"
		fmt.Fprintf(os.Stderr, "WARN: SPECCRITIC_MODEL not set, using default %s\n", modelStr)
	}

	// --- Step 3: Load spec ---
	logVerbose(flags.verbose, "Loading spec: %s", specPath)
	s, err := spec.Load(specPath)
	if err != nil {
		return codeError(3, "loading spec: %s", err)
	}

	// --- Step 4: Redact spec content ---
	s.Numbered = redact.Redact(s.Numbered)
	s.Raw = redact.Redact(s.Raw)

	// --- Step 5-6: Load and redact context files ---
	logVerbose(flags.verbose, "Loading %d context file(s)", len(flags.contextFiles))
	contextFiles, err := ctxpkg.Load(flags.contextFiles)
	if err != nil {
		return codeError(3, "loading context files: %s", err)
	}

	// --- Step 7: Load profile ---
	logVerbose(flags.verbose, "Loading profile: %s", flags.profileName)
	prof, err := profile.Get(flags.profileName)
	if err != nil {
		return codeError(3, "loading profile: %s", err)
	}

	// --- Step 8: Build LLM request ---
	sysPrompt := llm.BuildSystemPrompt(prof, flags.strict)
	userPrompt := llm.BuildUserPrompt(s, contextFiles)

	req := &llm.Request{
		SystemPrompt: sysPrompt,
		UserPrompt:   userPrompt,
		Temperature:  flags.temperature,
		MaxTokens:    flags.maxTokens,
	}

	// --- Step 9: Debug dump (includes file paths as-is; see PLAN.md security notes) ---
	if flags.debug {
		fmt.Fprintf(os.Stderr, "=== DEBUG: redacted prompt ===\n")
		fmt.Fprintf(os.Stderr, "[SYSTEM]\n%s\n\n[USER]\n%s\n", sysPrompt, userPrompt)
		fmt.Fprintf(os.Stderr, "=== END DEBUG ===\n")
	}

	// --- Step 10: Create LLM provider ---
	provider, err := llm.NewProvider(modelStr)
	if err != nil {
		return codeError(4, "creating LLM provider: %s", err)
	}

	// --- Step 11: Call LLM with retry ---
	logVerbose(flags.verbose, "Calling LLM: %s", modelStr)
	report, llmModel, callErr := callWithRetry(context.Background(), provider, req, s.LineCount, flags.verbose)
	if callErr != nil {
		return codeError(5, "%s", callErr)
	}

	// --- Step 12: Compute score and verdict from ALL issues (pre-filter) ---
	score := review.Score(report.Issues)
	verdict := review.Verdict(report.Issues, report.Questions)
	critical, warn, info := review.Counts(report.Issues)

	// --- Step 13: Populate report fields ---
	// Note: summary counts always reflect all issues before --severity-threshold filtering.
	// The issues array in output will be filtered (step 14), creating an intentional
	// mismatch that is documented in the output schema.
	report.Tool = "speccritic"
	report.Version = version
	report.Input = schema.Input{
		SpecFile:          specPath,
		SpecHash:          s.Hash,
		ContextFiles:      flags.contextFiles,
		Profile:           flags.profileName,
		Strict:            flags.strict,
		SeverityThreshold: flags.severityThreshold,
	}
	report.Summary = schema.Summary{
		Verdict:       verdict,
		Score:         score,
		CriticalCount: critical,
		WarnCount:     warn,
		InfoCount:     info,
	}
	report.Meta = schema.Meta{
		Model:       llmModel,
		Temperature: flags.temperature,
	}

	// --- Step 14: Apply severity threshold filter (output only, does not affect score/counts) ---
	severityFilter := parseSeverityThreshold(flags.severityThreshold)
	report.Issues = review.FilterBySeverity(report.Issues, severityFilter)

	// --- Step 15: Write patches ---
	if flags.patchOut != "" {
		logVerbose(flags.verbose, "Generating patches → %s", flags.patchOut)
		diffText := patch.GenerateDiff(s.Raw, report.Patches, os.Stderr)
		if err := os.WriteFile(flags.patchOut, []byte(diffText), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "WARN: patch write failed: %s\n", err)
			// Continue — patches are advisory per SPEC.md §12
		}
	}

	// --- Step 16: Render output ---
	logVerbose(flags.verbose, "Rendering output (format: %s)", flags.format)
	renderer, err := render.NewRenderer(flags.format)
	if err != nil {
		return codeError(3, "invalid format: %s", err)
	}
	outputBytes, err := renderer.Render(report)
	if err != nil {
		return codeError(3, "rendering output: %s", err)
	}

	// --- Step 17: Write output ---
	if flags.out != "" {
		if err := os.WriteFile(flags.out, outputBytes, 0o644); err != nil {
			return codeError(3, "writing output file: %s", err)
		}
	} else {
		if _, err := os.Stdout.Write(outputBytes); err != nil {
			return codeError(3, "writing output: %s", err)
		}
		// Ensure output ends with a newline for terminal friendliness.
		if len(outputBytes) > 0 && outputBytes[len(outputBytes)-1] != '\n' {
			fmt.Fprintln(os.Stdout)
		}
	}

	// --- Step 18: Evaluate --fail-on ---
	if flags.failOn != "" {
		verdictThreshold := schema.Verdict(flags.failOn)
		if schema.VerdictOrdinal(verdict) >= schema.VerdictOrdinal(verdictThreshold) {
			return codeError(2, "verdict %s meets or exceeds --fail-on threshold %s", verdict, verdictThreshold)
		}
	}

	return nil
}

// callWithRetry attempts an LLM call and retries once on validation failure.
// Returns the parsed report, the model string from the response, and any error.
func callWithRetry(ctx context.Context, provider llm.Provider, req *llm.Request, lineCount int, verbose bool) (*schema.Report, string, error) {
	resp, err := provider.Complete(ctx, req)
	if err != nil {
		return nil, "", fmt.Errorf("LLM call failed: %w", err)
	}

	report, parseErr := validate.Parse(resp.Content, lineCount)
	if parseErr == nil {
		return report, resp.Model, nil
	}

	logVerbose(verbose, "Validation failed, retrying: %s", parseErr)

	// Append a sanitized error description (not the raw LLM output) to avoid
	// prompt injection from the model's previous response.
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

// sanitizeErrForPrompt classifies a parse error into a fixed category string
// without echoing any LLM-generated content back into the retry prompt.
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

// validateFlags returns an error if any flag value is invalid.
func validateFlags(flags checkFlags) error {
	switch flags.format {
	case "json", "md":
	default:
		return fmt.Errorf("--format must be json or md, got %q", flags.format)
	}

	if flags.failOn != "" {
		switch schema.Verdict(flags.failOn) {
		case schema.VerdictValidWithGaps, schema.VerdictInvalid:
		default:
			return fmt.Errorf("--fail-on must be VALID_WITH_GAPS or INVALID, got %q", flags.failOn)
		}
	}

	switch flags.severityThreshold {
	case "info", "warn", "critical":
	default:
		return fmt.Errorf("--severity-threshold must be info, warn, or critical, got %q", flags.severityThreshold)
	}

	if flags.temperature < 0 || flags.temperature > 2 {
		return fmt.Errorf("--temperature must be between 0.0 and 2.0, got %g", flags.temperature)
	}

	if flags.maxTokens <= 0 {
		return fmt.Errorf("--max-tokens must be > 0, got %d", flags.maxTokens)
	}

	return nil
}

// parseSeverityThreshold converts a flag string to a schema.Severity.
func parseSeverityThreshold(s string) schema.Severity {
	switch s {
	case "warn":
		return schema.SeverityWarn
	case "critical":
		return schema.SeverityCritical
	default:
		return schema.SeverityInfo
	}
}

// logVerbose writes a timestamped message to stderr when verbose mode is enabled.
func logVerbose(verbose bool, format string, args ...any) {
	if verbose {
		fmt.Fprintf(os.Stderr, "INFO: "+format+"\n", args...)
	}
}
