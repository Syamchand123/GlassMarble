// Command completions pre-generates shell completions for gmb.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/cmd"
)

func main() {
	outDir := flag.String("o", "completions", "Output directory for completions")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating directory %s: %v\n", *outDir, err)
		os.Exit(1)
	}

	root := cmd.RootCmd()

	// Bash
	bashFile, err := os.Create(filepath.Join(*outDir, "gmb.bash"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating bash completion: %v\n", err)
		os.Exit(1)
	}
	defer bashFile.Close()
	if err := root.GenBashCompletionV2(bashFile, true); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating bash completion: %v\n", err)
		os.Exit(1)
	}

	// Zsh
	zshFile, err := os.Create(filepath.Join(*outDir, "gmb.zsh"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating zsh completion: %v\n", err)
		os.Exit(1)
	}
	defer zshFile.Close()
	if err := root.GenZshCompletion(zshFile); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating zsh completion: %v\n", err)
		os.Exit(1)
	}

	// Fish
	fishFile, err := os.Create(filepath.Join(*outDir, "gmb.fish"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating fish completion: %v\n", err)
		os.Exit(1)
	}
	defer fishFile.Close()
	if err := root.GenFishCompletion(fishFile, true); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating fish completion: %v\n", err)
		os.Exit(1)
	}

	// PowerShell
	psFile, err := os.Create(filepath.Join(*outDir, "gmb.ps1"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating powershell completion: %v\n", err)
		os.Exit(1)
	}
	defer psFile.Close()
	if err := root.GenPowerShellCompletionWithDesc(psFile); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating powershell completion: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated shell completions (bash, zsh, fish, ps1) into %s\n", *outDir)
}
