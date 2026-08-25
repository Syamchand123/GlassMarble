// Command man generates roff man pages for gmb and its subcommands.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/cmd"
	"github.com/Syamchand123/GlassMarble/internal/product"
	"github.com/spf13/cobra/doc"
)

func main() {
	outDir := flag.String("o", "man/man1", "Output directory for man pages")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating directory %s: %v\n", *outDir, err)
		os.Exit(1)
	}

	header := &doc.GenManHeader{
		Title:   "GMB",
		Section: "1",
		Source:  "GlassMarble " + product.Version,
		Manual:  "GlassMarble Manual",
	}

	root := cmd.RootCmd()
	if err := doc.GenManTree(root, header, *outDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating man pages: %v\n", err)
		os.Exit(1)
	}

	files, _ := filepath.Glob(filepath.Join(*outDir, "*.1"))
	fmt.Printf("Generated %d man page(s) into %s\n", len(files), *outDir)
}
