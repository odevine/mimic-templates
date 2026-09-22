// Command pack builds a versioned .mimic bundle. A .mimic file is a zip archive:
// bundle.json and manifest.json at the root, then one directory of layer PNGs
// per layer group, mirroring the on-disk layout the engine reads.
//
// It has two modes:
//
//	pack <dir> -version 1.2.0
//	    Bootstrap mode. Reads the manifest and layer PNGs from a loose extracted
//	    directory. Used once per template, or when the art is re-cut from the PSD.
//
//	pack -from-bundle prev.mimic -manifest templates/normal/manifest.json -version 1.3.0
//	    Repack mode. Carries the layer PNGs over from a previous bundle and pairs
//	    them with a hand-edited manifest. The bundle can be a local path or an
//	    https URL. This is the routine path: the manifest is the source of truth
//	    and the PNGs are stable, so a version usually differs only in its manifest.
//
// pack imports the engine's template package and validates the manifest the same
// way the running engine will, so a bundle that packs is one the engine accepts.
// minEngine defaults to the engine version this repo's go.mod pins.
package main

import (
	"archive/zip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
	out := fs.String("o", "", "output .mimic path; empty writes <template>.mimic beside the source")
	goMod := fs.String("go-mod", "go.mod", "go.mod to read the engine version from when -min-engine is empty")
	fromBundle := fs.String("from-bundle", "", "previous .mimic (path or https URL) to carry PNGs from; enables repack mode")
	manifestPath := fs.String("manifest", "", "manifest.json to pack; required with -from-bundle")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dir == "" && fs.NArg() == 1 {
		dir = fs.Arg(0)
	}
	if *version == "" {
		return fmt.Errorf("-version is required")
	}

	repack := *fromBundle != ""

	// Resolve the manifest the same way in both modes: through the engine's own
	// reader, so a bundle that packs is one the engine will accept, and read the
	// template name from it so the header and filename cannot disagree with the
	// contents. In repack mode the manifest is the hand-edited tracked file; in
	// bootstrap mode it is the one in the loose directory
	var (
		m             *template.Manifest
		manifestBytes []byte
		err           error
	)
	if repack {
		if *manifestPath == "" {
			return fmt.Errorf("-manifest is required with -from-bundle")
		}
		manifestBytes, err = os.ReadFile(*manifestPath)
		if err != nil {
			return err
		}
		m, err = validateManifestBytes(manifestBytes)
		if err != nil {
			return fmt.Errorf("validating %q: %w", *manifestPath, err)
		}
	} else {
		if dir == "" {
			return fmt.Errorf("expected a source directory argument or -from-bundle")
		}
		m, err = template.NewFSAssetProvider(dir).Manifest()
		if err != nil {
			return fmt.Errorf("validating manifest in %q: %w", dir, err)
		}
	}
	if m.Template == "" {
		return fmt.Errorf("manifest has no template name")
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
		base := dir
		if repack {
			base = *fromBundle
		}
		outPath = filepath.Join(filepath.Dir(filepath.Clean(base)), m.Template+".mimic")
	}

	var n int
	if repack {
		n, err = repackBundle(outPath, *fromBundle, manifestBytes, m, bundle)
	} else {
		n, err = writeBundle(outPath, dir, bundle)
	}
	if err != nil {
		return err
	}
	fmt.Printf("packed %q v%s (minEngine %s): %d entries -> %s\n",
		bundle.Template, bundle.Version, bundle.MinEngine, n, outPath)
	return nil
}

// writeBundle writes a .mimic zip from a loose directory: bundle.json first,
// then every file under dir except a stray bundle.json and dotfiles. PNGs are
// stored uncompressed because their pixel data is already DEFLATE-compressed
// inside the PNG, so zipping it again spends CPU on every read for no size gain
// and keeps each entry cheaply seekable. JSON is deflated. Returns the entry count
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

// repackBundle writes a new bundle that carries the PNG entries over from a
// previous bundle verbatim and pairs them with a fresh manifest and header. The
// PNGs never change on a manifest-only release, so copying them raw avoids
// re-reading and re-compressing a couple hundred MB. It then checks that every
// PNG the new manifest points at is actually present, so a manifest edit that
// references a missing layer fails here rather than at render time
func repackBundle(outPath, fromBundle string, manifestBytes []byte, m *template.Manifest, b Bundle) (int, error) {
	zr, closeSrc, err := openBundle(fromBundle)
	if err != nil {
		return 0, err
	}
	defer closeSrc()

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
	if err := writeEntry(zw, "manifest.json", zip.Deflate, manifestBytes); err != nil {
		return 0, err
	}
	entries++

	// Copy every carried entry as stored raw bytes, preserving each entry's own
	// compression method without decompressing and re-compressing it
	carried := map[string]bool{}
	for _, src := range zr.File {
		if src.Name == "bundle.json" || src.Name == "manifest.json" {
			continue
		}
		fh := src.FileHeader
		w, err := zw.CreateRaw(&fh)
		if err != nil {
			zw.Close()
			return 0, err
		}
		rc, err := src.OpenRaw()
		if err != nil {
			zw.Close()
			return 0, err
		}
		if _, err := io.Copy(w, rc); err != nil {
			zw.Close()
			return 0, err
		}
		carried[src.Name] = true
		entries++
	}

	if missing := missingLayers(m, carried); len(missing) > 0 {
		zw.Close()
		return 0, fmt.Errorf("manifest references %d layer PNG(s) not in %s:\n  %s",
			len(missing), fromBundle, strings.Join(missing, "\n  "))
	}

	if err := zw.Close(); err != nil {
		return 0, err
	}
	return entries, nil
}

// missingLayers returns the layer PNG paths the manifest points at that are not
// among the carried entries, sorted for a stable error message
func missingLayers(m *template.Manifest, carried map[string]bool) []string {
	var missing []string
	for _, layer := range m.Layers {
		for _, variant := range layer.ColorVariants {
			if !carried[variant.Path] {
				missing = append(missing, variant.Path)
			}
		}
	}
	sort.Strings(missing)
	return missing
}

// openBundle opens a .mimic for reading from a local path or an https URL,
// returning the zip reader and a cleanup. A URL is downloaded to a temp file
// first, because a zip reader needs random access to the central directory at
// the end of the archive
func openBundle(src string) (*zip.Reader, func(), error) {
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		path, err := downloadTemp(src)
		if err != nil {
			return nil, nil, err
		}
		zr, closeFile, err := openBundleFile(path)
		if err != nil {
			os.Remove(path)
			return nil, nil, err
		}
		return zr, func() { closeFile(); os.Remove(path) }, nil
	}
	return openBundleFile(src)
}

func openBundleFile(path string) (*zip.Reader, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("reading %q as a zip: %w", path, err)
	}
	return zr, func() { f.Close() }, nil
}

// downloadTemp fetches url to a temp file and returns its path
func downloadTemp(url string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %s: %s", url, resp.Status)
	}
	tmp, err := os.CreateTemp("", "pack-*.mimic")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

// validateManifestBytes runs the engine's own manifest reader against raw bytes
// by staging them where FSAssetProvider expects, so repack rejects a manifest
// the engine would reject
func validateManifestBytes(raw []byte) (*template.Manifest, error) {
	dir, err := os.MkdirTemp("", "pack-manifest-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o644); err != nil {
		return nil, err
	}
	return template.NewFSAssetProvider(dir).Manifest()
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
