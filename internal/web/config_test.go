package web

import "testing"

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	if config.Addr != "127.0.0.1:8080" {
		t.Fatalf("Addr = %q", config.Addr)
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
}

func TestConfigValidateRejectsInvalidValues(t *testing.T) {
	config := DefaultConfig()
	config.MaxUploadBytes = 0
	if err := config.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}
