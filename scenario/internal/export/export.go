// Package export turns an accepted story.Script into the three artifacts
// `gen export` writes: a TTS-ready transcript, a per-chapter (and per-
// paragraph) timing estimate carrying the pacing pauses, and YouTube
// upload metadata. Every function here is pure — no file I/O — cmd/gen
// handles reading the script and writing whatever --out path the user
// asked for.
package export

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/placeholder/scenario/internal/story"
)

// TTSOptions controls the TTS transcript's formatting. The zero value
// produces plain text with no markup — Kokoro (and most local/offline TTS
// engines) don't support SSML, so that's the default. Set SSML to opt into
// <break> tags for an engine that does support it; pacing is otherwise
// carried entirely by the timing manifest's PauseAfterMs fields.
type TTSOptions struct {
	ChapterBreakMS   int
	ParagraphBreakMS int
	SSML             bool // true: emit <break> tags; false (default): plain text
}

// TTS joins every chapter's TTS-ready Text (already digit-free — see
// story.ValidateChapterNoDigitsInTTSText) in chapter order. By default this
// is pure plain text (paragraphs separated by a blank line, chapters by a
// double blank line) with no embedded pacing markup — see the timing
// manifest (Timing/TimingJSON) for pause_after_ms instead. With SSML:true
// it embeds <break> tags directly for engines that accept them.
func TTS(script *story.Script, opts TTSOptions) string {
	paragraphSep := "\n\n"
	chapterSep := "\n\n\n"
	if opts.SSML {
		chapterBreakMS := opts.ChapterBreakMS
		if chapterBreakMS <= 0 {
			chapterBreakMS = 700
		}
		paragraphBreakMS := opts.ParagraphBreakMS
		if paragraphBreakMS <= 0 {
			paragraphBreakMS = 350
		}
		paragraphSep = fmt.Sprintf("\n<break time=\"%dms\"/>\n", paragraphBreakMS)
		chapterSep = fmt.Sprintf("\n<break time=\"%dms\"/>\n", chapterBreakMS)
	}

	parts := make([]string, len(script.Chapters))
	for i, ch := range script.Chapters {
		parts[i] = strings.Join(splitParagraphs(ch.Text), paragraphSep)
	}
	return strings.Join(parts, chapterSep)
}

