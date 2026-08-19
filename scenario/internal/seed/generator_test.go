package seed

import (
	"context"
	"errors"
	"math/rand"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/placeholder/scenario/internal/config"
	"github.com/placeholder/scenario/internal/story"
)

// memHistory is a minimal in-memory seed.History for these tests — it
// tracks exactly what the generator's dedup and alternation rules read back.
type memHistory struct {
	professions         []string // oldest first; RecentProfessions reverses this
	triplets            map[[3]string]bool
	lastObjectContainer string
	hasObjectContainer  bool
	lastDuration        int
	hasDuration         bool
	lastReckoningPlace  string
	hasReckoningPlace   bool
	legacyArtifacts     []string // oldest first; RecentLegacyArtifacts reverses this
}

func newMemHistory() *memHistory {
	return &memHistory{triplets: map[[3]string]bool{}}
}

func (h *memHistory) RecentProfessions(ctx context.Context, n int) ([]string, error) {
	if n <= 0 || len(h.professions) == 0 {
		return nil, nil
	}
	start := len(h.professions) - n
	if start < 0 {
		start = 0
	}
	src := h.professions[start:]
	out := make([]string, len(src))
	for i, p := range src {
		out[len(src)-1-i] = p // most recent first
	}
	return out, nil
}

func (h *memHistory) TripletExists(ctx context.Context, profession, antagonist, endingType string) (bool, error) {
	return h.triplets[[3]string{profession, antagonist, endingType}], nil
}

func (h *memHistory) LastObjectContainer(ctx context.Context) (string, bool, error) {
	return h.lastObjectContainer, h.hasObjectContainer, nil
}

func (h *memHistory) LastDuration(ctx context.Context) (int, bool, error) {
	return h.lastDuration, h.hasDuration, nil
}

func (h *memHistory) LastReckoningPlace(ctx context.Context) (string, bool, error) {
	return h.lastReckoningPlace, h.hasReckoningPlace, nil
}

func (h *memHistory) RecentLegacyArtifacts(ctx context.Context, n int) ([]string, error) {
	if n <= 0 || len(h.legacyArtifacts) == 0 {
		return nil, nil
	}
	start := len(h.legacyArtifacts) - n
	if start < 0 {
		start = 0
	}
	src := h.legacyArtifacts[start:]
	out := make([]string, len(src))
	for i, a := range src {
		out[len(src)-1-i] = a // most recent first
	}
	return out, nil
}

// record simulates persisting sd as an accepted script, updating every
// piece of history the generator's rules consult.
func (h *memHistory) record(sd story.Seed) {
	h.professions = append(h.professions, sd.Profession)
	h.triplets[[3]string{sd.Profession, sd.Antagonist, sd.EndingType}] = true
	h.lastObjectContainer, h.hasObjectContainer = sd.ObjectContainer, true
	h.lastDuration, h.hasDuration = sd.Duration, true
	h.lastReckoningPlace, h.hasReckoningPlace = sd.ReckoningPlace, true
	h.legacyArtifacts = append(h.legacyArtifacts, sd.LegacyArtifact)
}

func mkWeighted(values ...string) []config.WeightedValue {
	out := make([]config.WeightedValue, len(values))
	for i, v := range values {
		out[i] = config.WeightedValue{Value: v}
	}
	return out
}

func testAxes() config.Axes {
	return config.Axes{
		Antagonist:       mkWeighted("mother_in_law", "brother_in_law"),
		WeakAlly:         mkWeighted("father", "grandmother"),
		HumiliationType:  mkWeighted("dismissive_joke", "cold_dismissal"),
		Duration:         config.DurationCategory{Short: []int{3, 7}, Long: []int{9, 11}},
		WrittenOverreach: mkWeighted("typed_demand_list", "email_chain"),
		ObjectContainer:  mkWeighted("tin_box", "oak_cabinet"),
		LegacyArtifact: mkWeighted("childs_drawing", "fathers_letters", "mothers_recipe_card",
			"grandfathers_watch", "wedding_photo", "hand_stitched_quilt"),
		ReckoningPlace: config.ReckoningPlaceCategory{
			Private: []string{"kitchen_table", "hospital_waiting_room"},
			Public:  []string{"public_anniversary", "church_hall"},
		},
		EndingType:     mkWeighted("cold_silence", "quiet_acknowledgment", "narrator_walks_out"),
		ProtagonistSex: mkWeighted("female", "male"),
	}
}

