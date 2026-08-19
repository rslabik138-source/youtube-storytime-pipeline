package render

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"testing"
)

func TestFakeRendererReturnsDecodablePNG(t *testing.T) {
	f := &FakeRenderer{}
	data, err := f.Render(context.Background(), "<html></html>")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("expected a decodable PNG, got error: %v", err)
	}
	if img.Bounds().Dx() == 0 || img.Bounds().Dy() == 0 {
		t.Fatalf("expected a non-empty image, got bounds %v", img.Bounds())
	}
}

func TestFakeRendererRecordsHTML(t *testing.T) {
	f := &FakeRenderer{}
	if _, err := f.Render(context.Background(), "<html>one</html>"); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if _, err := f.Render(context.Background(), "<html>two</html>"); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(f.HTMLReceived) != 2 || f.HTMLReceived[0] != "<html>one</html>" || f.HTMLReceived[1] != "<html>two</html>" {
		t.Fatalf("unexpected HTMLReceived: %v", f.HTMLReceived)
	}
}

func TestFakeRendererReturnsConfiguredError(t *testing.T) {
	f := &FakeRenderer{Err: errors.New("boom")}
	if _, err := f.Render(context.Background(), "<html></html>"); err == nil {
		t.Fatalf("expected the configured error")
	}
}

func TestFakeRendererRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &FakeRenderer{}
	if _, err := f.Render(ctx, "<html></html>"); err == nil {
		t.Fatalf("expected an error for a canceled context")
	}
}
