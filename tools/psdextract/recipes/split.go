package recipes

import (
	"image"
	"strings"

	"github.com/odevine/mimic-templates/tools/psdextract/extract"
	"github.com/odevine/mimic/engine/template"
)

// Split is the recipe for the split frame, which draws both halves of a split
// card into one portrait frame from a single Proxyshop-style PSD at 4440x3264.
// The PSD is the card's reading view, the 3264x4440 card turned on its side, so
// the manifest is authored on that canvas, states its 1200 dpi, and rotates the
// finished composite 270 degrees clockwise to stand it upright.
//
// The Left and Right groups hold the two halves, which are laid out alike and
// sit 1912 pixels apart. Every half layer is cut once at the first half's frame
// and placed at both, and every half text box takes the first half's geometry.
// The colored frame art lives once at the top level of the PSD rather than in
// each half.
//
// The pinline shapes in the PSD are black and unfilled, with a stroke effect that
// draws the visible ring, so the recipe strokes each shape at the width the PSD
// states and tints it. The palette is the pinline color of each variant of the
// normal frame, and a dual blends its two colors left to right. The rings sit
// below the name and title plates, as in Proxyshop, so only their outer edge
// shows. The plates and the fuse bar are clipped to a base shape, so they cut
// with ApplyClip
//
// The fuse bar spans both halves and shows only on a fuse card. Its layers and
// its text box carry the fuse condition, and its color comes from a fuse slot.
// The PSD's Shadows layer exists only in the right half and stays off.
//
// The text boxes are point text anchored at their baseline, measured from each
// text layer's transform, except the rules box, which takes the textbox
// reference. The legal line is drawn after the rotation, in the delivered
// canvas, at the rows the PSD's Set and Artist layers occupy once turned upright
func Split() extract.Recipe {
	// A half's art window and rules box sit inside the half, and a half's
	// frame is the reference layer Proxyshop sizes each half's background to
	halves := &extract.HalvesRule{
		First:  extract.LayerRef{GroupPath: []string{"Left", "Background"}, LayerName: "Reference"},
		Second: extract.LayerRef{GroupPath: []string{"Right", "Background"}, LayerName: "Reference"},
	}
	black, white := "#000000", "#FFFFFF"
	// The fuse bar and the pinline stroked around it, cut small since they blend
	fuseBar := image.Rect(490, 2790, 4140, 3020)
	shadow := &template.ShadowSpec{Distance: 0.0907, Angle: 25}
	return extract.Recipe{
		Template:   "split",
		SourceFile: "split.psd",
		DPI:        1200,
		Rotate:     270,
		Halves:     halves,
		Layers: []extract.LayerRule{
			{
				ManifestName: "background", GroupPath: []string{"Background"},
				ColorSlot: "background", AllVariants: true, Halves: true,
			},
			{
				// The pinline around the rules textbox and the art window, a 24 pixel
				// stroke inside each shape, which the PSD leaves unfilled
				ManifestName: "pinlines_boxes", GroupPath: []string{"Left", "Pinlines", "Shape", "Normal"},
				ColorSlot: "pinlines", Halves: true,
				Tint: &extract.Tint{
					From: []string{"Textbox", "Middle Lines"}, Colors: pinlineColors(),
					Stroke: &extract.Stroke{Width: 24},
				},
			},
			{
				// The pinline around the title and type bars, a 30 pixel stroke outside
				// each, which the plates then sit inside of
				ManifestName: "pinlines_twins", GroupPath: []string{"Left", "Pinlines", "Shape", "Normal"},
				ColorSlot: "pinlines", Halves: true,
				Tint: &extract.Tint{
					From: []string{"Twins"}, Colors: pinlineColors(),
					Stroke: &extract.Stroke{Width: 30, Outside: true},
				},
			},
			{
				// The printed textbox is shaded just inside the edges that face the
				// light, which is not in the PSD, so the shadow is fitted to a scan of
				// the real card: about 17 pixels off the light, blurred 3, at 44 percent
				// strength, the way a Photoshop inner shadow would draw it
				ManifestName: "textbox", GroupPath: []string{"Textbox"},
				ColorSlot: "pinlines", AllVariants: true, Halves: true,
				Effects: &extract.Effects{
					Shape:       "W",
					InnerShadow: &extract.InnerShadow{Distance: 17.5, Sigma: 3, Angle: 45, Opacity: 0.44},
				},
			},
			{
				// The shape the color layers clip to is the group's Shape layer, so
				// it is not a variant of its own. A hybrid half asks for the colorless
				// plate, which a printed hybrid card draws in the PSD's warm silver Land
				// color, and the cool Colorless layer stays unused
				ManifestName: "name_title_boxes", GroupPath: []string{"Name & Title Boxes"},
				ColorSlot: "twins", ApplyClip: true, Halves: true,
				Effects: &extract.Effects{
					Shape:   "Shape",
					Bevel:   plateBevel(1.2),
					Outline: &extract.Outline{Width: 8, Color: "#000000"},
				},
				ColorVariants: map[string]string{
					"artifact": "Artifact", "b": "B", "colorless": "Land", "g": "G",
					"gold": "Gold", "r": "R", "u": "U", "w": "W",
				},
			},
			{
				ManifestName: "art_outline", GroupPath: []string{"Left"},
				Halves: true, ColorVariants: map[string]string{"any": "Art Outline"},
			},
			{
				ManifestName: "border", ColorVariants: map[string]string{"any": "Border"},
			},
			{
				// The fuse bar and its strip of border, drawn across both halves
				ManifestName: "border_fuse", Condition: "fuse",
				ColorVariants: map[string]string{"any": "Border Fuse"},
			},
			{
				// A fuse bar blends the colors of its two halves, so a key such as "rw"
				// draws the red and the white variants side by side
				ManifestName: "fuse_textbox", GroupPath: []string{"Fuse", "Textbox"},
				Condition: "fuse", ColorSlot: "fuse", ApplyClip: true,
				Effects: &extract.Effects{Shape: "Plate", Bevel: plateBevel(1.6)},
				Crop:    fuseBar, ColorBlend: true,
				ColorVariants: map[string]string{"gold": "Gold", "g": "G", "r": "R", "b": "B", "u": "U", "w": "W"},
			},
			{
				ManifestName: "fuse_pinlines", GroupPath: []string{"Fuse", "Pinlines", "Pinline"},
				Condition: "fuse", ColorSlot: "fuse_pinlines", Crop: fuseBar, ColorBlend: true,
				Tint: &extract.Tint{
					From: []string{"Twins"}, Colors: fusePinlineColors(),
					Stroke: &extract.Stroke{Width: 24, Outside: true},
				},
			},
		},
		ArtSlot: extract.ArtSlotRule{
			GroupPath: []string{"Left"}, LayerName: "Art Frame",
			After: "pinlines_boxes_2", Half: true,
		},
		TextBoxes: map[string]extract.TextBoxRule{
			"title": {
				Rect:     image.Rect(570, 505, 570+1610, 505+200),
				FontSize: 151, Align: "left", Color: black, VAlign: "baseline", Font: "title",
				ClearOf: "mana", Half: true,
			},
			"mana": {
				Rect:     image.Rect(1339, 502, 1339+841, 502+200),
				FontSize: 174, MinFontSize: 174, Align: "right", Color: black, VAlign: "baseline",
				Shadow: shadow, Half: true,
			},
			"type": {
				// Stops short of the expansion symbol that ends the type bar
				Rect:     image.Rect(575, 1866, 575+1440, 1866+200),
				FontSize: 102, Align: "left", Color: black, VAlign: "baseline", Font: "title",
				Half: true,
			},
			"oracle": {
				GroupPath: []string{"Left", "Text and Icons"}, LayerName: "Textbox Reference",
				FontSize: 150, Align: "left", Color: black, VAlign: "center", LineSpacing: 1,
				PaddingX: intPtr(28), PaddingY: intPtr(28), Half: true,
			},
			"oracle_fuse": {
				// A fuse card's halves leave the bottom of the card to the bar
				GroupPath: []string{"Left", "Text and Icons"}, LayerName: "Textbox Reference Fuse",
				FontSize: 150, Align: "left", Color: black, VAlign: "center", LineSpacing: 1,
				PaddingX: intPtr(28), PaddingY: intPtr(28), Box: "oracle", Condition: "fuse", Half: true,
			},
			"fuse": {
				Rect:     image.Rect(598, 2937, 598+3273, 2937+200),
				FontSize: 113, Align: "left", Color: black, VAlign: "baseline", Tracking: 20,
				Condition: "fuse",
			},
			"artist": {
				Rect:     image.Rect(332, 4094, 332+2100, 4094+120),
				FontSize: 77, Align: "left", Color: white, VAlign: "baseline", Font: "small-caps",
				Space: "output",
			},
			"set": {
				Rect:     image.Rect(332, 4168, 332+1100, 4168+120),
				FontSize: 68, Align: "left", Color: white, VAlign: "baseline", Font: "info",
				Space: "output",
			},
			"copyright": {
				Rect:     image.Rect(1500, 4168, 1500+1437, 4168+120),
				FontSize: 68, Align: "right", Color: white, VAlign: "baseline", Font: "info",
				Tracking: 40, Space: "output",
			},
		},
	}
}

