package library

import "strings"

// sanitizeFilename — copied from dock's handler_helpers.go. Removes
// anything not [A-Za-z0-9_-], replaces with '-'. Empty/all-bad input
// returns "untitled".
func sanitizeFilename(input string) string {
	if input == "" {
		return "untitled"
	}
	var b strings.Builder
	for _, r := range input {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "untitled"
	}
	return out
}
