// Command voice synthesizes a scenario script's audio through a local
// Kokoro-FastAPI instance and produces a final WAV plus a timing manifest
// for the background module to consume.
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
