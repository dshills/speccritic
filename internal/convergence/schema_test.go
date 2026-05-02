package convergence

import (
	"testing"
)

func TestToSchemaMeta(t *testing.T) {
	result := Result{
		Status: StatusComplete,
		Summary: Summary{
			Current:  CountSet{New: 1, StillOpen: 2},
			Previous: HistoricalCountSet{Resolved: 3, Dropped: 1},
			BySeverity: map[string]CountSet{
				"CRITICAL": {New: 1, Resolved: 1},
			},
			ByKind: map[string]CountSet{
				"issue": {StillOpen: 2},
			},
		},
		Notes: []string{"note"},
	}
	meta := ToSchemaMeta(result, ModeAuto, "sha256:old", "sha256:new")
	if !meta.Enabled || meta.Current.New != 1 || meta.Previous.Resolved != 3 || meta.BySeverity["CRITICAL"].Resolved != 1 {
		t.Fatalf("meta = %#v", meta)
	}
}
