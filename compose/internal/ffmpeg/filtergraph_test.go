package ffmpeg

import (
	"strings"
	"testing"

	"github.com/placeholder/compose/internal/config"
)

func testLayout() config.Layout {
	var l config.Layout
	l.Background.Blur = "20:2"
	l.Background.Brightness = -0.18
	l.Vignette.Angle = "PI/5"
	l.Portrait.Height = 864
	l.Portrait.X = 40
	l.Waveform.Width = 1536
	l.Waveform.Height = 120
	l.Waveform.Mode = "cline"
	l.Waveform.Color = "white@0.9"
	l.Waveform.Rate = 30
	l.Waveform.Draw = "full"
	l.Waveform.Gain = 1
	l.Waveform.Scale = "lin"
	l.Waveform.BottomMargin = 60
	l.Logo.X = 40
	l.Logo.Y = 40
	l.Logo.Height = 60
	l.Music.Volume = 0.12
	l.Music.DuckThreshold = 0.03
	l.Music.DuckRatio = 8
	l.Music.DuckAttack = 20
	l.Music.DuckRelease = 400
	return l
}

func testInputs() Inputs {
	return Inputs{
		Background: "bg.mp4", Voice: "voice.wav", PortraitCutout: "cutout.png",
		Music: "music.mp3", Logo: "logo.png",
	}
}

func TestBuildFilterComplexLayerOrderAndFormulas(t *testing.T) {
	opts := BuildOptions{Width: 1920, Height: 1080, FPS: 30, Layout: testLayout(), IncludeMusic: true, IncludeSubs: true, IncludeLogo: true, CaptionTrackPath: "captions.ffconcat"}
	ins := newInputSet()
	ins.add("background", "bg.mp4", "-stream_loop", "-1")
	ins.add("voice", "voice.wav")
	ins.add("portrait", "cutout.png", "-loop", "1")
	ins.add("music", "music.mp3", "-stream_loop", "-1")
	ins.add("logo", "logo.png", "-loop", "1")
	ins.add("captions", "captions.ffconcat", "-f", "concat", "-safe", "0")

	graph, videoOut, audioOut := BuildFilterComplex(testInputs(), ins, opts)

	if videoOut != "vout" {
		t.Fatalf("expected video output pad 'vout', got %q", videoOut)
	}
	if audioOut != "audio" {
		t.Fatalf("expected audio output pad 'audio' when music is included, got %q", audioOut)
	}

	// Layer order: background -> vignette -> portrait -> waveform ->
	// captions -> logo. Each must appear, in order.
	order := []string{
		"[0:v]scale=1920:1080,boxblur=20:2,eq=brightness=-0.18[bg]",
		"vignette=PI/5[bgv]",
		"[2:v]scale=-1:864[portrait]",
		"overlay=x=40:y=H-h[withportrait]",
		"[1:a]volume=1,showwaves=s=1536x120:mode=cline:colors=white@0.9:rate=30:scale=lin:draw=full[wave]",
		"overlay=x=(W-w)/2:y=900[withwave]", // 1080-120-60=900, matches spec's y=H-180
		"[5:v]format=rgba[cap]",             // captions is input index 5 (after music)
		"[withwave][cap]overlay=x=0:y=0:eof_action=pass[withsubs]",
		"[4:v]scale=-1:60[logo]",
		"overlay=x=40:y=40[vout]",
	}
	lastIdx := -1
	for _, want := range order {
		idx := strings.Index(graph, want)
		if idx < 0 {
			t.Fatalf("expected graph to contain %q, got:\n%s", want, graph)
		}
		if idx < lastIdx {
			t.Fatalf("expected %q to appear AFTER the previous layer, got out of order:\n%s", want, graph)
		}
		lastIdx = idx
	}
}

// TestBuildFilterComplexNoBlurLeavesBackgroundSharp locks in that a blank
// or "none" blur skips boxblur entirely — the background scales straight
// into the brightness pass, playing as generated.
func TestBuildFilterComplexNoBlurLeavesBackgroundSharp(t *testing.T) {
	for _, blur := range []string{"", "none", "0"} {
		l := testLayout()
		l.Background.Blur = blur
		opts := BuildOptions{Width: 1920, Height: 1080, FPS: 30, Layout: l}
		ins := newInputSet()
		ins.add("background", "bg.mp4", "-stream_loop", "-1")
		ins.add("voice", "voice.wav")
		ins.add("portrait", "cutout.png", "-loop", "1")

		graph, _, _ := BuildFilterComplex(testInputs(), ins, opts)
		if strings.Contains(graph, "boxblur") {
			t.Fatalf("blur=%q: expected no boxblur in the graph, got:\n%s", blur, graph)
		}
		if !strings.Contains(graph, "[0:v]scale=1920:1080,eq=brightness=-0.18[bg]") {
			t.Fatalf("blur=%q: expected the background to scale straight into eq, got:\n%s", blur, graph)
		}
	}
}

