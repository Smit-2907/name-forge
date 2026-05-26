package utils

import (
	"regexp"
	"strings"
)

var (
	nonAlphaRegex = regexp.MustCompile(`[^a-zA-Z]`)
	spacesRegex   = regexp.MustCompile(`\s+`)
)

// CleanInput trims spaces and removes extra internal spaces.
func CleanInput(s string) string {
	s = strings.TrimSpace(s)
	s = spacesRegex.ReplaceAllString(s, " ")
	return s
}

// IsAlphabetic returns true if the string consists entirely of letters A-Z or a-z.
func IsAlphabetic(s string) bool {
	if s == "" {
		return false
	}
	return !nonAlphaRegex.MatchString(s)
}

// SanitizeKeyword removes any non-alphabetic characters to make keywords safe for generation engines.
func SanitizeKeyword(s string) string {
	return nonAlphaRegex.ReplaceAllString(s, "")
}
