package render

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/chromedp"
)

// ChromedpRenderer is the real Renderer: writes html to a throwaway temp
// file, navigates a headless Chrome to it, runs the page's own
// window.fitText() (see templates/thumbnail.html) to shrink the font until
// every line fits, then screenshots the viewport.
type ChromedpRenderer struct {
	// ExecPath overrides chromedp's own Chrome/Chromium/Edge auto-detection.
	// Leave blank to let chromedp find one itself.
	ExecPath string
	// Timeout bounds one full render (navigate + fit + screenshot). <=0
	// falls back to 30s — a hung headless Chrome must not hang the CLI.
	Timeout time.Duration
	Width   int // <=0 falls back to 1280
	Height  int // <=0 falls back to 720
}

func (r ChromedpRenderer) Render(ctx context.Context, html string) ([]byte, error) {
	width, height := r.Width, r.Height
	if width <= 0 {
		width = 1280
	}
	if height <= 0 {
		height = 720
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	tmpFile, err := os.CreateTemp("", "thumbnail-*.html")
	if err != nil {
		return nil, fmt.Errorf("render: create temp html: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(html); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("render: write temp html: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return nil, fmt.Errorf("render: close temp html: %w", err)
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.WindowSize(width, height),
		chromedp.Flag("headless", true),
	)
	if r.ExecPath != "" {
		opts = append(opts, chromedp.ExecPath(r.ExecPath))
	}

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()
	taskCtx, cancelTask := chromedp.NewContext(allocCtx)
	defer cancelTask()
	taskCtx, cancelTimeout := context.WithTimeout(taskCtx, timeout)
	defer cancelTimeout()

	// file:/// + a forward-slashed path is the one file: URL form that
	// works for both Windows ("file:///C:/Users/...") and POSIX
	// ("file:////home/...") absolute paths.
	fileURL := "file:///" + filepath.ToSlash(tmpFile.Name())

	var png []byte
	err = chromedp.Run(taskCtx,
		chromedp.EmulateViewport(int64(width), int64(height)),
		chromedp.Navigate(fileURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Evaluate(`window.fitText()`, nil),
		chromedp.CaptureScreenshot(&png),
	)
	if err != nil {
		return nil, fmt.Errorf("render: chromedp run: %w", err)
	}
	return png, nil
}
