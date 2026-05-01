package chunk

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/schema/validate"
)

const maxChunkSummaryRunes = 600

func ParseChunkResponse(raw string, lineCount int, ch Chunk) (*schema.Report, error) {
	report, err := validate.Parse(raw, lineCount)
	if err != nil {
		return nil, err
	}
	if err := ValidateChunkReport(report, ch, lineCount); err != nil {
		return nil, err
	}
	return report, nil
}

func ValidateChunkReport(report *schema.Report, ch Chunk, lineCount int) error {
	if report == nil {
		return fmt.Errorf("chunk report is required")
	}
	if ch.ID == "" {
		return fmt.Errorf("chunk ID is required")
	}
	if strings.TrimSpace(report.Meta.ChunkSummary) == "" {
		return fmt.Errorf("chunk %s missing meta.chunk_summary", ch.ID)
	}
	if utf8.RuneCountInString(report.Meta.ChunkSummary) > maxChunkSummaryRunes {
		return fmt.Errorf("chunk %s meta.chunk_summary exceeds %d characters", ch.ID, maxChunkSummaryRunes)
	}
	return validateReportEvidence(report, ch, lineCount, false)
}

func ValidateSynthesisReport(report *schema.Report, lineCount int) error {
	if report == nil {
		return fmt.Errorf("synthesis report is required")
	}
	return validateReportEvidence(report, Chunk{}, lineCount, true)
}

func validateReportEvidence(report *schema.Report, ch Chunk, lineCount int, synthesis bool) error {
	for i, issue := range report.Issues {
		if !synthesis && !hasTag(issue.Tags, "chunk:"+ch.ID) {
			return fmt.Errorf("issue[%d] missing chunk tag chunk:%s", i, ch.ID)
		}
		for j, ev := range issue.Evidence {
			if err := validateChunkEvidence(ev, ch, lineCount, synthesis); err != nil {
				return fmt.Errorf("issue[%d].evidence[%d]: %w", i, j, err)
			}
		}
	}
	for i, question := range report.Questions {
		for j, ev := range question.Evidence {
			if err := validateChunkEvidence(ev, ch, lineCount, synthesis); err != nil {
				return fmt.Errorf("question[%d].evidence[%d]: %w", i, j, err)
			}
		}
	}
	return nil
}

func validateChunkEvidence(ev schema.Evidence, ch Chunk, lineCount int, synthesis bool) error {
	if ev.LineStart < 1 || ev.LineEnd < ev.LineStart || ev.LineEnd > lineCount {
		return fmt.Errorf("invalid original line range %d-%d for %d-line spec", ev.LineStart, ev.LineEnd, lineCount)
	}
	if synthesis {
		return nil
	}
	if ev.LineStart < ch.LineStart || ev.LineEnd > ch.LineEnd {
		return fmt.Errorf("line range %d-%d outside chunk primary range %d-%d", ev.LineStart, ev.LineEnd, ch.LineStart, ch.LineEnd)
	}
	return nil
}

func hasTag(tags []string, want string) bool {
	for _, tag := range tags {
		if strings.EqualFold(tag, want) {
			return true
		}
	}
	return false
}
