package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/placeholder/scenario/internal/story"
)

func newMemoryBackend(t *testing.T) Store {
	t.Helper()
	return NewMemoryStore()
}

func newSQLiteBackend(t *testing.T) Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// mkScript builds a fully-populated, distinguishable script for a given id.
func mkScript(id string, createdAt time.Time, status story.Status, seed story.Seed) *story.Script {
	return &story.Script{
		ID:        id,
		CreatedAt: createdAt,
		Seed:      seed,
		Bible: story.Bible{
			Narrator:      story.Person{Name: "Narrator " + id, Age: 40, Role: "narrator", City: "City " + id},
			Cast:          []story.Person{{Name: "Cast " + id, Age: 60, Role: "antagonist"}},
			Timeline:      []story.Event{{Year: 1, What: "start of " + id}},
			FamilyLaw:     "Aphorism " + id,
			RefrainPhrase: "refrain " + id,
			SeededLine:    "seeded " + id,
			Numbers:       map[string]string{"amount": "twelve hundred dollars"},
		},
		Title: "Title " + id,
		Chapters: []story.Chapter{
			{Index: 1, Beat: "hook", TargetWords: 170, Text: "text one " + id, DisplayText: "Text One " + id, Summary: "sum1 " + id},
			{Index: 2, Beat: "pivot", TargetWords: 170, Text: "text two " + id, DisplayText: "Text Two " + id, Summary: "sum2 " + id},
		},
		Status:    status,
		WordCount: 4,
		Provider:  "groq",
		Model:     "test-model",
		TokensIn:  100,
		TokensOut: 200,
	}
}

func testSeed(profession, antagonist, endingType, objectContainer string, duration int, reckoningPlace string) story.Seed {
	return story.Seed{
		Profession: profession, Epistemology: "e", RecordType: "r", ProtagonistSex: "female",
		Attire: "worn work clothes", Antagonist: antagonist, WeakAlly: "father", HumiliationType: "dismissive_joke",
		Betrayal: "savings_taken", Duration: duration, WrittenOverreach: "email_chain",
		ObjectContainer: objectContainer, LegacyArtifact: "childs_drawing",
		ReckoningPlace: reckoningPlace, EndingType: endingType, Region: "midwest",
	}
}

func mustSave(t *testing.T, s Store, script *story.Script) {
	t.Helper()
	if err := s.SaveScript(context.Background(), script); err != nil {
		t.Fatalf("SaveScript(%s): %v", script.ID, err)
	}
}

func withBothBackends(t *testing.T, run func(t *testing.T, s Store)) {
	t.Helper()
	for _, backend := range []struct {
		name    string
		factory func(t *testing.T) Store
	}{
		{"memory", newMemoryBackend},
		{"sqlite", newSQLiteBackend},
	} {
		t.Run(backend.name, func(t *testing.T) {
			run(t, backend.factory(t))
		})
	}
}

func TestSaveAndGetScriptRoundTrips(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		want := mkScript("s1", time.Now().UTC().Truncate(time.Second), story.StatusPending,
			testSeed("nurse", "mother_in_law", "cold_silence", "tin_box", 7, "kitchen_table"))
		mustSave(t, s, want)

		got, err := s.GetScript(ctx, "s1")
		if err != nil {
			t.Fatalf("GetScript: %v", err)
		}

		if got.ID != want.ID || got.Title != want.Title || got.Status != want.Status ||
			got.WordCount != want.WordCount || got.Provider != want.Provider || got.Model != want.Model ||
			got.TokensIn != want.TokensIn || got.TokensOut != want.TokensOut {
			t.Fatalf("scalar fields mismatch: got %+v, want %+v", got, want)
		}
		if !got.CreatedAt.Equal(want.CreatedAt) {
			t.Fatalf("CreatedAt mismatch: got %v, want %v", got.CreatedAt, want.CreatedAt)
		}
		if !reflect.DeepEqual(got.Seed, want.Seed) {
			t.Fatalf("Seed mismatch: got %+v, want %+v", got.Seed, want.Seed)
		}
		if !reflect.DeepEqual(got.Bible, want.Bible) {
			t.Fatalf("Bible mismatch: got %+v, want %+v", got.Bible, want.Bible)
		}
		if !reflect.DeepEqual(got.Chapters, want.Chapters) {
			t.Fatalf("Chapters mismatch: got %+v, want %+v", got.Chapters, want.Chapters)
		}
		if got.Quality != (story.QualityScores{}) {
			t.Fatalf("expected zero-value Quality before any review, got %+v", got.Quality)
		}
	})
}

