// Command psdextract converts a Proxyshop-style card template PSD into the
// Manifest JSON and PNG layer assets the engine's template package consumes.
// It is an internal, single-user dev tool: run it once per template, by hand,
// against a local PSD, then it has no further reason to run.
//
// Usage:
//
//	psdextract -template normal            extract using the normal recipe
//	psdextract -template normal -psd x.psd override the recipe's source file
//	psdextract -template normal -inspect   print the PSD layer tree, extract nothing
//
// The -psd path is resolved against -assets when relative. Output lands in
// <assets>/<template>/.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/odevine/mimic-templates/tools/psdextract/extract"
	"github.com/odevine/mimic-templates/tools/psdextract/recipes"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "psdextract:", err)
		os.Exit(1)
	}
}

func run() error {
	name := flag.String("template", "", "template recipe name to extract (e.g. normal)")
	psdPath := flag.String("psd", "", "override the recipe's source PSD path")
	assets := flag.String("assets", "assets", "output root; extraction writes <assets>/<template>/")
	inspect := flag.Bool("inspect", false, "print the PSD layer tree and exit without extracting")
	manifestOnly := flag.Bool("manifest-only", false, "regenerate manifest.json only, reusing existing PNGs (fast, skips pixel decode)")
	flag.Parse()

	if *name == "" {
		return fmt.Errorf("-template is required; known: %v", recipes.Names())
	}
	r, ok := recipes.Get(*name)
	if !ok {
		return fmt.Errorf("unknown template %q; known: %v", *name, recipes.Names())
	}

	src := *psdPath
	if src == "" {
		src = filepath.Join(*assets, r.SourceFile)
	}

	if *inspect {
		return extract.Inspect(src, os.Stdout)
	}

	sum, err := extract.Extract(src, r, *assets, !*manifestOnly)
	if err != nil {
		return err
	}
	printSummary(sum, !*manifestOnly)
	return nil
}

// printSummary reports what the run produced, including any declared color
// variants the PSD had no layer for, so a subtly wrong run still gives feedback
func printSummary(s *extract.Summary, wroteAssets bool) {
	verb, noun := "extracted", "PNGs"
	if !wroteAssets {
		verb, noun = "rebuilt manifest for", "variants"
	}
	fmt.Printf("%s %q: %dx%d canvas, %d %s, %d text boxes\n",
		verb, s.Template, s.CanvasWidth, s.CanvasHeight, s.PNGsWritten, noun, s.TextBoxes)
	if len(s.MissingVariants) > 0 {
		fmt.Printf("warning: %d declared variant(s) had no matching PSD layer:\n", len(s.MissingVariants))
		for _, v := range s.MissingVariants {
			fmt.Printf("  - %s\n", v)
		}
	}
}
