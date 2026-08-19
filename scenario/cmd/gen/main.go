// Command gen is the CLI for the "underestimated" long-form script
// generator: gen generate/list/show/regenerate/export/stats.
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
