package convergence

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/schema/validate"
)

// DefaultConfig returns the spec-defined convergence defaults.
func DefaultConfig() Config {
	return Config{
		Mode:                  ModeAuto,
		Report:                false,
		CurrentReviewCoverage: CoverageFull,
	}
}

// ParseMode validates and normalizes a convergence mode string.
func ParseMode(raw string) (Mode, error) {
	switch Mode(strings.ToLower(strings.TrimSpace(raw))) {
	case "", ModeAuto:
		return ModeAuto, nil
	case ModeOn:
		return ModeOn, nil
	case ModeOff:
		return ModeOff, nil
	default:
		return "", fmt.Errorf("invalid convergence mode %q", raw)
	}
}

// ValidateConfig validates flag-derived convergence settings.
func ValidateConfig(cfg Config) error {
	mode := cfg.Mode
	if mode == "" {
		mode = ModeAuto
	}
	switch mode {
	case ModeAuto, ModeOn, ModeOff:
	default:
		return fmt.Errorf("invalid convergence mode %q", cfg.Mode)
	}
	if cfg.SeverityThreshold != "" && !validSeverityThreshold(cfg.SeverityThreshold) {
		return fmt.Errorf("invalid convergence severity threshold %q", cfg.SeverityThreshold)
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
	if _, ok := severityRank(report.Input.SeverityThreshold); !ok {
		return nil, fmt.Errorf("previous report input.severity_threshold is invalid: %q", report.Input.SeverityThreshold)
	}
	extra, err := parseExtraMeta(raw)
	if err != nil {
		return nil, err
	}
	return &PreviousReport{
		Report:              report,
		RedactionConfigHash: extra.RedactionConfigHash,
	}, nil
}

type extraMeta struct {
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
	if err := readString(envelope.Meta, "redaction_config_hash", &out.RedactionConfigHash); err != nil {
		return extraMeta{}, err
	}
	return out, nil
}

func readString(m map[string]json.RawMessage, key string, dst *string) error {
	if len(m) == 0 {
		return nil
	}
	raw, ok := m[key]
	if !ok {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("meta.%s must be a string: %w", key, err)
	}
	*dst = s
	return nil
}

// CheckCompatibility compares previous-report metadata with the current
// convergence request. It returns structured status for auto-mode callers.
func CheckCompatibility(prev *PreviousReport, cfg Config) Compatibility {
	if prev == nil || prev.Report == nil {
		return unavailable(fmt.Errorf("previous report is required"))
	}
	if cfg.Mode == ModeOff {
		return Compatibility{Status: StatusUnavailable, Notes: []string{"convergence mode is off"}}
	}
	report := prev.Report
	var notes []string
	status := StatusComplete
	if cfg.StrictCompatibility && report.Input.Profile != cfg.Profile {
		return incompatible(cfg, fmt.Errorf("previous report profile %q does not match current profile %q", report.Input.Profile, cfg.Profile))
	}
	if !cfg.StrictCompatibility && cfg.Profile != "" && report.Input.Profile != cfg.Profile {
		status = StatusPartial
		notes = append(notes, "previous report profile differs from current profile")
	}
	if cfg.StrictCompatibility && report.Input.Strict != cfg.ReviewStrict {
		return incompatible(cfg, fmt.Errorf("previous report strict mode %t does not match current strict mode %t", report.Input.Strict, cfg.ReviewStrict))
	}
	if !cfg.StrictCompatibility && report.Input.Strict != cfg.ReviewStrict {
		status = StatusPartial
		notes = append(notes, "previous report strict mode differs from current strict mode")
	}
	if cfg.SeverityThreshold != "" {
		prevRank, prevOK := severityRank(report.Input.SeverityThreshold)
		curRank, curOK := severityRank(cfg.SeverityThreshold)
		if !prevOK || !curOK {
			return incompatible(cfg, fmt.Errorf("severity threshold is invalid"))
		}
		if prevRank > curRank {
			status = StatusPartial
			notes = append(notes, "previous severity threshold was more strict than current threshold")
		}
	}
	if cfg.RedactionConfigHash != "" && prev.RedactionConfigHash != "" && cfg.RedactionConfigHash != prev.RedactionConfigHash {
		if cfg.Mode == ModeOn {
			return incompatible(cfg, fmt.Errorf("previous report redaction config hash differs from current redaction config hash"))
		}
		status = StatusPartial
		notes = append(notes, "previous report redaction config hash differs from current redaction config hash")
	}
	return Compatibility{Status: status, Notes: notes}
}

func incompatible(cfg Config, err error) Compatibility {
	if cfg.Mode == ModeOn {
		return Compatibility{Status: StatusUnavailable, Err: err}
	}
	return Compatibility{Status: StatusUnavailable, Notes: []string{err.Error()}, Err: err}
}

func unavailable(err error) Compatibility {
	return Compatibility{Status: StatusUnavailable, Notes: []string{err.Error()}, Err: err}
}

func severityRank(raw string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "info", strings.ToLower(string(schema.SeverityInfo)):
		return 0, true
	case "warn", strings.ToLower(string(schema.SeverityWarn)):
		return 1, true
	case "critical", strings.ToLower(string(schema.SeverityCritical)):
		return 2, true
	default:
		return 0, false
	}
}

func validSeverityThreshold(raw string) bool {
	_, ok := severityRank(raw)
	return ok
}