func testProfessions() config.Professions {
	return config.Professions{Professions: []config.Profession{
		{Name: "nurse", Epistemology: "document, don't panic", RecordType: "care log",
			Exposes: []string{"savings_taken", "care_fund_drained"}},
		{Name: "mechanic", Epistemology: "listen for what's wrong", RecordType: "repair log",
			Exposes: []string{"credit_stolen"}},
		{Name: "electrician", Epistemology: "trace the circuit", RecordType: "job sheet",
			Exposes: []string{"inheritance_rewritten"}},
		{Name: "florist", Epistemology: "note the frost", RecordType: "field log",
			Exposes: []string{"business_hijacked"}},
	}}
}

func testNames() config.Names {
	return config.Names{Regions: []config.RegionNames{
		{Name: "midwest", Towns: []string{"Cedar Falls"}, Generations: map[string]config.NamePool{
			"young": {FirstNames: []string{"Dana"}, LastNames: []string{"Whitfield"}},
			"old":   {FirstNames: []string{"Carol"}, LastNames: []string{"Whitfield"}},
		}},
		{Name: "south", Towns: []string{"Millbrook"}, Generations: map[string]config.NamePool{
			"young": {FirstNames: []string{"Erin"}, LastNames: []string{"Voss"}},
			"old":   {FirstNames: []string{"Russell"}, LastNames: []string{"Voss"}},
		}},
	}}
}

// TestNextNeverViolatesBetrayalPairing is the property test the brief asks
// for: 10000 draws, every one checked against professions.yaml. History is
// deliberately never recorded here — that isolates the profession/betrayal
// pairing constraint (permanent and structural) from the dedup constraints,
// which have their own tests below (triplet uniqueness alone would exhaust
// this small fixture's combo space long before 10000 iterations).
func TestNextNeverViolatesBetrayalPairing(t *testing.T) {
	axes, professions := testAxes(), testProfessions()
	gen := NewGenerator(axes, professions, testNames(), newMemHistory(), Constraints{MaxAttempts: 50})
	rng := rand.New(rand.NewSource(1))

	for i := 0; i < 10000; i++ {
		sd, err := gen.Next(context.Background(), rng)
		if err != nil {
			t.Fatalf("iteration %d: Next: %v", i, err)
		}
		prof, ok := professions.Get(sd.Profession)
		if !ok {
			t.Fatalf("iteration %d: profession %q is not in professions.yaml", i, sd.Profession)
		}
		if !slices.Contains(prof.Exposes, sd.Betrayal) {
			t.Fatalf("iteration %d: betrayal %q is not exposed by profession %q (exposes %v)",
				i, sd.Betrayal, sd.Profession, prof.Exposes)
		}
	}
}

func TestCooldownBlocksRecentProfessions(t *testing.T) {
	axes, professions := testAxes(), testProfessions()
	hist := newMemHistory()
	gen := NewGenerator(axes, professions, testNames(), hist, Constraints{ProfessionCooldown: 2, MaxAttempts: 500})
	rng := rand.New(rand.NewSource(2))

	for i := 0; i < 20; i++ {
		recentBefore, _ := hist.RecentProfessions(context.Background(), 2)

		sd, err := gen.Next(context.Background(), rng)
		if err != nil {
			t.Fatalf("iteration %d: Next: %v", i, err)
		}
		if slices.Contains(recentBefore, sd.Profession) {
			t.Fatalf("iteration %d: profession %q should have been blocked by cooldown (recent=%v)", i, sd.Profession, recentBefore)
		}
		hist.record(sd)
	}
}

