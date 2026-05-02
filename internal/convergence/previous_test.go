package convergence

import (
	"strings"
	"testing"
)

func TestParsePreviousReportValid(t *testing.T) {
	prev, err := ParsePreviousReport([]byte(validPreviousReportJSON()))
	if err != nil {
		t.Fatalf("ParsePreviousReport() error = %v", err)
	}
	if prev.Report.Input.SpecHash != "sha256:old" {
		t.Fatalf("spec hash = %q", prev.Report.Input.SpecHash)
	}
}

func TestParsePreviousReportRejectsInvalidJSON(t *testing.T) {
	_, err := ParsePreviousReport([]byte(`{`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParsePreviousReportRejectsWrongTool(t *testing.T) {
	raw := strings.Replace(validPreviousReportJSON(), `"tool":"speccritic"`, `"tool":"other"`, 1)
	_, err := ParsePreviousReport([]byte(raw))
	if err == nil || !strings.Contains(err.Error(), "tool") {
		t.Fatalf("error = %v", err)
	}
}

func TestParsePreviousReportRejectsMissingSpecHash(t *testing.T) {
	raw := strings.Replace(validPreviousReportJSON(), `"spec_hash":"sha256:old"`, `"spec_hash":""`, 1)
	_, err := ParsePreviousReport([]byte(raw))
	if err == nil || !strings.Contains(err.Error(), "spec_hash") {
		t.Fatalf("error = %v", err)
	}
}

func TestParsePreviousReportRejectsInvalidFinding(t *testing.T) {
	raw := strings.Replace(validPreviousReportJSON(), `"id":"ISSUE-0001"`, `"id":"BAD-1"`, 1)
	_, err := ParsePreviousReport([]byte(raw))
	if err == nil || !strings.Contains(err.Error(), "ISSUE-XXXX") {
		t.Fatalf("error = %v", err)
	}
}

func TestCheckCompatibilityComplete(t *testing.T) {
	prev, err := ParsePreviousReport([]byte(validPreviousReportJSON()))
	if err != nil {
		t.Fatal(err)
	}
	compat := CheckCompatibility(prev, Config{
		Mode:              ModeAuto,
		Profile:           "general",
		ReviewStrict:      false,
		SeverityThreshold: "info",
	})
	if compat.Status != StatusComplete || compat.Err != nil {
		t.Fatalf("compat = %#v", compat)
	}
}

func TestCheckCompatibilityPartialForThresholdExpansion(t *testing.T) {
	raw := strings.Replace(validPreviousReportJSON(), `"severity_threshold":"info"`, `"severity_threshold":"critical"`, 1)
	prev, err := ParsePreviousReport([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	compat := CheckCompatibility(prev, Config{
		Mode:              ModeAuto,
		Profile:           "general",
		SeverityThreshold: "info",
	})
	if compat.Status != StatusPartial || len(compat.Notes) == 0 {
		t.Fatalf("compat = %#v", compat)
	}
}

func TestCheckCompatibilityStrictProfileMismatchErrorsInOnMode(t *testing.T) {
	prev, err := ParsePreviousReport([]byte(validPreviousReportJSON()))
	if err != nil {
		t.Fatal(err)
	}
	compat := CheckCompatibility(prev, Config{
		Mode:                ModeOn,
		StrictCompatibility: true,
		Profile:             "backend-api",
		SeverityThreshold:   "info",
	})
	if compat.Status != StatusUnavailable || compat.Err == nil {
		t.Fatalf("compat = %#v", compat)
	}
}

func TestValidateConfigRejectsBadMode(t *testing.T) {
	err := ValidateConfig(Config{Mode: Mode("bad")})
	if err == nil || !strings.Contains(err.Error(), "convergence mode") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateConfigRejectsBadSeverityThreshold(t *testing.T) {
	err := ValidateConfig(Config{SeverityThreshold: "verbose"})
	if err == nil || !strings.Contains(err.Error(), "severity threshold") {
		t.Fatalf("error = %v", err)
	}
}

func validPreviousReportJSON() string {
	return `{
		"tool":"speccritic",
		"version":"dev",
		"input":{
			"spec_file":"SPEC.md",
			"spec_hash":"sha256:old",
			"context_files":[],
			"profile":"general",
			"strict":false,
			"severity_threshold":"info"
		},
		"summary":{
			"verdict":"INVALID",
			"score":80,
			"critical_count":1,
			"warn_count":0,
			"info_count":0
		},
		"issues":[{
			"id":"ISSUE-0001",
			"severity":"CRITICAL",
			"category":"AMBIGUOUS_BEHAVIOR",
			"title":"Undefined behavior",
			"description":"Behavior is undefined.",
			"evidence":[{"path":"SPEC.md","line_start":1,"line_end":1,"quote":"TBD"}],
			"impact":"Implementers will guess.",
			"recommendation":"Define the behavior.",
			"blocking":true,
			"tags":[]
		}],
		"questions":[{
			"id":"Q-0001",
			"severity":"WARN",
			"question":"What happens on failure?",
			"why_needed":"Failure behavior is required.",
			"blocks":["ISSUE-0001"],
			"evidence":[{"path":"SPEC.md","line_start":1,"line_end":1,"quote":"TBD"}]
		}],
		"patches":[],
		"meta":{"model":"test:model","temperature":0.2}
	}`
}
