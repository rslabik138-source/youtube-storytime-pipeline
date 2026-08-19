package portrait

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestMetaJSONRoundTrips(t *testing.T) {
	m := Meta{ID: "s1", File: "portrait.png", Prompt: "a portrait", Provider: "gemini", Model: "test-model", CostUSD: 0.039}
	b, err := m.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var got Meta
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, m) {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, m)
	}
}

func TestMetaJSONOmitsFileWhenFilesIsSet(t *testing.T) {
	m := Meta{ID: "s1", Files: []string{"variant-1.png", "variant-2.png"}, Prompt: "a portrait", CostUSD: 0.078}
	b, err := m.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if strings.Contains(string(b), `"file"`) {
		t.Fatalf(`expected "file" to be omitted when Files is set, got: %s`, b)
	}
	if !strings.Contains(string(b), `"files"`) {
		t.Fatalf(`expected "files" in output, got: %s`, b)
	}
}

func TestCheckBudgetAllowsWithinLimit(t *testing.T) {
	if err := CheckBudget(0.02, 3, 0.50); err != nil {
		t.Fatalf("expected 3 images at $0.02 ($0.06 total) to fit under $0.50, got: %v", err)
	}
}

func TestCheckBudgetRefusesOverLimit(t *testing.T) {
	err := CheckBudget(0.20, 3, 0.50)
	if err == nil {
		t.Fatalf("expected an error: 3 images at $0.20 ($0.60 total) exceeds $0.50")
	}
	if !strings.Contains(err.Error(), "0.60") {
		t.Fatalf("expected the error to mention the estimated total, got: %v", err)
	}
}

func TestCheckBudgetDefaultsVariantsToOne(t *testing.T) {
	if err := CheckBudget(0.10, 0, 0.50); err != nil {
		t.Fatalf("expected variants<=0 to default to 1 (0.10 <= 0.50), got: %v", err)
	}
}

func TestCheckBudgetSkipsUnknownPricing(t *testing.T) {
	if err := CheckBudget(0, 100, 0.01); err != nil {
		t.Fatalf("expected unknown pricing (costPerImage<=0) to skip the check entirely, got: %v", err)
	}
}

func TestCheckBudgetExactlyAtLimitIsAllowed(t *testing.T) {
	if err := CheckBudget(0.25, 2, 0.50); err != nil {
		t.Fatalf("expected exactly-at-limit ($0.50) to be allowed, got: %v", err)
	}
}
