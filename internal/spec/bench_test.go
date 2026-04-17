package spec

import (
	"strings"
	"testing"
)

func buildSpec(lines int) string {
	var b strings.Builder
	for i := 0; i < lines; i++ {
		b.WriteString("This is a normal specification line describing a requirement.\n")
	}
	return b.String()
}

func BenchmarkAddLineNumbers2000(b *testing.B) {
	input := buildSpec(2000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = addLineNumbers(input)
	}
}
