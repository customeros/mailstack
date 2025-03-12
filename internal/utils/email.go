package utils

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
