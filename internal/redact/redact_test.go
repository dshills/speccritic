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

func assertNewlinesPreserved(t *testing.T, input, out string) {
	t.Helper()
	if before, after := strings.Count(input, "\n"), strings.Count(out, "\n"); before != after {
		t.Errorf("line count changed after redaction: before=%d after=%d\nin:  %q\nout: %q", before, after, input, out)
	}
}

func TestRedact_LineCountPreservedForEveryPattern(t *testing.T) {
	// Each case puts one secret between two ordinary lines. The secret must go,
	// the newline count must hold, and the surrounding lines must survive.
	cases := []struct {
		name   string
		input  string
		secret string
		want   string
	}{
		{
			name:   "aws access key",
			input:  "before\naccess_key = AKIAIOSFODNN7EXAMPLE\nafter line\n",
			secret: "AKIAIOSFODNN7EXAMPLE",
			want:   "before\naccess_key = [REDACTED]\nafter line\n",
		},
		{
			name:   "sk- key",
			input:  "before\nkey sk-abcdefghijklmnopqrstuvwxyz123456\nafter line\n",
			secret: "sk-abcdefghijklmnopqrstuvwxyz123456",
			want:   "before\nkey [REDACTED]\nafter line\n",
		},
		{
			name:   "jwt",
			input:  "before\njwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c\nafter line\n",
			secret: "eyJhbGciOiJIUzI1NiJ9",
			want:   "before\njwt [REDACTED]\nafter line\n",
		},
		{
			name:   "bearer token",
			input:  "before\nAuthorization: Bearer abcdefghijklmnopqrstuvwxyz0123456789\nafter line\n",
			secret: "abcdefghijklmnopqrstuvwxyz0123456789",
			want:   "before\nAuthorization: [REDACTED]\nafter line\n",
		},
		{
			name:   "password",
			input:  "before\npassword: supersecret123\nafter line\n",
			secret: "supersecret123",
			want:   "before\n[REDACTED]\nafter line\n",
		},
		{
			name:   "api key",
			input:  "before\napi_key = \"abc123def456\"\nafter line\n",
			secret: "abc123def456",
			want:   "before\n[REDACTED]\nafter line\n",
		},
		{
			name:   "client secret",
			input:  "before\nclient_secret: 'shh-very-secret'\nafter line\n",
			secret: "shh-very-secret",
			want:   "before\n[REDACTED]\nafter line\n",
		},
		{
			name:   "refresh token",
			input:  "before\nrefresh_token=tok_live_12345\nafter line\n",
			secret: "tok_live_12345",
			want:   "before\n[REDACTED]\nafter line\n",
		},
		{
			name:   "pem block",
			input:  "before\n-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n-----END RSA PRIVATE KEY-----\nafter line\n",
			secret: "MIIEowIBAAKCAQEA",
			want:   "before\n[REDACTED]\n[REDACTED]\n[REDACTED]\nafter line\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := Redact(tc.input)
			assertNewlinesPreserved(t, tc.input, out)
			if strings.Contains(out, tc.secret) {
				t.Errorf("secret not redacted: %q", out)
			}
			if out != tc.want {
				t.Errorf("unexpected output:\ngot:  %q\nwant: %q", out, tc.want)
			}
		})
	}

	// A pattern added to the table without a case above would go unchecked.
	for i, p := range patterns {
		covered := false
		for _, tc := range cases {
			if p.re.MatchString(tc.input) {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("patterns[%d] (%s) has no line-count case", i, p.re)
		}
	}
}

func TestRedact_KeyWithoutValueOnSameLine(t *testing.T) {
	// A key whose line ends at the separator names a field; whatever is on the
	// next line is not its value. Nothing here is a secret, so nothing may
	// change — in particular the match must not reach across the newline and
	// swallow the following line.
	inputs := []string{
		"## Login schema\nproperties:\n  password:\n    type: string\n  api_key:\n    type: string\nNext requirement line\n",
		"  Password:\n    type: string\n",
		"  apiKey:\n    type: string\n",
		"  api-key:\n    type: string\n",
		"  \"api_key\":\n    {\"type\": \"string\"}\n",
		"  client_secret:\n    type: string\n",
		"  clientSecret:\n    type: string\n",
		"  secret_key:\n    type: string\n",
		"  private_key:\n    type: string\n",
		"  auth_token:\n    type: string\n",
		"  accessToken:\n    type: string\n",
		"  refresh_token:\n    type: string\n",
		"Authorization: Bearer\nabcdefghijklmnopqrstuvwxyz0123456789\n",
		"password =\nnext line\n",
		"password: \t\nnext line\n",
		"password\n: next line\n",
		"auth_token\n=\nnext line\n",
		"password:\r\n  type: string\r\n",
		"api_key:\r\n  type: string\r\n",
		"password:",
	}
	for _, input := range inputs {
		out := Redact(input)
		assertNewlinesPreserved(t, input, out)
		if out != input {
			t.Errorf("key without a value was modified:\ngot:  %q\nwant: %q", out, input)
		}
		if ContainsSecret(input) {
			t.Errorf("ContainsSecret returned true for key without a value: %q", input)
		}
	}
}

func TestRedact_QuotedValueDoesNotSpanLines(t *testing.T) {
	// An opening quote with no closing quote on its own line must not pair
	// with a quote further down and take the lines in between with it.
	cases := []struct {
		name   string
		input  string
		secret string
	}{
		{name: "single quote closed by a later apostrophe", input: "api_key: 'see vault\nThe user's guide\nnext line\n"},
		{name: "double quote closed on a later line", input: "client_secret = \"see vault\nsaid \"hello\"\nnext line\n"},
		{name: "backtick closed on a later line", input: "auth_token: `see vault\nrun `make`\nnext line\n"},
		{
			name:   "quoted pem block",
			input:  "private_key: \"-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n-----END RSA PRIVATE KEY-----\"\nnext line\n",
			secret: "MIIEowIBAAKCAQEA",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := Redact(tc.input)
			assertNewlinesPreserved(t, tc.input, out)
			if tc.secret != "" {
				if strings.Contains(out, tc.secret) {
					t.Errorf("secret not redacted: %q", out)
				}
				if !strings.HasSuffix(out, "\nnext line\n") {
					t.Errorf("line after the block was modified: %q", out)
				}
				return
			}
			// Every line after the one holding the key must come through intact.
			_, wantRest, _ := strings.Cut(tc.input, "\n")
			_, gotRest, _ := strings.Cut(out, "\n")
			if gotRest != wantRest {
				t.Errorf("following lines were modified:\ngot:  %q\nwant: %q", gotRest, wantRest)
			}
		})
	}
}
