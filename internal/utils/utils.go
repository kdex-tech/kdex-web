package utils

import (
	"bytes"
	"strings"
)

func DomainsToMatcher(domains []string) string {
	var buffer bytes.Buffer
	var joiner rune

	for _, domain := range domains {
		domain = strings.ReplaceAll(domain, ".", "\\.")
		if strings.HasPrefix(domain, "*\\.") {
			domain = "." + domain
		}
		if joiner != 0 {
			buffer.WriteRune(joiner)
		}
		buffer.WriteString(domain)
		joiner = '|'
	}

	return buffer.String()
}

func IfElse[T any](predicate bool, trueVal T, elseVal T) T {
	if predicate {
		return trueVal
	}
	return elseVal
}

func MapSlice[T any, U any](slice []T, mapper func(T) U) []U {
	result := make([]U, len(slice))
	for i, v := range slice {
		result[i] = mapper(v)
	}
	return result
}
