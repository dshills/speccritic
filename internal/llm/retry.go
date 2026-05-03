package llm

import "strings"

const (
	DefaultMaxRepairTokens = 8192
	MaxRepairTokens        = 32768
)

// IncompleteJSON reports whether err indicates a truncated JSON response.
func IncompleteJSON(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "unexpected end of JSON input") ||
		strings.Contains(msg, "unexpected EOF")
}

// RepairMaxTokens returns the token budget to use for a repair retry call.
// It doubles the current budget (minimum +2048) capped at MaxRepairTokens.
// If current already exceeds the cap it is returned unchanged — we never
// reduce the budget on a retry.
func RepairMaxTokens(current int) int {
	if current <= 0 {
		return DefaultMaxRepairTokens
	}
	if current >= MaxRepairTokens {
		return current
	}
	next := current + 2048
	if doubled := current * 2; doubled > next {
		next = doubled
	}
	if next > MaxRepairTokens {
		return MaxRepairTokens
	}
	return next
}
