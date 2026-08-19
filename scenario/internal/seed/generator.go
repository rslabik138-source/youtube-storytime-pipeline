// Package seed draws story.Seed values that respect the hard
// profession -> betrayal pairing from professions.yaml, deduplicate against
// story history, and force alternation on the two axes that most change a
// script's feel (duration, reckoning_place).
package seed

import (
	"context"
	"fmt"
	"math/rand"
	"slices"

	"github.com/placeholder/scenario/internal/config"
	"github.com/placeholder/scenario/internal/story"
)

// History is the read-only slice of storage seed dedup needs. store.Store
// satisfies this structurally, so the real database plugs in with no
// adapter. Content-level dedup (character names, places, aphorisms) is
// checked later, at bible-generation time — Seed itself doesn't know about
// characters yet — so it isn't part of this interface.
type History interface {
	RecentProfessions(ctx context.Context, n int) ([]string, error)
	TripletExists(ctx context.Context, profession, antagonist, endingType string) (bool, error)
	LastObjectContainer(ctx context.Context) (string, bool, error)
	LastDuration(ctx context.Context) (int, bool, error)
	LastReckoningPlace(ctx context.Context) (string, bool, error)
	RecentLegacyArtifacts(ctx context.Context, n int) ([]string, error)
}

// legacyArtifactLookback bounds how many of the most recent accepted
// scripts' legacy_artifact values are off-limits for a new draw — a real
// run drew the same value (a child's wax crayon drawing) twice in a row,
// so this forces alternation across a short window instead of just
// forbidding an immediate repeat.
const legacyArtifactLookback = 4

type Constraints struct {
	ProfessionCooldown int // 0 disables the cooldown check entirely
	MaxAttempts        int // <= 0 defaults to 50
}

type Generator struct {
	axes        config.Axes
	professions config.Professions
	names       config.Names
	hist        History
	cons        Constraints
}

func NewGenerator(axes config.Axes, professions config.Professions, names config.Names, hist History, cons Constraints) *Generator {
	if cons.MaxAttempts <= 0 {
		cons.MaxAttempts = 50
	}
	return &Generator{axes: axes, professions: professions, names: names, hist: hist, cons: cons}
}

// Next draws a fully random seed satisfying every constraint.
func (g *Generator) Next(ctx context.Context, rng *rand.Rand) (story.Seed, error) {
	return g.draw(ctx, rng, "")
}

// NextWithProfession fixes the profession (`gen generate --profession`) and
// bypasses its cooldown check — an explicit request overrides automatic
// pacing. Every other constraint still applies.
func (g *Generator) NextWithProfession(ctx context.Context, rng *rand.Rand, profession string) (story.Seed, error) {
	if _, ok := g.professions.Get(profession); !ok {
		return story.Seed{}, fmt.Errorf("seed: unknown profession %q (not present in professions.yaml)", profession)
	}
	return g.draw(ctx, rng, profession)
}

type failReason int

const (
	reasonCooldown failReason = iota
	reasonTriplet
	reasonObjectContainerRepeat
	reasonLegacyArtifactRepeat
)

func (g *Generator) draw(ctx context.Context, rng *rand.Rand, forceProfession string) (story.Seed, error) {
	names := g.professions.Names()
	counts := map[failReason]int{}

	for attempt := 1; attempt <= g.cons.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return story.Seed{}, err
		}

		profession := forceProfession
		if profession == "" {
			profession = names[rng.Intn(len(names))]

			recent, err := g.hist.RecentProfessions(ctx, g.cons.ProfessionCooldown)
			if err != nil {
				return story.Seed{}, fmt.Errorf("seed: recent professions: %w", err)
			}
			if slices.Contains(recent, profession) {
				counts[reasonCooldown]++
				continue
			}
		}

		prof, _ := g.professions.Get(profession) // valid at this point either way
		betrayal := pickWeighted(rng, toWeighted(prof.Exposes))
		antagonist := pickWeighted(rng, g.axes.Antagonist)
		weakAlly := pickWeighted(rng, g.axes.WeakAlly)
		humiliationType := pickWeighted(rng, g.axes.HumiliationType)
		writtenOverreach := pickWeighted(rng, g.axes.WrittenOverreach)
		objectContainer := pickWeighted(rng, g.axes.ObjectContainer)
		legacyArtifact := pickWeighted(rng, g.axes.LegacyArtifact)
		endingType := pickWeighted(rng, g.axes.EndingType)
		protagonistSex := pickWeighted(rng, g.axes.ProtagonistSex)
		region := pickUniform(rng, g.names.RegionNames())

		duration, err := g.pickDuration(ctx, rng)
		if err != nil {
			return story.Seed{}, err
		}
		reckoningPlace, err := g.pickReckoningPlace(ctx, rng)
		if err != nil {
			return story.Seed{}, err
		}

		dup, err := g.hist.TripletExists(ctx, profession, antagonist, endingType)
		if err != nil {
			return story.Seed{}, fmt.Errorf("seed: triplet check: %w", err)
		}
		if dup {
			counts[reasonTriplet]++
			continue
		}

		lastContainer, ok, err := g.hist.LastObjectContainer(ctx)
		if err != nil {
			return story.Seed{}, fmt.Errorf("seed: last object container: %w", err)
		}
		if ok && lastContainer == objectContainer {
			counts[reasonObjectContainerRepeat]++
			continue
		}

		recentArtifacts, err := g.hist.RecentLegacyArtifacts(ctx, legacyArtifactLookback)
		if err != nil {
			return story.Seed{}, fmt.Errorf("seed: recent legacy artifacts: %w", err)
		}
		if slices.Contains(recentArtifacts, legacyArtifact) {
			counts[reasonLegacyArtifactRepeat]++
			continue
		}

		return story.Seed{
			Profession:       profession,
			Epistemology:     prof.Epistemology,
			RecordType:       prof.RecordType,
			Attire:           prof.Attire,
			ProtagonistSex:   protagonistSex,
			Antagonist:       antagonist,
			WeakAlly:         weakAlly,
			HumiliationType:  humiliationType,
			Betrayal:         betrayal,
			Duration:         duration,
			WrittenOverreach: writtenOverreach,
			ObjectContainer:  objectContainer,
			LegacyArtifact:   legacyArtifact,
			ReckoningPlace:   reckoningPlace,
			EndingType:       endingType,
			Region:           region,
		}, nil
	}

	return story.Seed{}, &ErrAxesExhausted{
		Attempts: g.cons.MaxAttempts,
		Reason:   describeMostCommon(counts, g.cons.ProfessionCooldown),
	}
}

