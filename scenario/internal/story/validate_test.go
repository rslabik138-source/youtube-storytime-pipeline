package story

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// words builds one sentence of exactly n whitespace-separated tokens, for
// precise sentence-length boundary tests.
func words(n int) string {
	w := make([]string, n)
	for i := range w {
		w[i] = "word"
	}
	return strings.Join(w, " ") + "."
}

var fillerPool = []string{
	"quiet", "kitchen", "table", "morning", "light", "across", "worn",
	"floor", "while", "everyone", "else", "kept", "talking", "about",
	"nothing", "much", "that", "particular", "evening", "until", "someone",
	"finally", "noticed", "long", "standing", "near", "door", "watching",
	"clock", "above", "sink", "outside", "wind", "moved", "through",
	"bare", "branches", "slow", "steady", "way", "nobody", "seemed",
	"hear", "hallway", "smelled", "like", "coffee", "and", "old",
	"paper", "distant", "radio", "hummed", "low", "voice", "drifting",
	"past", "screen", "porch", "gravel", "driveway", "settled", "under",
	"tires", "late", "arriving", "car", "shadow", "fell", "yard",
	"longer", "each", "passing", "year", "grew", "quieter", "still",
	"between", "them", "unspoken", "debt", "sat", "folded", "napkin",
	"beside", "plate", "again", "stayed", "silent", "instead", "waited",
}

// fillerWords builds n words of deterministic but varied filler (fixed
// seed 1) — never tripping the sentence-length or sentence-length-variance
// validators on its own (short 5-9 word sentences with an occasional
// 14-18 word one every 4th sentence, matching the "vary sentence length"
// style rule). Use fillerWordsSeed instead whenever more than one
// chapter's filler ends up in the same Script, so different chapters don't
// accidentally start with (or contain) the same words and trip the
// anti-repetition guards by test-fixture accident rather than by design.
func fillerWords(n int) string {
	return fillerWordsSeed(n, 1)
}

func fillerWordsSeed(n int, seed int64) string {
	rng := rand.New(rand.NewSource(seed))
	var b strings.Builder
	remaining := n
	sentenceIndex := 0
	for remaining > 0 {
		length := 5 + rng.Intn(5) // 5-9: short sentences
		if sentenceIndex%4 == 3 {
			length = 14 + rng.Intn(5) // 14-18: the occasional longer sentence
		}
		if length > remaining {
			length = remaining
		}

		for i := 0; i < length; i++ {
			if b.Len() > 0 {
				b.WriteString(" ")
			}
			b.WriteString(fillerPool[rng.Intn(len(fillerPool))])
		}
		b.WriteString(".")
		remaining -= length
		sentenceIndex++
		if remaining > 0 {
			b.WriteString(" ")
		}
	}
	return b.String()
}

func validBible() Bible {
	return Bible{
		Narrator: Person{Name: "Dana Whitfield", Age: 44, Role: "narrator", City: "Cedar Falls", Profession: "nurse"},
		Cast: []Person{
			{Name: "Carol Whitfield", Age: 71, Role: "mother_in_law", PhysicalTic: "taps her wedding ring on the table"},
			{Name: "Russell Voss", Age: 68, Role: "father_in_law", PhysicalTic: "clears his throat before he lies"},
		},
		Timeline: []Event{
			{Year: 2009, What: "Dana marries into the Whitfield family"},
			{Year: 2012, What: "The humiliation at Thanksgiving"},
			{Year: 2021, What: "The reckoning"},
		},
		FamilyLaw:     "In this family, love was a ledger, and Dana was always the one who owed.",
		RefrainPhrase: "You're not a real nurse, you just wipe people down",
		SeededLine:    "Carol always said she kept every receipt, just in case",
		Numbers:       map[string]string{"stolen": "ninety four thousand dollars"},
	}
}

func TestValidateBibleNamesUnique(t *testing.T) {
	t.Run("unique names pass", func(t *testing.T) {
		if v := ValidateBibleNamesUnique(validBible(), BibleValidatorConfig{}); len(v) != 0 {
			t.Fatalf("expected no violations, got %v", v)
		}
	})

	t.Run("cast member reuses the narrator's name", func(t *testing.T) {
		b := validBible()
		b.Cast[0].Name = b.Narrator.Name
		v := ValidateBibleNamesUnique(b, BibleValidatorConfig{})
		if len(v) != 1 || v[0].Rule != "bible_names_unique" {
			t.Fatalf("expected 1 bible_names_unique violation, got %v", v)
		}
	})

	t.Run("case-insensitive duplicate", func(t *testing.T) {
		b := validBible()
		b.Cast[1].Name = strings.ToUpper(b.Cast[0].Name)
		v := ValidateBibleNamesUnique(b, BibleValidatorConfig{})
		if len(v) != 1 {
			t.Fatalf("expected 1 violation for a case-insensitive duplicate, got %v", v)
		}
	})
}

