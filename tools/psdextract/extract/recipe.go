// Package extract walks a Photoshop template file against a per-template Recipe
// and writes the Manifest JSON plus PNG layer assets the engine's template
// package consumes. The walker is template-agnostic: every PSD-specific fact
// (group names, which layer is which color) lives in a Recipe, so adding a
// template means writing a new recipe, not touching this package.
package extract

import (
	"fmt"
	"image"
	"sort"

	"github.com/odevine/mimic/engine/template"
)

// Recipe describes how to turn one source PSD into a Manifest and its PNG
// layers. A recipe is authored by hand against the real layer tree open in
// Photoshop, so its group and layer names are matched case-sensitively and
// exactly. There is no config-file format on purpose: a Recipe value built in
// Go gets compiler-checked field names while it is written.
type Recipe struct {
	// Template is the manifest template name and the assets/<template>/ output
	// directory
	Template string
	// SourceFile is the default PSD filename. A -psd flag overrides it
	SourceFile string
	// Sources names several source PSDs by filename, such as a double-faced
	// frame's front and back files, for a recipe that pulls one manifest from
	// more than one. When it is set, SourceFile is unused and every layer, text
	// box, and art slot rule names its PSD with Source
	Sources map[string]string
	// Layers are frame layers, in bottom-to-top manifest order
	Layers []LayerRule
	// ArtSlot names the reference layer whose bounds become the art window
	ArtSlot ArtSlotRule
	// TextBoxes maps each manifest text box name to the shape layer that
	// gives it geometry, plus the render attributes the shape cannot carry
	TextBoxes map[string]TextBoxRule
	// DPI states the canvas resolution when it cannot be inferred from the width,
	// as for a frame authored on its side. Zero leaves it to the engine
	DPI int
	// Rotate is written through to the manifest, turning the finished composite
	// this many degrees clockwise
	Rotate int
	// Halves frames the two halves of a split card. A rule that sets Halves is cut
	// once at the first half's frame and placed at both
	Halves *HalvesRule
}

// LayerRule produces one manifest layer. Each ColorVariants entry names a
// layer inside the group at GroupPath, exported to its own PNG
type LayerRule struct {
	// Source names the entry in Recipe.Sources this layer is cut from. It is
	// unused by a recipe with a single SourceFile
	Source string
	// ManifestName is the layer name in the manifest and the PNG subdirectory
	ManifestName string
	// GroupPath is the case-sensitive path of nested group names to the group
	// holding the variant layers. Empty means the document root
	GroupPath []string
	// Condition gates the layer at render time, one of the engine's layer
	// conditions such as "legendary", "land", or "back,land"
	Condition string
	// Blend is a blend.Mode name, empty for normal
	Blend string
	// ColorSlot is written through to the manifest, naming the engine frame
	// slot a layer's variant is picked by. Empty leaves it for hand-tuning
	ColorSlot string
	// ColorVariants maps a manifest color key to the PSD layer name within the
	// group. The key "any" is the color-invariant fallback. Ignored when
	// AllVariants is set
	ColorVariants map[string]string
	// AllVariants exports every pixel layer directly inside the group instead
	// of an enumerated ColorVariants set, keying each by its lowercased PSD
	// layer name (W -> "w", GW -> "gw", "Color Indicator" child UBR -> "ubr").
	// It captures a whole color group, duals and all, without hand-listing
	// every key
	AllVariants bool
	// Mirror is written through to the manifest, flipping the layer left to
	// right when its condition holds. Nil never flips
	Mirror *template.LayerMirror
	// ApplyClip multiplies a variant that is clipped to a layer below it by that
	// layer's alpha, the way Photoshop shows it. A clipped variant spills past
	// its base, so a layer built on clipping needs it. It is off by default so a
	// recipe written before it existed cuts the same pixels
	ApplyClip bool
	// Halves cuts the layer once at the first half's frame, in place of the whole
	// canvas, and writes two manifest layers, named with a _1 and _2 suffix and
	// scoped to each half. It needs Recipe.Halves
	Halves bool
	// Tint builds the variants by recoloring shape layers rather than reading a
	// variant per color from the group
	Tint *Tint
	// Effects bakes the layer effects of a shape in the group, such as the bevel
	// and outline of a plate, into each color variant
	Effects *Effects
	// Crop cuts a whole-card layer to this rectangle instead of the document, such
	// as a bar that spans the card, and the manifest places it there. It is for a
	// layer that is not cut by Halves
	Crop image.Rectangle
	// ColorBlend is written through to the manifest, so a key of several color
	// letters draws as a blend of their variants
	ColorBlend bool
}

