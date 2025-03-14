package utils

import (
	"regexp"
	"strings"
)

func UniqueEmails(emails []string) []string {
	seen := make(map[string]struct{}, len(emails))
	unique := make([]string, 0, len(emails))

	for _, email := range emails {
		if _, exists := seen[email]; !exists {
			seen[email] = struct{}{}
			unique = append(unique, email)
		}
	}

	return unique
}

func NormalizeSubject(subject string) string {
	// Remove common prefixes, case insensitive
	re := regexp.MustCompile(`(?i)^(re|fwd|fw|aw|ant|sv|vs|r|रे|转发|转|答复){0,1}(\s*:|\s*\[\d+\]\s*:)*\s*`)
	normalized := re.ReplaceAllString(subject, "")

	// Trim spaces
	normalized = strings.TrimSpace(normalized)

	return normalized
}
