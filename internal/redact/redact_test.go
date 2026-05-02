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
