// Command pack wraps a loose extracted template directory into a single
// versioned .mimic bundle. A .mimic file is a zip archive: bundle.json and
// manifest.json at the root, then one directory of layer PNGs per layer group,
// mirroring the on-disk layout the extractor writes and the engine reads.
//
// Usage:
//
//	pack <dir> -version 1.2.0                 write <dir>/../<template>.mimic
//	pack <dir> -version 1.2.0 -o out.mimic    write to an explicit path
//	pack <dir> -version 1.2.0 -min-engine 0.3.0
//
// pack imports the engine's template package to validate the manifest the same
// way the running engine will, so a bundle that packs cannot fail to load for a
// reason pack could have caught. minEngine defaults to the engine version this
// repo's go.mod pins, so a bundle never claims to need an engine newer or older
// than the one it was built against unless -min-engine says so.
package main

import (
	"archive/zip"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odevine/mimic/engine/template"
)

// formatVersion is the container schema version written into every bundle. It
// is bumped only when the bundle structure itself changes incompatibly, not
// when a template's contents change
const formatVersion = 1

// Bundle is the bundle.json header, read first by the engine before it parses
// the larger manifest so it can reject an incompatible container cheaply. Its
// JSON tags are the on-disk schema
type Bundle struct {
	Format    int    `json:"format"`
	Template  string `json:"template"`
	Version   string `json:"version"`
	MinEngine string `json:"minEngine"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "pack:", err)
		os.Exit(1)
	}
}

func run() error {
	// The source directory may come before the flags ("pack dir -version x") or
	// after them, so it is pulled out before flag parsing rather than left to
	// the flag package, which stops at the first non-flag argument
	args := os.Args[1:]
	var dir string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		dir, args = args[0], args[1:]
	}

	fs := flag.NewFlagSet("pack", flag.ExitOnError)
	version := fs.String("version", "", "template semver to stamp into bundle.json (required)")
	minEngine := fs.String("min-engine", "", "minimum engine version; empty derives it from go.mod")
	out := fs.String("o", "", "output .mimic path; empty writes <template>.mimic beside the source directory")
	goMod := fs.String("go-mod", "go.mod", "go.mod to read the engine version from when -min-engine is empty")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if dir == "" && fs.NArg() == 1 {
		dir = fs.Arg(0)
	}
	if dir == "" {
		return fmt.Errorf("expected one source directory argument")
	}
	if *version == "" {
		return fmt.Errorf("-version is required")
	}

	// Validate through the engine's own reader so a bundle that packs is one
	// the engine will accept, and read the template name from the manifest so
	// the bundle header and its filename cannot disagree with its contents
	m, err := template.NewFSAssetProvider(dir).Manifest()
	if err != nil {
		return fmt.Errorf("validating manifest in %q: %w", dir, err)
	}
	if m.Template == "" {
		return fmt.Errorf("manifest in %q has no template name", dir)
	}

	minEng := *minEngine
	if minEng == "" {
		minEng, err = engineVersion(*goMod)
		if err != nil {
			return fmt.Errorf("deriving minEngine (pass -min-engine to override): %w", err)
		}
	}

	bundle := Bundle{
		Format:    formatVersion,
		Template:  m.Template,
		Version:   strings.TrimPrefix(*version, "v"),
		MinEngine: strings.TrimPrefix(minEng, "v"),
	}

	outPath := *out
	if outPath == "" {
		outPath = filepath.Join(filepath.Dir(filepath.Clean(dir)), m.Template+".mimic")
	}

	n, err := writeBundle(outPath, dir, bundle)
	if err != nil {
		return err
	}
	fmt.Printf("packed %q v%s (minEngine %s): %d entries -> %s\n",
		bundle.Template, bundle.Version, bundle.MinEngine, n, outPath)
	return nil
}

// writeBundle writes the .mimic zip at outPath: bundle.json first, then every
// file under dir except a stray bundle.json and dotfiles. PNGs are stored
// uncompressed because their pixel data is already DEFLATE-compressed inside
// the PNG, so zipping them again costs CPU on every read and saves nothing and
// keeps each entry cheaply seekable. JSON is deflated. Returns the entry count
func writeBundle(outPath, dir string, b Bundle) (int, error) {
	f, err := os.Create(outPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	entries := 0

	header, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := writeEntry(zw, "bundle.json", zip.Deflate, header); err != nil {
		return 0, err
	}
	entries++

	root := filepath.Clean(dir)
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		// bundle.json is written from the header above; a tracked one in the
		// source directory is stale by construction. Dotfiles are editor and
		// OS cruft that has no place in a shipped bundle
		if name == "bundle.json" || strings.HasPrefix(name, ".") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		method := zip.Store
		if strings.HasSuffix(name, ".json") {
			method = zip.Deflate
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := writeEntry(zw, filepath.ToSlash(rel), method, data); err != nil {
			return err
		}
		entries++
		return nil
	})
	if walkErr != nil {
		zw.Close()
		return 0, walkErr
	}
	if err := zw.Close(); err != nil {
		return 0, err
	}
	return entries, nil
}

// writeEntry adds one file to the zip with an explicit compression method
func writeEntry(zw *zip.Writer, name string, method uint16, data []byte) error {
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method})
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// engineVersion reads the engine module version pinned in a go.mod so a
// bundle's minEngine is derived from the engine it was actually built against
// rather than hand-written and left to drift
func engineVersion(goModPath string) (string, error) {
	const module = "github.com/odevine/mimic/engine"
	raw, err := os.ReadFile(goModPath)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		// Matches both a require-block line and a single-line "require m v"
		for i, f := range fields {
			if f == module && i+1 < len(fields) {
				return fields[i+1], nil
			}
		}
	}
	return "", fmt.Errorf("%s not found in %s", module, goModPath)
}
