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
		"Authorization: Bearer\n  type: string\n",
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

// A quoted value that really does span lines is a secret on every one of them.
// Limiting every pattern to a single line would leave such a value readable.
func TestRedact_QuotedValueSpanningLinesIsRedactedOnEveryLine(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		want     string
		wantGone []string
	}{
		{
			name:     "double quotes then a comment",
			input:    "before\nprivate_key: \"first part\nsecond part\nthird part\" # rotate yearly\nafter\n",
			want:     "before\n[REDACTED]\n[REDACTED]\n[REDACTED] # rotate yearly\nafter\n",
			wantGone: []string{"first part", "second part", "third part"},
		},
		{
			name:     "double quotes then a line comment",
			input:    "before\nprivate_key: \"first part\nsecond part\" // rotate yearly\nafter\n",
			want:     "before\n[REDACTED]\n[REDACTED] // rotate yearly\nafter\n",
			wantGone: []string{"first part", "second part"},
		},
		{
			name:     "double quotes then a block comment",
			input:    "before\nprivate_key: \"first part\nsecond part\"/* rotate yearly */\nafter\n",
			want:     "before\n[REDACTED]\n[REDACTED]/* rotate yearly */\nafter\n",
			wantGone: []string{"first part", "second part"},
		},
		{
			name:     "double quotes then more text",
			input:    "before\nprivate_key: \"first part\nsecond part\" is the signing key.\nafter\n",
			want:     "before\n[REDACTED]\n[REDACTED] is the signing key.\nafter\n",
			wantGone: []string{"first part", "second part"},
		},
		{
			name:     "single quotes at end of line",
			input:    "before\nclient_secret = 'first part\nsecond part'\nafter\n",
			want:     "before\n[REDACTED]\n[REDACTED]\nafter\n",
			wantGone: []string{"first part", "second part"},
		},
		{
			name:     "backticks at end of input",
			input:    "before\naccess_token: `first part\nsecond part`",
			want:     "before\n[REDACTED]\n[REDACTED]",
			wantGone: []string{"first part", "second part"},
		},
		{
			name:     "json string followed by a comma",
			input:    "{\n  \"api_key\": \"first part\nsecond part\",\n  \"name\": \"service\"\n}\n",
			want:     "{\n  \"[REDACTED]\n[REDACTED],\n  \"name\": \"service\"\n}\n",
			wantGone: []string{"first part", "second part"},
		},
		{
			name:     "windows line endings",
			input:    "before\r\nsecret_key: \"first part\r\nsecond part\"\r\nafter\r\n",
			want:     "before\r\n[REDACTED]\n[REDACTED]\r\nafter\r\n",
			wantGone: []string{"first part", "second part"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := Redact(tc.input)
			assertNewlinesPreserved(t, tc.input, out)
			for _, secret := range tc.wantGone {
				if strings.Contains(out, secret) {
					t.Errorf("secret %q still present: %q", secret, out)
				}
			}
			if out != tc.want {
				t.Errorf("unexpected output:\ngot:  %q\nwant: %q", out, tc.want)
			}
		})
	}
}

// A token-shaped run on the line after "Bearer" is treated as a wrapped token.
// A short word there, as in a schema, is not long enough to match.
func TestRedact_BearerTokenWrappedOntoNextLine(t *testing.T) {
	input := "before\nAuthorization: Bearer\n  abcdefghijklmnopqrstuvwxyz0123456789\nafter\n"
	want := "before\nAuthorization: [REDACTED]\n[REDACTED]\nafter\n"
	out := Redact(input)
	assertNewlinesPreserved(t, input, out)
	if out != want {
		t.Errorf("unexpected output:\ngot:  %q\nwant: %q", out, want)
	}
	if !ContainsSecret(input) {
		t.Error("ContainsSecret returned false for a wrapped bearer token")
	}
}

// When the quote that follows an unclosed one does not end a value, only the
// key's own line is redacted: the secret on it goes, the lines below stay.
func TestRedact_UnclosedQuoteIsRedactedToEndOfItsLine(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"no quote anywhere below", "api_key: \"abc def\nno quote on this line\nlast line\n", "[REDACTED]\nno quote on this line\nlast line\n"},
		{"apostrophe in later prose", "api_key: 'abc def\nThe user's guide\nnext line\n", "[REDACTED]\nThe user's guide\nnext line\n"},
		{"quoted phrase on a later line", "client_secret = \"abc def\nsaid \"hello\"\nnext line\n", "[REDACTED]\nsaid \"hello\"\nnext line\n"},
		{"inline code on a later line", "auth_token: `abc def\nrun `make`\nnext line\n", "[REDACTED]\nrun `make`\nnext line\n"},
		{"apostrophe before an accented letter", "api_key: 'abc def\nl'état des lieux\nnext line\n", "[REDACTED]\nl'état des lieux\nnext line\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if out := Redact(tc.input); out != tc.want {
				t.Errorf("unexpected output:\ngot:  %q\nwant: %q", out, tc.want)
			}
		})
	}
}

func TestRedact_QuotedValueOnOneLine(t *testing.T) {
	cases := map[string]struct{ input, want string }{
		"text after the closing quote is kept": {"api_key: \"abc\" is the key\n", "[REDACTED] is the key\n"},
		"empty value is not a secret":          {"api_key: \"\" # set from the environment\n", "api_key: \"\" # set from the environment\n"},
		"delimiter after the value is kept":    {"{\"api_key\": \"abc\", \"n\": 1}\n", "{\"[REDACTED], \"n\": 1}\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if out := Redact(tc.input); out != tc.want {
				t.Errorf("unexpected output:\ngot:  %q\nwant: %q", out, tc.want)
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