func TestValidateBibleTimelineMonotonic(t *testing.T) {
	t.Run("increasing years pass", func(t *testing.T) {
		if v := ValidateBibleTimelineMonotonic(validBible(), BibleValidatorConfig{}); len(v) != 0 {
			t.Fatalf("expected no violations, got %v", v)
		}
	})

	t.Run("same year twice is fine", func(t *testing.T) {
		b := validBible()
		b.Timeline = []Event{{Year: 2012, What: "a"}, {Year: 2012, What: "b"}}
		if v := ValidateBibleTimelineMonotonic(b, BibleValidatorConfig{}); len(v) != 0 {
			t.Fatalf("expected no violations for a repeated year, got %v", v)
		}
	})

	t.Run("year goes backward", func(t *testing.T) {
		b := validBible()
		b.Timeline = []Event{{Year: 2012, What: "a"}, {Year: 2005, What: "b"}}
		v := ValidateBibleTimelineMonotonic(b, BibleValidatorConfig{})
		if len(v) != 1 || v[0].Rule != "bible_timeline_monotonic" {
			t.Fatalf("expected 1 bible_timeline_monotonic violation, got %v", v)
		}
	})
}

func TestValidateBibleRefrainPhraseNonEmpty(t *testing.T) {
	t.Run("non-empty passes", func(t *testing.T) {
		if v := ValidateBibleRefrainPhraseNonEmpty(validBible(), BibleValidatorConfig{}); len(v) != 0 {
			t.Fatalf("expected no violations, got %v", v)
		}
	})

	t.Run("empty fails", func(t *testing.T) {
		b := validBible()
		b.RefrainPhrase = "   "
		v := ValidateBibleRefrainPhraseNonEmpty(b, BibleValidatorConfig{})
		if len(v) != 1 || v[0].Rule != "bible_refrain_phrase" {
			t.Fatalf("expected 1 bible_refrain_phrase violation, got %v", v)
		}
	})
}

func TestValidateBibleNamesNotReused(t *testing.T) {
	t.Run("no used names configured never matches", func(t *testing.T) {
		if v := ValidateBibleNamesNotReused(validBible(), BibleValidatorConfig{}); len(v) != 0 {
			t.Fatalf("expected no violations, got %v", v)
		}
	})

	t.Run("full narrator name reused from history", func(t *testing.T) {
		b := validBible()
		b.Cast = nil // isolate the narrator
		cfg := BibleValidatorConfig{UsedNames: []string{"dana whitfield", "some other name"}}
		v := ValidateBibleNamesNotReused(b, cfg)
		if len(v) != 1 || v[0].Rule != "bible_names_not_reused" {
			t.Fatalf("expected 1 bible_names_not_reused violation, got %v", v)
		}
	})

	t.Run("cast name reused, case-insensitive", func(t *testing.T) {
		b := validBible()
		b.Narrator.Name = "Marcus Hale" // no narrator collision
		b.Cast = []Person{{Name: "Carol Whitfield"}}
		cfg := BibleValidatorConfig{UsedNames: []string{"CAROL WHITFIELD"}}
		v := ValidateBibleNamesNotReused(b, cfg)
		if len(v) != 1 {
			t.Fatalf("expected 1 violation, got %v", v)
		}
	})

	t.Run("shared SURNAME from a previous script is caught", func(t *testing.T) {
		b := validBible()
		b.Narrator.Name = "Julian Vance"
		b.Cast = []Person{{Name: "Marcus Hale"}}
		cfg := BibleValidatorConfig{UsedNames: []string{"caleb vance"}} // a prior script's narrator
		v := ValidateBibleNamesNotReused(b, cfg)
		if len(v) != 1 || v[0].Rule != "bible_names_not_reused" {
			t.Fatalf("expected the reused surname 'Vance' to be caught, got %v", v)
		}
	})

	t.Run("shared FIRST name from history is caught", func(t *testing.T) {
		b := validBible()
		b.Narrator.Name = "Dana Kowalski"
		b.Cast = nil
		cfg := BibleValidatorConfig{UsedNames: []string{"dana whitfield"}}
		v := ValidateBibleNamesNotReused(b, cfg)
		if len(v) != 1 {
			t.Fatalf("expected the reused first name 'Dana' to be caught, got %v", v)
		}
	})

	t.Run("fully distinct names pass", func(t *testing.T) {
		b := validBible()
		b.Narrator.Name = "Marcus Hale"
		b.Cast = []Person{{Name: "Priya Anand"}}
		cfg := BibleValidatorConfig{UsedNames: []string{"caleb vance", "dana whitfield"}}
		v := ValidateBibleNamesNotReused(b, cfg)
		if len(v) != 0 {
			t.Fatalf("expected no violations for fully distinct names, got %v", v)
		}
	})
}

