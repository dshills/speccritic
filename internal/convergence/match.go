package convergence

import (
	"sort"
	"strings"
)

const (
	matchMethodStableID    = "stable_id"
	matchMethodFingerprint = "fingerprint"
	matchMethodEvidence    = "evidence"
	matchMethodSimilarity  = "similarity"

	minTitleSimilarity    = 0.92
	minEvidenceSimilarity = 0.88
	minWinnerMargin       = 0.05
)

// MatchFindings deterministically matches previous findings to current
// findings. Ambiguous current findings are returned as unmatched.
func MatchFindings(previous, current []TrackedFinding) []Match {
	previous = ComputeFingerprints(previous)
	current = ComputeFingerprints(current)
	for i := range previous {
		previous[i].SourceIndex = i
	}
	for i := range current {
		current[i].SourceIndex = i
	}

	bestByCurrent := make(map[int]Match, len(current))
	claimsByPrevious := make(map[int]int, len(previous))
	for currentIndex, cur := range current {
		candidates := matchCandidates(previous, cur)
		if len(candidates) == 0 {
			continue
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].Score != candidates[j].Score {
				return candidates[i].Score > candidates[j].Score
			}
			if candidates[i].Method != candidates[j].Method {
				return methodRank(candidates[i].Method) < methodRank(candidates[j].Method)
			}
			return candidates[i].Previous.SourceIndex < candidates[j].Previous.SourceIndex
		})
		if len(candidates) > 1 && candidates[0].Score-candidates[1].Score < minWinnerMargin {
			continue
		}
		match := candidates[0]
		match.Current.SourceIndex = currentIndex
		bestByCurrent[currentIndex] = match
		claimsByPrevious[match.Previous.SourceIndex]++
	}

	matches := make([]Match, 0, len(bestByCurrent))
	for currentIndex := 0; currentIndex < len(current); currentIndex++ {
		match, ok := bestByCurrent[currentIndex]
		if !ok || claimsByPrevious[match.Previous.SourceIndex] > 1 {
			continue
		}
		matches = append(matches, match)
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].Current.SourceIndex < matches[j].Current.SourceIndex
	})
	return matches
}

func matchCandidates(previous []TrackedFinding, cur TrackedFinding) []Match {
	var candidates []Match
	for _, prev := range previous {
		if prev.Kind != cur.Kind {
			continue
		}
		if prevStable, curStable := stableIdentity(prev.Tags), stableIdentity(cur.Tags); prevStable != "" && prevStable == curStable {
			candidates = append(candidates, Match{Previous: prev, Current: cur, Score: 1.0, Method: matchMethodStableID})
			continue
		}
		if prev.Fingerprint != "" && prev.Fingerprint == cur.Fingerprint {
			candidates = append(candidates, Match{Previous: prev, Current: cur, Score: 0.90, Method: matchMethodFingerprint})
			continue
		}
		if evidenceMatches(prev, cur) {
			candidates = append(candidates, Match{Previous: prev, Current: cur, Score: 0.80, Method: matchMethodEvidence})
			continue
		}
		score, ok := similarityMatch(prev, cur)
		if ok {
			candidates = append(candidates, Match{Previous: prev, Current: cur, Score: score, Method: matchMethodSimilarity})
		}
	}
	return candidates
}

func stableIdentity(tags []string) string {
	sortedTags := append([]string(nil), tags...)
	sort.Strings(sortedTags)
	for _, tag := range sortedTags {
		normalized := normalizeToken(tag)
		for _, prefix := range []string{"stable:", "convergence:", "finding:"} {
			if strings.HasPrefix(normalized, prefix) && len(normalized) > len(prefix) {
				return normalized
			}
		}
	}
	return ""
}

func evidenceMatches(prev, cur TrackedFinding) bool {
	if len(prev.Evidence) == 0 || len(cur.Evidence) == 0 {
		return false
	}
	prevText := evidenceText(prev.Evidence)
	curText := evidenceText(cur.Evidence)
	if prevText == "" || curText == "" {
		return false
	}
	if prevText == curText {
		return true
	}
	return levenshteinSimilarity(prevText, curText) >= minEvidenceSimilarity
}

func similarityMatch(prev, cur TrackedFinding) (float64, bool) {
	titleScore := levenshteinSimilarity(normalizeText(prev.Text), normalizeText(cur.Text))
	if titleScore < minTitleSimilarity {
		return 0, false
	}
	prevEvidence := evidenceText(prev.Evidence)
	curEvidence := evidenceText(cur.Evidence)
	if prevEvidence != "" && curEvidence != "" {
		evidenceScore := levenshteinSimilarity(prevEvidence, curEvidence)
		if evidenceScore < minEvidenceSimilarity {
			return 0, false
		}
		return (titleScore + evidenceScore) / 2, true
	}
	return titleScore, true
}

func methodRank(method string) int {
	switch method {
	case matchMethodStableID:
		return 0
	case matchMethodFingerprint:
		return 1
	case matchMethodEvidence:
		return 2
	default:
		return 3
	}
}

func levenshteinSimilarity(a, b string) float64 {
	if a == b {
		return 1
	}
	ar := []rune(a)
	br := []rune(b)
	if len(ar) == 0 || len(br) == 0 {
		return 0
	}
	distance := levenshteinDistance(ar, br)
	maxLen := len(ar)
	if len(br) > maxLen {
		maxLen = len(br)
	}
	return 1 - float64(distance)/float64(maxLen)
}

func levenshteinDistance(a, b []rune) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
