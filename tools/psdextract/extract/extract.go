package extract

import (
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/odevine/mimic/engine/template"
)

// Summary reports what a run produced. It is printed on completion so a run
// where something is subtly wrong, a misspelled variant that silently became a
// missing PNG, still leaves visible feedback
type Summary struct {
	Template     string
	CanvasWidth  int
	CanvasHeight int
	PNGsWritten  int
	TextBoxes    int
	// MissingVariants lists "manifestName/colorKey" pairs a recipe declared
	// but the PSD had no matching layer for. These are warnings, not errors: a
	// template genuinely lacking a variant and a typo look identical, so the
	// run reports them rather than guessing which it is
	MissingVariants []string
}

// pngEncoder pins the compression level so re-running with an unchanged recipe
// produces byte-identical PNGs, keeping iteration diffs to real changes
var pngEncoder = png.Encoder{CompressionLevel: png.BestCompression}

// Extract decodes the recipe's source PSDs, applies r, and writes
// manifest.json and layer PNGs under outRoot/<template>/. paths maps each
// source name the recipe uses to its PSD, with "" the one source of a recipe
// that has only SourceFile. It fails loudly on a missing group or a missing
// text-box or art layer, since those are almost always recipe bugs the author
// can fix immediately.
//
// writeAssets controls whether the layer PNGs are written, writeManifestFile
// whether manifest.json is. Full extraction sets both. manifest-only sets
// writeManifestFile alone, skipping pixel decoding and reusing the PNGs from a
// prior run, which is fast for iterating on conditions and geometry. png-only
// sets writeAssets alone, leaving a hand-tuned manifest untouched while the art
// is re-cut.
//
// The sources are decoded one at a time, since each can take gigabytes once
// its layer pixels are decoded, and the layers are then put back in recipe
// order
func Extract(paths map[string]string, r Recipe, outRoot string, writeAssets, writeManifestFile bool) (*Summary, error) {
	sources, err := r.sourceNames()
	if err != nil {
		return nil, err
	}
	outDir := filepath.Join(outRoot, r.Template)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating %q: %w", outDir, err)
	}

	sum := &Summary{Template: r.Template}
	m := template.Manifest{
		Template:  r.Template,
		TextBoxes: map[string]template.TextBoxSpec{},
	}
	layers := make([]template.LayerSpec, len(r.Layers))
	for _, src := range sources {
		path, ok := paths[src]
		if !ok {
			return nil, fmt.Errorf("no PSD path given for source %q", src)
		}
		doc, err := decodePSD(path, !writeAssets)
		if err != nil {
			return nil, err
		}
		w, h := doc.Config.Rect.Dx(), doc.Config.Rect.Dy()
		if m.Width == 0 {
			m.Width, m.Height = w, h
			sum.CanvasWidth, sum.CanvasHeight = w, h
		} else if w != m.Width || h != m.Height {
			return nil, fmt.Errorf("source %q is %dx%d, the others %dx%d", src, w, h, m.Width, m.Height)
		}

		// The PNGs are cut and encoded in parallel, and all of one source's
		// finish before its decoded layers are dropped
		pool := newPNGPool(runtime.NumCPU())
		for i, rule := range r.Layers {
			if rule.Source != src {
				continue
			}
			spec, err := extractLayer(doc, rule, outDir, sum, writeAssets, pool)
			if err != nil {
				return nil, err
			}
			if len(spec.ColorVariants) == 0 {
				// Every variant went missing. That is a recipe bug worth
				// stopping on, not a layer to drop silently from the manifest
				return nil, fmt.Errorf("layer %q: no color variant resolved to a PSD layer", rule.ManifestName)
			}
			layers[i] = spec
		}
		if err := pool.Wait(); err != nil {
			return nil, err
		}

		if r.ArtSlot.Source == src {
			art, err := extractBounds(doc, r.ArtSlot.GroupPath, r.ArtSlot.LayerName)
			if err != nil {
				return nil, fmt.Errorf("art slot: %w", err)
			}
			m.Art = template.ArtSlot{
				X: art.Min.X, Y: art.Min.Y, Width: art.Dx(), Height: art.Dy(),
				After: r.ArtSlot.After,
			}
		}

		for name, tb := range r.TextBoxes {
			if tb.Source != src {
				continue
			}
			bounds, err := extractBounds(doc, tb.GroupPath, tb.LayerName)
			if err != nil {
				return nil, fmt.Errorf("text box %q: %w", name, err)
			}
			m.TextBoxes[name] = template.TextBoxSpec{
				X: bounds.Min.X, Y: bounds.Min.Y, Width: bounds.Dx(), Height: bounds.Dy(),
				FontSize: tb.FontSize, Align: tb.Align, Color: tb.Color,
				Box: tb.Box, Condition: tb.Condition,
			}
		}
	}
	m.Layers = layers
	sum.TextBoxes = len(m.TextBoxes)

	if writeManifestFile {
		if err := writeManifest(outDir, &m); err != nil {
			return nil, err
		}
	}
	return sum, nil
}

