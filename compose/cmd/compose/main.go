// Command compose assembles the final 1920x1080 video for a script:
// blurred/darkened background, background-removed portrait, an audio-
// reactive waveform, burned-in subtitles, ducked background music, and a
// logo — from background/voiceover/avatar's own outputs, via ffmpeg.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
