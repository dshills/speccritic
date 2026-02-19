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
const defaultMaxTokens = 4096

// Request holds the parameters for an LLM completion call.
type Request struct {
	SystemPrompt string
	UserPrompt   string
	Temperature  float64
	MaxTokens    int
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

// NewProvider parses a "provider:model" string and returns the appropriate Provider.
// The API key is read from the environment at construction time and validated immediately.
// Example: "anthropic:claude-sonnet-4-6" or "openai:gpt-4o".
func NewProvider(providerModel string) (Provider, error) {
	parts := strings.SplitN(providerModel, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid model format %q: expected provider:model (e.g. anthropic:claude-sonnet-4-6)", providerModel)
	}
	switch parts[0] {
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
	default:
		return nil, fmt.Errorf("unknown provider %q: supported providers are anthropic, openai", parts[0])
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
