package textgen

import (
	"encoding/json"
	"fmt"
	"strings"
)

// extractJSON pulls the first balanced JSON object out of raw LLM text
// output. Models routinely wrap JSON in ```json fences or add a sentence of
// preamble/postamble around it despite instructions not to. Mirrors
// scenario's internal/llm.ExtractJSON — duplicated rather than imported
// since cross-module dependencies here are the exported file contract
// (manifest.json), never another module's internal packages.
func extractJSON(text string) (string, error) {
	obj, err := firstBalancedObject(stripFences(text))
	if err != nil {
		return "", err
	}
	if !json.Valid([]byte(obj)) {
		return "", fmt.Errorf("textgen: extracted text is not valid JSON: %s", truncate(obj, 200))
	}
	return obj, nil
}

func stripFences(text string) string {
	trimmed := strings.TrimSpace(text)
	start := strings.Index(trimmed, "```")
	if start == -1 {
		return trimmed
	}

	rest := trimmed[start+3:]
	if nl := strings.IndexByte(rest, '\n'); nl != -1 {
		tag := strings.TrimSpace(rest[:nl])
		if tag != "" && !strings.ContainsAny(tag, "{}\"") {
			rest = rest[nl+1:]
		}
	}

	if end := strings.Index(rest, "```"); end != -1 {
		return strings.TrimSpace(rest[:end])
	}
	return strings.TrimSpace(rest)
}

func firstBalancedObject(s string) (string, error) {
	start := strings.IndexByte(s, '{')
	if start == -1 {
		return "", fmt.Errorf("textgen: no JSON object found in text: %s", truncate(s, 200))
	}

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
			// brace inside a string literal — not structural
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1], nil
			}
		}
	}
	return "", fmt.Errorf("textgen: unbalanced JSON object in text: %s", truncate(s, 200))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
