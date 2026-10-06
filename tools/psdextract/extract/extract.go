package extract

import (
	"encoding/json"
	"fmt"
	"image"
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
	m := manifest{
		Template:  r.Template,
		DPI:       r.DPI,
		Rotate:    r.Rotate,
		TextBoxes: map[string]textBoxSpec{},
	}
	layers := make([][]layerSpec, len(r.Layers))
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

		var halves *halfGeometry
		if r.Halves != nil && r.Halves.First.Source == src {
			if halves, err = measureHalves(doc, *r.Halves); err != nil {
				return nil, err
			}
		}

		// The PNGs are cut and encoded in parallel, and all of one source's
		// finish before its decoded layers are dropped
		pool := newPNGPool(runtime.NumCPU())
		for i, rule := range r.Layers {
			if rule.Source != src {
				continue
			}
			specs, err := extractLayer(doc, rule, halves, outDir, sum, writeAssets, pool)
			if err != nil {
				return nil, err
			}
			layers[i] = specs
		}
		if err := pool.Wait(); err != nil {
			return nil, err
		}

		if r.ArtSlot.Source == src && (r.ArtSlot.LayerName != "" || r.ArtSlot.Half) {
			art, err := extractBounds(doc, r.ArtSlot.GroupPath, r.ArtSlot.LayerName)
			if err != nil {
				return nil, fmt.Errorf("art slot: %w", err)
			}
			slot := template.ArtSlot{
				X: art.Min.X, Y: art.Min.Y, Width: art.Dx(), Height: art.Dy(),
				After: r.ArtSlot.After,
			}
			if r.ArtSlot.Half {
				if halves == nil {
					return nil, fmt.Errorf("art slot: Half needs Recipe.Halves")
				}
				second := slot
				second.X += halves.shift
				m.Arts = []template.ArtSlot{slot, second}
			} else {
				m.Art = &slot
			}
		}

		for name, tb := range r.TextBoxes {
			if tb.Source != src {
				continue
			}
			bounds := tb.Rect
			if tb.LayerName != "" {
				if bounds, err = extractBounds(doc, tb.GroupPath, tb.LayerName); err != nil {
					return nil, fmt.Errorf("text box %q: %w", name, err)
				}
			}
			spec := textBoxSpec{
				TextBoxSpec: template.TextBoxSpec{
					X: bounds.Min.X, Y: bounds.Min.Y, Width: bounds.Dx(), Height: bounds.Dy(),
					FontSize: tb.FontSize, Align: tb.Align, Color: tb.Color,
					Box: tb.Box, Condition: tb.Condition,
					VAlign: tb.VAlign, Font: tb.Font, LineSpacing: tb.LineSpacing,
					MinFontSize: tb.MinFontSize, Tracking: tb.Tracking,
					Padding: tb.Padding, PaddingX: tb.PaddingX, PaddingY: tb.PaddingY,
					ClearOf: tb.ClearOf, Shadow: tb.Shadow,
				},
				Space: tb.Space,
			}
			if !tb.Half {
				m.TextBoxes[name] = spec
				continue
			}
			if halves == nil {
				return nil, fmt.Errorf("text box %q: Half needs Recipe.Halves", name)
			}
			if spec.Box == "" {
				spec.Box = name
			}
			first, second := spec, spec
			first.Half, second.Half = 1, 2
			second.X += halves.shift
			m.TextBoxes[name+"_1"], m.TextBoxes[name+"_2"] = first, second
		}
	}
	for _, specs := range layers {
		m.Layers = append(m.Layers, specs...)
	}
	sum.TextBoxes = len(m.TextBoxes)

	if writeManifestFile {
		if err := writeManifest(outDir, &m); err != nil {
			return nil, err
		}
	}
	return sum, nil
}

// halfGeometry is where the first half of a split card sits and how far the
// second is from it
type halfGeometry struct {
	frame image.Rectangle
	shift int
}

