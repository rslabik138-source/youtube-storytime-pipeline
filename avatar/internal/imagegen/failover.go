package imagegen

import (
	"context"
	"errors"
	"fmt"
)

// Failover tries each Generator in order, returning the first success —
// per the brief: Gemini primary, Imagen 4 Fast fallback. All failing
// returns a combined error naming every provider that was tried, not just
// the last one, so a caller can tell whether it's a primary-only blip or
// everything is down.
type Failover struct {
	generators []Generator
}

// NewFailover builds a Failover trying each generator in the given order.
func NewFailover(generators ...Generator) *Failover {
	return &Failover{generators: generators}
}

func (f *Failover) Generate(ctx context.Context, prompt string, opts Options) (Image, error) {
	if len(f.generators) == 0 {
		return Image{}, fmt.Errorf("imagegen: failover: no generators configured")
	}
	var errs []error
	for _, g := range f.generators {
		if err := ctx.Err(); err != nil {
			return Image{}, err
		}
		img, err := g.Generate(ctx, prompt, opts)
		if err == nil {
			return img, nil
		}
		errs = append(errs, err)
	}
	return Image{}, fmt.Errorf("imagegen: all providers failed: %w", errors.Join(errs...))
}
