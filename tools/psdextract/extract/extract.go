package extract

import (
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"

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

// Extract decodes the PSD at psdPath, applies r, and writes manifest.json and
// layer PNGs under outRoot/<template>/. It fails loudly on a missing group or
// a missing text-box or art layer, since those are almost always recipe bugs
// the author can fix immediately.
//
// With writeAssets false it regenerates only manifest.json, skipping pixel
// decoding and PNG writing. That is fast, for iterating on conditions, font
// sizes and text-box geometry, and assumes the PNGs from a prior full run are
// already in place
// writeAssets controls whether the layer PNGs are written; writeManifestFile
// controls whether manifest.json is. Full extraction sets both. manifest-only
// sets writeManifestFile alone, reusing existing PNGs. png-only sets writeAssets
// alone, leaving a hand-tuned manifest untouched while the art is re-cut.
func Extract(psdPath string, r Recipe, outRoot string, writeAssets, writeManifestFile bool) (*Summary, error) {
	doc, err := decodePSD(psdPath, !writeAssets)
	if err != nil {
		return nil, err
	}
	outDir := filepath.Join(outRoot, r.Template)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating %q: %w", outDir, err)
	}

	sum := &Summary{
		Template:     r.Template,
		CanvasWidth:  doc.Config.Rect.Dx(),
		CanvasHeight: doc.Config.Rect.Dy(),
	}
	m := template.Manifest{
		Template:  r.Template,
		Width:     doc.Config.Rect.Dx(),
		Height:    doc.Config.Rect.Dy(),
		TextBoxes: map[string]template.TextBoxSpec{},
	}

	for _, rule := range r.Layers {
		spec, err := extractLayer(doc, rule, outDir, sum, writeAssets)
		if err != nil {
			return nil, err
		}
		if len(spec.ColorVariants) == 0 {
			// Every variant went missing. That is a recipe bug worth stopping
			// on, not a layer to drop silently from the manifest
			return nil, fmt.Errorf("layer %q: no color variant resolved to a PSD layer", rule.ManifestName)
		}
		m.Layers = append(m.Layers, spec)
	}

	art, err := extractBounds(doc, r.ArtSlot.GroupPath, r.ArtSlot.LayerName)
	if err != nil {
		return nil, fmt.Errorf("art slot: %w", err)
	}
	m.Art = template.ArtSlot{
		X: art.Min.X, Y: art.Min.Y, Width: art.Dx(), Height: art.Dy(),
		After: r.ArtSlot.After,
	}

	for name, tb := range r.TextBoxes {
		bounds, err := extractBounds(doc, tb.GroupPath, tb.LayerName)
		if err != nil {
			return nil, fmt.Errorf("text box %q: %w", name, err)
		}
		m.TextBoxes[name] = template.TextBoxSpec{
			X: bounds.Min.X, Y: bounds.Min.Y, Width: bounds.Dx(), Height: bounds.Dy(),
			FontSize: tb.FontSize, Align: tb.Align, Color: tb.Color,
		}
	}
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
func extractLayer(doc *psdDoc, rule LayerRule, outDir string, sum *Summary, writeAssets bool) (template.LayerSpec, error) {
	group, err := resolveGroup(doc.Layer, rule.GroupPath)
	if err != nil {
		return template.LayerSpec{}, fmt.Errorf("layer %q: %w", rule.ManifestName, err)
	}
	spec := template.LayerSpec{
		Name:          rule.ManifestName,
		Condition:     rule.Condition,
		Blend:         rule.Blend,
		ColorVariants: map[string]template.LayerAsset{},
	}

	if rule.AllVariants {
		for i := range group {
			l := &group[i]
			if !exportable(l, writeAssets) {
				continue // a subgroup or an empty layer, not a variant
			}
			key := normalizeKey(l.Name)
			if err := exportVariant(doc, l, outDir, rule.ManifestName, key, &spec, sum, writeAssets); err != nil {
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
		if err := exportVariant(doc, l, outDir, rule.ManifestName, key, &spec, sum, writeAssets); err != nil {
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
func exportVariant(doc *psdDoc, l *psdLayer, outDir, manifestName, key string, spec *template.LayerSpec, sum *Summary, writeAssets bool) error {
	relPath := filepath.ToSlash(filepath.Join(manifestName, key+".png"))
	if writeAssets {
		img, err := renderLayer(doc, l)
		if err != nil {
			return fmt.Errorf("layer %q variant %q: %w", manifestName, key, err)
		}
		if err := writePNG(filepath.Join(outDir, filepath.FromSlash(relPath)), img); err != nil {
			return err
		}
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
