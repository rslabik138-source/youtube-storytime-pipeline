package llm

import "testing"

func TestExtractJSONPlain(t *testing.T) {
	got, err := ExtractJSON(`{"a": 1, "b": "two"}`)
	if err != nil {
		t.Fatalf("ExtractJSON: %v", err)
	}
	if got != `{"a": 1, "b": "two"}` {
		t.Fatalf("got %q", got)
	}
}

func TestExtractJSONFencedWithLanguageTag(t *testing.T) {
	got, err := ExtractJSON("```json\n{\"a\": 1}\n```")
	if err != nil {
		t.Fatalf("ExtractJSON: %v", err)
	}
	if got != `{"a": 1}` {
		t.Fatalf("got %q", got)
	}
}

func TestExtractJSONFencedWithoutLanguageTag(t *testing.T) {
	got, err := ExtractJSON("```\n{\"a\": 1}\n```")
	if err != nil {
		t.Fatalf("ExtractJSON: %v", err)
	}
	if got != `{"a": 1}` {
		t.Fatalf("got %q", got)
	}
}

func TestExtractJSONWithPreambleAndPostamble(t *testing.T) {
	got, err := ExtractJSON("Sure, here is the result:\n{\"a\": 1}\nLet me know if you need anything else.")
	if err != nil {
		t.Fatalf("ExtractJSON: %v", err)
	}
	if got != `{"a": 1}` {
		t.Fatalf("got %q", got)
	}
}

func TestExtractJSONNestedObjects(t *testing.T) {
	src := `{"a": {"b": {"c": 1}}, "d": [1, 2, 3]}`
	got, err := ExtractJSON("preamble " + src + " postamble")
	if err != nil {
		t.Fatalf("ExtractJSON: %v", err)
	}
	if got != src {
		t.Fatalf("got %q, want %q", got, src)
	}
}

func TestExtractJSONBracesInsideStrings(t *testing.T) {
	src := `{"text": "this has a } brace and a { one too", "n": 2}`
	got, err := ExtractJSON(src)
	if err != nil {
		t.Fatalf("ExtractJSON: %v", err)
	}
	if got != src {
		t.Fatalf("got %q, want %q", got, src)
	}
}

func TestExtractJSONEscapedQuoteInsideString(t *testing.T) {
	src := `{"text": "she said \"hi\" and left", "n": 2}`
	got, err := ExtractJSON(src)
	if err != nil {
		t.Fatalf("ExtractJSON: %v", err)
	}
	if got != src {
		t.Fatalf("got %q, want %q", got, src)
	}
}

func TestExtractJSONNoObjectFoundReturnsError(t *testing.T) {
	if _, err := ExtractJSON("no json here at all"); err == nil {
		t.Fatalf("expected an error when no JSON object is present")
	}
}

func TestExtractJSONUnbalancedReturnsError(t *testing.T) {
	if _, err := ExtractJSON(`{"a": 1, "b": {"c": 2}`); err == nil {
		t.Fatalf("expected an error for an unbalanced JSON object")
	}
}

func TestExtractJSONInvalidJSONReturnsError(t *testing.T) {
	if _, err := ExtractJSON(`{a: 1,}`); err == nil {
		t.Fatalf("expected an error for structurally-balanced but invalid JSON")
	}
}
