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

func ExtractDomainFromEmail(email string) string {
	if email == "" {
		return ""
	}

	// Remove any potential surrounding whitespace
	email = strings.TrimSpace(email)

	// Handle potential angle brackets in email (e.g., "Name <email@domain.com>")
	if strings.Contains(email, "<") && strings.Contains(email, ">") {
		startIdx := strings.LastIndex(email, "<") + 1
		endIdx := strings.LastIndex(email, ">")
		if startIdx > 0 && endIdx > startIdx {
			email = email[startIdx:endIdx]
		}
	}

	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return ""
	}

	domain := strings.TrimSpace(parts[1])

	domain = strings.ToLower(domain)

	return domain
}
