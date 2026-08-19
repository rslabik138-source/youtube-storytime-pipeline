// Command avatar generates a single portrait image for a scenario
// script's narrator, via Gemini (primary) or Imagen 4 Fast (fallback).
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
