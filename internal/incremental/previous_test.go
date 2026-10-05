package incremental

import (
	"strings"
	"testing"
)

const validPreviousReport = `{
  "tool": "speccritic",
  "version": "1.0",
  "input": {
    "spec_file": "SPEC.md",
    "spec_hash": "abc123",
    "context_files": [],
    "profile": "general",
    "strict": true,
    "severity_threshold": "info"
  },
  "summary": {
    "verdict": "INVALID",
    "score": 80,
    "critical_count": 1,
    "warn_count": 0,
    "info_count": 0
  },
  "issues": [
    {
      "id": "ISSUE-0001",
      "severity": "CRITICAL",
      "category": "UNSPECIFIED_CONSTRAINT",
      "title": "Missing behavior",
      "description": "desc",
      "evidence": [{"path": "SPEC.md", "line_start": 1, "line_end": 1, "quote": "q"}],
      "impact": "impact",
      "recommendation": "rec",
      "blocking": true,
      "tags": []
    }
  ],
  "questions": [
    {
      "id": "Q-0001",
      "severity": "WARN",
      "question": "What is the timeout?",
      "why_needed": "Needed for tests",
      "blocks": [],
      "evidence": [{"path": "SPEC.md", "line_start": 2, "line_end": 2, "quote": "q"}]
    }
  ],
  "patches": [],
  "meta": {
    "model": "openai:gpt-5",
    "temperature": 0.2,
    "profile_hash": "profile-a",
    "redaction_config_hash": "redact-a"
  }
}`

func TestParsePreviousReportValid(t *testing.T) {
	prev, err := ParsePreviousReport([]byte(validPreviousReport))
	if err != nil {
		t.Fatalf("ParsePreviousReport: %v", err)
	}
	if prev.Report.Input.SpecHash != "abc123" {
		t.Fatalf("spec hash = %q", prev.Report.Input.SpecHash)
	}
	if prev.ProfileHash != "profile-a" || prev.RedactionConfigHash != "redact-a" {
		t.Fatalf("extra meta = %#v", prev)
	}
}