// measureHalves reads the two half frames from their reference layers. The
// halves must be the same size, since one cut is placed at both
func measureHalves(doc *psdDoc, h HalvesRule) (*halfGeometry, error) {
	first, err := extractBounds(doc, h.First.GroupPath, h.First.LayerName)
	if err != nil {
		return nil, fmt.Errorf("first half frame: %w", err)
	}
	second, err := extractBounds(doc, h.Second.GroupPath, h.Second.LayerName)
	if err != nil {
		return nil, fmt.Errorf("second half frame: %w", err)
	}
	if first.Dx() != second.Dx() || first.Dy() != second.Dy() || first.Min.Y != second.Min.Y {
		return nil, fmt.Errorf("half frames %v and %v differ in size or row", first, second)
	}
	return &halfGeometry{frame: first, shift: second.Min.X - first.Min.X}, nil
}

// extractLayer resolves a layer rule's group and exports its color variants to
// PNGs, returning the manifest layers: one, or two for a rule that cuts a half.
// With AllVariants it exports every pixel layer in the group, with Tint it
// recolors shape layers, and otherwise it exports the enumerated ColorVariants,
// where a variant whose layer is missing is recorded on the summary and skipped
func extractLayer(doc *psdDoc, rule LayerRule, halves *halfGeometry, outDir string, sum *Summary, writeAssets bool, pool *pngPool) ([]layerSpec, error) {
	group, err := resolveGroup(doc.Layer, rule.GroupPath)
	if err != nil {
		return nil, fmt.Errorf("layer %q: %w", rule.ManifestName, err)
	}
	var crop image.Rectangle
	if rule.Halves {
		if halves == nil {
			return nil, fmt.Errorf("layer %q: Halves needs Recipe.Halves", rule.ManifestName)
		}
		crop = halves.frame
	} else {
		crop = rule.Crop
	}
	spec := template.LayerSpec{
		Name:          rule.ManifestName,
		Condition:     rule.Condition,
		Blend:         rule.Blend,
		ColorSlot:     rule.ColorSlot,
		ColorVariants: map[string]template.LayerAsset{},
		Mirror:        rule.Mirror,
	}
	var fx *effectPlanes
	if rule.Effects != nil && writeAssets {
		if fx, err = effectsFor(doc, group, rule, crop); err != nil {
			return nil, err
		}
	}
	cut := func(i int) func() (*nrgba, error) {
		l := &group[i]
		var base *psdLayer
		if rule.ApplyClip {
			base = clipBase(group, i)
		}
		return func() (*nrgba, error) {
			img, err := renderLayer(doc, l, crop, base)
			if err == nil && fx != nil {
				fx.apply(img)
			}
			return img, err
		}
	}

	switch {
	case rule.Tint != nil:
		if err := tintVariants(doc, group, rule, crop, outDir, &spec, sum, writeAssets, pool); err != nil {
			return nil, err
		}
	case rule.AllVariants:
		for i := range group {
			l := &group[i]
			if !exportable(l, writeAssets) {
				continue // a subgroup or an empty layer, not a variant
			}
			exportVariant(cut(i), outDir, rule.ManifestName, normalizeKey(l.Name), &spec, sum, writeAssets, pool)
		}
	default:
		for _, key := range sortedKeys(rule.ColorVariants) {
			name := rule.ColorVariants[key]
			i := indexOfLayer(group, name)
			if i < 0 || !exportable(&group[i], writeAssets) {
				sum.MissingVariants = append(sum.MissingVariants, rule.ManifestName+"/"+key)
				continue
			}
			exportVariant(cut(i), outDir, rule.ManifestName, key, &spec, sum, writeAssets, pool)
		}
	}
	if len(spec.ColorVariants) == 0 {
		// Every variant went missing. That is a recipe bug worth stopping on, not
		// a layer to drop silently from the manifest
		return nil, fmt.Errorf("layer %q: no color variant resolved to a PSD layer", rule.ManifestName)
	}

	if !rule.Halves {
		return []layerSpec{{LayerSpec: spec, X: crop.Min.X, Y: crop.Min.Y, ColorBlend: rule.ColorBlend}}, nil
	}
	first, second := spec, spec
	first.Name, second.Name = rule.ManifestName+"_1", rule.ManifestName+"_2"
	return []layerSpec{
		{LayerSpec: first, Half: 1, X: crop.Min.X, Y: crop.Min.Y, ColorBlend: rule.ColorBlend},
		{LayerSpec: second, Half: 2, X: crop.Min.X + halves.shift, Y: crop.Min.Y, ColorBlend: rule.ColorBlend},
	}, nil
}