func TestGetScriptNotFoundReturnsErrNotFound(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		_, err := s.GetScript(context.Background(), "missing")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestSaveScriptUpsertReplacesChapters(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		script := mkScript("s1", time.Now().UTC(), story.StatusPending, testSeed("nurse", "a", "b", "c", 3, "d"))
		mustSave(t, s, script)

		script.Chapters = []story.Chapter{
			{Index: 1, Beat: "hook", TargetWords: 170, Text: "resaved text", DisplayText: "Resaved Text", Summary: "resaved"},
		}
		mustSave(t, s, script)

		got, err := s.GetScript(ctx, "s1")
		if err != nil {
			t.Fatalf("GetScript: %v", err)
		}
		if len(got.Chapters) != 1 {
			t.Fatalf("expected exactly 1 chapter after resave, got %d: %+v", len(got.Chapters), got.Chapters)
		}
		if got.Chapters[0].Text != "resaved text" {
			t.Fatalf("expected the resaved chapter text, got %q", got.Chapters[0].Text)
		}
	})
}

func TestListScriptsFiltersByStatusAndOrdersDescending(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		base := time.Now().UTC().Truncate(time.Second)
		mustSave(t, s, mkScript("s1", base, story.StatusAccepted, testSeed("nurse", "a", "b", "c", 3, "d")))
		mustSave(t, s, mkScript("s2", base.Add(time.Hour), story.StatusRejected, testSeed("mechanic", "a", "b", "c", 3, "d")))
		mustSave(t, s, mkScript("s3", base.Add(2*time.Hour), story.StatusAccepted, testSeed("electrician", "a", "b", "c", 3, "d")))

		all, err := s.ListScripts(ctx, ListFilter{})
		if err != nil {
			t.Fatalf("ListScripts: %v", err)
		}
		if len(all) != 3 {
			t.Fatalf("expected 3 scripts, got %d", len(all))
		}
		if all[0].ID != "s3" || all[1].ID != "s2" || all[2].ID != "s1" {
			t.Fatalf("expected descending created_at order [s3 s2 s1], got %v", ids(all))
		}

		accepted, err := s.ListScripts(ctx, ListFilter{Status: story.StatusAccepted})
		if err != nil {
			t.Fatalf("ListScripts accepted: %v", err)
		}
		if len(accepted) != 2 || accepted[0].ID != "s3" || accepted[1].ID != "s1" {
			t.Fatalf("expected [s3 s1] accepted scripts, got %v", ids(accepted))
		}

		limited, err := s.ListScripts(ctx, ListFilter{Limit: 1})
		if err != nil {
			t.Fatalf("ListScripts limited: %v", err)
		}
		if len(limited) != 1 || limited[0].ID != "s3" {
			t.Fatalf("expected [s3], got %v", ids(limited))
		}
	})
}

func ids(summaries []ScriptSummary) []string {
	out := make([]string, len(summaries))
	for i, s := range summaries {
		out[i] = s.ID
	}
	return out
}

func TestUpdateChapterReplacesOnlyThatChapter(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		mustSave(t, s, mkScript("s1", time.Now().UTC(), story.StatusPending, testSeed("nurse", "a", "b", "c", 3, "d")))

		if err := s.UpdateChapter(ctx, "s1", story.Chapter{
			Index: 1, Beat: "hook", TargetWords: 170, Text: "updated", DisplayText: "Updated", Summary: "u",
		}); err != nil {
			t.Fatalf("UpdateChapter: %v", err)
		}

		got, err := s.GetScript(ctx, "s1")
		if err != nil {
			t.Fatalf("GetScript: %v", err)
		}
		ch1, ok := got.ChapterByIndex(1)
		if !ok || ch1.Text != "updated" {
			t.Fatalf("expected chapter 1 to be updated, got %+v", got.Chapters)
		}
		ch2, ok := got.ChapterByIndex(2)
		if !ok || ch2.Text != "text two s1" {
			t.Fatalf("expected chapter 2 untouched, got %+v", got.Chapters)
		}
	})
}

