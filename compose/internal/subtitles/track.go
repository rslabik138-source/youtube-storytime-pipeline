package subtitles

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
)

// TrackResult is what WriteTrack produced.
type TrackResult struct {
	// ConcatPath is the ffconcat list file — feed it to ffmpeg as
	// `-f concat -safe 0 -i <ConcatPath>` to get one video stream of the
	// full-frame caption images, timed, to overlay in a single step.
	ConcatPath string
	// CardCount is how many caption cards were actually written (lines
	// that fall within the render window).
	CardCount int
}

// WriteTrack renders every line that falls within [0, durationSeconds] as
// a full-frame caption PNG in outDir, plus one fully-transparent gap PNG,
// and writes an ffconcat list that plays them on the timeline (gap during
// pauses between captions). Returns the concat list path.
func WriteTrack(lines []Line, style CardStyle, frameW, frameH int, durationSeconds float64, outDir string) (TrackResult, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return TrackResult{}, fmt.Errorf("subtitles: create %s: %w", outDir, err)
	}

	gapName := "gap.png"
	if err := writePNG(filepath.Join(outDir, gapName), image.NewRGBA(image.Rect(0, 0, frameW, frameH))); err != nil {
		return TrackResult{}, err
	}

	var b strings.Builder
	b.WriteString("ffconcat version 1.0\n")

	// emit adds one timeline segment: show file for dur seconds. The
	// concat demuxer ignores a trailing entry's duration unless another
	// file line follows it, so callers must end with a final bare file
	// line (see the gap append after the loop).
	emit := func(file string, dur float64) {
		if dur <= 0 {
			return
		}
		fmt.Fprintf(&b, "file '%s'\nduration %.3f\n", file, dur)
	}

	cursor := 0.0
	cards := 0
	for _, l := range lines {
		start := l.Start
		if start < cursor {
			start = cursor // touching/overlapping lines: no negative gap
		}
		end := l.End
		if end > durationSeconds {
			end = durationSeconds
		}
		if start >= durationSeconds || end <= start {
			continue // entirely past the window, or nothing left after clamping
		}

		if start > cursor {
			emit(gapName, start-cursor)
		}

		img, err := RenderCardFrame(l.Text, style, frameW, frameH)
		if err != nil {
			return TrackResult{}, err
		}
		cardName := fmt.Sprintf("card_%d.png", cards)
		if err := writePNG(filepath.Join(outDir, cardName), img); err != nil {
			return TrackResult{}, err
		}
		emit(cardName, end-start)
		cards++
		cursor = end
	}

	if cursor < durationSeconds {
		emit(gapName, durationSeconds-cursor)
	}
	// The concat quirk fix: a final bare file line so the last real
	// segment's duration is honored.
	fmt.Fprintf(&b, "file '%s'\n", gapName)

	concatPath := filepath.Join(outDir, "captions.ffconcat")
	if err := os.WriteFile(concatPath, []byte(b.String()), 0o644); err != nil {
		return TrackResult{}, fmt.Errorf("subtitles: write %s: %w", concatPath, err)
	}
	return TrackResult{ConcatPath: concatPath, CardCount: cards}, nil
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("subtitles: create %s: %w", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return fmt.Errorf("subtitles: encode %s: %w", path, err)
	}
	return nil
}
