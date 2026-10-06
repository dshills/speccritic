package validate

import (
	"fmt"
	"strings"
	"testing"
)

const (
	writtenModelIssue = `{"id":"ISSUE-0001","severity":"WARN","category":"AMBIGUOUS_BEHAVIOR","title":"Model finding","evidence":[{"path":"SPEC.md","line_start":3,"line_end":3,"quote":"q"}],"tags":[]}`
	// One rule matched on two lines, so both findings carry its ID.
	writtenPreflightIssues = `{"id":"PREFLIGHT-VAGUE-001","severity":"WARN","category":"NON_TESTABLE_REQUIREMENT","title":"Vague term","evidence":[{"path":"SPEC.md","line_start":5,"line_end":5,"quote":"fast"}],"tags":["preflight","preflight-rule:PREFLIGHT-VAGUE-001"]},
		{"id":"PREFLIGHT-VAGUE-001","severity":"WARN","category":"NON_TESTABLE_REQUIREMENT","title":"Vague term","evidence":[{"path":"SPEC.md","line_start":9,"line_end":9,"quote":"quickly"}],"tags":["preflight","preflight-rule:PREFLIGHT-VAGUE-001"]}`
	writtenQuestion = `{"id":"Q-0001","severity":"WARN","question":"What is the timeout?","evidence":[{"path":"SPEC.md","line_start":2,"line_end":2,"quote":"q"}]}`
)

func writtenReport(issues, questions, patches string) string {
	return fmt.Sprintf(`{"tool":"speccritic","version":"1.0","input":{},"summary":{},"issues":[%s],"questions":[%s],"patches":[%s],"meta":{"model":"m"}}`, issues, questions, patches)
}

func TestParseReport_AcceptsPreflightRuleIDs(t *testing.T) {
	report, err := ParseReport([]byte(writtenReport(writtenPreflightIssues+","+writtenModelIssue, writtenQuestion, "")))
	if err != nil {
		t.Fatalf("ParseReport: %v", err)
	}
	if len(report.Issues) != 3 || len(report.Questions) != 1 {
		t.Fatalf("issues=%d questions=%d, want 3 and 1", len(report.Issues), len(report.Questions))
	}
}

func TestParseReport_DoesNotCheckEvidencePath(t *testing.T) {
	for _, path := range []string{"/work/specs/SPEC.md", "../shared/SPEC.md", ""} {
		issues := strings.ReplaceAll(writtenPreflightIssues+","+writtenModelIssue, `"path":"SPEC.md"`, fmt.Sprintf(`"path":%q`, path))
		report, err := ParseReport([]byte(writtenReport(issues, writtenQuestion, "")))
		if err != nil {
			t.Fatalf("path %q: %v", path, err)
		}
		if got := report.Issues[0].Evidence[0].Path; got != path {
			t.Errorf("path = %q, want %q kept as written", got, path)
		}
	}
}

func TestParseReport_DoesNotCheckPatches(t *testing.T) {
	// A completion patch names the preflight rule it came from, and a report
	// filtered by severity can keep a patch whose issue is no longer listed.
	patches := `{"issue_id":"PREFLIGHT-STRUCTURE-001","before":"a","after":"b"},{"issue_id":"ISSUE-0042","before":"c","after":"d"}`
	report, err := ParseReport([]byte(writtenReport(writtenModelIssue, "", patches)))
	if err != nil {
		t.Fatalf("ParseReport: %v", err)
	}
	if len(report.Patches) != 2 {
		t.Fatalf("patches = %d, want 2", len(report.Patches))
	}
}

func TestParseReport_AcceptsIDsPastFourDigits(t *testing.T) {
	issue := strings.Replace(writtenModelIssue, "ISSUE-0001", "ISSUE-10000", 1)
	question := strings.Replace(writtenQuestion, "Q-0001", "Q-10000", 1)
	if _, err := ParseReport([]byte(writtenReport(issue, question, ""))); err != nil {
		t.Fatalf("ParseReport: %v", err)
	}
}

