package utils

import "strings"

func IsStringInSlice(s string, slice []string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func SliceToString(slice []string) string {
	return strings.Join(slice, ",")
}

func StringToSlice(str string) []string {
	if str == "" {
		return []string{}
	}
	return strings.Split(str, ",")
}