// tintVariants exports one variant per color of a tint rule, each the united
// shape of the rule's source layers colored in
func tintVariants(doc *psdDoc, group []psdLayer, rule LayerRule, crop image.Rectangle, outDir string, spec *template.LayerSpec, sum *Summary, writeAssets bool, pool *pngPool) error {
	var shapes []*psdLayer
	for _, name := range rule.Tint.From {
		i := indexOfLayer(group, name)
		if i < 0 || !exportable(&group[i], writeAssets) {
			return fmt.Errorf("layer %q: tint source %q is not a pixel layer in its group", rule.ManifestName, name)
		}
		shapes = append(shapes, &group[i])
	}
	if crop.Empty() {
		crop = image.Rect(0, 0, doc.Config.Rect.Dx(), doc.Config.Rect.Dy())
	}
	var plane []uint8
	if writeAssets {
		plane = unionAlpha(shapes, crop)
		if s := rule.Tint.Stroke; s != nil {
			plane = strokePlane(plane, crop.Dx(), crop.Dy(), s.Width, s.Outside)
		}
	}
	for _, key := range tintKeys(rule.Tint.Colors) {
		colors := rule.Tint.Colors[key]
		exportVariant(func() (*nrgba, error) { return tintImage(plane, crop.Dx(), crop.Dy(), colors) },
			outDir, rule.ManifestName, key, spec, sum, writeAssets, pool)
	}
	return nil
}

func tintKeys(m map[string]string) []string { return sortedKeys(m) }

// indexOfLayer is the index of the direct-child layer with the given name, or -1
func indexOfLayer(layers []psdLayer, name string) int {
	for i := range layers {
		if layers[i].Name == name {
			return i
		}
	}
	return -1
}

// exportable reports whether a layer is a pixel variant worth exporting. Pixel
// data is only decoded when writing assets, so its presence is checked only then
func exportable(l *psdLayer, writeAssets bool) bool {
	if !l.HasImage() {
		return false
	}
	return !writeAssets || l.Picker != nil
}

// exportVariant records one variant on the spec and the summary, writing the
// image render produces to its PNG when writeAssets is set
func exportVariant(render func() (*nrgba, error), outDir, manifestName, key string, spec *template.LayerSpec, sum *Summary, writeAssets bool, pool *pngPool) {
	relPath := filepath.ToSlash(filepath.Join(manifestName, key+".png"))
	if writeAssets {
		pool.Go(func() error {
			img, err := render()
			if err != nil {
				return fmt.Errorf("layer %q variant %q: %w", manifestName, key, err)
			}
			return writePNG(filepath.Join(outDir, filepath.FromSlash(relPath)), img)
		})
	}
	spec.ColorVariants[key] = template.LayerAsset{Path: relPath}
	sum.PNGsWritten++
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
func writeManifest(outDir string, m *manifest) error {
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

// effectsFor measures the shape a rule's effects follow, inside crop
func effectsFor(doc *psdDoc, group []psdLayer, rule LayerRule, crop image.Rectangle) (*effectPlanes, error) {
	i := indexOfLayer(group, rule.Effects.Shape)
	if i < 0 || group[i].Picker == nil {
		return nil, fmt.Errorf("layer %q: effects shape %q is not a pixel layer in its group", rule.ManifestName, rule.Effects.Shape)
	}
	if crop.Empty() {
		crop = image.Rect(0, 0, doc.Config.Rect.Dx(), doc.Config.Rect.Dy())
	}
	shape := unionAlpha([]*psdLayer{&group[i]}, crop)
	return prepareEffects(shape, crop.Dx(), crop.Dy(), rule.Effects)
}
