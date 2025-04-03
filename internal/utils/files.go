package utils

import (
	"path/filepath"
	"strings"
)

// sanitizeFilename removes or replaces characters that might be problematic in filenames
func SanitizeFilename(filename string) string {
	// Replace problematic characters with underscores
	replacer := strings.NewReplacer(
		"/", "_", // Forward slash
		"\\", "_", // Backslash
		":", "_", // Colon
		"*", "_", // Asterisk
		"?", "_", // Question mark
		"\"", "_", // Double quote
		"<", "_", // Less than
		">", "_", // Greater than
		"|", "_", // Pipe
		"#", "_", // Hash
		"%", "_", // Percent
		"&", "_", // Ampersand
		"{", "_", // Left curly brace
		"}", "_", // Right curly brace
		"$", "_", // Dollar sign
		"!", "_", // Exclamation mark
		"'", "_", // Single quote
		"`", "_", // Backtick
		" ", "_", // Space
	)

	// Replace problematic characters
	safe := replacer.Replace(filename)

	// Trim spaces and dots at the beginning and end
	safe = strings.Trim(safe, " .")

	// Replace multiple sequential underscores with a single one
	for strings.Contains(safe, "__") {
		safe = strings.ReplaceAll(safe, "__", "_")
	}

	// If the filename is empty after sanitization, use a default name
	if safe == "" {
		safe = "file"
	}

	// Limit filename length to 255 characters (common filesystem limit)
	if len(safe) > 255 {
		// If there's an extension, preserve it
		ext := filepath.Ext(safe)
		basename := safe[:len(safe)-len(ext)]
		if len(ext) > 245 { // If extension itself is too long (unlikely)
			ext = ext[:245]
		}
		safe = basename[:255-len(ext)] + ext
	}

	return safe
}
