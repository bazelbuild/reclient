package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/bazelbuild/reclient/internal/pkg/version"
)

var (
	outFile = flag.String("out", "", "")
	suffix  = flag.String("suffix", "", "")
)

func main() {
	flag.Parse()
	if err := os.WriteFile(*outFile, []byte(version.CurrentVersion()+*suffix), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Unable to write %s: %v", *outFile, err)
		os.Exit(1)
	}
}
