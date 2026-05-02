package redact

import (
	"os"
	"regexp"
	"strings"
)

const redacted = "[REDACTED]"

// pemPattern matches PEM key blocks across multiple lines.
var pemPattern = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]+KEY-----.*?-----END [A-Z ]+KEY-----`)

const pemTrigger = "-----BEGIN"

// redactPattern pairs a secret regex with the literal substrings that must
// be present for the regex to have any chance of matching. We scan for the
// triggers once and skip regexes whose trigger set is absent.
//
// When fold is true, the regex is case-insensitive (`(?i)`) and its triggers
// must be matched against a lowercased copy of the input — otherwise a mixed-
// case secret like "Password: ..." would slip past the trigger gate even
// though the regex itself would match.
type redactPattern struct {
	re       *regexp.Regexp
	triggers []string
	fold     bool
}

// numPatterns is enforced as an array length below; adding a pattern without
// updating this constant is a compile-time error — preventing silent
// out-of-bounds in the per-pattern hit tracker.
const numPatterns = 5

var patterns = [numPatterns]redactPattern{
	// AWS access key IDs
	{re: regexp.MustCompile(`AKIA[0-9A-Z]{16}`), triggers: []string{"AKIA"}},
	// OpenAI / Anthropic secret keys — \b ensures we match the key without
	// consuming any surrounding separator character.
	{re: regexp.MustCompile(`\bsk-[a-zA-Z0-9]{20,}`), triggers: []string{"sk-"}},
	// JWT tokens (three base64url segments)
	{re: regexp.MustCompile(`eyJ[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+`), triggers: []string{"eyJ"}},
	// Bearer tokens — require minimum 20-char token to avoid false positives.
	// Regex is case-insensitive, so triggers are lowercase and matched against
	// a lowercased copy of the input.
	{re: regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9\-._~+/]{20,}=*`), triggers: []string{"bearer"}, fold: true},
	// Inline password assignments (case-insensitive regex; lowercase triggers).
	{re: regexp.MustCompile(`(?i)password\s*[:=]\s*\S+`), triggers: []string{"password"}, fold: true},
}

// Redact replaces known secret patterns in input with [REDACTED].
// Line structure is preserved — the number of newlines in the output
// always equals the number of newlines in the input.
func Redact(input string) string {
	hits, pemHit, any := detectPatternHits(input)
	if !any {
		return input
	}

	if pemHit {
		// Handle PEM blocks first: replace each line within the block individually
		// so that line count is preserved.
		input = pemPattern.ReplaceAllStringFunc(input, func(match string) string {
			lines := strings.Split(match, "\n")
			for i := range lines {
				lines[i] = redacted
			}
			return strings.Join(lines, "\n")
		})
	}

	// Apply only the single-line patterns whose triggers were observed.
	for i, p := range patterns {
		if hits[i] {
			input = p.re.ReplaceAllString(input, redacted)
		}
	}
	return input
}

// ContainsSecret reports whether input matches any configured redaction pattern.
func ContainsSecret(input string) bool {
	hits, pemHit, any := detectPatternHits(input)
	if !any {
		return false
	}
	if pemHit && pemPattern.MatchString(input) {
		return true
	}
	for i, hit := range hits {
		if hit && patterns[i].re.MatchString(input) {
			return true
		}
	}
	return false
}

func detectPatternHits(input string) ([numPatterns]bool, bool, bool) {
	var hits [numPatterns]bool
	pemHit := strings.Contains(input, pemTrigger)
	any := pemHit
	var lower string
	for _, p := range patterns {
		if p.fold {
			lower = strings.ToLower(input)
			break
		}
	}
	for i, p := range patterns {
		src := input
		if p.fold {
			src = lower
		}
		for _, t := range p.triggers {
			if strings.Contains(src, t) {
				hits[i] = true
				any = true
				break
			}
		}
	}
	return hits, pemHit, any
}

// RedactFile reads a file, redacts its content, and returns the result.
func RedactFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return Redact(string(data)), nil
}
