package story

import (
	"strings"
	"testing"
)

func TestRecordUsageAggregatesByRoleProviderModel(t *testing.T) {
	var s Script

	s.RecordUsage("generate", "google-ai-studio", "gemini-3.6-flash", 100, 200, 150, CauseInitial)
	s.RecordUsage("generate", "google-ai-studio", "gemini-3.6-flash", 50, 80, 60, CauseInitial)
	s.RecordUsage("summary", "google-ai-studio", "gemini-3.5-flash-lite", 20, 10, 0, CauseInitial)

	if s.TokensIn != 170 || s.TokensOut != 290 {
		t.Fatalf("expected aggregate tokens in=170 out=290, got in=%d out=%d", s.TokensIn, s.TokensOut)
	}
	if s.Provider != "google-ai-studio" {
		t.Fatalf("expected Provider to be set, got %q", s.Provider)
	}

	if len(s.Usage) != 2 {
		t.Fatalf("expected 2 usage entries (one per role+model), got %d: %+v", len(s.Usage), s.Usage)
	}

	var gen, sum *UsageEntry
	for i := range s.Usage {
		switch s.Usage[i].Role {
		case "generate":
			gen = &s.Usage[i]
		case "summary":
			sum = &s.Usage[i]
		}
	}
	if gen == nil || sum == nil {
		t.Fatalf("expected both generate and summary entries, got %+v", s.Usage)
	}

	if gen.Calls != 2 || gen.TokensIn != 150 || gen.TokensOut != 280 || gen.ThinkingTokens != 210 {
		t.Fatalf("unexpected generate entry: %+v", gen)
	}
	if sum.Calls != 1 || sum.TokensIn != 20 || sum.TokensOut != 10 || sum.ThinkingTokens != 0 {
		t.Fatalf("unexpected summary entry: %+v", sum)
	}
}

func TestRecordUsageSeparatesDifferentModelsUnderTheSameRole(t *testing.T) {
	var s Script
	s.RecordUsage("generate", "google-ai-studio", "gemini-3.6-flash", 10, 10, 0, CauseInitial)
	s.RecordUsage("generate", "groq", "llama-3.3-70b-versatile", 10, 10, 0, CauseInitial)

	if len(s.Usage) != 2 {
		t.Fatalf("expected 2 separate usage entries for 2 different providers, got %d: %+v", len(s.Usage), s.Usage)
	}
}

func TestRecordUsageSeparatesInitialFromRepairForTheSameRoleProviderModel(t *testing.T) {
	var s Script
	s.RecordUsage("generate", "google-ai-studio", "gemini-3.5-flash-lite", 100, 100, 0, CauseInitial)
	s.RecordUsage("generate", "google-ai-studio", "gemini-3.5-flash-lite", 50, 50, 0, CauseRepair)
	s.RecordUsage("generate", "google-ai-studio", "gemini-3.5-flash-lite", 50, 50, 0, CauseRepair)

	if len(s.Usage) != 2 {
		t.Fatalf("expected 2 usage entries (one per cause), got %d: %+v", len(s.Usage), s.Usage)
	}
	var initial, repair *UsageEntry
	for i := range s.Usage {
		switch s.Usage[i].Cause {
		case CauseInitial:
			initial = &s.Usage[i]
		case CauseRepair:
			repair = &s.Usage[i]
		}
	}
	if initial == nil || repair == nil {
		t.Fatalf("expected both an initial and a repair entry, got %+v", s.Usage)
	}
	if initial.Calls != 1 || initial.TokensIn != 100 {
		t.Fatalf("unexpected initial entry: %+v", initial)
	}
	if repair.Calls != 2 || repair.TokensIn != 100 {
		t.Fatalf("unexpected repair entry: %+v", repair)
	}
	// Aggregate totals must still count every call regardless of cause.
	if s.TokensIn != 200 {
		t.Fatalf("expected aggregate TokensIn to include both causes, got %d", s.TokensIn)
	}
}

