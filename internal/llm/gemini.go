package llm

import "context"

// geminiAPIURL is a var to allow test overrides via httptest.
var geminiAPIURL = "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"

// GeminiAPIURL returns the current Gemini API endpoint URL.
// Exposed for use by integration tests via httptest servers.
func GeminiAPIURL() string { return geminiAPIURL }

// SetGeminiAPIURL overrides the Gemini API endpoint URL.
// Intended for use in tests only.
func SetGeminiAPIURL(u string) { geminiAPIURL = u }

type geminiProvider struct {
	model  string
	apiKey string // unexported; never serialized by encoding/json
}

func (p *geminiProvider) Complete(ctx context.Context, req *Request) (*Response, error) {
	endpoint := chatEndpoint{name: "gemini", url: geminiAPIURL, apiKey: p.apiKey}
	return completeChat(ctx, endpoint, p.model, req)
}
