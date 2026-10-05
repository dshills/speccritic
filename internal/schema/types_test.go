package schema

import (
	"path/filepath"
	"testing"
)

func TestEvidencePath(t *testing.T) {
	tests := []struct {
		specPath string
		want     string
	}{
		{"", ""},
		{"SPEC.md", "SPEC.md"},
		{filepath.Join("specs", "api", "SPEC.md"), filepath.Join("specs", "api", "SPEC.md")},
		{filepath.Join(t.TempDir(), "SPEC.md"), "SPEC.md"},
		{filepath.Join("..", "shared", "SPEC.md"), "SPEC.md"},
	}
	for _, tt := range tests {
		if got := EvidencePath(tt.specPath); got != tt.want {
			t.Errorf("EvidencePath(%q) = %q, want %q", tt.specPath, got, tt.want)
		}
	}
}
