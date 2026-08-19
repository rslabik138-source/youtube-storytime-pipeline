package config

import "testing"

func TestParseHexColor(t *testing.T) {
	c, err := ParseHexColor("#FF8000", 1.0)
	if err != nil {
		t.Fatalf("ParseHexColor: %v", err)
	}
	if c.R != 0xFF || c.G != 0x80 || c.B != 0x00 || c.A != 255 {
		t.Fatalf("unexpected color: %+v", c)
	}
}

func TestParseHexColorWithoutHash(t *testing.T) {
	c, err := ParseHexColor("000000", 0.5)
	if err != nil {
		t.Fatalf("ParseHexColor: %v", err)
	}
	if c.R != 0 || c.G != 0 || c.B != 0 {
		t.Fatalf("unexpected rgb: %+v", c)
	}
	// 0.5 * 255 = 127.5 -> rounds to 128
	if c.A != 128 {
		t.Fatalf("expected alpha 128 for opacity 0.5, got %d", c.A)
	}
}

func TestParseHexColorClampsOpacity(t *testing.T) {
	over, _ := ParseHexColor("#FFFFFF", 5)
	if over.A != 255 {
		t.Fatalf("expected opacity clamped to 255, got %d", over.A)
	}
	under, _ := ParseHexColor("#FFFFFF", -1)
	if under.A != 0 {
		t.Fatalf("expected opacity clamped to 0, got %d", under.A)
	}
}

func TestParseHexColorRejectsBadInput(t *testing.T) {
	for _, bad := range []string{"", "#FFF", "#GGGGGG", "not-a-color"} {
		if _, err := ParseHexColor(bad, 1); err == nil {
			t.Fatalf("expected an error for %q", bad)
		}
	}
}
