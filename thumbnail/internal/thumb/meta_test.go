package thumb

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/placeholder/thumbnail/internal/textgen"
)

func TestMetaJSONRoundTrips(t *testing.T) {
	m := Meta{
		ID: "s1", FaceID: "face-01", TextModel: "gemini-3.5-flash-lite", CostUSD: 0.004,
		Variants: []VariantMeta{
			{File: "variant-1.png", Lines: []textgen.Line{{Text: "A", Color: "white"}}, FinalLine: "B"},
		},
	}
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

func TestCheckBudgetAllowsWithinLimit(t *testing.T) {
	if err := CheckBudget(0.001, 4, 0.05); err != nil {
		t.Fatalf("expected no error within budget, got: %v", err)
	}
}

func TestCheckBudgetRefusesOverLimit(t *testing.T) {
	if err := CheckBudget(0.05, 4, 0.05); err == nil {
		t.Fatalf("expected an error when 4 variants at $0.05 each exceeds a $0.05 budget")
	}
}

func TestCheckBudgetSkipsWhenCostUnknown(t *testing.T) {
	if err := CheckBudget(0, 100, 0.01); err != nil {
		t.Fatalf("expected no error when cost per call is unknown (<=0), got: %v", err)
	}
}

func TestCheckBudgetDefaultsVariantsToOne(t *testing.T) {
	if err := CheckBudget(0.01, 0, 0.005); err == nil {
		t.Fatalf("expected variants<=0 to default to 1 and still exceed a $0.005 budget")
	}
}