func TestUpdateChapterOnUnknownScriptReturnsErrNotFound(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		err := s.UpdateChapter(context.Background(), "missing", story.Chapter{Index: 1})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestSaveQualityScoresAppendsAttemptsAndGetScriptReturnsLatest(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		mustSave(t, s, mkScript("s1", time.Now().UTC(), story.StatusPending, testSeed("nurse", "a", "b", "c", 3, "d")))

		first := story.QualityScores{HookStrength: 4, ProfessionCausality: 4, Restraint: 4, SceneNotSummary: 4, PlantingPayoff: 4, RefusalPresent: 4, AISmell: 4, Comment: "weak"}
		second := story.QualityScores{HookStrength: 8, ProfessionCausality: 8, Restraint: 8, SceneNotSummary: 8, PlantingPayoff: 8, RefusalPresent: 8, AISmell: 8, Comment: "better"}

		if err := s.SaveQualityScores(ctx, "s1", first); err != nil {
			t.Fatalf("SaveQualityScores 1: %v", err)
		}
		if err := s.SaveQualityScores(ctx, "s1", second); err != nil {
			t.Fatalf("SaveQualityScores 2: %v", err)
		}

		got, err := s.GetScript(ctx, "s1")
		if err != nil {
			t.Fatalf("GetScript: %v", err)
		}
		if got.Quality.Comment != "better" {
			t.Fatalf("expected the latest quality attempt, got %+v", got.Quality)
		}
	})
}

func TestSaveQualityScoresOnUnknownScriptReturnsErrNotFound(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		err := s.SaveQualityScores(context.Background(), "missing", story.QualityScores{})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestSetStatusUpdatesAndOnUnknownScriptReturnsErrNotFound(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		mustSave(t, s, mkScript("s1", time.Now().UTC(), story.StatusPending, testSeed("nurse", "a", "b", "c", 3, "d")))

		if err := s.SetStatus(ctx, "s1", story.StatusAccepted); err != nil {
			t.Fatalf("SetStatus: %v", err)
		}
		got, err := s.GetScript(ctx, "s1")
		if err != nil {
			t.Fatalf("GetScript: %v", err)
		}
		if got.Status != story.StatusAccepted {
			t.Fatalf("expected status accepted, got %q", got.Status)
		}

		if err := s.SetStatus(ctx, "missing", story.StatusAccepted); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound for unknown script, got %v", err)
		}
	})
}

func TestSeedDedupOnlyConsidersAcceptedScripts(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		base := time.Now().UTC().Truncate(time.Second)

		mustSave(t, s, mkScript("rejected", base, story.StatusRejected,
			testSeed("nurse", "mother_in_law", "cold_silence", "tin_box", 7, "kitchen_table")))

		recent, err := s.RecentProfessions(ctx, 5)
		if err != nil {
			t.Fatalf("RecentProfessions: %v", err)
		}
		if len(recent) != 0 {
			t.Fatalf("expected a rejected script to not count toward RecentProfessions, got %v", recent)
		}

		exists, err := s.TripletExists(ctx, "nurse", "mother_in_law", "cold_silence")
		if err != nil {
			t.Fatalf("TripletExists: %v", err)
		}
		if exists {
			t.Fatalf("expected a rejected script's triplet to not count as existing")
		}

		_, ok, err := s.LastObjectContainer(ctx)
		if err != nil {
			t.Fatalf("LastObjectContainer: %v", err)
		}
		if ok {
			t.Fatalf("expected no last object container with only a rejected script saved")
		}
	})
}

