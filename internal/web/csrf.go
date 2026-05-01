package web

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func newNonce() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
