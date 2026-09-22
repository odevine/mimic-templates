// Package extract walks a Photoshop template file against a per-template Recipe
// and writes the Manifest JSON plus PNG layer assets the engine's template
// package consumes. The walker is template-agnostic: every PSD-specific fact
// (group names, which layer is which color) lives in a Recipe, so adding a
// template means writing a new recipe, not touching this package.
package extract

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
	// Layers are frame layers, in bottom-to-top manifest order
	Layers []LayerRule
	// ArtSlot names the reference layer whose bounds become the art window
	ArtSlot ArtSlotRule
	// TextBoxes maps each manifest text box name to the shape layer that
	// gives it geometry, plus the render attributes the shape cannot carry
	TextBoxes map[string]TextBoxRule
}

// LayerRule produces one manifest layer. Each ColorVariants entry names a
// layer inside the group at GroupPath, exported to its own PNG
type LayerRule struct {
	// ManifestName is the layer name in the manifest and the PNG subdirectory
	ManifestName string
	// GroupPath is the case-sensitive path of nested group names to the group
	// holding the variant layers. Empty means the document root
	GroupPath []string
	// Condition gates the layer at render time: "", "legendary",
	// "nonlegendary", "land", "nonland", or a color key
	Condition string
	// Blend is a blend.Mode name, empty for normal
	Blend string
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
}

// ArtSlotRule names the placeholder layer whose bounds define the art window.
// Reading a named reference layer avoids inferring the window from a hole in
// the frame, which is fragile and differs per template
type ArtSlotRule struct {
	GroupPath []string
	LayerName string
	// After is the manifest layer name the art sits directly above
	After string
}

// TextBoxRule ties a manifest text box to a PSD shape layer. The shape layer
// gives X, Y, Width and Height. FontSize, Align and Color have no PSD
// equivalent this project's text renderer uses, so the recipe supplies them
type TextBoxRule struct {
	GroupPath []string
	LayerName string
	FontSize  float64
	Align     string // "left" | "center" | "right"
	Color     string // "#RRGGBB"
}
