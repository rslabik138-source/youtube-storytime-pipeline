package render

import "context"

// Renderer turns finished HTML (from BuildHTML) into a screenshot PNG.
// The real implementation (ChromedpRenderer) is the only one that touches
// an actual browser; tests use FakeRenderer instead.
type Renderer interface {
	Render(ctx context.Context, html string) (png []byte, err error)
}
