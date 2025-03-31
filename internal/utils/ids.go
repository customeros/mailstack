package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
)

const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// generateMessageID creates a unique message ID for the email
func GenerateMessageID(domain, metadata string) string {
	id, err := gonanoid.Generate(alphabet, 12)
	if err != nil {
		panic(err)
	}

	timestamp := time.Now().UnixMicro()

	var hashComponent string
	if metadata != "" {
		hash := sha256.Sum256([]byte(metadata))
		hashComponent = fmt.Sprintf(".%x", hash[:4])
	}

	// Step 4: Format according to RFC 5322
	localPart := fmt.Sprintf("%d.%s%s", timestamp, id, hashComponent)
	return fmt.Sprintf("<%s@%s>", localPart, domain)
}

func GenerateNanoID(length int) string {
	id, err := gonanoid.Generate(alphabet, length)
	if err != nil {
		panic(err)
	}
	return id
}

func GenerateNanoIDWithPrefix(prefix string, length int) string {
	alphabet := "abcdefghijklmnopqrstuvwxyz0123456789"
	id, err := gonanoid.Generate(alphabet, length)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%s_%s", prefix, id)
}

func GenerateIMAPHash(mailboxID, folder string, uid uint32) string {
	// Create a deterministic string combining all elements
	// Using a separator that's unlikely to appear in mailboxID or folder
	combined := fmt.Sprintf("%s::%s::%d", mailboxID, folder, uid)

	// Generate SHA-256 hash
	hasher := sha256.New()
	hasher.Write([]byte(combined))
	hashBytes := hasher.Sum(nil)

	// Convert to hex string
	hashString := hex.EncodeToString(hashBytes)

	// Return a shortened version (first 16 characters is usually sufficient)
	// You can adjust the length based on your collision probability needs
	return hashString[:21]
}
