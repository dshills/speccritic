package speccritic

import (
	"context"
	"io"

	"github.com/dshills/speccritic/internal/app"
	"github.com/dshills/speccritic/internal/chunk"
	"github.com/dshills/speccritic/internal/incremental"
	"github.com/dshills/speccritic/internal/schema"
)

type Report = schema.Report
type Issue = schema.Issue
type Question = schema.Question
type Patch = schema.Patch
type Evidence = schema.Evidence
type Summary = schema.Summary
type Severity = schema.Severity
type Verdict = schema.Verdict
type ContextDocument = app.ContextDocument

type Error = app.Error
type ErrorKind = app.ErrorKind

const (
	ErrorInput       = app.ErrorInput
	ErrorProvider    = app.ErrorProvider
	ErrorModelOutput = app.ErrorModelOutput
)

type CheckOptions struct {
	Version                         string
	SpecPath                        string
	SpecName                        string
	SpecText                        string
	ContextPaths                    []string
	ContextDocuments                []ContextDocument
	Profile                         string
	Strict                          bool
	SeverityThreshold               string
	LLMProvider                     string
	LLMModel                        string
	Temperature                     float64
	MaxTokens                       int
	Offline                         bool
	Debug                           bool
	Verbose                         bool
	Preflight                       bool
	PreflightMode                   string
	PreflightProfile                string
	PreflightIgnore                 []string
	Chunking                        string
	ChunkLines                      int
	ChunkOverlap                    int
	ChunkMinLines                   int
	ChunkTokenThreshold             int
	ChunkConcurrency                int
	SynthesisLineThreshold          int
	IncrementalFrom                 string
	IncrementalFromText             string
	IncrementalBasePath             string
	IncrementalBaseText             string
	IncrementalMode                 string
	IncrementalMaxChangeRatio       float64
	IncrementalMaxRemapFailureRatio float64
	IncrementalContextLines         int
	IncrementalStrictReuse          bool
	IncrementalReport               bool
	ConvergenceFrom                 string
	ConvergenceFromText             string
	ConvergenceMode                 string
	ConvergenceStrict               bool
	ConvergenceReport               bool
	CompletionSuggestions           bool
	CompletionMode                  string
	CompletionTemplate              string
	CompletionMaxPatches            int
	CompletionOpenDecisions         bool
	ErrWriter                       io.Writer
}

type CheckResult struct {
	Report       *Report
	PatchDiff    string
	OriginalSpec string
	LineCount    int
	Model        string
}

func DefaultCheckOptions() CheckOptions {
	incrementalDefaults := incremental.DefaultConfig()
	return CheckOptions{
		Version:                         "api",
		Profile:                         "general",
		SeverityThreshold:               "info",
		Temperature:                     0.2,
		MaxTokens:                       8192,
		Preflight:                       true,
		PreflightMode:                   "warn",
		Chunking:                        string(chunk.ModeAuto),
		ChunkLines:                      chunk.DefaultChunkLines,
		ChunkOverlap:                    chunk.DefaultChunkOverlap,
		ChunkMinLines:                   chunk.DefaultChunkMinLines,
		ChunkTokenThreshold:             chunk.DefaultChunkTokenThreshold,
		ChunkConcurrency:                chunk.DefaultChunkConcurrency,
		SynthesisLineThreshold:          chunk.DefaultSynthesisLineThreshold,
		IncrementalMode:                 string(incremental.ModeAuto),
		IncrementalMaxChangeRatio:       incrementalDefaults.MaxChangeRatio,
		IncrementalMaxRemapFailureRatio: incrementalDefaults.MaxRemapFailureRatio,
		IncrementalContextLines:         incrementalDefaults.ContextLines,
		IncrementalStrictReuse:          true,
		ConvergenceMode:                 "auto",
		ConvergenceReport:               true,
		CompletionMode:                  schema.CompletionModeAuto,
		CompletionTemplate:              schema.CompletionTemplateProfile,
		CompletionMaxPatches:            8,
		CompletionOpenDecisions:         true,
		ErrWriter:                       io.Discard,
	}
}

func Check(ctx context.Context, opts CheckOptions) (*CheckResult, error) {
	req := app.CheckRequest{
		Version:                         opts.Version,
		SpecPath:                        opts.SpecPath,
		SpecName:                        opts.SpecName,
		SpecText:                        opts.SpecText,
		ContextPaths:                    opts.ContextPaths,
		ContextDocuments:                toAppContextDocuments(opts.ContextDocuments),
		Profile:                         opts.Profile,
		Strict:                          opts.Strict,
		SeverityThreshold:               opts.SeverityThreshold,
		LLMProvider:                     opts.LLMProvider,
		LLMModel:                        opts.LLMModel,
		Temperature:                     opts.Temperature,
		MaxTokens:                       opts.MaxTokens,
		Offline:                         opts.Offline,
		Debug:                           opts.Debug,
		Verbose:                         opts.Verbose,
		Preflight:                       opts.Preflight,
		PreflightMode:                   opts.PreflightMode,
		PreflightProfile:                opts.PreflightProfile,
		PreflightIgnore:                 opts.PreflightIgnore,
		Chunking:                        opts.Chunking,
		ChunkLines:                      opts.ChunkLines,
		ChunkOverlap:                    opts.ChunkOverlap,
		ChunkMinLines:                   opts.ChunkMinLines,
		ChunkTokenThreshold:             opts.ChunkTokenThreshold,
		ChunkConcurrency:                opts.ChunkConcurrency,
		SynthesisLineThreshold:          opts.SynthesisLineThreshold,
		IncrementalFrom:                 opts.IncrementalFrom,
		IncrementalFromText:             opts.IncrementalFromText,
		IncrementalBasePath:             opts.IncrementalBasePath,
		IncrementalBaseText:             opts.IncrementalBaseText,
		IncrementalMode:                 opts.IncrementalMode,
		IncrementalMaxChangeRatio:       opts.IncrementalMaxChangeRatio,
		IncrementalMaxRemapFailureRatio: opts.IncrementalMaxRemapFailureRatio,
		IncrementalContextLines:         opts.IncrementalContextLines,
		IncrementalStrictReuse:          opts.IncrementalStrictReuse,
		IncrementalReport:               opts.IncrementalReport,
		ConvergenceFrom:                 opts.ConvergenceFrom,
		ConvergenceFromText:             opts.ConvergenceFromText,
		ConvergenceMode:                 opts.ConvergenceMode,
		ConvergenceStrict:               opts.ConvergenceStrict,
		ConvergenceReport:               opts.ConvergenceReport,
		CompletionSuggestions:           opts.CompletionSuggestions,
		CompletionMode:                  opts.CompletionMode,
		CompletionTemplate:              opts.CompletionTemplate,
		CompletionMaxPatches:            opts.CompletionMaxPatches,
		CompletionOpenDecisions:         opts.CompletionOpenDecisions,
		Source:                          app.SourceCLI,
		ErrWriter:                       opts.ErrWriter,
	}
	result, err := app.NewChecker().Check(ctx, req)
	if err != nil {
		return nil, err
	}
	return &CheckResult{
		Report:       result.Report,
		PatchDiff:    result.PatchDiff,
		OriginalSpec: result.OriginalSpec,
		LineCount:    result.LineCount,
		Model:        result.Model,
	}, nil
}

func toAppContextDocuments(docs []ContextDocument) []app.ContextDocument {
	out := make([]app.ContextDocument, len(docs))
	copy(out, docs)
	return out
}
