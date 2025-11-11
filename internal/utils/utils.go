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
