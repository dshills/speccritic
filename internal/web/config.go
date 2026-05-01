package web

import (
	"fmt"
	"time"
)

type Config struct {
	Addr              string
	RequestTimeout    time.Duration
	MaxUploadBytes    int64
	MaxRetainedChecks int
	RetainedCheckTTL  time.Duration
}

func DefaultConfig() Config {
	return Config{
		Addr:              "127.0.0.1:8080",
		RequestTimeout:    120 * time.Second,
		MaxUploadBytes:    1 << 20,
		MaxRetainedChecks: 25,
		RetainedCheckTTL:  30 * time.Minute,
	}
}

func (c Config) Validate() error {
	if c.Addr == "" {
		return fmt.Errorf("addr is required")
	}
	if c.RequestTimeout <= 0 {
		return fmt.Errorf("request timeout must be > 0")
	}
	if c.MaxUploadBytes <= 0 {
		return fmt.Errorf("max upload bytes must be > 0")
	}
	if c.MaxRetainedChecks <= 0 {
		return fmt.Errorf("max retained checks must be > 0")
	}
	if c.RetainedCheckTTL <= 0 {
		return fmt.Errorf("retained check TTL must be > 0")
	}
	return nil
}
