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

// tailGroup names the capture group holding text that a pattern matched only to
// confirm where a value ends. That text is not part of the secret and is put
// back unchanged.
const tailGroup = "tail"

// assignedValue matches a separator and the value assigned after it. The
// separator and the start of the value must be on the key's own line, so a key
// with nothing after it (as in a YAML schema) does not pull in the line below.
//
// The value is, in order of preference:
//   - a quoted value closed on the same line;
//   - a quoted value closed on a later line, unless the closing quote runs
//     straight into a letter or digit. That is what an apostrophe ("user's")
//     or the opening quote of a later phrase looks like, and treating one as
//     the end of the value would let an unclosed quote swallow the lines
//     below it. Anything else after the quote (end of line or input, space,
//     delimiter, comment) ends the value, so the doubt goes toward redacting;
//   - an opening quote that is never closed that way, redacted to the end of
//     its own line;
//   - an unquoted value, which stops at whitespace and delimiters.
const assignedValue = `[ \t]*[:=][ \t]*(?:` +
	`"[^"\n]+"|` + "`[^`\n]+`" + `|'[^'\n]+'|` +
	`(?:"[^"]+"|` + "`[^`]+`" + `|'[^']+')(?P<` + tailGroup + `>[^\pL\pN_]|\z)|` +
	`"[^"\n]+|` + "`[^`\n]+" + `|'[^'\n]+|` +
	`[^\s"',;{}[\]()]+)`

// quotedPhrase matches a value opened with the quote character q that holds a
// space or tab, without leaving its line. A backslash-escaped or doubled q
// inside the value does not close it. The value runs through its closing quote
// and anything attached to that quote up to a delimiter; if the quote is never
// closed on the line, it runs to the end of the line.
func quotedPhrase(q string) string {
	body := `(?:\\[^\n]|` + q + q + `|[^` + q + `\n])*`
	return q + body + `[ \t]` + body + `(?:` + q + `[^\s,;{}[\]()]*|(?m:$))`
}

// numPatterns is enforced as an array length below; adding a pattern without
// updating this constant is a compile-time error — preventing silent
// out-of-bounds in the per-pattern hit tracker.
const numPatterns = 8

// A key and its separator never match across a line break, so a key with no
// value on its line is left alone. A quoted value, a PEM block, or a token
// wrapped onto the line after "Bearer" may span lines; Redact replaces such a
// match line by line, so the number of lines never changes.
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
	// Inline password assignments (case-insensitive regex; lowercase triggers) —
	// optional closing quote handles JSON keys.
	// The value is a run of non-whitespace, not cut at `,;{}[]()` — passwords
	// legitimately contain those. A quoted value ("val", 'val', `val`) holding a
	// space or tab is instead matched as a quotedPhrase, so a passphrase does
	// not leak past its first word; one without is already covered whole by \S+.
	{re: regexp.MustCompile(`(?i)password"?[ \t]*[:=][ \t]*(?:` + quotedPhrase(`"`) + `|` + quotedPhrase("`") + `|` + quotedPhrase(`'`) + `|\S+)`), triggers: []string{"password"}, fold: true},
	// api_key / apiKey / api-key assignments — optional closing quote handles JSON keys.
	{re: regexp.MustCompile(`(?i)api[-_]?key"?` + assignedValue), triggers: []string{"api_key", "apikey", "api-key"}, fold: true},
	// client_secret / clientSecret / secret_key / private_key OAuth secret assignments.
	// [_-]? handles both snake_case and camelCase variants.
	{re: regexp.MustCompile(`(?i)(?:client[_-]?secret|secret[_-]?key|private[_-]?key)"?` + assignedValue), triggers: []string{"secret", "private_key"}, fold: true},
	// auth_token / accessToken / refresh_token (snake_case and camelCase) assignments.
	{re: regexp.MustCompile(`(?i)(?:auth|access|refresh)[_-]?token"?` + assignedValue), triggers: []string{"token"}, fold: true},
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
		input = pemPattern.ReplaceAllStringFunc(input, redactLines)
	}

	// Apply only the patterns whose triggers were observed.
	for i, p := range patterns {
		if hits[i] {
			input = redactMatches(p.re, input)
		}
	}
	return input
}

// redactMatches redacts every match of re in input, keeping any text the
// pattern captured in its tail group.
func redactMatches(re *regexp.Regexp, input string) string {
	matches := re.FindAllStringSubmatchIndex(input, -1)
	if len(matches) == 0 {
		return input
	}
	tail := re.SubexpIndex(tailGroup)
	var b strings.Builder
	b.Grow(len(input))
	last := 0
	for _, m := range matches {
		secretEnd := m[1]
		if tail >= 0 && m[2*tail] >= 0 {
			secretEnd = m[2*tail]
		}
		b.WriteString(input[last:m[0]])
		b.WriteString(redactLines(input[m[0]:secretEnd]))
		last = secretEnd
	}
	b.WriteString(input[last:])
	return b.String()
}

// redactLines replaces a match with the redaction marker, once per line the
// match covers, so the line count of the input is unchanged.
func redactLines(match string) string {
	n := strings.Count(match, "\n")
	if n == 0 {
		return redacted
	}
	return redacted + strings.Repeat("\n"+redacted, n)
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