func TestSeedDedupQueriesAfterAcceptance(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		base := time.Now().UTC().Truncate(time.Second)

		mustSave(t, s, mkScript("s1", base, story.StatusAccepted,
			testSeed("nurse", "mother_in_law", "cold_silence", "tin_box", 7, "kitchen_table")))
		mustSave(t, s, mkScript("s2", base.Add(time.Hour), story.StatusAccepted,
			testSeed("mechanic", "brother_in_law", "quiet_acknowledgment", "oak_cabinet", 9, "church_hall")))

		recent, err := s.RecentProfessions(ctx, 5)
		if err != nil {
			t.Fatalf("RecentProfessions: %v", err)
		}
		if len(recent) != 2 || recent[0] != "mechanic" || recent[1] != "nurse" {
			t.Fatalf("expected [mechanic nurse] most-recent-first, got %v", recent)
		}

		limited, err := s.RecentProfessions(ctx, 1)
		if err != nil {
			t.Fatalf("RecentProfessions limit 1: %v", err)
		}
		if len(limited) != 1 || limited[0] != "mechanic" {
			t.Fatalf("expected [mechanic], got %v", limited)
		}

		exists, err := s.TripletExists(ctx, "nurse", "mother_in_law", "cold_silence")
		if err != nil || !exists {
			t.Fatalf("expected the first script's triplet to exist, got exists=%v err=%v", exists, err)
		}
		exists, err = s.TripletExists(ctx, "nurse", "mother_in_law", "quiet_acknowledgment")
		if err != nil || exists {
			t.Fatalf("expected an unused triplet to not exist, got exists=%v err=%v", exists, err)
		}

		container, ok, err := s.LastObjectContainer(ctx)
		if err != nil || !ok || container != "oak_cabinet" {
			t.Fatalf("expected last object container oak_cabinet, got %q ok=%v err=%v", container, ok, err)
		}
		duration, ok, err := s.LastDuration(ctx)
		if err != nil || !ok || duration != 9 {
			t.Fatalf("expected last duration 9, got %d ok=%v err=%v", duration, ok, err)
		}
		place, ok, err := s.LastReckoningPlace(ctx)
		if err != nil || !ok || place != "church_hall" {
			t.Fatalf("expected last reckoning place church_hall, got %q ok=%v err=%v", place, ok, err)
		}
	})
}

func TestRecentLegacyArtifacts(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		base := time.Now().UTC().Truncate(time.Second)

		mustSave(t, s, mkScript("rejected", base, story.StatusRejected,
			testSeed("nurse", "a", "b", "c", 3, "d")))

		s1 := testSeed("nurse", "a", "b", "c", 3, "d")
		s1.LegacyArtifact = "childs_drawing"
		s2 := testSeed("mechanic", "a", "b", "c", 3, "d")
		s2.LegacyArtifact = "fathers_letters"
		mustSave(t, s, mkScript("s1", base.Add(time.Hour), story.StatusAccepted, s1))
		mustSave(t, s, mkScript("s2", base.Add(2*time.Hour), story.StatusAccepted, s2))

		recent, err := s.RecentLegacyArtifacts(ctx, 4)
		if err != nil {
			t.Fatalf("RecentLegacyArtifacts: %v", err)
		}
		if len(recent) != 2 || recent[0] != "fathers_letters" || recent[1] != "childs_drawing" {
			t.Fatalf("expected [fathers_letters childs_drawing] most-recent-first (rejected script excluded), got %v", recent)
		}

		limited, err := s.RecentLegacyArtifacts(ctx, 1)
		if err != nil {
			t.Fatalf("RecentLegacyArtifacts limit 1: %v", err)
		}
		if len(limited) != 1 || limited[0] != "fathers_letters" {
			t.Fatalf("expected [fathers_letters], got %v", limited)
		}
	})
}

func TestSeedDedupEmptyStoreReturnsNotOK(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		if recent, err := s.RecentProfessions(ctx, 5); err != nil || len(recent) != 0 {
			t.Fatalf("expected no recent professions, got %v err=%v", recent, err)
		}
		if _, ok, err := s.LastObjectContainer(ctx); err != nil || ok {
			t.Fatalf("expected ok=false, got ok=%v err=%v", ok, err)
		}
		if _, ok, err := s.LastDuration(ctx); err != nil || ok {
			t.Fatalf("expected ok=false, got ok=%v err=%v", ok, err)
		}
		if _, ok, err := s.LastReckoningPlace(ctx); err != nil || ok {
			t.Fatalf("expected ok=false, got ok=%v err=%v", ok, err)
		}
		if recent, err := s.RecentLegacyArtifacts(ctx, 4); err != nil || len(recent) != 0 {
			t.Fatalf("expected no recent legacy artifacts, got %v err=%v", recent, err)
		}
	})
}

