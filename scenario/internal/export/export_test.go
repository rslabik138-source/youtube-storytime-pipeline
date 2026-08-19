package export

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/placeholder/scenario/internal/story"
)

func testScript() *story.Script {
	return &story.Script{
		Title: "The Ledger Never Lies",
		Seed:  story.Seed{Profession: "nurse"},
		Bible: story.Bible{RefrainPhrase: "I kept the ledger the way I kept everything"},
		Chapters: []story.Chapter{
			{Index: 1, Beat: "hook", Text: "I kept the ledger.\n\nExact, always exact.", Summary: "She opens with the ledger habit."},
			{Index: 2, Beat: "close", Text: "I put the folder down.\n\nI did not press charges."},
		},
	}
}

func TestTTSDefaultIsPlainTextWithNoMarkup(t *testing.T) {
	got := TTS(testScript(), TTSOptions{})
	if strings.Contains(got, "<break") {
		t.Fatalf("expected no SSML tags by default (Kokoro doesn't support SSML), got: %s", got)
	}
	want := "I kept the ledger.\n\nExact, always exact.\n\n\nI put the folder down.\n\nI did not press charges."
	if got != want {
		t.Fatalf("TTS mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestTTSGoldenFile(t *testing.T) {
	got := TTS(testScript(), TTSOptions{})
	wantBytes, err := os.ReadFile(filepath.Join("testdata", "tts_golden.txt"))
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}
	want := strings.TrimRight(string(wantBytes), "\n")
	if got != want {
		t.Fatalf("TTS output doesn't match golden file:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestTTSSSMLOptInInsertsBreaksBetweenParagraphsAndChapters(t *testing.T) {
	got := TTS(testScript(), TTSOptions{SSML: true})
	want := "I kept the ledger.\n<break time=\"350ms\"/>\nExact, always exact." +
		"\n<break time=\"700ms\"/>\n" +
		"I put the folder down.\n<break time=\"350ms\"/>\nI did not press charges."
	if got != want {
		t.Fatalf("TTS SSML mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestTTSSSMLCustomBreakDurations(t *testing.T) {
	got := TTS(testScript(), TTSOptions{SSML: true, ChapterBreakMS: 1000, ParagraphBreakMS: 200})
	if !strings.Contains(got, `<break time="200ms"/>`) {
		t.Fatalf("expected a 200ms paragraph break, got: %s", got)
	}
	if !strings.Contains(got, `<break time="1000ms"/>`) {
		t.Fatalf("expected a 1000ms chapter break, got: %s", got)
	}
}

func TestTimingComputesEstSecondsFromWords(t *testing.T) {
	script := &story.Script{Chapters: []story.Chapter{
		{Index: 1, Beat: "hook", Text: "one two three four five"},
	}}
	entries := Timing(script, 150, 700, 350) // 150 wpm = 2.5 words/sec
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Words != 5 {
		t.Fatalf("expected 5 words, got %d", entries[0].Words)
	}
	if entries[0].EstSeconds != 2.0 {
		t.Fatalf("expected 2.0 seconds (5 words / 2.5 wps), got %v", entries[0].EstSeconds)
	}
	if entries[0].Chapter != 1 || entries[0].Beat != "hook" {
		t.Fatalf("unexpected chapter/beat: %+v", entries[0])
	}
}

func TestTimingDefaultsWhenZero(t *testing.T) {
	script := &story.Script{Chapters: []story.Chapter{{Index: 1, Text: "one two three"}}}
	withZero := Timing(script, 0, 0, 0)
	withDefault := Timing(script, 150, 700, 350)
	if withZero[0].EstSeconds != withDefault[0].EstSeconds {
		t.Fatalf("expected wpm<=0 to default to 150, got %v vs %v", withZero[0].EstSeconds, withDefault[0].EstSeconds)
	}
	if withZero[0].PauseAfterMs != withDefault[0].PauseAfterMs {
		t.Fatalf("expected chapterBreakMs<=0 to default to 700, got %v vs %v", withZero[0].PauseAfterMs, withDefault[0].PauseAfterMs)
	}
}

func TestTimingIncludesPauseAfterMsForChaptersAndParagraphs(t *testing.T) {
	script := &story.Script{Chapters: []story.Chapter{
		{Index: 1, Beat: "hook", Text: "one two.\n\nthree four."},
		{Index: 2, Beat: "close", Text: "five six."},
	}}
	entries := Timing(script, 150, 700, 350)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Chapter 1 isn't the last chapter, so it carries the chapter pause.
	if entries[0].PauseAfterMs != 700 {
		t.Fatalf("expected chapter 1's pause_after_ms to be 700, got %d", entries[0].PauseAfterMs)
	}
	// The last chapter has nothing after it.
	if entries[1].PauseAfterMs != 0 {
		t.Fatalf("expected the last chapter's pause_after_ms to be 0, got %d", entries[1].PauseAfterMs)
	}

	if len(entries[0].Paragraphs) != 2 {
		t.Fatalf("expected 2 paragraphs in chapter 1, got %d", len(entries[0].Paragraphs))
	}
	if entries[0].Paragraphs[0].PauseAfterMs != 350 {
		t.Fatalf("expected the first paragraph's pause to be 350, got %d", entries[0].Paragraphs[0].PauseAfterMs)
	}
	if entries[0].Paragraphs[1].PauseAfterMs != 0 {
		t.Fatalf("expected the last paragraph in a chapter to have no pause of its own, got %d", entries[0].Paragraphs[1].PauseAfterMs)
	}
	if entries[0].Paragraphs[0].Words != 2 || entries[0].Paragraphs[1].Words != 2 {
		t.Fatalf("unexpected paragraph word counts: %+v", entries[0].Paragraphs)
	}
}

func TestTimingJSONShape(t *testing.T) {
	script := &story.Script{Chapters: []story.Chapter{{Index: 1, Beat: "hook", Text: "one two"}}}
	b, err := TimingJSON(script, 150, 700, 350)
	if err != nil {
		t.Fatalf("TimingJSON: %v", err)
	}
	var entries []TimingEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 1 || entries[0].Chapter != 1 || entries[0].Beat != "hook" || entries[0].Words != 2 {
		t.Fatalf("unexpected round-tripped entries: %+v", entries)
	}
}

// TestTimingSumWithinToleranceOfTargetRuntime mirrors the brief's own
// approximation ("~8500 words ~55min@150wpm"): a script totalling 8500
// words should estimate within +/-10% of 55 minutes at 150 wpm.
func TestTimingSumWithinToleranceOfTargetRuntime(t *testing.T) {
	wordCounts := []int{170, 85, 170, 600, 680, 680, 765, 85, 1190, 850, 85, 765, 1105, 595, 510, 170} // chapters.yaml, sums to 8505
	script := &story.Script{}
	total := 0
	for i, w := range wordCounts {
		script.Chapters = append(script.Chapters, story.Chapter{Index: i + 1, Text: fillerOfLength(w)})
		total += w
	}
	if total != 8505 {
		t.Fatalf("test fixture bug: word counts sum to %d, want 8505", total)
	}

	entries := Timing(script, 150, 700, 350)
	var sumSeconds float64
	for _, e := range entries {
		sumSeconds += e.EstSeconds
	}

	targetSeconds := 55.0 * 60
	tolerance := targetSeconds * 0.10
	if sumSeconds < targetSeconds-tolerance || sumSeconds > targetSeconds+tolerance {
		t.Fatalf("total estimated runtime %.0fs is outside +/-10%% of %.0fs (55min)", sumSeconds, targetSeconds)
	}
}

func fillerOfLength(words int) string {
	w := make([]string, words)
	for i := range w {
		w[i] = "word"
	}
	return strings.Join(w, " ")
}

func TestBuildMetaIncludesTitleTeaserTagsAndDramatizationNote(t *testing.T) {
	meta := BuildMeta(testScript(), "hospital drama")

	if meta.Title != "The Ledger Never Lies" {
		t.Fatalf("unexpected title: %q", meta.Title)
	}
	if !strings.Contains(meta.Description, "She opens with the ledger habit.") {
		t.Fatalf("expected description to include chapter 1's summary, got: %s", meta.Description)
	}
	if !strings.Contains(meta.Description, "fictional dramatization") {
		t.Fatalf("expected the fictional-dramatization note, got: %s", meta.Description)
	}
	if !containsStr(meta.Tags, "nurse") || !containsStr(meta.Tags, "hospital drama") {
		t.Fatalf("expected profession and extra tags present, got: %v", meta.Tags)
	}
}

func TestBuildMetaFallsBackToRefrainWhenNoSummary(t *testing.T) {
	script := &story.Script{
		Title: "T", Bible: story.Bible{RefrainPhrase: "the refrain"},
		Chapters: []story.Chapter{{Index: 1, Text: "x"}}, // no summary
	}
	meta := BuildMeta(script)
	if !strings.Contains(meta.Description, "the refrain") {
		t.Fatalf("expected fallback to the bible's refrain phrase, got: %s", meta.Description)
	}
}

func TestMetaJSONRoundTrips(t *testing.T) {
	meta := BuildMeta(testScript())
	b, err := meta.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var got Meta
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Title != meta.Title || len(got.Tags) != len(meta.Tags) {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, meta)
	}
}

func testScriptForBundle() *story.Script {
	s := testScript()
	s.ID = "script-123"
	s.WordCount = 12
	s.Seed.ProtagonistSex = "female"
	s.Seed.Profession = "accountant"
	s.Seed.Attire = "a plain button-down with the sleeves rolled"
	s.Seed.Antagonist = "mother_in_law"
	s.Seed.Betrayal = "savings_taken"
	s.Seed.EndingType = "cold_silence"
	s.Bible.Narrator = story.Person{
		Name: "Clara Vance", Age: 43, Role: "narrator",
		Build: "average", Hair: "gray, cropped short", FaceNote: "reading glasses pushed up into the hair",
	}
	s.Bible.FamilyLaw = "the numbers always add up eventually"
	s.Bible.SeededLine = "a receipt never lies"
	s.Bible.Numbers = map[string]string{"amount": "twelve hundred dollars"}
	return s
}

func TestBundleManifestCarriesAppearanceAndAttire(t *testing.T) {
	script := testScriptForBundle()
	_, manifestJSON, err := Bundle(script)
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}
	var m BundleManifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if m.Profession != "accountant" {
		t.Fatalf("expected profession %q, got %q", "accountant", m.Profession)
	}
	if m.Narrator.Build != "average" || m.Narrator.Hair != "gray, cropped short" ||
		m.Narrator.FaceNote != "reading glasses pushed up into the hair" {
		t.Fatalf("unexpected narrator appearance fields: %+v", m.Narrator)
	}
	if m.Narrator.Attire != "a plain button-down with the sleeves rolled" {
		t.Fatalf("expected attire from Seed.Attire, got %q", m.Narrator.Attire)
	}
}

func TestBundleManifestCarriesStoryFactsForThumbnail(t *testing.T) {
	script := testScriptForBundle()
	_, manifestJSON, err := Bundle(script)
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}
	var m BundleManifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if m.Story.FamilyLaw != "the numbers always add up eventually" {
		t.Fatalf("expected family_law from Bible, got %q", m.Story.FamilyLaw)
	}
	if m.Story.RefrainPhrase != "I kept the ledger the way I kept everything" {
		t.Fatalf("expected refrain_phrase from Bible, got %q", m.Story.RefrainPhrase)
	}
	if m.Story.SeededLine != "a receipt never lies" {
		t.Fatalf("expected seeded_line from Bible, got %q", m.Story.SeededLine)
	}
	if m.Story.Antagonist != "mother_in_law" || m.Story.Betrayal != "savings_taken" || m.Story.EndingType != "cold_silence" {
		t.Fatalf("expected seed facts to carry through, got %+v", m.Story)
	}
	if m.Story.Numbers["amount"] != "twelve hundred dollars" {
		t.Fatalf("expected bible numbers to carry through, got %+v", m.Story.Numbers)
	}
}

func TestBundleScriptTextMatchesTTSExactly(t *testing.T) {
	script := testScriptForBundle()
	scriptText, _, err := Bundle(script)
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}
	want := TTS(script, TTSOptions{})
	if string(scriptText) != want {
		t.Fatalf("Bundle's script text diverged from TTS():\ngot:  %q\nwant: %q", scriptText, want)
	}
}

func TestBundleManifestNarratorAndWordCount(t *testing.T) {
	script := testScriptForBundle()
	_, manifestJSON, err := Bundle(script)
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}
	var m BundleManifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if m.ID != "script-123" || m.Title != "The Ledger Never Lies" || m.WordCount != 12 {
		t.Fatalf("unexpected manifest top-level fields: %+v", m)
	}
	if m.Narrator.Name != "Clara Vance" || m.Narrator.Age != 43 || m.Narrator.Sex != "female" {
		t.Fatalf("expected narrator from Bible.Narrator + Seed.ProtagonistSex, got: %+v", m.Narrator)
	}
}