func splitParagraphs(text string) []string {
	raw := strings.Split(text, "\n\n")
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ParagraphTiming is one paragraph's word count, estimated narration time,
// and the pause to insert after it (0 for a chapter's last paragraph —
// the chapter-level pause takes over there instead).
type ParagraphTiming struct {
	Index        int     `json:"index"`
	Words        int     `json:"words"`
	EstSeconds   float64 `json:"est_seconds"`
	PauseAfterMs int     `json:"pause_after_ms"`
}

// TimingEntry is one chapter's word count, estimated narration time, the
// pause after the whole chapter (0 for the script's last chapter), and its
// paragraph-level breakdown.
type TimingEntry struct {
	Chapter      int               `json:"chapter"`
	Beat         string            `json:"beat"`
	Words        int               `json:"words"`
	EstSeconds   float64           `json:"est_seconds"`
	PauseAfterMs int               `json:"pause_after_ms"`
	Paragraphs   []ParagraphTiming `json:"paragraphs"`
}

// Timing estimates narration time per chapter and per paragraph from
// actual word counts at wpm words per minute, and carries the pacing
// pauses (chapterBreakMs after each chapter, paragraphBreakMs after each
// paragraph) that used to live in TTS's SSML markup — now here instead,
// since the default TTS output is plain text with no embedded pauses.
// wpm <= 0 falls back to 150; chapterBreakMs/paragraphBreakMs <= 0 fall
// back to 700/350.
func Timing(script *story.Script, wpm, chapterBreakMs, paragraphBreakMs int) []TimingEntry {
	if wpm <= 0 {
		wpm = 150
	}
	if chapterBreakMs <= 0 {
		chapterBreakMs = 700
	}
	if paragraphBreakMs <= 0 {
		paragraphBreakMs = 350
	}
	wordsPerSecond := float64(wpm) / 60

	out := make([]TimingEntry, len(script.Chapters))
	for i, ch := range script.Chapters {
		paragraphs := splitParagraphs(ch.Text)
		pTimings := make([]ParagraphTiming, len(paragraphs))
		for j, p := range paragraphs {
			words := len(strings.Fields(p))
			pause := paragraphBreakMs
			if j == len(paragraphs)-1 {
				pause = 0 // the chapter-level pause applies after the last paragraph instead
			}
			pTimings[j] = ParagraphTiming{Index: j + 1, Words: words, EstSeconds: float64(words) / wordsPerSecond, PauseAfterMs: pause}
		}

		words := len(strings.Fields(ch.Text))
		pause := chapterBreakMs
		if i == len(script.Chapters)-1 {
			pause = 0 // nothing follows the last chapter
		}
		out[i] = TimingEntry{
			Chapter: ch.Index, Beat: ch.Beat, Words: words, EstSeconds: float64(words) / wordsPerSecond,
			PauseAfterMs: pause, Paragraphs: pTimings,
		}
	}
	return out
}

// TimingJSON is Timing marshaled as the array `gen export --format timing`
// writes.
func TimingJSON(script *story.Script, wpm, chapterBreakMs, paragraphBreakMs int) ([]byte, error) {
	b, err := json.MarshalIndent(Timing(script, wpm, chapterBreakMs, paragraphBreakMs), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("export: marshal timing: %w", err)
	}
	return b, nil
}

const fictionalDramatizationNote = "This video is a fictional dramatization. Any resemblance to real people or events, living or dead, is coincidental."

// Meta is the YouTube upload metadata `gen export --format meta` writes.
type Meta struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

// BuildMeta assembles Meta from the script: title, a description built from
// the hook chapter's summary (or the bible's refrain phrase if no summary
// is available) plus the fictional-dramatization note, and tags seeded
// from the profession and genre plus whatever extra tags the caller adds.
func BuildMeta(script *story.Script, extraTags ...string) Meta {
	description := fmt.Sprintf("%s\n\n%s", metaTeaser(script), fictionalDramatizationNote)

	tags := []string{"underestimated", "quiet revenge", "life story"}
	if script.Seed.Profession != "" {
		tags = append(tags, script.Seed.Profession)
	}
	tags = append(tags, extraTags...)

	return Meta{Title: script.Title, Description: description, Tags: tags}
}

func metaTeaser(script *story.Script) string {
	if len(script.Chapters) > 0 && strings.TrimSpace(script.Chapters[0].Summary) != "" {
		return script.Chapters[0].Summary
	}
	return script.Bible.RefrainPhrase
}

// JSON marshals Meta the way `gen export --format meta` writes it.
func (m Meta) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("export: marshal meta: %w", err)
	}
	return b, nil
}

// BundleNarrator is the manifest's narrator block — enough for voiceover's
// catalog.Select to pick a matching voice (sex, an age to check against a
// voice's age_feel range) and for avatar's portrait prompt (build, hair,
// face_note, attire), without either module touching scenario's
// story.Bible, config, or SQLite directly.
type BundleNarrator struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
	Sex  string `json:"sex"`
	// Build, Hair, FaceNote come straight from the bible LLM call
	// (story.Person's appearance fields) — see prompts/bible.tmpl's
	// appearance instructions.
	Build    string `json:"build"`
	Hair     string `json:"hair"`
	FaceNote string `json:"face_note"`
	// Attire comes from Seed.Attire (configs/professions.yaml, resolved at
	// seed-draw time) — what the narrator wears for their job, not a
	// general appearance description.
	Attire string `json:"attire"`
}

// BundleChapter is one chapter's position within the bundle's script.txt.
// CharStart/CharEnd are byte offsets into that exact file — voiceover's
// internal/chunk uses them to know where a chapter boundary falls without
// re-deriving chapter breaks from the raw text itself.
type BundleChapter struct {
	Index     int    `json:"index"`
	Beat      string `json:"beat"`
	Words     int    `json:"words"`
	CharStart int    `json:"char_start"`
	CharEnd   int    `json:"char_end"`
}