func TestFullTextJoinsChapterTextInOrder(t *testing.T) {
	s := Script{Chapters: []Chapter{
		{Index: 1, Beat: "hook", Text: "First chapter text."},
		{Index: 2, Beat: "pivot", Text: "Second chapter text."},
	}}
	got := s.FullText()
	want := "First chapter text.\n\nSecond chapter text."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSummaryTextJoinsChapterSummariesWithIndexAndBeat(t *testing.T) {
	s := Script{Chapters: []Chapter{
		{Index: 1, Beat: "hook", Text: "long text nobody should see here", Summary: "narrator introduces the ledger"},
		{Index: 2, Beat: "pivot", Text: "more long text", Summary: "time passes, the habit forms"},
	}}
	got := s.SummaryText()
	want := "Chapter 1 (hook): narrator introduces the ledger\n\nChapter 2 (pivot): time passes, the habit forms"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if strings.Contains(got, "long text") {
		t.Fatalf("expected SummaryText to never include chapter Text, got %q", got)
	}
}

func fullBible() Bible {
	return Bible{
		Narrator: Person{
			Name: "Clara Vance", Age: 43, Role: "narrator", City: "Millbrook",
			Profession: "accountant", Sex: "female",
			Build: "average", Hair: "gray, cropped short",
			FaceNote: "reading glasses pushed up into the hair",
		},
		Cast:          []Person{{Name: "Dale", Role: "antagonist", Age: 50}},
		FamilyLaw:     "we don't ask what we can't afford to answer",
		RefrainPhrase: "the numbers don't lie",
		SeededLine:    "someone always keeps the real count",
		Numbers:       map[string]string{"years": "12"},
	}
}

func TestBibleForChaptersClearsOnlyAppearanceFields(t *testing.T) {
	want := fullBible()
	got := want.ForChapters()

	if got.Narrator.Build != "" || got.Narrator.Hair != "" || got.Narrator.FaceNote != "" {
		t.Fatalf("expected Build/Hair/FaceNote cleared, got %+v", got.Narrator)
	}
	if got.Narrator.Sex != want.Narrator.Sex {
		t.Fatalf("expected Sex to survive (needed for pronoun consistency), got %q want %q", got.Narrator.Sex, want.Narrator.Sex)
	}
	if got.Narrator.Name != want.Narrator.Name || got.Narrator.Age != want.Narrator.Age ||
		got.Narrator.City != want.Narrator.City || got.Narrator.Profession != want.Narrator.Profession {
		t.Fatalf("expected every non-appearance narrator field untouched, got %+v want %+v", got.Narrator, want.Narrator)
	}
	if got.FamilyLaw != want.FamilyLaw || got.RefrainPhrase != want.RefrainPhrase ||
		got.SeededLine != want.SeededLine || len(got.Cast) != len(want.Cast) || len(got.Numbers) != len(want.Numbers) {
		t.Fatalf("expected every non-narrator field untouched, got %+v want %+v", got, want)
	}

	// ForChapters must not mutate the receiver — a value method on a struct
	// with no pointer/slice-of-pointer fields on Narrator should already
	// guarantee this, but the whole point of this method existing is to
	// keep b.Full() from ever silently losing data through it.
	if want.Narrator.Build == "" {
		t.Fatalf("test bug: fullBible() must set a non-empty Build for this test to mean anything")
	}
}

func TestBibleFullReturnsEveryFieldUnchanged(t *testing.T) {
	want := fullBible()
	got := want.Full()
	if got.Narrator.Build != want.Narrator.Build || got.Narrator.Hair != want.Narrator.Hair ||
		got.Narrator.FaceNote != want.Narrator.FaceNote {
		t.Fatalf("expected Full() to keep appearance fields, got %+v want %+v", got.Narrator, want.Narrator)
	}
}