func intPtr(v int) *int { return &v }

// pinline holds the flat color of each frame color's pinline, read from the
// normal frame's pinline art
var pinline = map[string]string{
	"w": "#f8f4f0", "u": "#006dae", "b": "#393431", "r": "#de3c23", "g": "#006d42",
	"gold": "#efd16b", "colorless": "#e6e7e9", "artifact": "#e6e7e9",
}

// pairs are the ten two-color blends, each in the order the PSD names its dual
// layers, which is the order they run from the left of a half to the right. Most
// are in WUBRG order, but the PSD calls three of them GW, RW and GU, and its
// backgrounds run in that direction
var pairs = [][2]string{
	{"w", "u"}, {"w", "b"}, {"r", "w"}, {"g", "w"}, {"u", "b"},
	{"u", "r"}, {"g", "u"}, {"b", "r"}, {"b", "g"}, {"r", "g"},
}

// pairKey is the key the frame files a dual under, its colors in WUBRG order,
// whichever way the dual runs
func pairKey(p [2]string) string {
	if strings.Index("wubrg", p[0]) > strings.Index("wubrg", p[1]) {
		return p[1] + p[0]
	}
	return p[0] + p[1]
}

// pinlineColors is the tint of every pinline variant a half can ask for: the
// flat colors, and a blend of two for each dual
func pinlineColors() map[string]string {
	out := make(map[string]string, len(pinline)+len(pairs))
	for k, c := range pinline {
		out[k] = c
	}
	for _, p := range pairs {
		out[pairKey(p)] = pinline[p[0]] + ">" + pinline[p[1]]
	}
	return out
}

// fusePinlineColors is the tint of the fuse bar's pinline, which is gold or one
// of the five colors
func fusePinlineColors() map[string]string {
	out := map[string]string{"gold": pinline["gold"]}
	for _, k := range []string{"w", "u", "b", "r", "g"} {
		out[k] = pinline[k]
	}
	return out
}

// plateBevel is the inner bevel the PSD gives a plate, a 14 pixel one pressed in
// and lit from the lower left, whose depth differs between the name plates and
// the fuse bar
func plateBevel(depth float64) *extract.Bevel {
	return &extract.Bevel{
		Size: 14, Depth: depth, Angle: -135, Altitude: 30,
		Highlight: 0.8, Shadow: 0.6, Down: true,
	}
}
