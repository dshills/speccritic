package llm

import (
	"context"
	"strings"
)

// openaiAPIURL is a var to allow test overrides via httptest.
var openaiAPIURL = "https://api.openai.com/v1/chat/completions"

// OpenAIAPIURL returns the current OpenAI API endpoint URL.
// Exposed for use by integration tests via httptest servers.
func OpenAIAPIURL() string { return openaiAPIURL }

// SetOpenAIAPIURL overrides the OpenAI API endpoint URL.
// Intended for use in tests only.
func SetOpenAIAPIURL(u string) { openaiAPIURL = u }

type openaiProvider struct {
	model  string
	apiKey string // unexported; never serialized by encoding/json
	state  chatState
}

func (p *openaiProvider) Complete(ctx context.Context, req *Request) (*Response, error) {
	endpoint := chatEndpoint{
		name:             "openai",
		url:              openaiAPIURL,
		apiKey:           p.apiKey,
		completionTokens: openaiUsesMaxCompletionTokens,
	}
	return completeChat(ctx, endpoint, &p.state, p.model, req)
}

// openaiUsesMaxCompletionTokens reports whether model takes
// max_completion_tokens. Every model since GPT-5 requires it, so it is the
// assumption for any model not known to predate it; completeChat swaps the
// parameter if the API disagrees.
func openaiUsesMaxCompletionTokens(model string) bool {
	model = strings.ToLower(model)
	return !strings.HasPrefix(model, "gpt-3.5") && !strings.HasPrefix(model, "gpt-4")
}