// ArtSlotRule names the placeholder layer whose bounds define the art window.
// Reading a named reference layer avoids inferring the window from a hole in
// the frame, which is fragile and differs per template
type ArtSlotRule struct {
	// Source names the Recipe.Sources entry holding the layer
	Source    string
	GroupPath []string
	LayerName string
	// After is the manifest layer name the art sits directly above
	After string
	// Half writes one slot per half, the second shifted by the distance between
	// the halves, in the manifest's arts list
	Half bool
}

// TextBoxRule ties a manifest text box to a PSD shape layer. The shape layer
// gives X, Y, Width and Height. FontSize, Align and Color have no PSD
// equivalent this project's text renderer uses, so the recipe supplies them
type TextBoxRule struct {
	// Source names the Recipe.Sources entry holding the shape layer
	Source    string
	GroupPath []string
	LayerName string
	FontSize  float64
	Align     string // "left" | "center" | "right"
	Color     string // "#RRGGBB"
	// Box and Condition let several rules fill one logical text box under
	// different conditions, written through to the manifest as they are
	Box       string
	Condition string
	// Rect is the box's geometry when no layer gives it, as for a point text box
	// anchored at its baseline. Used when LayerName is empty
	Rect image.Rectangle
	// Half draws the box once per half, with the first half's geometry and the
	// second shifted by the distance between the halves. Keys gain a _1 and _2
	Half bool
	// Space is "output" for a box laid out in the delivered canvas
	Space string
	// The remaining fields are written through to the manifest unchanged
	VAlign      string
	Font        string
	LineSpacing float64
	MinFontSize float64
	Tracking    float64
	Padding     int
	PaddingX    *int
	PaddingY    *int
	ClearOf     string
	Shadow      *template.ShadowSpec
}

// sourceNames reports the source each of r's rules reads from, sorted, and
// checks that every rule names a known one. A recipe with a single SourceFile
// reads everything from the source named "", which is all it has
func (r Recipe) sourceNames() ([]string, error) {
	if len(r.Sources) == 0 {
		return []string{""}, nil
	}
	check := func(what, src string) error {
		if _, ok := r.Sources[src]; !ok {
			return fmt.Errorf("%s names source %q, which the recipe does not list", what, src)
		}
		return nil
	}
	for _, l := range r.Layers {
		if err := check("layer "+l.ManifestName, l.Source); err != nil {
			return nil, err
		}
	}
	for name, tb := range r.TextBoxes {
		if err := check("text box "+name, tb.Source); err != nil {
			return nil, err
		}
	}
	if err := check("art slot", r.ArtSlot.Source); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(r.Sources))
	for name := range r.Sources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// LayerRef names one layer by where it sits in a source PSD
type LayerRef struct {
	// Source names the Recipe.Sources entry holding the layer
	Source    string
	GroupPath []string
	LayerName string
}

// HalvesRule names the layer whose bounds frame each half of a split card. The
// first half's frame is the rectangle a Halves layer is cropped to, and the
// distance between the two frames is how far the second half sits from the
// first. The two halves are laid out alike, so they must be the same size
type HalvesRule struct {
	First, Second LayerRef
}

// Tint recolors the alpha of one or more shape layers, for a frame whose PSD
// carries the shape once and colors it when the card is built. Each entry of
// Colors becomes one color variant, keyed like any other
type Tint struct {
	// From names the layers inside the rule's group whose shapes are united
	From []string
	// Colors maps a color key to "#RRGGBB", or to "#RRGGBB>#RRGGBB" for a
	// gradient from the left of the layer's frame to the right
	Colors map[string]string
	// Stroke draws only a ring along the united shape's edge instead of the shape
	// itself, the way a shape with no fill and a stroke effect shows in Photoshop
	Stroke *Stroke
}

// Stroke is a ring of Width pixels along a shape's edge
type Stroke struct {
	Width int
	// Outside puts the ring outside the shape's edge, and otherwise it is inside
	Outside bool
}