func TestParsePreviousReportInvalidJSON(t *testing.T) {
	if _, err := ParsePreviousReport([]byte("{bad json")); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

func TestParsePreviousReportRequiresSpecCriticTool(t *testing.T) {
	raw := strings.Replace(validPreviousReport, `"speccritic"`, `"other"`, 1)
	if _, err := ParsePreviousReport([]byte(raw)); err == nil {
		t.Fatal("expected tool mismatch error")
	}
}

func TestParsePreviousReportRejectsInvalidID(t *testing.T) {
	raw := strings.Replace(validPreviousReport, `"ISSUE-0001"`, `"uuid-1"`, 1)
	if _, err := ParsePreviousReport([]byte(raw)); err == nil {
		t.Fatal("expected invalid ID error")
	}
}

func TestParsePreviousReportAcceptsPreflightFindings(t *testing.T) {
	// A single-call review keeps preflight findings under their rule IDs, and
	// older reports give them the spec path as it was passed on the command line.
	preflight := `{
      "id": "PREFLIGHT-STRUCTURE-001",
      "severity": "CRITICAL",
      "category": "UNSPECIFIED_CONSTRAINT",
      "title": "Missing purpose",
      "description": "desc",
      "evidence": [{"path": "/work/specs/SPEC.md", "line_start": 1, "line_end": 1, "quote": "q"}],
      "impact": "impact",
      "recommendation": "rec",
      "blocking": true,
      "tags": ["preflight", "preflight-rule:PREFLIGHT-STRUCTURE-001"]
    },
    `
	raw := strings.Replace(validPreviousReport, `"issues": [`, `"issues": [`+preflight, 1)
	raw = strings.Replace(raw, `"patches": []`, `"patches": [{"issue_id": "PREFLIGHT-STRUCTURE-001", "before": "q", "after": "r"}]`, 1)
	prev, err := ParsePreviousReport([]byte(raw))
	if err != nil {
		t.Fatalf("ParsePreviousReport: %v", err)
	}
	if len(prev.Report.Issues) != 2 {
		t.Fatalf("issues = %d, want 2", len(prev.Report.Issues))
	}
}

func TestValidatePreviousCompatibilityRejectsPreflightOnlyReport(t *testing.T) {
	raw := strings.Replace(validPreviousReport, `"model": "openai:gpt-5"`, `"model": "preflight"`, 1)
	prev, err := ParsePreviousReport([]byte(raw))
	if err != nil {
		t.Fatalf("ParsePreviousReport: %v", err)
	}
	cfg := DefaultConfig()
	cfg.Profile = "general"
	cfg.Strict = true
	cfg.SeverityThreshold = "info"
	err = ValidatePreviousCompatibility(prev, cfg)
	if err == nil || !strings.Contains(err.Error(), "no model review to reuse") {
		t.Fatalf("error = %v, want a preflight-only baseline rejected", err)
	}
}

func TestValidatePreviousCompatibility(t *testing.T) {
	prev, err := ParsePreviousReport([]byte(validPreviousReport))
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Profile = "general"
	cfg.Strict = true
	cfg.SeverityThreshold = "warn"
	cfg.ProfileHash = "profile-a"
	cfg.RedactionConfigHash = "redact-a"
	if err := ValidatePreviousCompatibility(prev, cfg); err != nil {
		t.Fatalf("ValidatePreviousCompatibility: %v", err)
	}
}

func TestValidatePreviousCompatibilityProfileMismatch(t *testing.T) {
	prev, err := ParsePreviousReport([]byte(validPreviousReport))
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Profile = "backend-api"
	cfg.Strict = true
	cfg.SeverityThreshold = "info"
	if err := ValidatePreviousCompatibility(prev, cfg); err == nil {
		t.Fatal("expected profile mismatch")
	}
}

func TestValidatePreviousCompatibilityStrictMismatch(t *testing.T) {
	prev, err := ParsePreviousReport([]byte(validPreviousReport))
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Profile = "general"
	cfg.Strict = false
	cfg.SeverityThreshold = "info"
	if err := ValidatePreviousCompatibility(prev, cfg); err == nil {
		t.Fatal("expected strict mismatch")
	}
}

func TestValidatePreviousCompatibilityRejectsLessStrictThreshold(t *testing.T) {
	prev, err := ParsePreviousReport([]byte(strings.Replace(validPreviousReport, `"severity_threshold": "info"`, `"severity_threshold": "warn"`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Profile = "general"
	cfg.Strict = true
	cfg.SeverityThreshold = "info"
	if err := ValidatePreviousCompatibility(prev, cfg); err == nil {
		t.Fatal("expected less strict threshold rejection")
	}
}

func TestValidatePreviousCompatibilityRequiresConfiguredHashes(t *testing.T) {
	raw := strings.Replace(validPreviousReport, `    "profile_hash": "profile-a",
    "redaction_config_hash": "redact-a"`, `    "model": "openai:gpt-5"`, 1)
	prev, err := ParsePreviousReport([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Profile = "general"
	cfg.Strict = true
	cfg.SeverityThreshold = "info"
	cfg.ProfileHash = "profile-a"
	if err := ValidatePreviousCompatibility(prev, cfg); err == nil {
		t.Fatal("expected missing profile hash rejection")
	}
}

func TestValidateConfig(t *testing.T) {
	cfg := DefaultConfig()
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("default config: %v", err)
	}
	cfg.MaxRemapFailureRatio = 2
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("expected invalid remap ratio")
	}
}

func TestParseMode(t *testing.T) {
	mode, err := ParseMode("ON")
	if err != nil {
		t.Fatalf("ParseMode: %v", err)
	}
	if mode != ModeOn {
		t.Fatalf("mode = %q", mode)
	}
	if _, err := ParseMode("sometimes"); err == nil {
		t.Fatal("expected invalid mode")
	}
}
