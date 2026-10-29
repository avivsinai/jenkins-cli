package shared

import (
	"strings"
	"unicode"
)

// BoundedHumanText produces one bounded line for terminal output. It removes
// control characters and collapses whitespace while preserving Unicode text.
// Structured outputs should retain the original value instead.
func BoundedHumanText(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	var out strings.Builder
	count := 0
	space := false
	for _, r := range value {
		if unicode.IsSpace(r) {
			space = count > 0
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		if count >= limit || (space && count+1 >= limit) {
			out.WriteString("...")
			break
		}
		if space {
			out.WriteByte(' ')
			count++
			space = false
		}
		out.WriteRune(r)
		count++
	}
	return out.String()
}
