package chunk

import (
	"fmt"
	"strings"

	"github.com/dshills/speccritic/internal/llm"
	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/schema/validate"
)

const maxChunkSummaryRunes = 600

// chunkTagPrefix starts the tag that records which chunk produced an issue.
const chunkTagPrefix = "chunk:"

// ParseChunkResponse reads a complete chunk review response. See
// parseChunkResponse for what is kept and what is dropped.
func ParseChunkResponse(raw string, lineCount int, ch Chunk) (*schema.Report, error) {
	parsed, err := parseChunkResponse(raw, lineCount, ch)
	if err != nil {
		return nil, err
	}
	if parsed.Incomplete != nil {
		return nil, parsed.Incomplete
	}
	return parsed.Report, nil
}

// parseChunkResponse reads a chunk review response that may have been cut
// off. Findings that cite lines outside the chunk's primary range are dropped;
// the neighbouring chunk reviews those lines. The chunk tag and evidence path
// are set here rather than trusted from the model, and an over-long summary is
// shortened. A missing summary is allowed: it only feeds synthesis.
func parseChunkResponse(raw string, lineCount int, ch Chunk) (llm.Parsed, error) {
	res, err := validate.ParseResponse(raw, validate.Options{
		LineCount: lineCount,
		SpecPath:  ch.Path,
		CheckIssue: func(issue *schema.Issue) error {
			if err := evidenceInPrimaryRange(issue.Evidence, ch); err != nil {
				return err
			}
			issue.Tags = withChunkTag(issue.Tags, ch.ID)
			return nil
		},
		CheckQuestion: func(question *schema.Question) error {
			return evidenceInPrimaryRange(question.Evidence, ch)
		},
	})
	if err != nil {
		return llm.Parsed{}, err
	}
	res.Report.Meta.ChunkSummary = truncateRunes(strings.TrimSpace(res.Report.Meta.ChunkSummary), maxChunkSummaryRunes)
	return llm.Parsed{Report: res.Report, Incomplete: res.Incomplete, Dropped: res.Dropped}, nil
}

func evidenceInPrimaryRange(evidence []schema.Evidence, ch Chunk) error {
	for i, ev := range evidence {
		if ev.LineStart < ch.LineStart || ev.LineEnd > ch.LineEnd {
			return fmt.Errorf("evidence[%d]: line range %d-%d outside chunk primary range %d-%d", i, ev.LineStart, ev.LineEnd, ch.LineStart, ch.LineEnd)
		}
	}
	return nil
}

// withChunkTag returns tags with exactly one chunk tag, the one for chunkID.
// Any chunk tag the model wrote is replaced.
func withChunkTag(tags []string, chunkID string) []string {
	out := make([]string, 0, len(tags)+1)
	for _, tag := range tags {
		if len(tag) >= len(chunkTagPrefix) && strings.EqualFold(tag[:len(chunkTagPrefix)], chunkTagPrefix) {
			continue
		}
		out = append(out, tag)
	}
	return append(out, chunkTagPrefix+chunkID)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func hasTag(tags []string, want string) bool {
	for _, tag := range tags {
		if strings.EqualFold(tag, want) {
			return true
		}
	}
	return false
}
