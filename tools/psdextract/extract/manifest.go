package extract

import "github.com/odevine/mimic/engine/template"

// manifest is the manifest.json the extractor writes. It carries every field of
// the engine's template.Manifest and adds the ones a two-half frame needs, which
// the engine reads once it supports them and ignores until then. A manifest with
// none of the additions serializes the same as the engine's own type
type manifest struct {
	Template string `json:"template"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	DPI      int    `json:"dpi,omitempty"`
	// Rotate turns the finished composite this many degrees clockwise, a
	// multiple of 90, so a frame authored in its reading view delivers upright
	Rotate    int                    `json:"rotate,omitempty"`
	Layers    []layerSpec            `json:"layers"`
	TextBoxes map[string]textBoxSpec `json:"textBoxes"`
	// Art is the one art window of a single-faced frame, Arts one per half
	Art  *template.ArtSlot  `json:"art,omitempty"`
	Arts []template.ArtSlot `json:"arts,omitempty"`
}

// layerSpec is a frame layer, optionally scoped to one half of the card and
// placed at an offset in the document, so one half-sized cut can draw at either
// half
type layerSpec struct {
	template.LayerSpec
	// Half is 0 for the whole card, 1 for the first face and 2 for the second
	Half int `json:"half,omitempty"`
	X    int `json:"x,omitempty"`
	Y    int `json:"y,omitempty"`
	// ColorBlend draws a color key of several letters by blending each letter's
	// variant across the layer
	ColorBlend bool `json:"colorBlend,omitempty"`
}

// textBoxSpec is a text box, optionally scoped to one half of the card, or drawn
// in the delivered canvas rather than the authored one
type textBoxSpec struct {
	template.TextBoxSpec
	Half int `json:"half,omitempty"`
	// Space is "output" for a box laid out after the rotation, in the delivered
	// canvas's own coordinates, and empty for one in the authored canvas
	Space string `json:"space,omitempty"`
}