func TestValidateBible(t *testing.T) {
	t.Run("valid bible has zero violations", func(t *testing.T) {
		if v := ValidateBible(validBible(), BibleValidatorConfig{}); len(v) != 0 {
			t.Fatalf("expected no violations, got %v", v)
		}
	})

	t.Run("accumulates violations from independent checks", func(t *testing.T) {
		b := validBible()
		b.RefrainPhrase = ""
		b.Timeline = []Event{{Year: 2012, What: "a"}, {Year: 2000, What: "b"}}
		v := ValidateBible(b, BibleValidatorConfig{})
		rules := map[string]bool{}
		for _, viol := range v {
			rules[viol.Rule] = true
		}
		if !rules["bible_refrain_phrase"] || !rules["bible_timeline_monotonic"] {
			t.Fatalf("expected both bible_refrain_phrase and bible_timeline_monotonic, got %v", v)
		}
	})
}

func validChapter(index int, beat string, targetWords int) Chapter {
	text := fillerWordsSeed(targetWords, int64(index)+1)
	switch beat {
	case "the_cut":
		text += " Carol said I was never a real nurse, just someone who wipes people down for money." +
			" She told me that real nurses went to real schools, not community programs." +
			" Carol snapped that everyone in the family already knew I wasn't cut out for this."
	case "the_demand":
		text += " Russell said the deed needed to be signed back to the family right away." +
			" He told me the paperwork was already prepared and only needed a signature." +
			" Russell muttered that he expected no argument from someone who owed the family this much." +
			" Russell handed over a typed letter demanding the deed be signed back to the family."
	case "the_reckoning":
		text += " Karen said the ledger had never once been wrong in all these years." +
			" She told me that quiet people had no business asking loud questions." +
			" Karen snapped that the family had already decided this without me."
	}
	return Chapter{Index: index, Beat: beat, TargetWords: targetWords, Text: text}
}

func TestValidateChapterLength(t *testing.T) {
	cfg := DefaultChapterValidatorConfig(nil)

	tests := []struct {
		name          string
		targetWords   int
		actualWords   int
		wantViolation bool
	}{
		{"exactly at target", 500, 500, false},
		{"at lower tolerance bound", 500, 400, false}, // 500*0.8
		{"just below lower bound", 500, 399, true},
		{"at upper tolerance bound", 500, 600, false}, // 500*1.2
		{"just above upper bound", 500, 601, true},
		{"zero target disables the check", 0, 5, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Chapter{Index: 1, Beat: "cast", TargetWords: tt.targetWords, Text: fillerWords(tt.actualWords)}
			v := ValidateChapterLength(c, cfg)
			if (len(v) > 0) != tt.wantViolation {
				t.Fatalf("target=%d actual=%d: got violations=%v, want violation=%v", tt.targetWords, tt.actualWords, v, tt.wantViolation)
			}
			if tt.wantViolation && v[0].Severity != SeverityWarning {
				t.Fatalf("expected chapter_length to be a warning (style signal, not blocking), got %v", v[0].Severity)
			}
		})
	}
}

func TestValidateChapterSentenceLength(t *testing.T) {
	cfg := DefaultChapterValidatorConfig(nil)

	tests := []struct {
		name         string
		n            int
		wantSeverity Severity // "" == no violation
	}{
		{"at warn threshold exactly", 25, ""},
		{"just above warn threshold", 26, SeverityWarning},
		{"at error threshold exactly", 28, SeverityWarning},
		{"just above error threshold", 29, SeverityError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Chapter{Index: 3, Beat: "cast", Text: words(tt.n)}
			v := ValidateChapterSentenceLength(c, cfg)
			if tt.wantSeverity == "" {
				if len(v) != 0 {
					t.Fatalf("n=%d: expected no violations, got %v", tt.n, v)
				}
				return
			}
			if len(v) != 1 || v[0].Severity != tt.wantSeverity {
				t.Fatalf("n=%d: expected a single %v violation, got %v", tt.n, tt.wantSeverity, v)
			}
		})
	}
}

func TestValidateChapterBannedPhrases(t *testing.T) {
	cfg := DefaultChapterValidatorConfig([]string{"little did i know", "imagine this"})

	t.Run("clean chapter", func(t *testing.T) {
		c := validChapter(1, "cast", 50)
		if v := ValidateChapterBannedPhrases(c, cfg); len(v) != 0 {
			t.Fatalf("expected no violations, got %v", v)
		}
	})

	t.Run("banned phrase, case-insensitive", func(t *testing.T) {
		c := validChapter(1, "cast", 50)
		c.Text += " Little Did I Know how bad it would get."
		v := ValidateChapterBannedPhrases(c, cfg)
		if len(v) != 1 || v[0].Rule != "banned_phrase" {
			t.Fatalf("expected 1 banned_phrase violation, got %v", v)
		}
	})
}