func TestRecordAcceptanceAndRecentUsedValues(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		base := time.Now().UTC()

		s1 := mkScript("s1", base, story.StatusAccepted, testSeed("nurse", "a", "b", "c", 3, "d"))
		s2 := mkScript("s2", base.Add(time.Second), story.StatusAccepted, testSeed("mechanic", "a", "b", "c", 3, "d"))
		mustSave(t, s, s1)
		mustSave(t, s, s2)

		if err := s.RecordAcceptance(ctx, s1); err != nil {
			t.Fatalf("RecordAcceptance s1: %v", err)
		}
		if err := s.RecordAcceptance(ctx, s2); err != nil {
			t.Fatalf("RecordAcceptance s2: %v", err)
		}

		names, err := s.RecentUsedNames(ctx, 30)
		if err != nil {
			t.Fatalf("RecentUsedNames: %v", err)
		}
		// Each "First Last" name now books the full name PLUS each part
		// (first, last), so a reused surname is caught later — two scripts x
		// (narrator + 1 cast) therefore records the full names and the shared
		// "Narrator"/"Cast"/"s1"/"s2" tokens, not just 4 whole names.
		gotNames := map[string]bool{}
		for _, n := range names {
			gotNames[n] = true
		}
		for _, want := range []string{"Narrator s1", "Cast s1", "Narrator s2", "Cast s2", "Narrator", "Cast", "s1", "s2"} {
			if !gotNames[want] {
				t.Fatalf("expected %q among recorded names, got %v", want, names)
			}
		}
		// s2 recorded later -> its full name comes before s1's (recent-first).
		posN2, posN1 := -1, -1
		for i, n := range names {
			if n == "Narrator s2" && posN2 == -1 {
				posN2 = i
			}
			if n == "Narrator s1" && posN1 == -1 {
				posN1 = i
			}
		}
		if posN2 == -1 || posN1 == -1 || posN2 > posN1 {
			t.Fatalf("expected s2's names before s1's, got %v", names)
		}

		places, err := s.RecentUsedPlaces(ctx, 30)
		if err != nil {
			t.Fatalf("RecentUsedPlaces: %v", err)
		}
		if len(places) != 2 || places[0] != "City s2" || places[1] != "City s1" {
			t.Fatalf("expected [City s2 City s1], got %v", places)
		}

		aphorisms, err := s.RecentUsedAphorisms(ctx, 30)
		if err != nil {
			t.Fatalf("RecentUsedAphorisms: %v", err)
		}
		if len(aphorisms) != 2 || aphorisms[0] != "Aphorism s2" || aphorisms[1] != "Aphorism s1" {
			t.Fatalf("expected [Aphorism s2 Aphorism s1], got %v", aphorisms)
		}

		limited, err := s.RecentUsedNames(ctx, 1)
		if err != nil {
			t.Fatalf("RecentUsedNames limit 1: %v", err)
		}
		if len(limited) != 1 {
			t.Fatalf("expected exactly 1 name with limit 1, got %v", limited)
		}
	})
}

func TestRecentUsedValuesEmptyStoreReturnsNil(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		if names, err := s.RecentUsedNames(ctx, 30); err != nil || len(names) != 0 {
			t.Fatalf("expected no used names, got %v err=%v", names, err)
		}
	})
}

func TestTopUsedPhrasesAndPhraseOverlaps(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		base := time.Now().UTC()

		s1 := mkScript("s1", base, story.StatusAccepted, testSeed("nurse", "a", "b", "c", 3, "d"))
		s1.Chapters = []story.Chapter{
			{Index: 1, Beat: "hook", Text: "this is a quiet reckoning not a shouting match across a crowded room today"},
		}
		s2 := mkScript("s2", base.Add(time.Second), story.StatusAccepted, testSeed("mechanic", "a", "b", "c", 3, "d"))
		s2.Chapters = []story.Chapter{
			{Index: 1, Beat: "hook", Text: "this is a quiet reckoning not a shouting match across a different town today"},
		}
		mustSave(t, s, s1)
		mustSave(t, s, s2)
		if err := s.RecordAcceptance(ctx, s1); err != nil {
			t.Fatalf("RecordAcceptance s1: %v", err)
		}
		if err := s.RecordAcceptance(ctx, s2); err != nil {
			t.Fatalf("RecordAcceptance s2: %v", err)
		}

		top, err := s.TopUsedPhrases(ctx, 10, 40)
		if err != nil {
			t.Fatalf("TopUsedPhrases: %v", err)
		}
		found := false
		for _, p := range top {
			if p == "this is a quiet reckoning not" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected the phrase shared by both scripts in top phrases, got %v", top)
		}

		overlaps, err := s.PhraseOverlaps(ctx,
			[]string{"this is a quiet reckoning not", "a phrase that appears nowhere else"}, 10)
		if err != nil {
			t.Fatalf("PhraseOverlaps: %v", err)
		}
		ids := overlaps["this is a quiet reckoning not"]
		if len(ids) != 2 {
			t.Fatalf("expected 2 script ids sharing the phrase, got %v", ids)
		}
		if _, ok := overlaps["a phrase that appears nowhere else"]; ok {
			t.Fatalf("expected no entry for a phrase that was never recorded, got %v", overlaps)
		}
	})
}

