package context

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dshills/speccritic/internal/redact"
)

// ContextFile holds a loaded grounding document after redaction.
type ContextFile struct {
	Path    string
	Content string // after redaction
}

// Load reads a list of context files from disk and redacts each one.
func Load(paths []string) ([]ContextFile, error) {
	files := make([]ContextFile, 0, len(paths))
	for _, p := range paths {
		content, err := redact.RedactFile(p)
		if err != nil {
			return nil, fmt.Errorf("loading context file %q: %w", p, err)
		}
		files = append(files, ContextFile{
			Path:    p,
			Content: content,
		})
	}
	return files, nil
}

// FormatForPrompt wraps each context file in XML-style tags for prompt insertion.
func FormatForPrompt(files []ContextFile) string {
	if len(files) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, f := range files {
		name := filepath.Base(f.Path)
		sb.WriteString(fmt.Sprintf("<context file=%q>\n", name))
		sb.WriteString(f.Content)
		if !strings.HasSuffix(f.Content, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("</context>\n")
	}
	return sb.String()
}
