package redact

import (
	"strings"
	"testing"
)

func TestRedact_AnthropicKey(t *testing.T) {
	input := `api_key = sk-abcdefghijklmnopqrstuvwxyz123456`
	out := Redact(input)
	if strings.Contains(out, "sk-abcdefghijklmno") {
		t.Errorf("Anthropic key not redacted: %q", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in output: %q", out)
	}
}

func TestRedact_AWSKey(t *testing.T) {
	input := "access_key = AKIAIOSFODNN7EXAMPLE"
	out := Redact(input)
	if strings.Contains(out, "AKIA") {
		t.Errorf("AWS key not redacted: %q", out)
	}
}

func TestRedact_BearerToken(t *testing.T) {
	// Token must be ≥20 chars to avoid false positives
	input := "Authorization: Bearer abcdefghijklmnopqrstuvwxyz0123456789"
	out := Redact(input)
	if strings.Contains(out, "abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Errorf("bearer token not redacted: %q", out)
	}
}

func TestRedact_JWT(t *testing.T) {
	input := "token = eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	out := Redact(input)
	if strings.Contains(out, "eyJhbGci") {
		t.Errorf("JWT not redacted: %q", out)
	}
}

func TestRedact_Password(t *testing.T) {
	input := "password: supersecret123"
	out := Redact(input)
	if strings.Contains(out, "supersecret123") {
		t.Errorf("password not redacted: %q", out)
	}
}

func TestRedact_PEMBlock(t *testing.T) {
	input := "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n-----END RSA PRIVATE KEY-----"
	out := Redact(input)
	if strings.Contains(out, "MIIEowIBAAKCAQEA") {
		t.Errorf("PEM block not redacted: %q", out)
	}
}

func TestRedact_NonSecretUnchanged(t *testing.T) {
	input := "This is a normal specification with no secrets.\nIt has multiple lines."
	out := Redact(input)
	if out != input {
		t.Errorf("non-secret text was modified:\ngot:  %q\nwant: %q", out, input)
	}
}

func TestContainsSecret(t *testing.T) {
	if !ContainsSecret("access_key = AKIAIOSFODNN7EXAMPLE") {
		t.Fatal("ContainsSecret returned false for AWS access key")
	}
	if ContainsSecret("This is a normal specification with no secrets.") {
		t.Fatal("ContainsSecret returned true for normal text")
	}
}

func TestRedact_MixedCasePasswordAlongsideOtherTrigger(t *testing.T) {
	// Regression: a mixed-case password on one line alongside another
	// secret type must still be redacted — the per-pattern trigger gate
	// must match the regex's case-insensitive semantics.
	input := "api_key = sk-abcdefghijklmnopqrstuvwxyz123456\nPassword: supersecret123"
	out := Redact(input)
	if strings.Contains(out, "supersecret123") {
		t.Errorf("mixed-case Password not redacted: %q", out)
	}
}

func TestRedact_MixedCaseBearerAlongsideOtherTrigger(t *testing.T) {
	input := "access_key = AKIAIOSFODNN7EXAMPLE\nAuthorization: BEARER abcdefghijklmnopqrstuvwxyz0123456789"
	out := Redact(input)
	if strings.Contains(out, "abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Errorf("uppercase BEARER token not redacted: %q", out)
	}
}

func TestRedact_LineCountPreserved(t *testing.T) {
	// PEM block spans multiple lines — after redaction line count must be unchanged.
	input := "line1\n-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n-----END RSA PRIVATE KEY-----\nline5"
	out := Redact(input)
	inLines := strings.Count(input, "\n")
	outLines := strings.Count(out, "\n")
	if inLines != outLines {
		t.Errorf("line count changed after redaction: before=%d after=%d\nout: %q", inLines, outLines, out)
	}
	if strings.Contains(out, "MIIEowIBAAKCAQEA") {
		t.Errorf("PEM content still present after redaction: %q", out)
	}
}

// A key at the end of a line has no value on that line, so nothing on it or on
// the line below is a secret. YAML schema snippets in specs look exactly like
// this, and matching across the line break used to merge the two lines.
func TestRedact_KeyAtEndOfLineLeavesBothLinesAlone(t *testing.T) {
	keys := []string{
		"password:", "Password =", "api_key:", "apiKey:", `"api-key":`,
		"client_secret:", "secret_key:", "private_key:",
		"auth_token:", "accessToken:", "refresh_token =",
		"Authorization: Bearer",
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			input := "properties:\n  " + key + "\n    type: string_of_at_least_twenty_chars\nnext requirement\n"
			if out := Redact(input); out != input {
				t.Errorf("input changed:\n in: %q\nout: %q", input, out)
			}
		})
	}
}

