package spec

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strconv"
	"strings"
)

var lineEndingReplacer = strings.NewReplacer("\r\n", "\n", "\r", "\n")

// Spec holds a loaded specification file with derived metadata.
type Spec struct {
	Path      string
	Hash      string // "sha256:<hex>"
	Raw       string // original content
	Numbered  string // content with "L1: …" prefixes
	LineCount int
}

// Load reads a spec file from disk, computes its hash, and line-numbers its content.
func Load(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading spec file: %w", err)
	}

	return New(path, string(data)), nil
}

// LoadText constructs a Spec from in-memory text, using name as the display path.
func LoadText(name, raw string) (*Spec, error) {
	if name == "" {
		name = "SPEC.md"
	}
	return New(name, raw), nil
}

// New constructs a Spec from raw text, computes its hash, and line-numbers it.
func New(path, raw string) *Spec {
	sum := sha256.Sum256([]byte(raw))
	hash := fmt.Sprintf("sha256:%x", sum)
	numbered, lineCount := addLineNumbers(raw)

	return &Spec{
		Path:      path,
		Hash:      hash,
		Raw:       raw,
		Numbered:  numbered,
		LineCount: lineCount,
	}
}

// addLineNumbers prefixes every line with "L{n}: " and returns the result
// along with the total line count. A trailing empty segment after a final
// newline is counted but not emitted as a spurious numbered line.
func addLineNumbers(content string) (string, int) {
	lines := Lines(content)
	if len(lines) == 0 {
		return "", 0
	}

	var b strings.Builder
	// Grow once: each emitted line adds ~6 bytes of prefix ("L" + digits + ": ").
	b.Grow(len(content) + len(lines)*6)

	var numBuf [20]byte
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteByte('L')
		b.Write(strconv.AppendInt(numBuf[:0], int64(i+1), 10))
		b.WriteString(": ")
		b.WriteString(line)
	}
	return b.String(), len(lines)
}

// Lines returns the canonical line view used for prompt numbering and web annotations.
// It normalizes CRLF/CR to LF and treats a trailing newline as a terminator,
// not as an additional empty line.
func Lines(content string) []string {
	if content == "" {
		return nil
	}
	content = lineEndingReplacer.Replace(content)
	trimmed := strings.TrimSuffix(content, "\n")
	if trimmed == "" {
		return []string{""}
	}
	return strings.Split(trimmed, "\n")
}
