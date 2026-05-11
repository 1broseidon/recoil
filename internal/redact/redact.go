package redact

import (
	"regexp"
	"strings"
)

var (
	privateKeyRE = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	bearerRE     = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]+`)
	openAIKeyRE  = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`)
	awsKeyRE     = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
	secretEnvRE  = regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:API[_-]?KEY|TOKEN|SECRET|PASSWORD|PRIVATE[_-]?KEY)[A-Z0-9_]*)\s*=\s*("[^"\r\n]*"|'[^'\r\n]*'|[^\s\r\n]+)`)
)

func Content(s string) string {
	s = privateKeyRE.ReplaceAllString(s, "[REDACTED PRIVATE KEY]")
	s = bearerRE.ReplaceAllStringFunc(s, func(match string) string {
		if strings.HasPrefix(strings.ToLower(match), "bearer ") {
			return match[:7] + "[REDACTED]"
		}
		return "Bearer [REDACTED]"
	})
	s = openAIKeyRE.ReplaceAllString(s, "sk-[REDACTED]")
	s = awsKeyRE.ReplaceAllString(s, "AKIA[REDACTED]")
	s = secretEnvRE.ReplaceAllString(s, "$1=[REDACTED]")
	return s
}
