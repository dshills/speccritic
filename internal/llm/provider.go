package llm

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// sharedHTTPClient is used by all providers; a 5-minute timeout covers slow LLM responses.
var sharedHTTPClient = &http.Client{
	Timeout: 5 * time.Minute,
}

// defaultMaxTokens is the fallback when Request.MaxTokens is not set.
const defaultMaxTokens = 16384

const (
	// DefaultProvider and DefaultModel are used when model configuration is omitted.
	DefaultProvider = "anthropic"
	DefaultModel    = "claude-sonnet-4-20250514"
)

// Request holds the parameters for an LLM completion call.
//
// UserPromptCachedPrefix is an optional stable prefix prepended to the user
// message. When set, the Anthropic provider attaches a cache_control
// breakpoint to it; OpenAI and Gemini benefit via automatic prefix caching
// as long as the bytes are stable across calls.
type Request struct {
	SystemPrompt           string
	UserPromptCachedPrefix string
	UserPrompt             string
	// Temperature is a pointer so callers can distinguish "unset" (nil,
	// provider default) from an explicit value — including 0.0, which
	// callers use to request deterministic output.
	Temperature *float64
	MaxTokens   int
	// Model overrides the provider's configured model when non-empty.
	Model string
}

// Response holds the result of an LLM completion call.
type Response struct {
	Content string
	Model   string // actual model used, echoed back for meta
}

// Provider is the interface for LLM completion backends.
type Provider interface {
	Complete(ctx context.Context, req *Request) (*Response, error)
}

func IsSupportedProvider(provider string) bool {
	switch strings.ToLower(provider) {
	case "anthropic", "openai", "gemini":
		return true
	default:
		return false
	}
}

func DefaultModelForProvider(provider string) string {
	switch strings.ToLower(provider) {
	case "openai":
		return "gpt-4o"
	case "gemini":
		return "gemini-2.0-flash"
	default:
		return DefaultModel
	}
}

func ProviderForModel(model string) string {
	model = strings.ToLower(model)
	switch {
	case strings.HasPrefix(model, "claude"):
		return "anthropic"
	case strings.HasPrefix(model, "gpt-"), isOpenAIReasoningModel(model):
		return "openai"
	case strings.HasPrefix(model, "gemini"):
		return "gemini"
	default:
		return ""
	}
}

func isOpenAIReasoningModel(model string) bool {
	return len(model) >= 2 && model[0] == 'o' && model[1] >= '0' && model[1] <= '9'
}

// NewProvider parses a "provider:model" string and returns the appropriate Provider.
// The API key is read from the environment at construction time and validated immediately.
// Example: "anthropic:claude-sonnet-4-20250514" or "openai:gpt-4o".
func NewProvider(providerModel string) (Provider, error) {
	parts := strings.SplitN(providerModel, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid model format %q: expected provider:model (e.g. anthropic:claude-sonnet-4-20250514)", providerModel)
	}
	provider := strings.ToLower(parts[0])
	switch provider {
	case "anthropic":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable not set")
		}
		return &anthropicProvider{model: parts[1], apiKey: apiKey}, nil
	case "openai":
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY environment variable not set")
		}
		return &openaiProvider{model: parts[1], apiKey: apiKey}, nil
	case "gemini":
		apiKey := os.Getenv("GEMINI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("GEMINI_API_KEY environment variable not set")
		}
		return &geminiProvider{model: parts[1], apiKey: apiKey}, nil
	default:
		return nil, fmt.Errorf("unknown provider %q: supported providers are anthropic, openai, gemini", parts[0])
	}
}

// truncate limits a string to maxLen runes, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen]) + "..."
}
