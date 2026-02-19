package render

import (
	"fmt"

	"github.com/dshills/speccritic/internal/schema"
)

// Renderer formats a Report into bytes for output.
type Renderer interface {
	Render(report *schema.Report) ([]byte, error)
}

// NewRenderer returns a Renderer for the given format string.
// Supported formats: "json" (default), "md".
func NewRenderer(format string) (Renderer, error) {
	switch format {
	case "json":
		return &jsonRenderer{}, nil
	case "md":
		return &markdownRenderer{}, nil
	default:
		return nil, fmt.Errorf("unknown format %q: supported formats are json, md", format)
	}
}