func TestTopUsedPhrasesAndPhraseOverlapsEmptyStoreReturnNil(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		if top, err := s.TopUsedPhrases(ctx, 10, 40); err != nil || len(top) != 0 {
			t.Fatalf("expected no top phrases, got %v err=%v", top, err)
		}
		if overlaps, err := s.PhraseOverlaps(ctx, []string{"some six word phrase right here"}, 10); err != nil || len(overlaps) != 0 {
			t.Fatalf("expected no overlaps, got %v err=%v", overlaps, err)
		}
	})
}

func TestSaveRunAndStats(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		base := time.Now().UTC()

		mustSave(t, s, mkScript("s1", base, story.StatusAccepted, testSeed("nurse", "a", "b", "c", 3, "d")))
		mustSave(t, s, mkScript("s2", base, story.StatusRejected, testSeed("mechanic", "a", "b", "c", 3, "d")))
		mustSave(t, s, mkScript("s3", base, story.StatusPending, testSeed("electrician", "a", "b", "c", 3, "d")))

		if err := s.SaveQualityScores(ctx, "s1", story.QualityScores{
			HookStrength: 8, ProfessionCausality: 8, Restraint: 8, SceneNotSummary: 8,
			PlantingPayoff: 8, RefusalPresent: 8, AISmell: 8,
		}); err != nil {
			t.Fatalf("SaveQualityScores: %v", err)
		}

		if err := s.SaveRun(ctx, Run{
			ID: "run1", ScriptID: "s1", Provider: "groq", Outcome: "accepted",
			StartedAt: base, FinishedAt: base.Add(time.Minute),
		}); err != nil {
			t.Fatalf("SaveRun: %v", err)
		}

		stats, err := s.Stats(ctx)
		if err != nil {
			t.Fatalf("Stats: %v", err)
		}
		if stats.TotalScripts != 3 || stats.AcceptedScripts != 1 || stats.RejectedScripts != 1 || stats.PendingScripts != 1 {
			t.Fatalf("unexpected stats: %+v", stats)
		}
		if stats.AverageWordCount != 4 {
			t.Fatalf("expected average word count 4, got %v", stats.AverageWordCount)
		}
		if stats.AverageQuality != 8 {
			t.Fatalf("expected average quality 8 (only s1 scored), got %v", stats.AverageQuality)
		}
	})
}

func TestStatsOnEmptyStore(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		stats, err := s.Stats(context.Background())
		if err != nil {
			t.Fatalf("Stats: %v", err)
		}
		if stats.TotalScripts != 0 || stats.AverageQuality != 0 || stats.AverageWordCount != 0 {
			t.Fatalf("expected zero-value stats, got %+v", stats)
		}
		if len(stats.Usage) != 0 {
			t.Fatalf("expected no usage entries on an empty store, got %+v", stats.Usage)
		}
	})
}

func TestSaveScriptRoundTripsUsageBreakdown(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		script := mkScript("s1", time.Now().UTC(), story.StatusPending, testSeed("nurse", "a", "b", "c", 3, "d"))
		script.RecordUsage("generate", "google-ai-studio", "gemini-3.6-flash", 1000, 500, 300, story.CauseInitial)
		script.RecordUsage("generate", "google-ai-studio", "gemini-3.6-flash", 200, 100, 50, story.CauseInitial)
		script.RecordUsage("summary", "google-ai-studio", "gemini-3.5-flash-lite", 50, 10, 0, story.CauseInitial)
		mustSave(t, s, script)

		got, err := s.GetScript(ctx, "s1")
		if err != nil {
			t.Fatalf("GetScript: %v", err)
		}
		if len(got.Usage) != 2 {
			t.Fatalf("expected 2 usage entries, got %d: %+v", len(got.Usage), got.Usage)
		}

		var gen, sum *story.UsageEntry
		for i := range got.Usage {
			switch got.Usage[i].Role {
			case "generate":
				gen = &got.Usage[i]
			case "summary":
				sum = &got.Usage[i]
			}
		}
		if gen == nil || gen.Calls != 2 || gen.TokensIn != 1200 || gen.TokensOut != 600 || gen.ThinkingTokens != 350 {
			t.Fatalf("unexpected generate usage entry: %+v", gen)
		}
		if gen.Cause != story.CauseInitial {
			t.Fatalf("expected generate entry's cause to round-trip as %q, got %q", story.CauseInitial, gen.Cause)
		}
		if sum == nil || sum.Calls != 1 || sum.TokensIn != 50 || sum.TokensOut != 10 {
			t.Fatalf("unexpected summary usage entry: %+v", sum)
		}

		// Resaving should replace, not double, the usage entries.
		mustSave(t, s, script)
		got2, err := s.GetScript(ctx, "s1")
		if err != nil {
			t.Fatalf("GetScript after resave: %v", err)
		}
		if len(got2.Usage) != 2 {
			t.Fatalf("expected resaving to still have exactly 2 usage entries, got %d: %+v", len(got2.Usage), got2.Usage)
		}
	})
}

