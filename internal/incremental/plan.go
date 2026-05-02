package incremental

import (
	"fmt"
	"strings"

	"github.com/dshills/speccritic/internal/spec"
)

const renameSimilarityThreshold = 0.90

// PlanChanges compares previous and current spec text and returns review and
// reuse ranges for an incremental run.
func PlanChanges(previousRaw, currentRaw string, cfg Config) (Plan, error) {
	if cfg.Mode == "" {
		cfg.Mode = ModeAuto
	}
	if cfg.ChunkTokenThreshold == 0 {
		cfg.ChunkTokenThreshold = defaultChunkTokenThreshold
	}
	if cfg.MaxChangeRatio == 0 {
		cfg.MaxChangeRatio = defaultMaxChangeRatio
	}
	if err := ValidateConfig(cfg); err != nil {
		return Plan{}, err
	}

	prevSpec := spec.New("previous", previousRaw)
	curSpec := spec.New("current", currentRaw)
	prevSections := buildSections(previousRaw)
	curSections := buildSections(currentRaw)

	usedPrev := make(map[int]bool)
	changes := make([]SectionChange, 0, len(curSections)+len(prevSections))
	reviews := make([]ReviewRange, 0)
	reuses := make([]ReuseRange, 0)

	for _, cur := range curSections {
		match := matchSection(cur, prevSections, usedPrev)
		classification := match.classification
		var prevRange LineRange
		if match.index >= 0 {
			usedPrev[match.index] = true
			prev := prevSections[match.index]
			prevRange = prev.Range
			if classification == ClassUnchanged || classification == ClassMoved {
				reuses = append(reuses, ReuseRange{ID: cur.ID, Previous: prev.Range, Current: cur.Range})
			} else {
				reviews = append(reviews, makeReviewRange(cur.ID, cur.Range, curSpec.LineCount, cfg.ContextLines))
			}
		} else {
			reviews = append(reviews, makeReviewRange(cur.ID, cur.Range, curSpec.LineCount, cfg.ContextLines))
		}
		changes = append(changes, SectionChange{
			ID:             cur.ID,
			PreviousRange:  prevRange,
			CurrentRange:   cur.Range,
			Classification: classification,
			HeadingPath:    append([]string(nil), cur.HeadingPath...),
		})
	}

	for i, prev := range prevSections {
		if usedPrev[i] {
			continue
		}
		changes = append(changes, SectionChange{
			ID:             prev.ID,
			PreviousRange:  prev.Range,
			Classification: ClassDeleted,
			HeadingPath:    append([]string(nil), prev.HeadingPath...),
		})
	}

	reviews = coalesceReviewRanges(reviews, spec.Lines(currentRaw), cfg)
	return Plan{
		PreviousHash: prevSpec.Hash,
		CurrentHash:  curSpec.Hash,
		Mode:         cfg.Mode,
		Sections:     changes,
		ReviewRanges: reviews,
		ReuseRanges:  reuses,
	}, nil
}

type sectionMatch struct {
	index          int
	classification string
}

func matchSection(cur section, prev []section, used map[int]bool) sectionMatch {
	if idx := findSection(prev, used, func(candidate section) bool {
		return candidate.IdentityPath == cur.IdentityPath && candidate.ContentHash == cur.ContentHash
	}); idx >= 0 {
		if prev[idx].Range == cur.Range {
			return sectionMatch{index: idx, classification: ClassUnchanged}
		}
		return sectionMatch{index: idx, classification: ClassMoved}
	}
	if idx := findSection(prev, used, func(candidate section) bool {
		return candidate.ContentHash == cur.ContentHash && candidate.HeadingText == cur.HeadingText
	}); idx >= 0 {
		return sectionMatch{index: idx, classification: ClassMoved}
	}
	if idx := findSection(prev, used, func(candidate section) bool {
		return candidate.IdentityPath == cur.IdentityPath
	}); idx >= 0 {
		return sectionMatch{index: idx, classification: ClassChanged}
	}

	var candidates []int
	for i, candidate := range prev {
		if used[i] {
			continue
		}
		if candidate.LocalAnchor == cur.LocalAnchor || (tokenJaccard(candidate.Sample, cur.Sample) >= 0.50 && similarity(candidate.Sample, cur.Sample) >= renameSimilarityThreshold) {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 1 {
		idx := candidates[0]
		if prev[idx].HeadingText != cur.HeadingText {
			return sectionMatch{index: idx, classification: ClassRenamed}
		}
		return sectionMatch{index: idx, classification: ClassChanged}
	}
	if len(candidates) > 1 {
		return sectionMatch{index: -1, classification: ClassAmbiguous}
	}
	return sectionMatch{index: -1, classification: ClassAdded}
}

func findSection(sections []section, used map[int]bool, pred func(section) bool) int {
	for i, candidate := range sections {
		if used[i] {
			continue
		}
		if pred(candidate) {
			return i
		}
	}
	return -1
}

func makeReviewRange(id string, primary LineRange, lineCount, contextLines int) ReviewRange {
	context := LineRange{Start: primary.Start - contextLines, End: primary.End + contextLines}
	if context.Start < 1 {
		context.Start = 1
	}
	if context.End > lineCount {
		context.End = lineCount
	}
	return ReviewRange{ID: id, Primary: primary, Context: context}
}

func coalesceReviewRanges(ranges []ReviewRange, lines []string, cfg Config) []ReviewRange {
	if len(ranges) < 2 {
		return ranges
	}
	out := []ReviewRange{ranges[0]}
	for _, next := range ranges[1:] {
		last := &out[len(out)-1]
		if next.Context.Start > last.Context.End+1 {
			out = append(out, next)
			continue
		}
		merged := ReviewRange{
			ID: fmt.Sprintf("%s-%s", last.ID, next.ID),
			Primary: LineRange{
				Start: last.Primary.Start,
				End:   next.Primary.End,
			},
			Context: LineRange{
				Start: last.Context.Start,
				End:   next.Context.End,
			},
		}
		if withinTokenBudget(merged, lines, cfg.ChunkTokenThreshold) {
			*last = merged
			continue
		}
		out = append(out, next)
	}
	return out
}

func withinTokenBudget(r ReviewRange, lines []string, threshold int) bool {
	if threshold <= 0 {
		threshold = defaultChunkTokenThreshold
	}
	text := rangeText(lines, r.Context)
	estimated := estimateTokens(text)
	return float64(estimated)*1.2 <= float64(threshold)*0.8
}

func rangeText(lines []string, r LineRange) string {
	if r.Start < 1 || r.End < r.Start || r.Start > len(lines) {
		return ""
	}
	end := r.End
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[r.Start-1:end], "\n")
}
