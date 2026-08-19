package ffmpeg

import (
	"fmt"

	"github.com/placeholder/compose/internal/config"
)

// Encoder is one video-encoding choice — NVENC (hardware) or libx264
// (software fallback). See SelectEncoder/RunWithFallback.
type Encoder struct {
	Name string   // "h264_nvenc" or "libx264"
	Args []string // -c:v <name> plus whatever preset/bitrate flags it needs
}

// NVENCEncoder builds the hardware-encoded option from settings.
func NVENCEncoder(s config.Settings) Encoder {
	return Encoder{Name: "h264_nvenc", Args: []string{"-c:v", "h264_nvenc", "-preset", s.NVENCPreset, "-b:v", s.NVENCBitrate}}
}

// LibX264Encoder builds the software fallback from settings.
func LibX264Encoder(s config.Settings) Encoder {
	return Encoder{Name: "libx264", Args: []string{"-c:v", "libx264", "-preset", s.LibX264Preset}}
}

// BuildArgs assembles the complete ffmpeg CLI argument list: inputs
// (each looped/still-imaged as appropriate), the filter graph, stream
// mapping, duration cap, encoder, and output path. Pure string
// construction — no process is started here.
func BuildArgs(in Inputs, opts BuildOptions, encoder Encoder, outputPath string) []string {
	ins := newInputSet()
	ins.add("background", in.Background, "-stream_loop", "-1")
	ins.add("voice", in.Voice)
	ins.add("portrait", in.PortraitCutout, "-loop", "1")
	if opts.IncludeMusic {
		ins.add("music", in.Music, "-stream_loop", "-1")
	}
	if opts.IncludeLogo {
		ins.add("logo", in.Logo, "-loop", "1")
	}
	if opts.IncludeSubs {
		// The caption track is an ffconcat list — the concat demuxer plays
		// its full-frame PNGs on the timeline; BuildFilterComplex overlays
		// the result. Its own timing (durations in the list) is what places
		// each card, so no -stream_loop/-loop here.
		ins.add("captions", opts.CaptionTrackPath, "-f", "concat", "-safe", "0")
	}

	graph, videoOut, audioOut := BuildFilterComplex(in, ins, opts)

	args := []string{"-y"}
	args = append(args, ins.args...)
	args = append(args, "-filter_complex", graph)
	args = append(args, "-map", fmt.Sprintf("[%s]", videoOut))
	if audioOut != "" {
		args = append(args, "-map", fmt.Sprintf("[%s]", audioOut))
	} else {
		args = append(args, "-map", ins.a("voice"))
	}
	if opts.DurationSeconds > 0 {
		args = append(args, "-t", fmt.Sprintf("%.3f", opts.DurationSeconds))
	}
	args = append(args, "-r", fmt.Sprintf("%d", opts.FPS))
	args = append(args, encoder.Args...)
	args = append(args, "-c:a", "aac", "-b:a", "192k")
	args = append(args, "-pix_fmt", "yuv420p")
	args = append(args, outputPath)
	return args
}
