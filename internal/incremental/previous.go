package incremental

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/schema/validate"
)

const (
	defaultMaxChangeRatio       = 0.35
	defaultMaxRemapFailureRatio = 0.25
	defaultContextLines         = 20
)

// DefaultConfig returns the spec-defined incremental defaults.
func DefaultConfig() Config {
	return Config{
		Mode:                 ModeAuto,
		MaxChangeRatio:       defaultMaxChangeRatio,
		MaxRemapFailureRatio: defaultMaxRemapFailureRatio,
		ContextLines:         defaultContextLines,
		StrictReuse:          true,
	}
}

// ParseMode validates and normalizes an incremental mode string.
func ParseMode(raw string) (Mode, error) {
	switch Mode(strings.ToLower(strings.TrimSpace(raw))) {
	case "", ModeAuto:
		return ModeAuto, nil
	case ModeOn:
		return ModeOn, nil
	case ModeOff:
		return ModeOff, nil
	default:
		return "", fmt.Errorf("invalid incremental mode %q", raw)
	}
}

// ValidateConfig validates flag-derived incremental settings.
func ValidateConfig(cfg Config) error {
	mode := cfg.Mode
	if mode == "" {
		mode = ModeAuto
	}
	switch mode {
	case ModeAuto, ModeOn, ModeOff:
	default:
		return fmt.Errorf("invalid incremental mode %q", cfg.Mode)
	}
	if cfg.MaxChangeRatio <= 0 || cfg.MaxChangeRatio > 1 {
		return fmt.Errorf("--incremental-max-change-ratio must be > 0 and <= 1, got %g", cfg.MaxChangeRatio)
	}
	if cfg.MaxRemapFailureRatio < 0 || cfg.MaxRemapFailureRatio > 1 {
		return fmt.Errorf("--incremental-max-remap-failure-ratio must be >= 0 and <= 1, got %g", cfg.MaxRemapFailureRatio)
	}
	if cfg.ContextLines < 0 {
		return fmt.Errorf("--incremental-context-lines must be >= 0, got %d", cfg.ContextLines)
	}
	return nil
}

// LoadPreviousReport loads and validates a previous SpecCritic JSON report.
func LoadPreviousReport(path string) (*PreviousReport, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParsePreviousReport(raw)
}

// ParsePreviousReport validates raw JSON from a previous SpecCritic report.
func ParsePreviousReport(raw []byte) (*PreviousReport, error) {
	report, err := validate.Parse(string(raw), 0)
	if err != nil {
		return nil, err
	}
	if report.Tool != "speccritic" {
		return nil, fmt.Errorf("previous report tool must be %q, got %q", "speccritic", report.Tool)
	}
	if report.Version == "" {
		return nil, fmt.Errorf("previous report version is required")
	}
	if report.Input.SpecHash == "" {
		return nil, fmt.Errorf("previous report input.spec_hash is required")
	}
	if report.Input.Profile == "" {
		return nil, fmt.Errorf("previous report input.profile is required")
	}
	if report.Input.SeverityThreshold == "" {
		return nil, fmt.Errorf("previous report input.severity_threshold is required")
	}
	extra, err := parseExtraMeta(raw)
	if err != nil {
		return nil, err
	}
	return &PreviousReport{
		Report:              report,
		ProfileHash:         extra.ProfileHash,
		RedactionConfigHash: extra.RedactionConfigHash,
	}, nil
}

type extraMeta struct {
	ProfileHash         string
	RedactionConfigHash string
}

func parseExtraMeta(raw []byte) (extraMeta, error) {
	var envelope struct {
		Meta map[string]json.RawMessage `json:"meta"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return extraMeta{}, err
	}
	var out extraMeta
	readString(envelope.Meta, "profile_hash", &out.ProfileHash)
	readString(envelope.Meta, "redaction_config_hash", &out.RedactionConfigHash)
	return out, nil
}

func readString(m map[string]json.RawMessage, key string, dst *string) {
	if len(m) == 0 {
		return
	}
	raw, ok := m[key]
	if !ok {
		return
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		*dst = s
	}
}

// ValidatePreviousCompatibility checks whether a previous report can be used
// as the baseline for the current incremental request.
func ValidatePreviousCompatibility(prev *PreviousReport, cfg Config) error {
	if prev == nil || prev.Report == nil {
		return fmt.Errorf("previous report is required")
	}
	report := prev.Report
	if report.Input.Profile != cfg.Profile {
		return fmt.Errorf("previous report profile %q does not match current profile %q", report.Input.Profile, cfg.Profile)
	}
	if report.Input.Strict != cfg.Strict {
		return fmt.Errorf("previous report strict mode %t does not match current strict mode %t", report.Input.Strict, cfg.Strict)
	}
	if cfg.ProfileHash != "" {
		if prev.ProfileHash == "" {
			return fmt.Errorf("previous report profile hash is missing")
		}
		if cfg.ProfileHash != prev.ProfileHash {
			return fmt.Errorf("previous report profile hash differs from current profile hash")
		}
	}
	if cfg.RedactionConfigHash != "" {
		if prev.RedactionConfigHash == "" {
			return fmt.Errorf("previous report redaction config hash is missing")
		}
		if cfg.RedactionConfigHash != prev.RedactionConfigHash {
			return fmt.Errorf("previous report redaction config hash differs from current redaction config hash")
		}
	}
	return validateSeverityThresholdTransition(report.Input.SeverityThreshold, cfg.SeverityThreshold)
}

func validateSeverityThresholdTransition(previous, current string) error {
	prev, ok := severityRank(previous)
	if !ok {
		return fmt.Errorf("previous report has invalid severity threshold %q", previous)
	}
	cur, ok := severityRank(current)
	if !ok {
		return fmt.Errorf("current severity threshold %q is invalid for incremental reuse", current)
	}
	if cur < prev {
		return fmt.Errorf("current severity threshold %q is less strict than previous threshold %q", current, previous)
	}
	return nil
}

func severityRank(raw string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "info", string(schema.SeverityInfo):
		return 0, true
	case "warn", string(schema.SeverityWarn):
		return 1, true
	case "critical", string(schema.SeverityCritical):
		return 2, true
	default:
		return 0, false
	}
}