func TestValidateChapterNoDigitsInTTSText(t *testing.T) {
	cfg := DefaultChapterValidatorConfig(nil)

	t.Run("all numbers spelled out", func(t *testing.T) {
		c := Chapter{Index: 1, Text: "She kept ninety four thousand dollars in that box for eleven years."}
		if v := ValidateChapterNoDigitsInTTSText(c, cfg); len(v) != 0 {
			t.Fatalf("expected no violations, got %v", v)
		}
	})

	t.Run("digit present", func(t *testing.T) {
		c := Chapter{Index: 1, Text: "She kept 94 thousand dollars in that box."}
		v := ValidateChapterNoDigitsInTTSText(c, cfg)
		if len(v) != 1 || v[0].Rule != "tts_digits" {
			t.Fatalf("expected 1 tts_digits violation, got %v", v)
		}
	})

	t.Run("dollar sign present", func(t *testing.T) {
		c := Chapter{Index: 1, Text: "She kept it hidden, worth about $94,000, for years."}
		v := ValidateChapterNoDigitsInTTSText(c, cfg)
		if len(v) != 1 {
			t.Fatalf("expected 1 tts_digits violation, got %v", v)
		}
	})
}

func TestValidateChapterMissingPunctuation(t *testing.T) {
	cfg := DefaultChapterValidatorConfig(nil)

	t.Run("properly punctuated", func(t *testing.T) {
		c := Chapter{Index: 1, Text: "She set the jar down on a Tuesday morning. She paused to wave at the mailman."}
		if v := ValidateChapterMissingPunctuation(c, cfg); len(v) != 0 {
			t.Fatalf("expected no violations, got %v", v)
		}
	})

	t.Run("missing period before a capitalized pronoun", func(t *testing.T) {
		c := Chapter{Index: 1, Text: "She set the jar down on a Tuesday morning She paused to wave at the mailman."}
		v := ValidateChapterMissingPunctuation(c, cfg)
		if len(v) != 1 || v[0].Rule != "missing_punctuation" {
			t.Fatalf("expected 1 missing_punctuation violation, got %v", v)
		}
		if v[0].Phrase == "" {
			t.Fatalf("expected a locatable Phrase so this can be point-fixed, got empty")
		}
	})

	t.Run("multiple missing marks are all reported", func(t *testing.T) {
		c := Chapter{Index: 1, Text: "She waved The mailman waved back He didn't stop walking."}
		v := ValidateChapterMissingPunctuation(c, cfg)
		if len(v) != 2 {
			t.Fatalf("expected 2 missing_punctuation violations, got %d: %v", len(v), v)
		}
	})

	t.Run("comma before the capitalized word is not flagged", func(t *testing.T) {
		c := Chapter{Index: 1, Text: "She waved, The mailman didn't notice."}
		if v := ValidateChapterMissingPunctuation(c, cfg); len(v) != 0 {
			t.Fatalf("expected no violations (a comma already separates the clauses), got %v", v)
		}
	})
}

func TestValidateChapterDirectSpeechCount(t *testing.T) {
	cfg := DefaultChapterValidatorConfig(nil)

	for _, beat := range []string{"the_cut", "the_demand", "the_reckoning"} {
		t.Run(beat+" with 3+ speech lines passes", func(t *testing.T) {
			c := validChapter(6, beat, 50)
			if v := ValidateChapterDirectSpeechCount(c, cfg); len(v) != 0 {
				t.Fatalf("expected no violations, got %v", v)
			}
		})

		t.Run(beat+" with no direct speech fails", func(t *testing.T) {
			c := Chapter{Index: 6, Beat: beat, Text: fillerWords(50)}
			v := ValidateChapterDirectSpeechCount(c, cfg)
			if len(v) != 1 || v[0].Rule != beat+"_direct_speech_count" {
				t.Fatalf("expected 1 %s_direct_speech_count violation, got %v", beat, v)
			}
		})

		t.Run(beat+" with only 1 speech line still fails (need 3)", func(t *testing.T) {
			c := Chapter{Index: 6, Beat: beat, Text: fillerWords(50) + " Someone said this wasn't over."}
			v := ValidateChapterDirectSpeechCount(c, cfg)
			if len(v) != 1 {
				t.Fatalf("expected 1 violation for only 1 of 3 required speech lines, got %v", v)
			}
		})
	}

	t.Run("other beats are never checked", func(t *testing.T) {
		c := Chapter{Index: 4, Beat: "family_law", Text: fillerWords(50)}
		if v := ValidateChapterDirectSpeechCount(c, cfg); len(v) != 0 {
			t.Fatalf("expected the check to be skipped for a beat with no dialogue requirement, got %v", v)
		}
	})

	t.Run("SkipDialogueRequirement silences the check even for a required beat", func(t *testing.T) {
		skipCfg := cfg
		skipCfg.SkipDialogueRequirement = true
		c := Chapter{Index: 6, Beat: "the_cut", Text: fillerWords(50)}
		if v := ValidateChapterDirectSpeechCount(c, skipCfg); len(v) != 0 {
			t.Fatalf("expected no violations when SkipDialogueRequirement is set, got %v", v)
		}
	})
}