// pickDuration forces alternation between the short and long buckets based
// on the previous accepted script — never a coin flip once there's history.
func (g *Generator) pickDuration(ctx context.Context, rng *rand.Rand) (int, error) {
	last, ok, err := g.hist.LastDuration(ctx)
	if err != nil {
		return 0, fmt.Errorf("seed: last duration: %w", err)
	}

	bucket := g.axes.Duration.Short
	switch {
	case !ok:
		if rng.Intn(2) == 1 {
			bucket = g.axes.Duration.Long
		}
	case slices.Contains(g.axes.Duration.Short, last):
		bucket = g.axes.Duration.Long
	default:
		bucket = g.axes.Duration.Short
	}
	return bucket[rng.Intn(len(bucket))], nil
}

// pickReckoningPlace forces alternation between private and public venues,
// same rule as pickDuration.
func (g *Generator) pickReckoningPlace(ctx context.Context, rng *rand.Rand) (string, error) {
	last, ok, err := g.hist.LastReckoningPlace(ctx)
	if err != nil {
		return "", fmt.Errorf("seed: last reckoning place: %w", err)
	}

	bucket := g.axes.ReckoningPlace.Private
	switch {
	case !ok:
		if rng.Intn(2) == 1 {
			bucket = g.axes.ReckoningPlace.Public
		}
	case slices.Contains(g.axes.ReckoningPlace.Private, last):
		bucket = g.axes.ReckoningPlace.Public
	default:
		bucket = g.axes.ReckoningPlace.Private
	}
	return bucket[rng.Intn(len(bucket))], nil
}

func describeMostCommon(counts map[failReason]int, cooldown int) string {
	best, bestN := failReason(-1), -1
	for _, r := range []failReason{reasonCooldown, reasonTriplet, reasonObjectContainerRepeat, reasonLegacyArtifactRepeat} {
		if counts[r] > bestN {
			best, bestN = r, counts[r]
		}
	}

	switch best {
	case reasonCooldown:
		return fmt.Sprintf(
			"every profession falls under cooldown (last %d scripts) — add professions to professions.yaml or lower profession_cooldown in settings.yaml",
			cooldown)
	case reasonTriplet:
		return "every available (profession, antagonist, ending_type) triplet is already used — add antagonists or ending types to axes.yaml"
	case reasonObjectContainerRepeat:
		return "the only available object_container matches the previous script's — add more object_container values to axes.yaml"
	case reasonLegacyArtifactRepeat:
		return fmt.Sprintf(
			"every available legacy_artifact was used in one of the last %d accepted scripts — add more legacy_artifact values to axes.yaml",
			legacyArtifactLookback)
	default:
		return "could not draw a seed for an unknown reason"
	}
}

func toWeighted(values []string) []config.WeightedValue {
	out := make([]config.WeightedValue, len(values))
	for i, v := range values {
		out[i] = config.WeightedValue{Value: v}
	}
	return out
}

// pickWeighted does a standard weighted-random draw; a zero or negative
// weight is treated as 1 (config.Validate already rejects negatives, this
// is just a defensive floor).
func pickWeighted(rng *rand.Rand, values []config.WeightedValue) string {
	total := 0.0
	for _, v := range values {
		total += effectiveWeight(v)
	}

	r := rng.Float64() * total
	for _, v := range values {
		w := effectiveWeight(v)
		if r < w {
			return v.Value
		}
		r -= w
	}
	return values[len(values)-1].Value // floating-point rounding fallback
}

func effectiveWeight(v config.WeightedValue) float64 {
	if v.Weight <= 0 {
		return 1
	}
	return v.Weight
}

func pickUniform(rng *rand.Rand, values []string) string {
	return values[rng.Intn(len(values))]
}
