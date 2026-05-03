package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dshills/speccritic/internal/app"
	"github.com/dshills/speccritic/internal/chunk"
	"github.com/dshills/speccritic/internal/convergence"
	"github.com/dshills/speccritic/internal/incremental"
	"github.com/dshills/speccritic/internal/render"
	"github.com/dshills/speccritic/internal/review"
	"github.com/dshills/speccritic/internal/schema"
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
	format                          string
	out                             string
	contextFiles                    []string
	profileName                     string
	strict                          bool
	failOn                          string
	severityThreshold               string
	patchOut                        string
	llmProvider                     string
	llmModel                        string
	temperature                     float64
	maxTokens                       int
	offline                         bool
	verbose                         bool
	debug                           bool
	preflight                       bool
	preflightMode                   string
	preflightProfile                string
	preflightIgnore                 []string
	chunking                        string
	chunkLines                      int
	chunkOverlap                    int
	chunkMinLines                   int
	chunkTokenThreshold             int
	chunkConcurrency                int
	synthesisLineThreshold          int
	incrementalFrom                 string
	incrementalBase                 string
	incrementalMode                 string
	incrementalMaxChangeRatio       float64
	incrementalMaxRemapFailureRatio float64
	incrementalContextLines         int
	incrementalStrictReuse          bool
	incrementalReport               bool
	convergenceFrom                 string
	convergenceMode                 string
	convergenceStrict               bool
	convergenceReport               bool
	completionSuggestions           bool
	completionMode                  string
	completionTemplate              string
	completionMaxPatches            int
	completionOpenDecisions         bool
	envErrors                       []string
}

