package spec

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
)

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

	raw := string(data)
	sum := sha256.Sum256(data)
	hash := fmt.Sprintf("sha256:%x", sum)

	numbered, lineCount := addLineNumbers(raw)

	return &Spec{
		Path:      path,
		Hash:      hash,
		Raw:       raw,
		Numbered:  numbered,
		LineCount: lineCount,
	}, nil
}

// addLineNumbers prefixes every line with "L{n}: " and returns the result
// along with the total line count.
func addLineNumbers(content string) (string, int) {
	lines := strings.Split(content, "\n")
	// If the file ends with a newline, Split produces a trailing empty element.
	// We include it in the count but don't emit a spurious numbered line for it.
	out := make([]string, 0, len(lines))
	lineCount := 0
	for i, line := range lines {
		// Don't number the trailing empty string after a final newline
		if i == len(lines)-1 && line == "" {
			break
		}
		lineCount++
		out = append(out, fmt.Sprintf("L%d: %s", lineCount, line))
	}
	return strings.Join(out, "\n"), lineCount
}