func TestParseReport_Rejects(t *testing.T) {
	untagged := strings.Replace(writtenModelIssue, "ISSUE-0001", "PREFLIGHT-VAGUE-001", 1)
	tests := []struct {
		name      string
		issues    string
		questions string
		want      string
	}{
		{"unknown issue ID", strings.Replace(writtenModelIssue, "ISSUE-0001", "BAD-1", 1), "", "ISSUE-XXXX"},
		{"rule ID on a finding not tagged preflight", untagged, "", "ISSUE-XXXX"},
		{"malformed rule ID", strings.Replace(writtenPreflightIssues, `"id":"PREFLIGHT-VAGUE-001"`, `"id":"PREFLIGHT-vague"`, 1), "", "ISSUE-XXXX"},
		{"repeated issue number", writtenModelIssue + "," + writtenModelIssue, "", "duplicate issue ID"},
		{"invalid severity", strings.Replace(writtenModelIssue, `"WARN"`, `"HIGH"`, 1), "", "invalid severity"},
		{"unknown category", strings.Replace(writtenModelIssue, "AMBIGUOUS_BEHAVIOR", "NOT_A_CATEGORY", 1), "", "unknown category"},
		{"missing title", strings.Replace(writtenModelIssue, `"title":"Model finding"`, `"title":""`, 1), "", "title is required"},
		{"evidence line below one", strings.Replace(writtenModelIssue, `"line_start":3`, `"line_start":0`, 1), "", "line_start"},
		{"evidence range reversed", strings.Replace(writtenModelIssue, `"line_end":3`, `"line_end":2`, 1), "", "line_end"},
		{"unknown question ID", writtenModelIssue, strings.Replace(writtenQuestion, "Q-0001", "QUESTION-1", 1), "Q-XXXX"},
		{"repeated question ID", writtenModelIssue, writtenQuestion + "," + writtenQuestion, "duplicate question ID"},
		{"question evidence line below one", writtenModelIssue, strings.Replace(writtenQuestion, `"line_start":2`, `"line_start":0`, 1), "line_start"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseReport([]byte(writtenReport(tt.issues, tt.questions, "")))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func TestParseReport_RejectsInvalidJSONAndMeta(t *testing.T) {
	if _, err := ParseReport([]byte(`{`)); err == nil || !strings.Contains(err.Error(), "JSON parse failed") {
		t.Fatalf("error = %v, want a JSON parse failure", err)
	}
	badMeta := strings.Replace(writtenReport(writtenModelIssue, "", ""), `"meta":{"model":"m"}`, `"meta":{"model":"m","convergence":{"status":"done"}}`, 1)
	if _, err := ParseReport([]byte(badMeta)); err == nil || !strings.Contains(err.Error(), "meta.convergence.status") {
		t.Fatalf("error = %v, want an invalid convergence status", err)
	}
}

// What ParseReport allows in a written report stays invalid in model output.
func TestParse_KeepsModelOutputRules(t *testing.T) {
	preflightID := writtenReport(writtenPreflightIssues, "", "")
	if _, err := Parse(preflightID, 0); err == nil || !strings.Contains(err.Error(), "does not match ISSUE-XXXX format") {
		t.Errorf("Parse with a preflight rule ID: error = %v", err)
	}
	absolutePath := writtenReport(strings.Replace(writtenModelIssue, `"path":"SPEC.md"`, `"path":"/work/SPEC.md"`, 1), "", "")
	if _, err := Parse(absolutePath, 0); err == nil || !strings.Contains(err.Error(), "local relative path") {
		t.Errorf("Parse with an absolute evidence path: error = %v", err)
	}
	// The only finding is dropped, which leaves the response with nothing usable.
	if _, err := ParseResponse(absolutePath, Options{}); err == nil || !strings.Contains(err.Error(), "local relative path") {
		t.Errorf("ParseResponse with an absolute evidence path: error = %v", err)
	}
}
