package main

import (
	"fmt"
	"os"
)

// version is set at build time via ldflags.
var version = "dev"

func main() {
	fmt.Fprintf(os.Stderr, "heygen-cli %s\n", version)
	os.Exit(0)
}
