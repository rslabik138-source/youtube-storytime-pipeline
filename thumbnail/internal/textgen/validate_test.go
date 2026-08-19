package textgen

import "testing"

// validText is a dense, competitor-style mini-story: a setup, the
// antagonist's verbatim quoted cruelty, the narrator's restraint, a turn,
// and a cut-off cliffhanger — 7 lines + final, one quote, 3 non-white
// colors, ~45 words.
func validText() ThumbnailText {
	return ThumbnailText{
		Lines: []Line{
			{Text: "FOR 12 YEARS I KEPT MY MOTHER-IN-LAW'S BOOKS", Color: "white"},
			{Text: "SHE TOOK $1,200 FROM MY SAVINGS", Color: "yellow"},
			{Text: "AND SNARLED: \"YOU'LL NEVER AMOUNT TO ANYTHING\"", Color: "red"},
			{Text: "I SAID NOTHING", Color: "white"},
			{Text: "THAT WAS EIGHT MONTHS AGO", Color: "green"},
			{Text: "THIS MORNING SHE CALLED ME", Color: "white"},
			{Text: "SHAKING, AFTER THE AUDITORS", Color: "white"},
		},
		FinalLine: "THEN I FOUND THE RECEIPT",
	}
}

func TestValidateAcceptsAGoodResponse(t *testing.T) {
	if vs := Validate(validText()); len(vs) != 0 {
		t.Fatalf("expected no violations, got %v", vs)
	}
}

func TestValidateRejectsTooFewTotalLines(t *testing.T) {
	text := ThumbnailText{
		Lines: []Line{
			{Text: "SHE SAID \"YOU ARE NOTHING\"", Color: "white"},
			{Text: "I KEPT EVERY RECEIPT SHE MISSED", Color: "yellow"},
		},
		FinalLine: "THEN THE AUDITORS ARRIVED",
	}
	vs := Validate(text)
	if len(vs) == 0 {
		t.Fatalf("expected a violation for too few lines (3 total)")
	}
}

func TestValidateRejectsTooManyTotalLines(t *testing.T) {
	text := ThumbnailText{FinalLine: "AND THEN THE TRUTH CAME OUT"}
	for i := 0; i < 11; i++ {
		text.Lines = append(text.Lines, Line{Text: "SHE SAID \"NO\" AGAIN AND AGAIN", Color: "white"})
	}
	vs := Validate(text)
	if len(vs) == 0 {
		t.Fatalf("expected a violation for too many lines (12 total)")
	}
}

func TestValidateRejectsInvalidColor(t *testing.T) {
	text := validText()
	text.Lines[0].Color = "blue"
	vs := Validate(text)
	if len(vs) == 0 {
		t.Fatalf("expected a violation for an invalid color")
	}
}

func TestValidateRejectsMoreThanFourNonWhiteColors(t *testing.T) {
	text := validText()
	text.Lines[0].Color = "yellow"
	text.Lines[1].Color = "green"
	text.Lines[2].Color = "magenta"
	text.Lines[3].Color = "cyan"
	text.Lines[4].Color = "red"
	vs := Validate(text)
	if len(vs) == 0 {
		t.Fatalf("expected a violation for 5 distinct non-white colors")
	}
}

func TestValidateAllowsExactlyThreeNonWhiteColors(t *testing.T) {
	// validText uses exactly three non-white colors (yellow, red, green);
	// exactly three must be allowed.
	if vs := Validate(validText()); len(vs) != 0 {
		t.Fatalf("expected exactly 3 non-white colors to be allowed, got violations: %v", vs)
	}
}

func TestValidateRejectsOverWordLimit(t *testing.T) {
	text := ThumbnailText{FinalLine: "AND THEN SHE \"SAW IT\""}
	long := ""
	for i := 0; i < 70; i++ {
		long += "word "
	}
	text.Lines = []Line{
		{Text: long, Color: "white"},
		{Text: "SHE SNARLED \"YOU ARE NOTHING\"", Color: "yellow"},
		{Text: "I SAID NOTHING AT ALL", Color: "white"},
		{Text: "EIGHT MONTHS LATER SHE CALLED", Color: "green"},
		{Text: "SHAKING ON THE PHONE", Color: "magenta"},
	}
	vs := Validate(text)
	if len(vs) == 0 {
		t.Fatalf("expected a violation for exceeding the %d-word total", MaxTotalWords)
	}
}

func TestValidateRejectsTooSparse(t *testing.T) {
	text := ThumbnailText{
		Lines: []Line{
			{Text: "SHE SAID \"NO\"", Color: "white"},
			{Text: "I KEPT QUIET", Color: "yellow"},
			{Text: "SHE LAUGHED HARD", Color: "green"},
			{Text: "I WALKED AWAY", Color: "white"},
			{Text: "MONTHS PASSED BY", Color: "magenta"},
		},
		FinalLine: "THEN IT CHANGED",
	}
	vs := Validate(text)
	if len(vs) == 0 {
		t.Fatalf("expected a violation for too-sparse text (< %d words)", MinTotalWords)
	}
}

func TestValidateRejectsMissingQuote(t *testing.T) {
	text := validText()
	// Strip the only quote (line 2) so no verbatim quote remains.
	text.Lines[2].Text = "AND TOLD ME I WOULD NEVER AMOUNT TO ANYTHING"
	vs := Validate(text)
	if len(vs) == 0 {
		t.Fatalf("expected a violation for missing a verbatim quote")
	}
}

func TestValidateRejectsEmptyFinalLine(t *testing.T) {
	text := validText()
	text.FinalLine = ""
	vs := Validate(text)
	if len(vs) == 0 {
		t.Fatalf("expected a violation for an empty final_line")
	}
}

func TestValidateRejectsEmptyLineText(t *testing.T) {
	text := validText()
	text.Lines[0].Text = ""
	vs := Validate(text)
	if len(vs) == 0 {
		t.Fatalf("expected a violation for an empty line's text")
	}
}
