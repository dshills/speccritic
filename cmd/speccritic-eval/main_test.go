package main

import (
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/eval"
	"github.com/dshills/speccritic/pkg/speccritic"
)

func TestFilterCases(t *testing.T) {
	cases := []eval.Case{{ID: "rate-limiter-clean"}, {ID: "rate-limiter-seeded"}, {ID: "job-queue-clean"}}
	tests := map[string]struct {
		fragments string
		want      string
	}{
		"no filter":     {"", "rate-limiter-clean,rate-limiter-seeded,job-queue-clean"},
		"one fragment":  {"seeded", "rate-limiter-seeded"},
		"two fragments": {"job, seeded ,", "rate-limiter-seeded,job-queue-clean"},
		"no match":      {"missing", ""},
	}
	for name, tc := range tests {
		var ids []string
		for _, c := range filterCases(cases, tc.fragments) {
			ids = append(ids, c.ID)
		}
		if got := strings.Join(ids, ","); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestDescribeModel(t *testing.T) {
	tests := map[string]struct {
		envProvider, envModel string
		opts                  speccritic.CheckOptions
		want                  string
	}{
		"flags win over the environment":   {"openai", "gpt-4o", speccritic.CheckOptions{LLMProvider: "gemini", LLMModel: "gemini-3.8-flash"}, "gemini:gemini-3.8-flash"},
		"environment":                      {"openai", "gpt-6.1-sol", speccritic.CheckOptions{}, "openai:gpt-6.1-sol"},
		"provider inferred from the model": {"", "", speccritic.CheckOptions{LLMModel: "gpt-6.1-sol"}, "openai:gpt-6.1-sol"},
		"provider default model":           {"gemini", "", speccritic.CheckOptions{}, "gemini:" + speccritic.DefaultModelForProvider("gemini")},
		"nothing configured":               {"", "", speccritic.CheckOptions{}, "anthropic:" + speccritic.DefaultModelForProvider("anthropic")},
		"effort is shown":                  {"", "", speccritic.CheckOptions{LLMModel: "claude-opus-5-5", Effort: "high"}, "anthropic:claude-opus-5-5, effort high"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Setenv("SPECCRITIC_LLM_PROVIDER", tc.envProvider)
			t.Setenv("SPECCRITIC_LLM_MODEL", tc.envModel)
			if got := describeModel(tc.opts); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// The eval must review with the CLI's own defaults, or it measures a setup
// nobody runs.
func TestCheckOptionsStartFromTheShippedDefaults(t *testing.T) {
	defaults := speccritic.DefaultCheckOptions()
	got := options{}.checkOptions()
	if got.MaxTokens != defaults.MaxTokens || got.Chunking != defaults.Chunking || got.ChunkLines != defaults.ChunkLines ||
		got.ChunkMinLines != defaults.ChunkMinLines || got.Preflight != defaults.Preflight || got.SeverityThreshold != "info" {
		t.Errorf("options without overrides = %+v, want the shipped defaults", got)
	}
	overridden := options{maxTokens: 999, chunking: "off", chunkLines: 50, chunkMinLines: 60, effort: "low", model: "m"}.checkOptions()
	if overridden.MaxTokens != 999 || overridden.Chunking != "off" || overridden.ChunkLines != 50 || overridden.ChunkMinLines != 60 || overridden.Effort != "low" || overridden.LLMModel != "m" {
		t.Errorf("overrides were not applied: %+v", overridden)
	}
}