func TestTripletNeverRepeats(t *testing.T) {
	axes, professions := testAxes(), testProfessions()
	hist := newMemHistory()
	gen := NewGenerator(axes, professions, testNames(), hist, Constraints{MaxAttempts: 500})
	rng := rand.New(rand.NewSource(3))

	seen := map[[3]string]bool{}
	for i := 0; i < 15; i++ {
		sd, err := gen.Next(context.Background(), rng)
		if err != nil {
			t.Fatalf("iteration %d: Next: %v", i, err)
		}
		key := [3]string{sd.Profession, sd.Antagonist, sd.EndingType}
		if seen[key] {
			t.Fatalf("iteration %d: triplet %v repeated", i, key)
		}
		seen[key] = true
		hist.record(sd)
	}
}

func TestObjectContainerNeverRepeatsConsecutively(t *testing.T) {
	axes, professions := testAxes(), testProfessions()
	hist := newMemHistory()
	gen := NewGenerator(axes, professions, testNames(), hist, Constraints{MaxAttempts: 500})
	rng := rand.New(rand.NewSource(4))

	var lastContainer string
	for i := 0; i < 15; i++ {
		sd, err := gen.Next(context.Background(), rng)
		if err != nil {
			t.Fatalf("iteration %d: Next: %v", i, err)
		}
		if i > 0 && sd.ObjectContainer == lastContainer {
			t.Fatalf("iteration %d: object_container %q repeated consecutively", i, sd.ObjectContainer)
		}
		lastContainer = sd.ObjectContainer
		hist.record(sd)
	}
}

func TestLegacyArtifactDoesNotRepeatWithinLastFour(t *testing.T) {
	axes, professions := testAxes(), testProfessions()
	hist := newMemHistory()
	gen := NewGenerator(axes, professions, testNames(), hist, Constraints{MaxAttempts: 500})
	rng := rand.New(rand.NewSource(10))

	for i := 0; i < 20; i++ {
		recentBefore, _ := hist.RecentLegacyArtifacts(context.Background(), legacyArtifactLookback)

		sd, err := gen.Next(context.Background(), rng)
		if err != nil {
			t.Fatalf("iteration %d: Next: %v", i, err)
		}
		if slices.Contains(recentBefore, sd.LegacyArtifact) {
			t.Fatalf("iteration %d: legacy_artifact %q should have been blocked (recent=%v)", i, sd.LegacyArtifact, recentBefore)
		}
		hist.record(sd)
	}
}

func TestDurationAlternatesShortAndLong(t *testing.T) {
	axes, professions := testAxes(), testProfessions()
	hist := newMemHistory()
	gen := NewGenerator(axes, professions, testNames(), hist, Constraints{MaxAttempts: 500})
	rng := rand.New(rand.NewSource(5))

	var lastWasShort *bool
	for i := 0; i < 20; i++ {
		sd, err := gen.Next(context.Background(), rng)
		if err != nil {
			t.Fatalf("iteration %d: Next: %v", i, err)
		}
		short := slices.Contains(axes.Duration.Short, sd.Duration)
		if lastWasShort != nil && *lastWasShort == short {
			t.Fatalf("iteration %d: duration bucket (short=%v) repeated two draws in a row", i, short)
		}
		lastWasShort = &short
		hist.record(sd)
	}
}

func TestReckoningPlaceAlternatesPrivateAndPublic(t *testing.T) {
	axes, professions := testAxes(), testProfessions()
	hist := newMemHistory()
	gen := NewGenerator(axes, professions, testNames(), hist, Constraints{MaxAttempts: 500})
	rng := rand.New(rand.NewSource(6))

	var lastWasPrivate *bool
	for i := 0; i < 20; i++ {
		sd, err := gen.Next(context.Background(), rng)
		if err != nil {
			t.Fatalf("iteration %d: Next: %v", i, err)
		}
		private := slices.Contains(axes.ReckoningPlace.Private, sd.ReckoningPlace)
		if lastWasPrivate != nil && *lastWasPrivate == private {
			t.Fatalf("iteration %d: reckoning_place category (private=%v) repeated two draws in a row", i, private)
		}
		lastWasPrivate = &private
		hist.record(sd)
	}
}