// extractLayer resolves a layer rule's group and exports its color variants to
// PNGs, returning the manifest spec. With AllVariants it exports every pixel
// layer in the group; otherwise it exports the enumerated ColorVariants, and a
// variant whose layer is missing is recorded on the summary and skipped
func extractLayer(doc *psdDoc, rule LayerRule, outDir string, sum *Summary, writeAssets bool, pool *pngPool) (template.LayerSpec, error) {
	group, err := resolveGroup(doc.Layer, rule.GroupPath)
	if err != nil {
		return template.LayerSpec{}, fmt.Errorf("layer %q: %w", rule.ManifestName, err)
	}
	spec := template.LayerSpec{
		Name:          rule.ManifestName,
		Condition:     rule.Condition,
		Blend:         rule.Blend,
		ColorSlot:     rule.ColorSlot,
		ColorVariants: map[string]template.LayerAsset{},
		Mirror:        rule.Mirror,
	}

	if rule.AllVariants {
		for i := range group {
			l := &group[i]
			if !exportable(l, writeAssets) {
				continue // a subgroup or an empty layer, not a variant
			}
			key := normalizeKey(l.Name)
			if err := exportVariant(doc, l, outDir, rule.ManifestName, key, &spec, sum, writeAssets, pool); err != nil {
				return template.LayerSpec{}, err
			}
		}
		if len(spec.ColorVariants) == 0 {
			return template.LayerSpec{}, fmt.Errorf("layer %q: group has no pixel layers to export", rule.ManifestName)
		}
		return spec, nil
	}

	for _, key := range sortedKeys(rule.ColorVariants) {
		l := findLayer(group, rule.ColorVariants[key])
		if l == nil || !exportable(l, writeAssets) {
			sum.MissingVariants = append(sum.MissingVariants, rule.ManifestName+"/"+key)
			continue
		}
		if err := exportVariant(doc, l, outDir, rule.ManifestName, key, &spec, sum, writeAssets, pool); err != nil {
			return template.LayerSpec{}, err
		}
	}
	return spec, nil
}

// exportable reports whether a layer is a pixel variant worth exporting. Pixel
// data is only decoded when writing assets, so its presence is checked only then
func exportable(l *psdLayer, writeAssets bool) bool {
	if !l.HasImage() {
		return false
	}
	return !writeAssets || l.Picker != nil
}

// exportVariant records one variant on the spec and the summary, writing its
// PNG when writeAssets is set
func exportVariant(doc *psdDoc, l *psdLayer, outDir, manifestName, key string, spec *template.LayerSpec, sum *Summary, writeAssets bool, pool *pngPool) error {
	relPath := filepath.ToSlash(filepath.Join(manifestName, key+".png"))
	if writeAssets {
		pool.Go(func() error {
			img, err := renderLayer(doc, l)
			if err != nil {
				return fmt.Errorf("layer %q variant %q: %w", manifestName, key, err)
			}
			return writePNG(filepath.Join(outDir, filepath.FromSlash(relPath)), img)
		})
	}
	spec.ColorVariants[key] = template.LayerAsset{Path: relPath}
	sum.PNGsWritten++
	return nil
}

// wubrg is Magic's canonical color order. Dual and multi-color keys are sorted
// by it so a key matches the engine's frame logic regardless of the letter
// order a PSD layer happens to name a pair in
const wubrg = "wubrg"

// normalizeKey turns a PSD layer name into a manifest color key: lowercased,
// with spaces folded to underscores so a multi-word name stays a single token.
// A name made only of color letters is reordered into WUBRG order, so the PSD's
// "RW" and "GW" become "wr" and "wg", the keys the engine looks a dual up by
func normalizeKey(name string) string {
	k := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "_")
	if isColorCombo(k) {
		return canonicalColorOrder(k)
	}
	return k
}

// isColorCombo reports whether k is made entirely of WUBRG color letters
func isColorCombo(k string) bool {
	if k == "" {
		return false
	}
	for i := 0; i < len(k); i++ {
		if strings.IndexByte(wubrg, k[i]) < 0 {
			return false
		}
	}
	return true
}

// canonicalColorOrder sorts a string of color letters into WUBRG order
func canonicalColorOrder(k string) string {
	b := []byte(k)
	sort.Slice(b, func(i, j int) bool {
		return strings.IndexByte(wubrg, b[i]) < strings.IndexByte(wubrg, b[j])
	})
	return string(b)
}

// writePNG encodes img to path with the pinned encoder, creating parents
func writePNG(path string, img *nrgba) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %q: %w", path, err)
	}
	defer f.Close()
	if err := pngEncoder.Encode(f, img); err != nil {
		return fmt.Errorf("encoding %q: %w", path, err)
	}
	return nil
}

// writeManifest serializes m with sorted map keys and a trailing newline.
// encoding/json already sorts map keys, giving a stable manifest.json across
// runs with an unchanged recipe
func writeManifest(outDir string, m *template.Manifest) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}
	raw = append(raw, '\n')
	path := filepath.Join(outDir, "manifest.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("writing %q: %w", path, err)
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// pngPool runs layer exports on a bounded number of goroutines, keeping the
// first error. Encoding a full-canvas PNG at the best compression is the slow
// step of an extraction, and each is independent of the others
type pngPool struct {
	sem chan struct{}
	wg  sync.WaitGroup
	mu  sync.Mutex
	err error
}

func newPNGPool(n int) *pngPool {
	return &pngPool{sem: make(chan struct{}, max(n, 1))}
}

// Go runs fn once a slot is free
func (p *pngPool) Go(fn func() error) {
	p.sem <- struct{}{}
	p.wg.Go(func() {
		defer func() { <-p.sem }()
		if err := fn(); err != nil {
			p.mu.Lock()
			if p.err == nil {
				p.err = err
			}
			p.mu.Unlock()
		}
	})
}

// Wait blocks until every export has finished and returns the first error
func (p *pngPool) Wait() error {
	p.wg.Wait()
	return p.err
}
