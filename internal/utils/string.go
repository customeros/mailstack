package utils

import (
	"regexp"
	"strings"
)

func IsStringInSlice(s string, slice []string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// normalizeSubject removes prefixes like Re:, Fwd:, etc. from a subject
func NormalizeEmailSubject(subject string) string {
	subject = strings.TrimSpace(subject)
	prefixRegex := regexp.MustCompile(`(?i)^(Re|Fwd|Fw)(\[\d+\])?:\s*`)
	for prefixRegex.MatchString(subject) {
		subject = prefixRegex.ReplaceAllString(subject, "")
		subject = strings.TrimSpace(subject)
	}
	return subject
}

func NormalizeMessageID(messageID string) string {
	messageID = strings.TrimSpace(messageID)
	messageID = strings.TrimPrefix(messageID, "<")
	messageID = strings.TrimSuffix(messageID, ">")
	return messageID
}