func TestBuildFilterComplexAudioDuckingChain(t *testing.T) {
	opts := BuildOptions{Width: 1920, Height: 1080, FPS: 30, Layout: testLayout(), IncludeMusic: true}
	ins := newInputSet()
	ins.add("background", "bg.mp4", "-stream_loop", "-1")
	ins.add("voice", "voice.wav")
	ins.add("portrait", "cutout.png", "-loop", "1")
	ins.add("music", "music.mp3", "-stream_loop", "-1")
	ins.add("logo", "logo.png", "-loop", "1")

	graph, _, audioOut := BuildFilterComplex(testInputs(), ins, opts)
	if audioOut != "audio" {
		t.Fatalf("expected audio pad 'audio', got %q", audioOut)
	}
	for _, want := range []string{
		"[3:a]aloop=loop=-1:size=2e9,volume=0.12[m]",
		"[m][1:a]sidechaincompress=threshold=0.03:ratio=8:attack=20:release=400[ducked]",
		"[ducked][1:a]amix=inputs=2:duration=longest[audio]",
	} {
		if !strings.Contains(graph, want) {
			t.Fatalf("expected graph to contain %q, got:\n%s", want, graph)
		}
	}
}

func TestBuildFilterComplexNoMusicOmitsDuckingChain(t *testing.T) {
	opts := BuildOptions{Width: 1920, Height: 1080, FPS: 30, Layout: testLayout(), IncludeMusic: false}
	ins := newInputSet()
	ins.add("background", "bg.mp4", "-stream_loop", "-1")
	ins.add("voice", "voice.wav")
	ins.add("portrait", "cutout.png", "-loop", "1")
	ins.add("logo", "logo.png", "-loop", "1")

	graph, _, audioOut := BuildFilterComplex(testInputs(), ins, opts)
	// With no music the voice is still routed through the graph (anull) to a
	// mappable [audio] pad — mapping the raw [1:a] directly is illegal once
	// showwaves has consumed it.
	if audioOut != "audio" {
		t.Fatalf("expected the voice-passthrough [audio] pad when music is excluded, got %q", audioOut)
	}
	if !strings.Contains(graph, "[1:a]anull[audio]") {
		t.Fatalf("expected the voice passed through anull to [audio], got:\n%s", graph)
	}
	if strings.Contains(graph, "sidechaincompress") || strings.Contains(graph, "amix") {
		t.Fatalf("expected no ducking chain in the graph when music is excluded, got:\n%s", graph)
	}
}

func TestBuildFilterComplexNoSubsSkipsSubtitleFilter(t *testing.T) {
	opts := BuildOptions{Width: 1920, Height: 1080, FPS: 30, Layout: testLayout(), IncludeSubs: false, IncludeLogo: true}
	ins := newInputSet()
	ins.add("background", "bg.mp4", "-stream_loop", "-1")
	ins.add("voice", "voice.wav")
	ins.add("portrait", "cutout.png", "-loop", "1")
	ins.add("logo", "logo.png", "-loop", "1")

	graph, _, _ := BuildFilterComplex(testInputs(), ins, opts)
	if strings.Contains(graph, "[cap]") || strings.Contains(graph, "format=rgba") {
		t.Fatalf("expected no caption overlay when IncludeSubs is false, got:\n%s", graph)
	}
	// The logo overlay must still chain from the waveform stage directly.
	if !strings.Contains(graph, "[withwave][logo]overlay=") {
		t.Fatalf("expected the logo to overlay directly onto withwave when subs are skipped, got:\n%s", graph)
	}
}

// TestBuildFilterComplexNoLogoEndsAtLastLayer locks in that with no logo
// the graph never builds a [logo] pad and the final video output is the
// last real layer (captions here) — mapped directly, no placeholder.
func TestBuildFilterComplexNoLogoEndsAtLastLayer(t *testing.T) {
	opts := BuildOptions{Width: 1920, Height: 1080, FPS: 30, Layout: testLayout(), IncludeSubs: true, IncludeLogo: false, CaptionTrackPath: "captions.ffconcat"}
	ins := newInputSet()
	ins.add("background", "bg.mp4", "-stream_loop", "-1")
	ins.add("voice", "voice.wav")
	ins.add("portrait", "cutout.png", "-loop", "1")
	ins.add("captions", "captions.ffconcat", "-f", "concat", "-safe", "0")

	graph, videoOut, _ := BuildFilterComplex(testInputs(), ins, opts)
	if strings.Contains(graph, "[logo]") || strings.Contains(graph, "overlay=x=40:y=40") {
		t.Fatalf("expected no logo layer when IncludeLogo is false, got:\n%s", graph)
	}
	if videoOut != "withsubs" {
		t.Fatalf("expected the caption stage to be the final video output when there's no logo, got %q", videoOut)
	}
}