func TestAxesExhaustedReportsReason(t *testing.T) {
	axes := testAxes()
	axes.Antagonist = mkWeighted("mother_in_law") // narrowed to exactly one option
	axes.EndingType = mkWeighted("cold_silence")  // ditto

	professions := config.Professions{Professions: []config.Profession{
		{Name: "nurse", Epistemology: "x", RecordType: "y", Exposes: []string{"savings_taken"}},
	}}

	hist := newMemHistory()
	gen := NewGenerator(axes, professions, testNames(), hist, Constraints{MaxAttempts: 20})
	rng := rand.New(rand.NewSource(7))

	// With a single profession, single antagonist, and single ending, this
	// is the only triplet that will ever exist. First draw succeeds...
	sd, err := gen.Next(context.Background(), rng)
	if err != nil {
		t.Fatalf("first draw should succeed: %v", err)
	}
	hist.record(sd)

	// ...every draw after that must exhaust MaxAttempts on the triplet check.
	_, err = gen.Next(context.Background(), rng)
	var exhausted *ErrAxesExhausted
	if !errors.As(err, &exhausted) {
		t.Fatalf("expected *ErrAxesExhausted, got %v (%T)", err, err)
	}
	if exhausted.Attempts != 20 {
		t.Fatalf("expected Attempts=20, got %d", exhausted.Attempts)
	}
	if !strings.Contains(exhausted.Reason, "triplet") {
		t.Fatalf("expected Reason to mention the exhausted triplet constraint, got %q", exhausted.Reason)
	}
}

func TestNextWithProfessionBypassesCooldown(t *testing.T) {
	axes, professions := testAxes(), testProfessions()
	hist := newMemHistory()
	gen := NewGenerator(axes, professions, testNames(), hist, Constraints{ProfessionCooldown: 8, MaxAttempts: 50})
	rng := rand.New(rand.NewSource(8))

	for i := 0; i < 8; i++ {
		hist.record(story.Seed{Profession: "nurse", Antagonist: "mother_in_law", EndingType: "cold_silence", ObjectContainer: "tin_box"})
	}

	sd, err := gen.NextWithProfession(context.Background(), rng, "nurse")
	if err != nil {
		t.Fatalf("NextWithProfession: %v", err)
	}
	if sd.Profession != "nurse" {
		t.Fatalf("expected profession nurse, got %q", sd.Profession)
	}
}

func TestNextWithProfessionRejectsUnknownProfessionImmediately(t *testing.T) {
	axes, professions := testAxes(), testProfessions()
	gen := NewGenerator(axes, professions, testNames(), newMemHistory(), Constraints{MaxAttempts: 50})

	_, err := gen.NextWithProfession(context.Background(), rand.New(rand.NewSource(9)), "unknown_profession")
	if err == nil {
		t.Fatalf("expected an error for an unknown profession")
	}
	var exhausted *ErrAxesExhausted
	if errors.As(err, &exhausted) {
		t.Fatalf("an unknown profession should fail immediately, not via ErrAxesExhausted")
	}
}

func TestNextIsDeterministicForAFixedRNGSeed(t *testing.T) {
	axes, professions := testAxes(), testProfessions()

	run := func(seed int64) []story.Seed {
		hist := newMemHistory()
		gen := NewGenerator(axes, professions, testNames(), hist, Constraints{MaxAttempts: 50})
		rng := rand.New(rand.NewSource(seed))

		var out []story.Seed
		for i := 0; i < 5; i++ {
			sd, err := gen.Next(context.Background(), rng)
			if err != nil {
				t.Fatalf("Next: %v", err)
			}
			out = append(out, sd)
			hist.record(sd)
		}
		return out
	}

	a, b := run(42), run(42)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("expected identical seed sequences for the same rng seed, got:\n%+v\nvs\n%+v", a, b)
	}
}