func TestBundleChapterCharOffsetsMatchScriptText(t *testing.T) {
	script := testScriptForBundle()
	scriptText, manifestJSON, err := Bundle(script)
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}
	var m BundleManifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if len(m.Chapters) != 2 {
		t.Fatalf("expected 2 chapters, got %d", len(m.Chapters))
	}

	ch1 := m.Chapters[0]
	if ch1.Index != 1 || ch1.Beat != "hook" || ch1.Words != 7 {
		t.Fatalf("unexpected chapter 1 metadata: %+v", ch1)
	}
	got1 := string(scriptText[ch1.CharStart:ch1.CharEnd])
	want1 := "I kept the ledger.\n\nExact, always exact."
	if got1 != want1 {
		t.Fatalf("chapter 1 char range mismatch:\ngot:  %q\nwant: %q", got1, want1)
	}

	ch2 := m.Chapters[1]
	got2 := string(scriptText[ch2.CharStart:ch2.CharEnd])
	want2 := "I put the folder down.\n\nI did not press charges."
	if got2 != want2 {
		t.Fatalf("chapter 2 char range mismatch:\ngot:  %q\nwant: %q", got2, want2)
	}

	// The two ranges shouldn't overlap and should be separated by exactly
	// the chapter separator ("\n\n\n").
	if string(scriptText[ch1.CharEnd:ch2.CharStart]) != "\n\n\n" {
		t.Fatalf("expected exactly the chapter separator between ranges, got %q", scriptText[ch1.CharEnd:ch2.CharStart])
	}
}

func TestBundleManifestRoundTripsAsValidJSON(t *testing.T) {
	script := testScriptForBundle()
	_, manifestJSON, err := Bundle(script)
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(manifestJSON, &generic); err != nil {
		t.Fatalf("manifest.json is not valid JSON: %v", err)
	}
	for _, key := range []string{"id", "title", "word_count", "narrator", "chapters"} {
		if _, ok := generic[key]; !ok {
			t.Fatalf("expected manifest.json to have key %q, got: %s", key, manifestJSON)
		}
	}
}

func containsStr(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