// BundleStory carries the seed/bible facts the thumbnail module's text
// generation needs — deliberately just the abstract facts (who, the
// betrayal, how it resolves), not full chapter prose. Thumbnail text is
// meant to tease the story, not summarize it, so it works from the same
// facts that shaped the story rather than reading the finished chapters.
type BundleStory struct {
	FamilyLaw     string `json:"family_law"`
	RefrainPhrase string `json:"refrain_phrase"`
	// SeededLine is a bible-authored quotable line — the thumbnail prompt's
	// "quote it directly if possible" instruction is written with exactly
	// this (and RefrainPhrase) in mind.
	SeededLine string `json:"seeded_line"`
	Antagonist string `json:"antagonist"`  // Seed.Antagonist — the wrongdoer's role
	Betrayal   string `json:"betrayal"`    // Seed.Betrayal
	EndingType string `json:"ending_type"` // Seed.EndingType — how the reckoning resolves
	// Numbers are the concrete facts (dollar amounts, durations) already
	// established in the bible, reused as-is rather than invented anew —
	// matches the chapter-generation prompt's own "reuse exactly" rule.
	Numbers map[string]string `json:"numbers"`
}

// BundleManifest is manifest.json — the ONLY metadata format voiceover
// (a separate module) is allowed to depend on. Changing this struct's JSON
// shape is a cross-module contract change, not a local refactor.
type BundleManifest struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Profession is the narrator's job, in Seed.Profession's exact words
	// (matches configs/professions.yaml's `name`) — avatar's portrait
	// prompt uses it directly for the workplace setting.
	Profession string          `json:"profession"`
	WordCount  int             `json:"word_count"`
	Narrator   BundleNarrator  `json:"narrator"`
	Story      BundleStory     `json:"story"`
	Chapters   []BundleChapter `json:"chapters"`
}

// Bundle builds script.txt and manifest.json together, from the same
// per-chapter rendering TTS(script, TTSOptions{}) uses — computing the
// manifest's char offsets against a separately-built text risks the two
// drifting out of sync, so this renders once and derives both from it.
// scriptText equals TTS(script, TTSOptions{}) byte-for-byte.
func Bundle(script *story.Script) (scriptText []byte, manifestJSON []byte, err error) {
	rendered := make([]string, len(script.Chapters))
	for i, ch := range script.Chapters {
		rendered[i] = strings.Join(splitParagraphs(ch.Text), bundleParagraphSep)
	}
	text := strings.Join(rendered, bundleChapterSep)

	chapters := make([]BundleChapter, len(script.Chapters))
	offset := 0
	for i, ch := range script.Chapters {
		start := offset
		end := start + len(rendered[i])
		chapters[i] = BundleChapter{
			Index: ch.Index, Beat: ch.Beat, Words: len(strings.Fields(ch.Text)),
			CharStart: start, CharEnd: end,
		}
		offset = end + len(bundleChapterSep)
	}

	fullNarrator := script.Bible.Full().Narrator
	manifest := BundleManifest{
		ID:         script.ID,
		Title:      script.Title,
		Profession: script.Seed.Profession,
		WordCount:  script.WordCount,
		Narrator: BundleNarrator{
			Name:     fullNarrator.Name,
			Age:      fullNarrator.Age,
			Sex:      script.Seed.ProtagonistSex,
			Build:    fullNarrator.Build,
			Hair:     fullNarrator.Hair,
			FaceNote: fullNarrator.FaceNote,
			Attire:   script.Seed.Attire,
		},
		Story: BundleStory{
			FamilyLaw:     script.Bible.Full().FamilyLaw,
			RefrainPhrase: script.Bible.Full().RefrainPhrase,
			SeededLine:    script.Bible.Full().SeededLine,
			Antagonist:    script.Seed.Antagonist,
			Betrayal:      script.Seed.Betrayal,
			EndingType:    script.Seed.EndingType,
			Numbers:       script.Bible.Full().Numbers,
		},
		Chapters: chapters,
	}
	mj, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("export: marshal bundle manifest: %w", err)
	}
	return []byte(text), mj, nil
}

// bundleParagraphSep/bundleChapterSep mirror TTS's own plain-text (SSML:
// false) separators exactly — kept as named constants here so Bundle can't
// silently drift from TTS if one of the two is ever edited alone.
const (
	bundleParagraphSep = "\n\n"
	bundleChapterSep   = "\n\n\n"
)
