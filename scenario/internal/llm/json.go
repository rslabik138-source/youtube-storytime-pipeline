package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ExtractJSON pulls the first balanced JSON object out of raw LLM text
// output. Models routinely wrap JSON in ```json fences or add a sentence of
// preamble/postamble around it despite instructions not to — generate,
// review, and continuity all call this instead of parsing resp.Text
// directly.
func ExtractJSON(text string) (string, error) {
	obj, err := firstBalancedObject(stripFences(text))
	if err != nil {
		return "", err
	}
	if !json.Valid([]byte(obj)) {
		return "", fmt.Errorf("llm: extracted text is not valid JSON: %s", truncate(obj, 200))
	}
	return obj, nil
}

// stripFences removes a leading/trailing ``` or ```json code fence if one is
// present, leaving whatever is between untouched. Text without fences is
// returned trimmed and otherwise unchanged.
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

// firstBalancedObject scans for the first '{' and returns the substring up
// to its matching '}', tracking string literals so braces inside quoted
// text don't throw off the depth count.
func firstBalancedObject(s string) (string, error) {
	start := strings.IndexByte(s, '{')
	if start == -1 {
		return "", fmt.Errorf("llm: no JSON object found in text: %s", truncate(s, 200))
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
	return "", fmt.Errorf("llm: unbalanced JSON object in text: %s", truncate(s, 200))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
