package llm

import (
	"os"
	"strings"
	"testing"

	ctx "github.com/dshills/speccritic/internal/context"
	"github.com/dshills/speccritic/internal/profile"
	"github.com/dshills/speccritic/internal/spec"
)

func writeTempSpec(t *testing.T, content string) *spec.Spec {
	t.Helper()
	f, err := os.CreateTemp("", "spec*.md")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	s, err := spec.Load(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBuildUserPrompt_ContainsLineNumberedSpec(t *testing.T) {
	s := writeTempSpec(t, "line one\nline two\n")
	prompt := BuildUserPrompt(s, nil)

	if !strings.Contains(prompt, "L1: line one") {
		t.Errorf("prompt missing line-numbered spec content: %q", prompt)
	}
	if !strings.Contains(prompt, "L2: line two") {
		t.Errorf("prompt missing L2: %q", prompt)
	}
}

func TestBuildUserPrompt_ContainsContextXMLTags(t *testing.T) {
	s := writeTempSpec(t, "spec content\n")
	files := []ctx.ContextFile{
		{Path: "glossary.md", Content: "term: definition\n"},
	}
	prompt := BuildUserPrompt(s, files)

	if !strings.Contains(prompt, `<context file="glossary.md">`) {
		t.Errorf("prompt missing context XML tag: %q", prompt)
	}
	if !strings.Contains(prompt, "term: definition") {
		t.Errorf("prompt missing context content: %q", prompt)
	}
}

func TestBuildUserPrompt_NoContextFiles_NoXMLTags(t *testing.T) {
	s := writeTempSpec(t, "spec content\n")
	prompt := BuildUserPrompt(s, nil)

	if strings.Contains(prompt, "<context") {
		t.Errorf("prompt should not contain context tags when no context files: %q", prompt)
	}
}

func TestBuildSystemPrompt_ContainsProfileRules(t *testing.T) {
	p, err := profile.Get("backend-api")
	if err != nil {
		t.Fatalf("profile.Get: %v", err)
	}
	sys := BuildSystemPrompt(p, false)

	// Check that the profile's FormatRulesForPrompt output is included.
	rules := p.FormatRulesForPrompt()
	if !strings.Contains(sys, rules) {
		t.Errorf("system prompt does not contain profile rules output")
	}
}

func TestBuildSystemPrompt_StrictModeInjected(t *testing.T) {
	p, err := profile.Get("general")
	if err != nil {
		t.Fatalf("profile.Get: %v", err)
	}
	sys := BuildSystemPrompt(p, true)

	if !strings.Contains(sys, "STRICT MODE ENABLED") {
		t.Errorf("system prompt missing strict mode text: %q", sys)
	}
}

func TestBuildSystemPrompt_NoStrictMode(t *testing.T) {
	p, err := profile.Get("general")
	if err != nil {
		t.Fatalf("profile.Get: %v", err)
	}
	sys := BuildSystemPrompt(p, false)

	if strings.Contains(sys, "STRICT MODE ENABLED") {
		t.Errorf("system prompt should not contain strict mode text when not enabled: %q", sys)
	}
}

func TestNewProvider_UnknownPrefix(t *testing.T) {
	_, err := NewProvider("gemini:gemini-pro")
	if err == nil {
		t.Error("expected error for unknown provider prefix, got nil")
	}
}

func TestNewProvider_InvalidFormat(t *testing.T) {
	_, err := NewProvider("nocoIon")
	if err == nil {
		t.Error("expected error for missing colon separator, got nil")
	}
}

func TestNewProvider_Anthropic_NoKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, err := NewProvider("anthropic:claude-sonnet-4-6")
	if err == nil {
		t.Error("expected error when ANTHROPIC_API_KEY not set, got nil")
	}
}

func TestNewProvider_OpenAI_NoKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	_, err := NewProvider("openai:gpt-4o")
	if err == nil {
		t.Error("expected error when OPENAI_API_KEY not set, got nil")
	}
}

func TestNewProvider_Anthropic_WithKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-key-for-construction-only")
	p, err := NewProvider("anthropic:claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if p == nil {
		t.Error("expected non-nil provider")
	}
}

func TestNewProvider_OpenAI_WithKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test-key-for-construction-only")
	p, err := NewProvider("openai:gpt-4o")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if p == nil {
		t.Error("expected non-nil provider")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate short string: got %q", got)
	}
	if got := truncate("hello world", 5); got != "hello..." {
		t.Errorf("truncate long string: got %q", got)
	}
	// Multi-byte: é is 2 bytes but 1 rune; truncating at 3 runes should not cut mid-codepoint.
	if got := truncate("héllo", 3); got != "hél..." {
		t.Errorf("truncate multibyte: got %q, want %q", got, "hél...")
	}
}