// TestSaveScriptKeepsInitialAndRepairUsageAsSeparateRows exercises the
// migration that widened usage_entries' primary key to include cause
// (0005_usage_entries_cause.sql) — the same (role, provider, model) must
// persist as two separate rows when the calls behind them have different
// causes, not collide and silently sum into one.
func TestSaveScriptKeepsInitialAndRepairUsageAsSeparateRows(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		script := mkScript("s1", time.Now().UTC(), story.StatusPending, testSeed("nurse", "a", "b", "c", 3, "d"))
		script.RecordUsage("generate", "google-ai-studio", "gemini-3.5-flash-lite", 1000, 500, 300, story.CauseInitial)
		script.RecordUsage("generate", "google-ai-studio", "gemini-3.5-flash-lite", 200, 100, 50, story.CauseRepair)
		mustSave(t, s, script)

		got, err := s.GetScript(ctx, "s1")
		if err != nil {
			t.Fatalf("GetScript: %v", err)
		}
		if len(got.Usage) != 2 {
			t.Fatalf("expected 2 separate usage entries (one per cause), got %d: %+v", len(got.Usage), got.Usage)
		}

		var initial, repair *story.UsageEntry
		for i := range got.Usage {
			switch got.Usage[i].Cause {
			case story.CauseInitial:
				initial = &got.Usage[i]
			case story.CauseRepair:
				repair = &got.Usage[i]
			}
		}
		if initial == nil || initial.TokensIn != 1000 {
			t.Fatalf("unexpected initial entry: %+v", initial)
		}
		if repair == nil || repair.TokensIn != 200 {
			t.Fatalf("unexpected repair entry: %+v", repair)
		}
	})
}

func TestStatsAggregatesUsageAcrossScripts(t *testing.T) {
	withBothBackends(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		base := time.Now().UTC()

		s1 := mkScript("s1", base, story.StatusAccepted, testSeed("nurse", "a", "b", "c", 3, "d"))
		s1.RecordUsage("generate", "google-ai-studio", "gemini-3.6-flash", 1000, 500, 300, story.CauseInitial)
		mustSave(t, s, s1)

		s2 := mkScript("s2", base.Add(time.Hour), story.StatusAccepted, testSeed("mechanic", "a", "b", "c", 3, "d"))
		s2.RecordUsage("generate", "google-ai-studio", "gemini-3.6-flash", 2000, 700, 400, story.CauseInitial)
		s2.RecordUsage("review", "google-ai-studio", "gemini-3.5-flash-lite", 900, 30, 0, story.CauseInitial)
		mustSave(t, s, s2)

		stats, err := s.Stats(ctx)
		if err != nil {
			t.Fatalf("Stats: %v", err)
		}
		if len(stats.Usage) != 2 {
			t.Fatalf("expected 2 aggregated usage entries (generate, review), got %d: %+v", len(stats.Usage), stats.Usage)
		}

		var gen, rev *story.UsageEntry
		for i := range stats.Usage {
			switch stats.Usage[i].Role {
			case "generate":
				gen = &stats.Usage[i]
			case "review":
				rev = &stats.Usage[i]
			}
		}
		if gen == nil || gen.Calls != 2 || gen.TokensIn != 3000 || gen.TokensOut != 1200 || gen.ThinkingTokens != 700 {
			t.Fatalf("expected generate usage summed across both scripts, got %+v", gen)
		}
		if rev == nil || rev.Calls != 1 || rev.TokensIn != 900 {
			t.Fatalf("unexpected review usage: %+v", rev)
		}
	})
}
