package ffmpeg

import (
	"strings"
	"testing"

	"github.com/placeholder/compose/internal/config"
)

func indexOfArg(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

func TestBuildArgsInputOrderAndFlags(t *testing.T) {
	opts := BuildOptions{Width: 1920, Height: 1080, FPS: 30, Layout: testLayout(), IncludeMusic: true, IncludeSubs: true, IncludeLogo: true, CaptionTrackPath: "captions.ffconcat", DurationSeconds: 123.456}
	encoder := Encoder{Name: "libx264", Args: []string{"-c:v", "libx264", "-preset", "faster"}}

	args := BuildArgs(testInputs(), opts, encoder, "output.mp4")
	joined := strings.Join(args, " ")

	// Background is stream_loop'd, portrait and logo are still-image
	// loop'd, music is stream_loop'd — exactly matching what an infinite
	// image/loop source needs to overlay onto an ongoing video.
	if !strings.Contains(joined, "-stream_loop -1 -i bg.mp4") {
		t.Fatalf("expected background to use -stream_loop -1, got: %s", joined)
	}
	if !strings.Contains(joined, "-loop 1 -i cutout.png") {
		t.Fatalf("expected the portrait cutout to use -loop 1, got: %s", joined)
	}
	if !strings.Contains(joined, "-loop 1 -i logo.png") {
		t.Fatalf("expected the logo to use -loop 1, got: %s", joined)
	}
	if !strings.Contains(joined, "-stream_loop -1 -i music.mp3") {
		t.Fatalf("expected music to use -stream_loop -1, got: %s", joined)
	}
	if !strings.Contains(joined, "-i voice.wav") {
		t.Fatalf("expected the voice input, got: %s", joined)
	}
	if !strings.Contains(joined, "-f concat -safe 0 -i captions.ffconcat") {
		t.Fatalf("expected the caption track added as a concat-demuxer input, got: %s", joined)
	}

	if idx := indexOfArg(args, "-t"); idx < 0 || args[idx+1] != "123.456" {
		t.Fatalf("expected -t 123.456, got args: %v", args)
	}
	if idx := indexOfArg(args, "-r"); idx < 0 || args[idx+1] != "30" {
		t.Fatalf("expected -r 30, got args: %v", args)
	}
	if !strings.Contains(joined, "-c:v libx264 -preset faster") {
		t.Fatalf("expected the encoder's own args to appear verbatim, got: %s", joined)
	}
	if !strings.Contains(joined, "-pix_fmt yuv420p") {
		t.Fatalf("expected -pix_fmt yuv420p, got: %s", joined)
	}
	if args[len(args)-1] != "output.mp4" {
		t.Fatalf("expected the output path last, got: %v", args)
	}
}

func TestBuildArgsMapsAudioFromDuckedGraphWhenMusicIncluded(t *testing.T) {
	opts := BuildOptions{Width: 1920, Height: 1080, FPS: 30, Layout: testLayout(), IncludeMusic: true}
	encoder := Encoder{Name: "libx264", Args: []string{"-c:v", "libx264"}}
	args := BuildArgs(testInputs(), opts, encoder, "out.mp4")
	if !argsContainSequence(args, "-map", "[audio]") {
		t.Fatalf("expected -map [audio] when music is included, got: %v", args)
	}
}

func TestBuildArgsMapsVoicePassthroughWhenMusicExcluded(t *testing.T) {
	opts := BuildOptions{Width: 1920, Height: 1080, FPS: 30, Layout: testLayout(), IncludeMusic: false}
	encoder := Encoder{Name: "libx264", Args: []string{"-c:v", "libx264"}}
	args := BuildArgs(testInputs(), opts, encoder, "out.mp4")
	// Without music the voice is routed through the graph (anull) to an
	// [audio] pad and mapped from there — mapping the raw [1:a] directly is
	// illegal once showwaves has consumed that input.
	if !argsContainSequence(args, "-map", "[audio]") {
		t.Fatalf("expected -map [audio] (voice passthrough pad) when music is excluded, got: %v", args)
	}
}

func argsContainSequence(args []string, seq ...string) bool {
	for i := 0; i+len(seq) <= len(args); i++ {
		match := true
		for j, s := range seq {
			if args[i+j] != s {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func TestNVENCAndLibX264EncodersUseSettings(t *testing.T) {
	s := config.Settings{NVENCPreset: "p5", NVENCBitrate: "8M", LibX264Preset: "veryfast"}
	nv := NVENCEncoder(s)
	if nv.Name != "h264_nvenc" || !strings.Contains(strings.Join(nv.Args, " "), "p5") || !strings.Contains(strings.Join(nv.Args, " "), "8M") {
		t.Fatalf("unexpected NVENC encoder: %+v", nv)
	}
	x264 := LibX264Encoder(s)
	if x264.Name != "libx264" || !strings.Contains(strings.Join(x264.Args, " "), "veryfast") {
		t.Fatalf("unexpected libx264 encoder: %+v", x264)
	}
}
