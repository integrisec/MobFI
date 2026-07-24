package main

import (
	_ "embed"
	"fmt"
	"io"
)

// asciiLogo is the CLI banner. It mirrors the repo-root brand asset
// mobfi-logo-ascii.txt; go:embed cannot reach the repo root, so this copy in
// the package directory is embedded into the binary.
//
//go:embed mobfi-logo-ascii.txt
var asciiLogo string

// printLogo writes the ASCII banner followed by a blank line.
func printLogo(w io.Writer) {
	fmt.Fprintf(w, "%s\n\n", asciiLogo)
}
