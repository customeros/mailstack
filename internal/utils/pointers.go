package utils

// GetOrDefault returns the value if the pointer is not nil, otherwise returns the default value
func GetOrDefault[T any](ptr *T, defaultVal T) T {
	if ptr == nil {
		return defaultVal
	}
	return *ptr
}
