// Command thumb generates a 1280x720 YouTube thumbnail for a scenario
// script: LLM-written text (colored lines + a red-plated cliffhanger)
// composited over one of the channel's standing portraits via HTML/CSS and
// a headless-Chrome screenshot — never an image-generation model, which
// reliably mangles rendered text.
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
