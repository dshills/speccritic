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
	format                 string
	out                    string
	contextFiles           []string
	profileName            string
	strict                 bool
	failOn                 string
	severityThreshold      string
	patchOut               string
	temperature            float64
	maxTokens              int
	offline                bool
	verbose                bool
	debug                  bool
	preflight              bool
	preflightMode          string
	preflightProfile       string
	preflightIgnore        []string
	chunking               string
	chunkLines             int
	chunkOverlap           int
	chunkMinLines          int
	chunkTokenThreshold    int
	chunkConcurrency       int
	synthesisLineThreshold int
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
	f.Float64Var(&flags.temperature, "temperature", 0.2, "LLM temperature")
	f.IntVar(&flags.maxTokens, "max-tokens", 16384, "Maximum response tokens")
	f.BoolVar(&flags.offline, "offline", false, "Exit 3 if SPECCRITIC_MODEL env var is not set; use to enforce explicit model config in CI")
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
		Version:                version,
		SpecPath:               specPath,
		ContextPaths:           flags.contextFiles,
		Profile:                flags.profileName,
		Strict:                 flags.strict,
		SeverityThreshold:      flags.severityThreshold,
		Temperature:            flags.temperature,
		MaxTokens:              flags.maxTokens,
		Offline:                flags.offline,
		Debug:                  flags.debug,
		Verbose:                flags.verbose,
		Preflight:              flags.preflight,
		PreflightMode:          flags.preflightMode,
		PreflightProfile:       flags.preflightProfile,
		PreflightIgnore:        flags.preflightIgnore,
		Chunking:               flags.chunking,
		ChunkLines:             flags.chunkLines,
		ChunkOverlap:           flags.chunkOverlap,
		ChunkMinLines:          flags.chunkMinLines,
		ChunkTokenThreshold:    flags.chunkTokenThreshold,
		ChunkConcurrency:       flags.chunkConcurrency,
		SynthesisLineThreshold: flags.synthesisLineThreshold,
		Source:                 app.SourceCLI,
		ErrWriter:              os.Stderr,
	})
	if err != nil {
		return mapAppError(err)
	}
	report := cloneReport(result.Report)

	// --- Step 14: Apply severity threshold filter (output only, does not affect score/counts) ---
	severityFilter := parseSeverityThreshold(flags.severityThreshold)
	report.Issues = review.FilterBySeverity(report.Issues, severityFilter)

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
	switch flags.preflightMode {
	case "warn", "gate", "only":
	default:
		return fmt.Errorf("--preflight-mode must be warn, gate, or only, got %q", flags.preflightMode)
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
}

// logVerbose writes a timestamped message to stderr when verbose mode is enabled.
func logVerbose(verbose bool, format string, args ...any) {
	if verbose {
		fmt.Fprintf(os.Stderr, "INFO: "+format+"\n", args...)
	}
}