func TestValidateChapterMentionsWrittenDocument(t *testing.T) {
	cfg := DefaultChapterValidatorConfig(nil)

	t.Run("the_demand mentioning a letter passes", func(t *testing.T) {
		c := validChapter(12, "the_demand", 50)
		if v := ValidateChapterMentionsWrittenDocument(c, cfg); len(v) != 0 {
			t.Fatalf("expected no violations, got %v", v)
		}
	})

	t.Run("the_demand without any document keyword fails", func(t *testing.T) {
		c := Chapter{Index: 12, Beat: "the_demand", Text: fillerWords(50)}
		v := ValidateChapterMentionsWrittenDocument(c, cfg)
		if len(v) != 1 || v[0].Rule != "the_demand_written_document" {
			t.Fatalf("expected 1 the_demand_written_document violation, got %v", v)
		}
	})

	t.Run("other beats are never checked", func(t *testing.T) {
		c := Chapter{Index: 9, Beat: "the_years", Text: fillerWords(50)}
		if v := ValidateChapterMentionsWrittenDocument(c, cfg); len(v) != 0 {
			t.Fatalf("expected the check to be skipped for a non-the_demand beat, got %v", v)
		}
	})
}

func TestNumberToWords(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "zero"}, {7, "seven"}, {19, "nineteen"},
		{20, "twenty"}, {34, "thirty-four"}, {47, "forty-seven"}, {99, "ninety-nine"},
		{100, "one hundred"}, {105, "one hundred five"}, {144, "one hundred forty-four"},
	}
	for _, tt := range tests {
		if got := numberToWords(tt.n); got != tt.want {
			t.Errorf("numberToWords(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestContainsNumberWordAcceptsHyphenAndSpaceVariants(t *testing.T) {
	if !containsNumberWord("She was forty-seven years old.", 47) {
		t.Fatalf("expected the hyphenated form to match")
	}
	if !containsNumberWord("She was forty seven years old.", 47) {
		t.Fatalf("expected the spaced form to match")
	}
	if containsNumberWord("She was thirty years old.", 47) {
		t.Fatalf("expected no match for a different number")
	}
}

func TestValidateChapterHookIdentity(t *testing.T) {
	cfg := DefaultChapterValidatorConfig(nil)
	cfg.Bible = validBible() // narrator: Dana Whitfield, age 44

	t.Run("hook with name, age, and direct speech passes", func(t *testing.T) {
		c := Chapter{Index: 1, Beat: "hook", Text: "My name is Dana Whitfield and I am forty-four years old. " +
			"Carol said I was never a real nurse."}
		if v := ValidateChapterHookIdentity(c, cfg); len(v) != 0 {
			t.Fatalf("expected no violations, got %v", v)
		}
	})

	t.Run("missing name, age, and direct speech all flagged", func(t *testing.T) {
		c := Chapter{Index: 1, Beat: "hook", Text: fillerWords(50)}
		v := ValidateChapterHookIdentity(c, cfg)
		rules := map[string]bool{}
		for _, viol := range v {
			rules[viol.Rule] = true
		}
		if !rules["hook_missing_identity"] || !rules["hook_missing_age"] || !rules["hook_missing_direct_speech"] {
			t.Fatalf("expected all 3 hook violations, got %v", v)
		}
	})

	t.Run("name present but age missing", func(t *testing.T) {
		c := Chapter{Index: 1, Beat: "hook", Text: "Dana Whitfield said this was the truth. " + fillerWords(30)}
		v := ValidateChapterHookIdentity(c, cfg)
		rules := map[string]bool{}
		for _, viol := range v {
			rules[viol.Rule] = true
		}
		if rules["hook_missing_identity"] || rules["hook_missing_direct_speech"] {
			t.Fatalf("expected only the age check to fail, got %v", v)
		}
		if !rules["hook_missing_age"] {
			t.Fatalf("expected hook_missing_age, got %v", v)
		}
	})

	t.Run("no Bible configured is a no-op", func(t *testing.T) {
		c := Chapter{Index: 1, Beat: "hook", Text: fillerWords(50)}
		if v := ValidateChapterHookIdentity(c, DefaultChapterValidatorConfig(nil)); len(v) != 0 {
			t.Fatalf("expected no violations without a configured Bible, got %v", v)
		}
	})

	t.Run("other beats are never checked", func(t *testing.T) {
		c := Chapter{Index: 3, Beat: "pivot", Text: fillerWords(50)}
		if v := ValidateChapterHookIdentity(c, cfg); len(v) != 0 {
			t.Fatalf("expected the check to be skipped for a non-hook beat, got %v", v)
		}
	})
}

func TestValidateNumberReuseAcrossEntities(t *testing.T) {
	bible := validBible() // Numbers: {"stolen": "ninety four thousand dollars"}

	t.Run("used once, no violation", func(t *testing.T) {
		s := &Script{Bible: bible, Chapters: []Chapter{
			{Index: 1, Text: "She hid ninety four thousand dollars in the box."},
		}}
		if v := ValidateNumberReuseAcrossEntities(s); len(v) != 0 {
			t.Fatalf("expected no violations, got %v", v)
		}
	})

	t.Run("used more than 4 times for the same fact flags the excess", func(t *testing.T) {
		text := strings.Repeat("She mentioned ninety four thousand dollars again. ", 5)
		s := &Script{Bible: bible, Chapters: []Chapter{{Index: 1, Text: text}}}
		v := ValidateNumberReuseAcrossEntities(s)
		found := false
		for _, viol := range v {
			if viol.Rule == "number_reuse_limit" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected a number_reuse_limit violation, got %v", v)
		}
	})

	t.Run("applied to different entities flags the mismatch as a warning, not an error", func(t *testing.T) {
		// Severity: Warning is deliberate, not an oversight — nextWordAfter
		// is a crude same-fact proxy (just the next word after the number),
		// so this fires on legitimate rephrasing as often as on a real
		// reused number. As an Error it forced expensive, often-unwinnable
		// full chapter regenerations chasing what amounted to a coin flip.
		s := &Script{Bible: bible, Chapters: []Chapter{
			{Index: 1, Text: "She kept ninety four thousand dollars stitched into the mattress."},
			{Index: 2, Text: "Court records confirmed ninety four thousand dollars missing from the fund."},
		}}
		v := ValidateNumberReuseAcrossEntities(s)
		var found *Violation
		for i := range v {
			if v[i].Rule == "number_reuse_different_entities" {
				found = &v[i]
			}
		}
		if found == nil {
			t.Fatalf("expected a number_reuse_different_entities violation, got %v", v)
		}
		if found.Severity != SeverityWarning {
			t.Fatalf("expected number_reuse_different_entities to be SeverityWarning, got %v", found.Severity)
		}
	})

	t.Run("empty Bible.Numbers is a no-op", func(t *testing.T) {
		s := &Script{Bible: Bible{Numbers: map[string]string{}}, Chapters: []Chapter{
			{Index: 1, Text: "Nothing numeric here at all."},
		}}
		if v := ValidateNumberReuseAcrossEntities(s); len(v) != 0 {
			t.Fatalf("expected no violations, got %v", v)
		}
	})
}

func TestValidateChapter(t *testing.T) {
	cfg := DefaultChapterValidatorConfig([]string{"imagine this"})

	t.Run("valid chapter has no blocking violations", func(t *testing.T) {
		// A well-formed chapter can still carry style warnings (e.g. it
		// won't happen to contain a 20+ word sentence) — only Severity
		// error is what generate.go treats as blocking.
		c := validChapter(1, "cast", 300)
		v := ValidateChapter(c, cfg)
		for _, viol := range v {
			if viol.Severity == SeverityError {
				t.Fatalf("expected no blocking (error) violations, got %v", v)
			}
		}
	})

	t.Run("accumulates violations from independent checks", func(t *testing.T) {
		c := validChapter(1, "cast", 300)
		c.Text += " 42 "     // triggers tts_digits
		c.TargetWords = 5000 // now the length check should also fail
		v := ValidateChapter(c, cfg)
		rules := map[string]bool{}
		for _, viol := range v {
			rules[viol.Rule] = true
		}
		if !rules["tts_digits"] || !rules["chapter_length"] {
			t.Fatalf("expected tts_digits and chapter_length violations, got %v", v)
		}
	})
}

func validScript() *Script {
	return &Script{
		Chapters: []Chapter{
			validChapter(1, "hook", 170),
			validChapter(6, "the_cut", 680),
			validChapter(12, "the_demand", 765),
		},
	}
}

func TestValidateAntiRepetitionParagraphStarts(t *testing.T) {
	t.Run("no repetition passes", func(t *testing.T) {
		if v := ValidateAntiRepetitionParagraphStarts(validScript()); len(v) != 0 {
			t.Fatalf("expected no violations, got %v", v)
		}
	})

	t.Run("same paragraph opening three times fails", func(t *testing.T) {
		s := &Script{Chapters: []Chapter{
			{Index: 1, Text: "I want you to understand something about my mother.\n\nShe never once apologized."},
			{Index: 2, Text: "I want you to understand something about my sister.\n\nShe knew the whole time."},
			{Index: 3, Text: "I want you to understand something about that house.\n\nIt was never really ours."},
		}}
		v := ValidateAntiRepetitionParagraphStarts(s)
		if len(v) != 1 || v[0].Rule != "repeated_paragraph_start" {
			t.Fatalf("expected 1 repeated_paragraph_start violation, got %v", v)
		}
	})

	t.Run("twice is not yet a violation", func(t *testing.T) {
		s := &Script{Chapters: []Chapter{
			{Index: 1, Text: "I want you to understand something about my mother."},
			{Index: 2, Text: "I want you to understand something about my sister."},
		}}
		if v := ValidateAntiRepetitionParagraphStarts(s); len(v) != 0 {
			t.Fatalf("expected no violations at exactly 2 repeats, got %v", v)
		}
	})
}

func TestValidateAntiRepetitionNGrams(t *testing.T) {
	t.Run("no repetition passes", func(t *testing.T) {
		if v := ValidateAntiRepetitionNGrams(validScript()); len(v) != 0 {
			t.Fatalf("expected no violations, got %v", v)
		}
	})

	t.Run("same six-word phrase three times fails", func(t *testing.T) {
		phrase := "the ledger never once balanced itself"
		s := &Script{Chapters: []Chapter{
			{Index: 1, Text: fillerWordsSeed(20, 101) + " " + phrase + "."},
			{Index: 2, Text: fillerWordsSeed(20, 102) + " " + phrase + "."},
			{Index: 3, Text: fillerWordsSeed(20, 103) + " " + phrase + "."},
		}}
		v := ValidateAntiRepetitionNGrams(s)
		if len(v) != 1 || v[0].Rule != "repeated_ngram" {
			t.Fatalf("expected 1 repeated_ngram violation, got %v", v)
		}
	})

	t.Run("twice is not yet a violation", func(t *testing.T) {
		phrase := "the ledger never once balanced itself"
		s := &Script{Chapters: []Chapter{
			{Index: 1, Text: fillerWordsSeed(20, 201) + " " + phrase + "."},
			{Index: 2, Text: fillerWordsSeed(20, 202) + " " + phrase + "."},
		}}
		if v := ValidateAntiRepetitionNGrams(s); len(v) != 0 {
			t.Fatalf("expected no violations at exactly 2 repeats, got %v", v)
		}
	})
}

func TestValidateScript(t *testing.T) {
	cfg := DefaultChapterValidatorConfig(nil)

	t.Run("valid script has no blocking violations", func(t *testing.T) {
		v := ValidateScript(validScript(), cfg)
		for _, viol := range v {
			if viol.Severity == SeverityError {
				t.Fatalf("expected no blocking (error) violations, got %v", v)
			}
		}
	})

	t.Run("accumulates violations across chapters and cross-chapter guards", func(t *testing.T) {
		s := validScript()
		s.Chapters[0].Text += " 7 " // tts_digits in chapter 1
		phrase := "the ledger never once balanced itself"
		s.Chapters = append(s.Chapters,
			Chapter{Index: 13, Beat: "the_reckoning", Text: fillerWords(20) + " " + phrase + "."},
			Chapter{Index: 14, Beat: "the_refusal", Text: fillerWords(20) + " " + phrase + "."},
			Chapter{Index: 15, Beat: "aftermath", Text: fillerWords(20) + " " + phrase + "."},
		)
		v := ValidateScript(s, cfg)
		rules := map[string]bool{}
		for _, viol := range v {
			rules[viol.Rule] = true
		}
		if !rules["tts_digits"] || !rules["repeated_ngram"] {
			t.Fatalf("expected tts_digits and repeated_ngram violations, got %v", v)
		}
	})
}

func TestValidateChapterSentenceLengthVariance(t *testing.T) {
	cfg := DefaultChapterValidatorConfig(nil)

	t.Run("at least 2 long (20+ word) sentences pass", func(t *testing.T) {
		c := Chapter{Index: 1, Text: words(6) + " " + words(20) + " " + words(6) + " " + words(21)}
		if v := ValidateChapterSentenceLengthVariance(c, cfg); len(v) != 0 {
			t.Fatalf("expected no violations, got %v", v)
		}
	})

	t.Run("fewer than 2 long sentences is a warning, not an error", func(t *testing.T) {
		c := Chapter{Index: 1, Text: words(8) + " " + words(8) + " " + words(8) + " " + words(8)}
		v := ValidateChapterSentenceLengthVariance(c, cfg)
		if len(v) != 1 || v[0].Rule != "sentence_length_variance" {
			t.Fatalf("expected 1 sentence_length_variance violation, got %v", v)
		}
		if v[0].Severity != SeverityWarning {
			t.Fatalf("expected sentence_length_variance to be a warning (style signal, not blocking), got %v", v[0].Severity)
		}
	})

	t.Run("fewer than 2 sentences is never checked", func(t *testing.T) {
		c := Chapter{Index: 1, Text: words(10)}
		if v := ValidateChapterSentenceLengthVariance(c, cfg); len(v) != 0 {
			t.Fatalf("expected no violations with a single sentence, got %v", v)
		}
	})
}

func TestValidateSentenceOpeningRepetition(t *testing.T) {
	t.Run("no repetition passes", func(t *testing.T) {
		if v := ValidateSentenceOpeningRepetition(validScript()); len(v) != 0 {
			t.Fatalf("expected no violations, got %v", v)
		}
	})

	t.Run("same opening nine times fails on the ninth", func(t *testing.T) {
		var chapters []Chapter
		for i := 1; i <= 9; i++ {
			chapters = append(chapters, Chapter{Index: i, Text: "I remember that day. " + fillerWordsSeed(10, int64(300+i))})
		}
		s := &Script{Chapters: chapters}
		v := ValidateSentenceOpeningRepetition(s)
		if len(v) != 1 || v[0].Rule != "repeated_sentence_opening" {
			t.Fatalf("expected exactly 1 violation (the 9th occurrence), got %v", v)
		}
		if v[0].Chapter != 9 {
			t.Fatalf("expected the violation to point at chapter 9 (the excess occurrence), got chapter %d", v[0].Chapter)
		}
	})

	t.Run("exactly eight is not yet a violation", func(t *testing.T) {
		var chapters []Chapter
		for i := 1; i <= 8; i++ {
			chapters = append(chapters, Chapter{Index: i, Text: "I remember that day. " + fillerWordsSeed(10, int64(400+i))})
		}
		if v := ValidateSentenceOpeningRepetition(&Script{Chapters: chapters}); len(v) != 0 {
			t.Fatalf("expected no violations at exactly 8 repeats, got %v", v)
		}
	})
}

func TestValidateRefrainPhrasePlacement(t *testing.T) {
	refrain := "I kept the ledger the way I kept everything"

	t.Run("exactly once in chapter 6 and 13 passes", func(t *testing.T) {
		s := &Script{
			Bible: Bible{RefrainPhrase: refrain},
			Chapters: []Chapter{
				{Index: 6, Beat: "the_cut", Text: fillerWordsSeed(20, 501) + " " + refrain + "."},
				{Index: 13, Beat: "the_reckoning", Text: fillerWordsSeed(20, 502) + " " + refrain + "."},
			},
		}
		if v := ValidateRefrainPhrasePlacement(s); len(v) != 0 {
			t.Fatalf("expected no violations, got %v", v)
		}
	})

	t.Run("missing from chapter 6 fails", func(t *testing.T) {
		s := &Script{
			Bible: Bible{RefrainPhrase: refrain},
			Chapters: []Chapter{
				{Index: 6, Beat: "the_cut", Text: fillerWordsSeed(20, 503)},
				{Index: 13, Beat: "the_reckoning", Text: fillerWordsSeed(20, 504) + " " + refrain + "."},
			},
		}
		v := ValidateRefrainPhrasePlacement(s)
		if len(v) != 1 || v[0].Chapter != 6 {
			t.Fatalf("expected 1 violation on chapter 6, got %v", v)
		}
	})

	t.Run("appearing in a third chapter fails", func(t *testing.T) {
		s := &Script{
			Bible: Bible{RefrainPhrase: refrain},
			Chapters: []Chapter{
				{Index: 6, Beat: "the_cut", Text: fillerWordsSeed(20, 505) + " " + refrain + "."},
				{Index: 9, Beat: "the_years", Text: fillerWordsSeed(20, 506) + " " + refrain + "."},
				{Index: 13, Beat: "the_reckoning", Text: fillerWordsSeed(20, 507) + " " + refrain + "."},
			},
		}
		v := ValidateRefrainPhrasePlacement(s)
		if len(v) != 1 || v[0].Chapter != 9 {
			t.Fatalf("expected 1 violation on chapter 9, got %v", v)
		}
	})

	t.Run("empty refrain phrase is never checked", func(t *testing.T) {
		s := &Script{Bible: Bible{RefrainPhrase: ""}, Chapters: []Chapter{{Index: 6, Text: "anything at all."}}}
		if v := ValidateRefrainPhrasePlacement(s); len(v) != 0 {
			t.Fatalf("expected no violations with an empty refrain phrase, got %v", v)
		}
	})
}

func TestValidateMoneyAmountRepetition(t *testing.T) {
	t.Run("no repetition passes", func(t *testing.T) {
		if v := ValidateMoneyAmountRepetition(validScript()); len(v) != 0 {
			t.Fatalf("expected no violations, got %v", v)
		}
	})

	t.Run("same amount six times fails on the sixth", func(t *testing.T) {
		var chapters []Chapter
		for i := 1; i <= 6; i++ {
			chapters = append(chapters, Chapter{Index: i, DisplayText: fmt.Sprintf("She held $14,800 of my wages, chapter %d.", i)})
		}
		v := ValidateMoneyAmountRepetition(&Script{Chapters: chapters})
		if len(v) != 1 || v[0].Chapter != 6 {
			t.Fatalf("expected exactly 1 violation on chapter 6, got %v", v)
		}
	})

	t.Run("exactly five is not yet a violation", func(t *testing.T) {
		var chapters []Chapter
		for i := 1; i <= 5; i++ {
			chapters = append(chapters, Chapter{Index: i, DisplayText: fmt.Sprintf("She held $14,800 of my wages, chapter %d.", i)})
		}
		if v := ValidateMoneyAmountRepetition(&Script{Chapters: chapters}); len(v) != 0 {
			t.Fatalf("expected no violations at exactly 5 repeats, got %v", v)
		}
	})

	t.Run("different amounts don't count toward each other", func(t *testing.T) {
		s := &Script{Chapters: []Chapter{
			{Index: 1, DisplayText: "She held $14,800 of my wages."},
			{Index: 2, DisplayText: "He owed $200 for the repair."},
		}}
		if v := ValidateMoneyAmountRepetition(s); len(v) != 0 {
			t.Fatalf("expected no violations across two distinct amounts, got %v", v)
		}
	})
}
