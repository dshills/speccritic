package app

import (
	"context"
	"testing"

	"github.com/dshills/speccritic/internal/llm"
)

func BenchmarkCheckerChunkedMock(b *testing.B) {
	specText := longSpec(1000)
	provider := &chunkAwareProvider{emptyChunks: true}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}
	req := CheckRequest{
		Version:                "bench",
		SpecName:               "SPEC.md",
		SpecText:               specText,
		Profile:                "general",
		SeverityThreshold:      "info",
		Temperature:            0.2,
		MaxTokens:              1000,
		Preflight:              false,
		Chunking:               "on",
		ChunkLines:             120,
		ChunkOverlap:           20,
		ChunkConcurrency:       4,
		SynthesisLineThreshold: 2000,
		Source:                 SourceCLI,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := checker.Check(context.Background(), req)
		if err != nil {
			b.Fatalf("Check: %v", err)
		}
		if result.Report == nil {
			b.Fatal("missing report")
		}
	}
}
