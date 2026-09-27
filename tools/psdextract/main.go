// Command psdextract converts a Proxyshop-style card template PSD into the
// Manifest JSON and PNG layer assets the engine's template package consumes.
// It is a bootstrap tool: run it once when adding a template to seed the
// manifest and layer PNGs, then the manifest becomes the tracked source of
// truth that a contributor hand-tunes. The PSD bounds are only a starting
// point, so routine releases repack a bundle from the tracked manifest and
// never re-run this tool. It runs again only to re-cut the art from a changed
// PSD, where -png-only leaves the tuned manifest alone.
//
// Usage:
//
//	psdextract -template normal              seed manifest.json and layer PNGs
//	psdextract -template normal -psd x.psd   override the recipe's source PSD
//	psdextract -template normal -png-only    rewrite PNGs, keep the tuned manifest
//	psdextract -template normal -inspect     print the PSD layer tree, extract nothing
//
// -psd is used as given. Without it the source is <psd-dir>/<recipe source
// file>, and a recipe that reads several sources finds each in <psd-dir>, where
// -psd does not apply. -psd-dir defaults to <assets>. Output lands in
// <assets>/<template>/
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

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
	psdDir := flag.String("psd-dir", "", "directory holding the recipe's source PSDs; empty means the -assets root")
	inspect := flag.Bool("inspect", false, "print the PSD layer tree and exit without extracting")
	manifestOnly := flag.Bool("manifest-only", false, "regenerate manifest.json only, reusing existing PNGs (fast, skips pixel decode)")
	pngOnly := flag.Bool("png-only", false, "regenerate layer PNGs only, leaving a hand-tuned manifest.json untouched (for an art re-cut)")
	flag.Parse()

	if *manifestOnly && *pngOnly {
		return fmt.Errorf("-manifest-only and -png-only are mutually exclusive")
	}
	if *name == "" {
		return fmt.Errorf("-template is required; known: %v", recipes.Names())
	}
	r, ok := recipes.Get(*name)
	if !ok {
		return fmt.Errorf("unknown template %q; known: %v", *name, recipes.Names())
	}

	dir := *psdDir
	if dir == "" {
		dir = *assets
	}
	paths := map[string]string{}
	switch {
	case len(r.Sources) > 0:
		if *psdPath != "" && !*inspect {
			return fmt.Errorf("%s reads several PSDs from -psd-dir, so -psd does not apply", r.Template)
		}
		for name, file := range r.Sources {
			paths[name] = filepath.Join(dir, file)
		}
	case *psdPath != "":
		paths[""] = *psdPath
	default:
		paths[""] = filepath.Join(dir, r.SourceFile)
	}

	if *inspect {
		if *psdPath != "" {
			return extract.Inspect(*psdPath, os.Stdout)
		}
		for _, name := range sortedNames(paths) {
			if name != "" {
				fmt.Printf("== source %s: %s\n", name, paths[name])
			}
			if err := extract.Inspect(paths[name], os.Stdout); err != nil {
				return err
			}
		}
		return nil
	}

	writeAssets := !*manifestOnly
	writeManifestFile := !*pngOnly
	sum, err := extract.Extract(paths, r, *assets, writeAssets, writeManifestFile)
	if err != nil {
		return err
	}
	printSummary(sum, writeAssets)
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

func sortedNames(m map[string]string) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