func TestRedact_PreservesEveryLine(t *testing.T) {
	cases := map[string]struct {
		input      string
		wantGone   []string
		wantIntact []string
	}{
		"yaml schema with secret-named keys": {
			input:      "## Login schema\nproperties:\n  password:\n    type: string\n  api_key:\n    type: string\nNext requirement line\n",
			wantIntact: []string{"  password:", "    type: string", "  api_key:", "Next requirement line"},
		},
		"values on the same line are still redacted": {
			input:      "password: hunter2\n  api_key: abc123\nclient_secret = \"s3cret value\"\nrefresh_token: 'tok en'\nplain line\n",
			wantGone:   []string{"hunter2", "abc123", "s3cret value", "tok en"},
			wantIntact: []string{"plain line"},
		},
		"quoted value over several lines is redacted on every line": {
			input:      "before\nprivate_key: \"first part\nsecond part\nthird part\" # rotate yearly\nafter\n",
			wantGone:   []string{"first part", "second part", "third part"},
			wantIntact: []string{"before", "after"},
		},
		"single-quoted value over several lines": {
			input:      "before\nclient_secret = 'first part\nsecond part'\nafter\n",
			wantGone:   []string{"first part", "second part"},
			wantIntact: []string{"before", "after"},
		},
		"backtick value over several lines": {
			input:      "before\naccess_token: `first part\nsecond part`\nafter\n",
			wantGone:   []string{"first part", "second part"},
			wantIntact: []string{"before", "after"},
		},
		"quote never closed is redacted to the end of its line": {
			input:      "api_key: \"abc def\nno quote on this line\nlast line\n",
			wantGone:   []string{"abc def"},
			wantIntact: []string{"no quote on this line", "last line"},
		},
		"empty quoted value is not a secret": {
			input:      "api_key: \"\" # set from the environment\nnext line\n",
			wantIntact: []string{"api_key: \"\" # set from the environment", "next line"},
		},
		"bearer token wrapped onto the next line": {
			input:      "before\nAuthorization: Bearer\n  abcdefghijklmnopqrstuvwxyz012345\nafter\n",
			wantGone:   []string{"abcdefghijklmnopqrstuvwxyz012345"},
			wantIntact: []string{"before", "after"},
		},
		"pem block": {
			input:      "before\n-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n-----END RSA PRIVATE KEY-----\nafter\n",
			wantGone:   []string{"MIIEowIBAAKCAQEA"},
			wantIntact: []string{"before", "after"},
		},
		"windows line endings": {
			input:      "password:\r\n  type: string\r\napi_key: abc123\r\nend\r\n",
			wantGone:   []string{"abc123"},
			wantIntact: []string{"password:\r", "  type: string\r", "end\r"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			out := Redact(tc.input)
			inLines := strings.Split(tc.input, "\n")
			outLines := strings.Split(out, "\n")
			if len(inLines) != len(outLines) {
				t.Fatalf("line count changed: before=%d after=%d\nout: %q", len(inLines), len(outLines), out)
			}
			for _, secret := range tc.wantGone {
				if strings.Contains(out, secret) {
					t.Errorf("secret %q still present: %q", secret, out)
				}
			}
			for _, line := range tc.wantIntact {
				kept := false
				for i := range inLines {
					if inLines[i] == line && outLines[i] == line {
						kept = true
						break
					}
				}
				if !kept {
					t.Errorf("line %q was changed or moved:\nout: %q", line, out)
				}
			}
		})
	}
}

// Whatever the patterns match, Redact must hand back the same number of lines.
// Line breaks are placed at every position of pairs of secret-shaped samples to
// cover matches that start, end or continue across a break.
func TestRedact_LineCountSurvivesBreaksAnywhere(t *testing.T) {
	samples := []string{
		"AKIAABCDEFGHIJKLMNOP", "sk-abcdefghijklmnopqrstuvwx", "eyJabc.def.ghi",
		"Bearer abcdefghijklmnopqrstuvwxyz", "password: hunter2",
		`api_key: "abc def"`, "client_secret: 'abc def'", "access_token: `abc def`",
		`"api_key": "abc"`, "private_key=abc", "refresh_token = abc",
		"-----BEGIN RSA PRIVATE KEY-----MIIE-----END RSA PRIVATE KEY-----",
	}
	for _, a := range samples {
		for _, b := range samples {
			joined := a + " " + b
			for cut := 1; cut < len(joined); cut++ {
				input := joined[:cut] + "\n" + joined[cut:] + "\ntrailing line\n"
				out := Redact(input)
				if got, want := strings.Count(out, "\n"), strings.Count(input, "\n"); got != want {
					t.Fatalf("line count %d, want %d\n in: %q\nout: %q", got, want, input, out)
				}
				if !strings.HasSuffix(out, "\ntrailing line\n") {
					t.Fatalf("trailing line lost\n in: %q\nout: %q", input, out)
				}
			}
		}
	}
}
