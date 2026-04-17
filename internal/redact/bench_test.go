package redact

import (
	"strings"
	"testing"
)

// buildCleanSpec returns a secret-free spec of roughly the given line count.
func buildCleanSpec(lines int) string {
	var b strings.Builder
	for i := 0; i < lines; i++ {
		b.WriteString("This is a normal specification line describing a requirement.\n")
	}
	return b.String()
}

// buildDirtySpec returns a spec with several embedded secrets near the end,
// so the regex passes must scan most of the file before finding them.
func buildDirtySpec(lines int) string {
	var b strings.Builder
	for i := 0; i < lines-5; i++ {
		b.WriteString("This is a normal specification line describing a requirement.\n")
	}
	b.WriteString("access_key = AKIAIOSFODNN7EXAMPLE\n")
	b.WriteString("api_key = sk-abcdefghijklmnopqrstuvwxyz123456\n")
	b.WriteString("Authorization: Bearer abcdefghijklmnopqrstuvwxyz0123456789\n")
	b.WriteString("password: supersecret123\n")
	b.WriteString("token = eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c\n")
	return b.String()
}

func BenchmarkRedactClean2000(b *testing.B) {
	input := buildCleanSpec(2000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Redact(input)
	}
}

func BenchmarkRedactDirty2000(b *testing.B) {
	input := buildDirtySpec(2000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Redact(input)
	}
}
