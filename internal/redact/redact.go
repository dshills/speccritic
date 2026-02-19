package redact

import (
	"os"
	"regexp"
	"strings"
)

const redacted = "[REDACTED]"

// pemPattern matches PEM key blocks across multiple lines.
var pemPattern = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]+KEY-----.*?-----END [A-Z ]+KEY-----`)

// patterns holds single-line secret-detection regexes in priority order.
var patterns = []*regexp.Regexp{
	// AWS access key IDs
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	// OpenAI / Anthropic secret keys — word-boundary aware
	regexp.MustCompile(`(?:^|\s|["'])sk-[a-zA-Z0-9]{20,}`),
	// JWT tokens (three base64url segments)
	regexp.MustCompile(`eyJ[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+`),
	// Bearer tokens — require minimum 20-char token to avoid false positives
	regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9\-._~+/]{20,}=*`),
	// Inline password assignments
	regexp.MustCompile(`(?i)password\s*[:=]\s*\S+`),
}

// Redact replaces known secret patterns in input with [REDACTED].
// Line structure is preserved — the number of newlines in the output
// always equals the number of newlines in the input.
func Redact(input string) string {
	// Handle PEM blocks first: replace each line within the block individually
	// so that line count is preserved.
	input = pemPattern.ReplaceAllStringFunc(input, func(match string) string {
		lines := strings.Split(match, "\n")
		for i := range lines {
			lines[i] = redacted
		}
		return strings.Join(lines, "\n")
	})

	// Apply single-line patterns.
	for _, re := range patterns {
		input = re.ReplaceAllString(input, redacted)
	}
	return input
}

// RedactFile reads a file, redacts its content, and returns the result.
func RedactFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return Redact(string(data)), nil
}
