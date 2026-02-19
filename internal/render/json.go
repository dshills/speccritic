package render

import (
	"encoding/json"

	"github.com/dshills/speccritic/internal/schema"
)

type jsonRenderer struct{}

func (r *jsonRenderer) Render(report *schema.Report) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}
