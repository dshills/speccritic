package preflight

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/spec"
)

func BenchmarkRunTenThousandLineSpec(b *testing.B) {
	s := spec.New("SPEC.md", syntheticLargeSpec(10000))
	cfg := Config{Enabled: true, Mode: ModeOnly, Profile: "general"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := Run(s, cfg)
		if err != nil {
			b.Fatalf("Run: %v", err)
		}
		if len(result.Issues) != 0 {
			b.Fatalf("issues = %d, want 0", len(result.Issues))
		}
	}
}

func syntheticLargeSpec(lines int) string {
	required := []string{
		"# Large Synthetic Spec",
		"",
		"## Purpose",
		"The service validates deterministic benchmark behavior.",
		"",
		"## Non-goals",
		"The service does not manage payments.",
		"",
		"## Requirements",
		"",
	}
	var b strings.Builder
	for _, line := range required {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for i := len(required) + 1; i <= lines-3; i++ {
		fmt.Fprintf(&b, "- Requirement %05d returns status 200 within 100 ms for request type %05d.\n", i, i)
	}
	b.WriteString("\n## Acceptance Criteria\n")
	b.WriteString("- A benchmark request receives status 200 within 100 ms.\n")
	return b.String()
}
