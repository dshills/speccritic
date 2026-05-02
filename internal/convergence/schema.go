package convergence

import "github.com/dshills/speccritic/internal/schema"

// ToSchemaMeta converts a convergence result to public report metadata.
func ToSchemaMeta(result Result, mode Mode, previousHash, currentHash string) *schema.ConvergenceMeta {
	return &schema.ConvergenceMeta{
		Enabled:          true,
		Mode:             string(mode),
		Status:           string(result.Status),
		PreviousSpecHash: previousHash,
		CurrentSpecHash:  currentHash,
		Current: schema.ConvergenceCurrentCounts{
			New:       result.Summary.Current.New,
			StillOpen: result.Summary.Current.StillOpen,
			Untracked: result.Summary.Current.Untracked,
		},
		Previous: schema.ConvergenceHistoricalCounts{
			Resolved:  result.Summary.Previous.Resolved,
			Dropped:   result.Summary.Previous.Dropped,
			Untracked: result.Summary.Previous.Untracked,
		},
		BySeverity: convertCounts(result.Summary.BySeverity),
		ByKind:     convertCounts(result.Summary.ByKind),
		Notes:      append([]string(nil), result.Notes...),
	}
}

func convertCounts(in map[string]CountSet) map[string]schema.ConvergenceCounts {
	out := make(map[string]schema.ConvergenceCounts, len(in))
	for key, value := range in {
		out[key] = schema.ConvergenceCounts{
			New:       value.New,
			StillOpen: value.StillOpen,
			Resolved:  value.Resolved,
			Dropped:   value.Dropped,
			Untracked: value.Untracked,
		}
	}
	return out
}
