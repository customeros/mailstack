package utils

import (
	"crypto/rand"
	"math/big"
	"regexp"
	"strings"
)

const (
	charsetAlphaNumeric      = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	charsetLowerAlphaNumeric = "abcdefghijklmnopqrstuvwxyz0123456789"
	charsetLowerAlpha        = "abcdefghijklmnopqrstuvwxyz"
	charsetSpecial           = "!@#$%^&*()-_=+[]{}<>?"
)

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

func GenerateLowerAlpha(length int) string {
	if length < 1 {
		return ""
	}
	bytes := make([]byte, length)
	for i := range bytes {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charsetLowerAlpha))))
		if err != nil {
			panic(err)
		}
		bytes[i] = charsetLowerAlpha[num.Int64()]
	}
	return string(bytes)
}

func GenerateKey(length int, includeSpecial bool) string {
	alphaNumericLength := length
	if includeSpecial {
		alphaNumericLength--
	}
	if alphaNumericLength < 1 {
		return ""
	}
	bytes := make([]byte, alphaNumericLength)
	for i := range bytes {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charsetLowerAlphaNumeric))))
		if err != nil {
			panic(err)
		}
		bytes[i] = charsetLowerAlphaNumeric[num.Int64()]
	}
	if includeSpecial {
		specialCharIndex, err := rand.Int(rand.Reader, big.NewInt(int64(len(charsetSpecial))))
		if err != nil {
			panic(err)
		}
		bytes = append(bytes, charsetSpecial[specialCharIndex.Int64()])
	}
	return string(bytes)
}
