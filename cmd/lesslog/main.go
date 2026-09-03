// Command lesslog views JSON logs like less, colored by log level.
package main

import (
	"fmt"
	"os"

	"github.com/imunhatep/lesslog/internal/cli"
)

// version is overridden at build time: -ldflags "-X main.version=…" (see Makefile).
var version = "0.1.0"

func main() {
	if err := cli.Run(version, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "lesslog:", err)
		os.Exit(1)
	}
}