func main() {
	root := &cobra.Command{
		Use:           "speccritic",
		Short:         "Evaluate software specifications for defects",
		Long:          "SpecCritic evaluates SPEC.md files as formal contracts, identifying defects before implementation begins.",
		SilenceErrors: true,
	}

	var flags checkFlags
	checkCmd := &cobra.Command{
		Use:   "check <spec-file>",
		Short: "Analyze a specification and produce a review",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			applyEnvDefaults(cmd, &flags)
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
	f.StringVar(&flags.llmProvider, "llm-provider", "", "LLM provider override: anthropic, openai, or gemini")
	f.StringVar(&flags.llmModel, "llm-model", "", "LLM model override")
	f.Float64Var(&flags.temperature, "temperature", 0.2, "LLM temperature")
	f.IntVar(&flags.maxTokens, "max-tokens", 4096, "Maximum response tokens")
	f.BoolVar(&flags.offline, "offline", false, "Exit 3 if LLM provider/model config is not set; use to enforce explicit model config in CI")
	f.BoolVar(&flags.verbose, "verbose", false, "Print processing steps to stderr")
	f.BoolVar(&flags.debug, "debug", false, "Dump full prompt (including spec and context file contents) to stderr; use only in trusted environments")
	f.BoolVar(&flags.preflight, "preflight", true, "Run deterministic preflight checks before LLM review")
	f.StringVar(&flags.preflightMode, "preflight-mode", "warn", "Preflight mode: warn, gate, or only")
	f.StringVar(&flags.preflightProfile, "preflight-profile", "", "Override preflight rule profile")
	f.StringArrayVar(&flags.preflightIgnore, "preflight-ignore", nil, "Preflight rule ID to suppress (may be repeated)")
	f.StringVar(&flags.chunking, "chunking", "auto", "Chunking mode: auto, on, or off")
	f.IntVar(&flags.chunkLines, "chunk-lines", 180, "Target maximum source lines per chunk before overlap")
	f.IntVar(&flags.chunkOverlap, "chunk-overlap", 20, "Neighboring lines included before and after each chunk for context")
	f.IntVar(&flags.chunkMinLines, "chunk-min-lines", 120, "Minimum line count before auto chunking may run")
	f.IntVar(&flags.chunkTokenThreshold, "chunk-token-threshold", 4000, "Estimated prompt-token count before auto chunking may run")
	f.IntVar(&flags.chunkConcurrency, "chunk-concurrency", 3, "Maximum concurrent chunk LLM calls")
	f.IntVar(&flags.synthesisLineThreshold, "synthesis-line-threshold", 240, "Minimum total line count before no-finding chunked review may run synthesis")
	f.StringVar(&flags.incrementalFrom, "incremental-from", "", "Path to previous SpecCritic JSON report for incremental rerun")
	f.StringVar(&flags.incrementalBase, "incremental-base", "", "Path to previous spec text used by --incremental-from when the current spec changed")
	f.StringVar(&flags.incrementalMode, "incremental-mode", "auto", "Incremental mode: auto, on, or off")
	f.Float64Var(&flags.incrementalMaxChangeRatio, "incremental-max-change-ratio", 0.35, "Maximum changed-section line ratio before auto fallback")
	f.Float64Var(&flags.incrementalMaxRemapFailureRatio, "incremental-max-remap-failure-ratio", 0.25, "Maximum prior-finding remap failure ratio before auto fallback")
	f.IntVar(&flags.incrementalContextLines, "incremental-context-lines", 20, "Neighboring unchanged lines included around each incremental review range")
	f.BoolVar(&flags.incrementalStrictReuse, "incremental-strict-reuse", true, "Reuse prior findings only when evidence remaps safely")
	f.BoolVar(&flags.incrementalReport, "incremental-report", false, "Include optional meta.incremental details in JSON output")
	f.StringVar(&flags.convergenceFrom, "convergence-from", "", "Path to previous SpecCritic JSON report for convergence tracking")
	f.StringVar(&flags.convergenceMode, "convergence-mode", "auto", "Convergence mode: auto, on, or off")
	f.BoolVar(&flags.convergenceStrict, "convergence-strict", false, "Require strict convergence compatibility checks")
	f.BoolVar(&flags.convergenceReport, "convergence-report", true, "Include optional meta.convergence details when convergence is requested")
	f.BoolVar(&flags.completionSuggestions, "completion-suggestions", false, "Generate profile-specific advisory completion patches after review")
	f.StringVar(&flags.completionMode, "completion-mode", "auto", "Completion mode: auto, on, or off")
	f.StringVar(&flags.completionTemplate, "completion-template", "profile", "Completion template: profile, general, backend-api, regulated-system, or event-driven")
	f.IntVar(&flags.completionMaxPatches, "completion-max-patches", 8, "Maximum completion patches to emit")
	f.BoolVar(&flags.completionOpenDecisions, "completion-open-decisions", true, "Insert OPEN DECISION placeholders instead of inventing unstated behavior")

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

	result, err := app.NewChecker().Check(cmdContext(), app.CheckRequest{
		Version:                         version,
		SpecPath:                        specPath,
		ContextPaths:                    flags.contextFiles,
		Profile:                         flags.profileName,
		Strict:                          flags.strict,
		SeverityThreshold:               flags.severityThreshold,
		LLMProvider:                     flags.llmProvider,
		LLMModel:                        flags.llmModel,
		Temperature:                     flags.temperature,
		MaxTokens:                       flags.maxTokens,
		Offline:                         flags.offline,
		Debug:                           flags.debug,
		Verbose:                         flags.verbose,
		Preflight:                       flags.preflight,
		PreflightMode:                   flags.preflightMode,
		PreflightProfile:                flags.preflightProfile,
		PreflightIgnore:                 flags.preflightIgnore,
		Chunking:                        flags.chunking,
		ChunkLines:                      flags.chunkLines,
		ChunkOverlap:                    flags.chunkOverlap,
		ChunkMinLines:                   flags.chunkMinLines,
		ChunkTokenThreshold:             flags.chunkTokenThreshold,
		ChunkConcurrency:                flags.chunkConcurrency,
		SynthesisLineThreshold:          flags.synthesisLineThreshold,
		IncrementalFrom:                 flags.incrementalFrom,
		IncrementalBasePath:             flags.incrementalBase,
		IncrementalMode:                 flags.incrementalMode,
		IncrementalMaxChangeRatio:       flags.incrementalMaxChangeRatio,
		IncrementalMaxRemapFailureRatio: flags.incrementalMaxRemapFailureRatio,
		IncrementalContextLines:         flags.incrementalContextLines,
		IncrementalStrictReuse:          flags.incrementalStrictReuse,
		IncrementalReport:               flags.incrementalReport,
		ConvergenceFrom:                 flags.convergenceFrom,
		ConvergenceMode:                 flags.convergenceMode,
		ConvergenceStrict:               flags.convergenceStrict,
		ConvergenceReport:               flags.convergenceReport,
		CompletionSuggestions:           flags.completionSuggestions,
		CompletionMode:                  flags.completionMode,
		CompletionTemplate:              flags.completionTemplate,
		CompletionMaxPatches:            flags.completionMaxPatches,
		CompletionOpenDecisions:         flags.completionOpenDecisions,
		Source:                          app.SourceCLI,
		ErrWriter:                       os.Stderr,
	})
	if err != nil {
		return mapAppError(err)
	}
	report := cloneReport(result.Report)

	// --- Step 14: Apply severity threshold filter (output only, does not affect score/counts) ---
	severityFilter := parseSeverityThreshold(flags.severityThreshold)
	report.Issues = review.FilterBySeverity(report.Issues, severityFilter)
	report.Questions = review.FilterQuestionsBySeverity(report.Questions, severityFilter)

	// --- Step 15: Write patches ---
	if flags.patchOut != "" {
		logVerbose(flags.verbose, "Generating patches → %s", flags.patchOut)
		if err := os.WriteFile(flags.patchOut, []byte(result.PatchDiff), 0o644); err != nil {
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
			_, _ = fmt.Fprintln(os.Stdout)
		}
	}

	// --- Step 18: Evaluate --fail-on ---
	if flags.failOn != "" {
		verdictThreshold := schema.Verdict(flags.failOn)
		verdict := report.Summary.Verdict
		if schema.VerdictOrdinal(verdict) >= schema.VerdictOrdinal(verdictThreshold) {
			return codeError(2, "verdict %s meets or exceeds --fail-on threshold %s", verdict, verdictThreshold)
		}
	}

	return nil
}

func cmdContext() context.Context {
	return context.Background()
}

func mapAppError(err error) error {
	var appErr *app.Error
	if errors.As(err, &appErr) {
		switch appErr.Kind {
		case app.ErrorProvider:
			return codeError(4, "%s", appErr)
		case app.ErrorModelOutput:
			return codeError(5, "%s", appErr)
		default:
			return codeError(3, "%s", appErr)
		}
	}
	return codeError(3, "%s", err)
}

func cloneReport(report *schema.Report) *schema.Report {
	if report == nil {
		return &schema.Report{}
	}
	clone := *report
	clone.Issues = append([]schema.Issue(nil), report.Issues...)
	clone.Questions = append([]schema.Question(nil), report.Questions...)
	clone.Patches = append([]schema.Patch(nil), report.Patches...)
	return &clone
}

// validateFlags returns an error if any flag value is invalid.
func validateFlags(flags checkFlags) error {
	if len(flags.envErrors) > 0 {
		return fmt.Errorf("%s", strings.Join(flags.envErrors, "; "))
	}
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
	if err := chunk.ValidateConfig(chunk.WithDefaults(chunk.Config{
		Mode:                   chunk.Mode(flags.chunking),
		ChunkLines:             flags.chunkLines,
		ChunkOverlap:           flags.chunkOverlap,
		ChunkMinLines:          flags.chunkMinLines,
		ChunkTokenThreshold:    flags.chunkTokenThreshold,
		ChunkConcurrency:       flags.chunkConcurrency,
		SynthesisLineThreshold: flags.synthesisLineThreshold,
	})); err != nil {
		return err
	}
	if flags.incrementalFrom != "" || flags.incrementalMode != "" {
		incrementalCfg := incremental.DefaultConfig()
		if flags.incrementalMode != "" {
			incrementalCfg.Mode = incremental.Mode(flags.incrementalMode)
		}
		incrementalCfg.MaxChangeRatio = flags.incrementalMaxChangeRatio
		incrementalCfg.MaxRemapFailureRatio = flags.incrementalMaxRemapFailureRatio
		incrementalCfg.ContextLines = flags.incrementalContextLines
		if flags.chunkTokenThreshold != 0 {
			incrementalCfg.ChunkTokenThreshold = flags.chunkTokenThreshold
		}
		if err := incremental.ValidateConfig(incrementalCfg); err != nil {
			return err
		}
	}
	if flags.convergenceFrom != "" || flags.convergenceMode != "" {
		convergenceCfg := convergence.DefaultConfig()
		if flags.convergenceMode != "" {
			convergenceCfg.Mode = convergence.Mode(flags.convergenceMode)
		}
		convergenceCfg.Report = flags.convergenceReport
		convergenceCfg.StrictCompatibility = flags.convergenceStrict
		convergenceCfg.SeverityThreshold = flags.severityThreshold
		if err := convergence.ValidateConfig(convergenceCfg); err != nil {
			return err
		}
		if convergenceCfg.Mode == convergence.ModeOn && flags.convergenceFrom == "" {
			return fmt.Errorf("--convergence-from is required when --convergence-mode=on")
		}
	}
	switch flags.preflightMode {
	case "warn", "gate", "only":
	default:
		return fmt.Errorf("--preflight-mode must be warn, gate, or only, got %q", flags.preflightMode)
	}
	switch flags.completionMode {
	case schema.CompletionModeAuto, schema.CompletionModeOn, schema.CompletionModeOff:
	default:
		return fmt.Errorf("--completion-mode must be auto, on, or off, got %q", flags.completionMode)
	}
	if !schema.IsCompletionInputTemplateName(flags.completionTemplate) {
		return fmt.Errorf("--completion-template must be one of %s, got %q", strings.Join(schema.CompletionInputTemplateNames(), ", "), flags.completionTemplate)
	}
	if flags.completionMaxPatches < 0 {
		return fmt.Errorf("--completion-max-patches must be >= 0, got %d", flags.completionMaxPatches)
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

// applyEnvDefaults sets flag values from SPECCRITIC_* environment variables
// for any flag not explicitly provided on the command line.
func applyEnvDefaults(cmd *cobra.Command, flags *checkFlags) {
	envStr := func(flagName, envKey string, dst *string) {
		if !cmd.Flags().Changed(flagName) {
			if v := os.Getenv(envKey); v != "" {
				*dst = v
			}
		}
	}
	envBool := func(flagName, envKey string, dst *bool) {
		if !cmd.Flags().Changed(flagName) {
			if v := os.Getenv(envKey); v != "" {
				b, err := strconv.ParseBool(v)
				if err != nil {
					fmt.Fprintf(os.Stderr, "WARN: invalid value for %s=%q, ignoring: %v\n", envKey, v, err)
					return
				}
				*dst = b
			}
		}
	}
	envFloat64 := func(flagName, envKey string, dst *float64) {
		if !cmd.Flags().Changed(flagName) {
			if v := os.Getenv(envKey); v != "" {
				f, err := strconv.ParseFloat(v, 64)
				if err != nil {
					fmt.Fprintf(os.Stderr, "WARN: invalid value for %s=%q, ignoring: %v\n", envKey, v, err)
					return
				}
				*dst = f
			}
		}
	}
	envInt := func(flagName, envKey string, dst *int) {
		if !cmd.Flags().Changed(flagName) {
			if v := os.Getenv(envKey); v != "" {
				i, err := strconv.Atoi(v)
				if err != nil {
					fmt.Fprintf(os.Stderr, "WARN: invalid value for %s=%q, ignoring: %v\n", envKey, v, err)
					return
				}
				*dst = i
			}
		}
	}
	envBoolStrict := func(flagName, envKey string, dst *bool) {
		if cmd.Flags().Changed(flagName) {
			return
		}
		v := os.Getenv(envKey)
		if v == "" {
			return
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			flags.envErrors = append(flags.envErrors, fmt.Sprintf("%s=%q is invalid: %v", envKey, v, err))
			return
		}
		*dst = b
	}
	envIntStrict := func(flagName, envKey string, dst *int) {
		if cmd.Flags().Changed(flagName) {
			return
		}
		v := os.Getenv(envKey)
		if v == "" {
			return
		}
		i, err := strconv.Atoi(v)
		if err != nil {
			flags.envErrors = append(flags.envErrors, fmt.Sprintf("%s=%q is invalid: %v", envKey, v, err))
			return
		}
		*dst = i
	}
	envStringArray := func(flagName, envKey string, dst *[]string) {
		if cmd.Flags().Changed(flagName) {
			return
		}
		v := os.Getenv(envKey)
		if v == "" {
			return
		}
		var values []string
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				values = append(values, part)
			}
		}
		*dst = values
	}

	envStr("format", "SPECCRITIC_FORMAT", &flags.format)
	envStr("profile", "SPECCRITIC_PROFILE", &flags.profileName)
	envBool("strict", "SPECCRITIC_STRICT", &flags.strict)
	envStr("fail-on", "SPECCRITIC_FAIL_ON", &flags.failOn)
	envStr("severity-threshold", "SPECCRITIC_SEVERITY_THRESHOLD", &flags.severityThreshold)
	if !cmd.Flags().Changed("llm-provider") && !cmd.Flags().Changed("llm-model") {
		envStr("llm-provider", "SPECCRITIC_LLM_PROVIDER", &flags.llmProvider)
		envStr("llm-model", "SPECCRITIC_LLM_MODEL", &flags.llmModel)
	}
	envFloat64("temperature", "SPECCRITIC_LLM_TEMPERATURE", &flags.temperature)
	envInt("max-tokens", "SPECCRITIC_LLM_MAX_TOKENS", &flags.maxTokens)
	envBool("verbose", "SPECCRITIC_VERBOSE", &flags.verbose)
	envBool("debug", "SPECCRITIC_DEBUG", &flags.debug)
	envBool("preflight", "SPECCRITIC_PREFLIGHT", &flags.preflight)
	envStr("preflight-mode", "SPECCRITIC_PREFLIGHT_MODE", &flags.preflightMode)
	envStr("preflight-profile", "SPECCRITIC_PREFLIGHT_PROFILE", &flags.preflightProfile)
	envStringArray("preflight-ignore", "SPECCRITIC_PREFLIGHT_IGNORE", &flags.preflightIgnore)
	envStr("chunking", "SPECCRITIC_CHUNKING", &flags.chunking)
	envInt("chunk-lines", "SPECCRITIC_CHUNK_LINES", &flags.chunkLines)
	envInt("chunk-overlap", "SPECCRITIC_CHUNK_OVERLAP", &flags.chunkOverlap)
	envInt("chunk-min-lines", "SPECCRITIC_CHUNK_MIN_LINES", &flags.chunkMinLines)
	envInt("chunk-token-threshold", "SPECCRITIC_CHUNK_TOKEN_THRESHOLD", &flags.chunkTokenThreshold)
	envInt("chunk-concurrency", "SPECCRITIC_CHUNK_CONCURRENCY", &flags.chunkConcurrency)
	envInt("synthesis-line-threshold", "SPECCRITIC_SYNTHESIS_LINE_THRESHOLD", &flags.synthesisLineThreshold)
	envStr("incremental-from", "SPECCRITIC_INCREMENTAL_FROM", &flags.incrementalFrom)
	envStr("incremental-base", "SPECCRITIC_INCREMENTAL_BASE", &flags.incrementalBase)
	envStr("incremental-mode", "SPECCRITIC_INCREMENTAL_MODE", &flags.incrementalMode)
	envFloat64("incremental-max-change-ratio", "SPECCRITIC_INCREMENTAL_MAX_CHANGE_RATIO", &flags.incrementalMaxChangeRatio)
	envFloat64("incremental-max-remap-failure-ratio", "SPECCRITIC_INCREMENTAL_MAX_REMAP_FAILURE_RATIO", &flags.incrementalMaxRemapFailureRatio)
	envInt("incremental-context-lines", "SPECCRITIC_INCREMENTAL_CONTEXT_LINES", &flags.incrementalContextLines)
	envBool("incremental-strict-reuse", "SPECCRITIC_INCREMENTAL_STRICT_REUSE", &flags.incrementalStrictReuse)
	envBool("incremental-report", "SPECCRITIC_INCREMENTAL_REPORT", &flags.incrementalReport)
	envStr("convergence-from", "SPECCRITIC_CONVERGENCE_FROM", &flags.convergenceFrom)
	envStr("convergence-mode", "SPECCRITIC_CONVERGENCE_MODE", &flags.convergenceMode)
	envBool("convergence-strict", "SPECCRITIC_CONVERGENCE_STRICT", &flags.convergenceStrict)
	envBool("convergence-report", "SPECCRITIC_CONVERGENCE_REPORT", &flags.convergenceReport)
	envBoolStrict("completion-suggestions", "SPECCRITIC_COMPLETION_SUGGESTIONS", &flags.completionSuggestions)
	envStr("completion-mode", "SPECCRITIC_COMPLETION_MODE", &flags.completionMode)
	envStr("completion-template", "SPECCRITIC_COMPLETION_TEMPLATE", &flags.completionTemplate)
	envIntStrict("completion-max-patches", "SPECCRITIC_COMPLETION_MAX_PATCHES", &flags.completionMaxPatches)
	envBoolStrict("completion-open-decisions", "SPECCRITIC_COMPLETION_OPEN_DECISIONS", &flags.completionOpenDecisions)
}

// logVerbose writes a timestamped message to stderr when verbose mode is enabled.
func logVerbose(verbose bool, format string, args ...any) {
	if verbose {
		fmt.Fprintf(os.Stderr, "INFO: "+format+"\n", args...)
	}
}
