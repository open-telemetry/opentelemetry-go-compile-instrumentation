// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pgx

import (
	"os"
	"strings"
	"unicode"
)

const rawQueryEnv = "OTEL_GO_INSTRUMENTATION_PGX_RAW_QUERY"

// captureRawQuery returns true if raw, unsanitized query text has been explicitly opted into.
func captureRawQuery() bool {
	return strings.EqualFold(os.Getenv(rawQueryEnv), "true")
}

func skipLineComment(chars []rune, i int, b *strings.Builder) int {
	n := len(chars)
	for i < n && chars[i] != '\n' {
		b.WriteRune(chars[i])
		i++
	}
	return i
}

func skipBlockComment(chars []rune, i int, b *strings.Builder) int {
	n := len(chars)
	_, _ = b.WriteString("/*")
	i += 2
	for i+1 < n && (chars[i] != '*' || chars[i+1] != '/') {
		b.WriteRune(chars[i])
		i++
	}
	if i+1 < n {
		_, _ = b.WriteString("*/")
		i += 2
	}
	return i
}

func sanitizeSingleQuote(chars []rune, i int, b *strings.Builder) int {
	n := len(chars)
	b.WriteRune('?')
	i++
	for i < n {
		switch {
		case chars[i] == '\'':
			if i+1 < n && chars[i+1] == '\'' {
				i += 2
				continue
			}
			return i + 1
		case chars[i] == '\\' && i+1 < n:
			i += 2
		default:
			i++
		}
	}
	return i
}

func sanitizeDollar(chars []rune, i int, b *strings.Builder) int {
	n := len(chars)
	if i+1 < n && unicode.IsDigit(chars[i+1]) {
		b.WriteRune('$')
		i++
		for i < n && unicode.IsDigit(chars[i]) {
			b.WriteRune(chars[i])
			i++
		}
		return i
	}

	tagEnd := -1
	for j := i + 1; j < n; j++ {
		if chars[j] == '$' {
			tagEnd = j
			break
		}
		if unicode.IsSpace(chars[j]) || chars[j] == ';' || chars[j] == ',' || chars[j] == '(' {
			break
		}
	}
	if tagEnd == -1 {
		b.WriteRune('$')
		return i + 1
	}

	tag := string(chars[i : tagEnd+1])
	b.WriteRune('?')
	i = tagEnd + 1
	rest := string(chars[i:])
	closeIdx := strings.Index(rest, tag)
	if closeIdx != -1 {
		return i + closeIdx + len(tag)
	}
	return n
}

func sanitizeNumber(chars []rune, i int, b *strings.Builder) int {
	n := len(chars)
	prevIsIdent := i > 0 && (unicode.IsLetter(chars[i-1]) || chars[i-1] == '_' || chars[i-1] == '$')
	if prevIsIdent {
		b.WriteRune(chars[i])
		return i + 1
	}

	b.WriteRune('?')
	for i < n {
		c := chars[i]
		if unicode.IsDigit(c) || c == '.' || c == 'e' || c == 'E' {
			i++
			continue
		}
		if (c == '+' || c == '-') && i > 0 && (chars[i-1] == 'e' || chars[i-1] == 'E') {
			i++
			continue
		}
		break
	}
	return i
}

// sanitizeSQL sanitizes literal strings, numbers, and dollar-quoted literals from SQL queries
// to prevent leaking sensitive values in telemetry spans.
func sanitizeSQL(query string) string {
	if captureRawQuery() {
		return query
	}

	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(trimmed))
	chars := []rune(trimmed)
	n := len(chars)
	i := 0

	for i < n {
		c := chars[i]
		switch {
		case c == '-' && i+1 < n && chars[i+1] == '-':
			i = skipLineComment(chars, i, &b)
		case c == '/' && i+1 < n && chars[i+1] == '*':
			i = skipBlockComment(chars, i, &b)
		case c == '\'':
			i = sanitizeSingleQuote(chars, i, &b)
		case c == '$':
			i = sanitizeDollar(chars, i, &b)
		case unicode.IsDigit(c):
			i = sanitizeNumber(chars, i, &b)
		default:
			b.WriteRune(c)
			i++
		}
	}

	return b.String()
}
