package recipes

import "github.com/odevine/mimic-templates/tools/psdextract/extract"

// Normal is the recipe for the standard card frame PSD, a Proxyshop-style file
// at 3264x4440. Group and layer names are matched case-sensitively against the
// tree printed by -inspect.
//
// This recipe extracts every frame-artwork layer the PSD carries, including
// ones no render path drives yet (nyx, companion, color indicator, dual-color
// pinlines, the dark P/T box). Color groups use AllVariants, which exports
// every pixel layer in the group keyed by its lowercased PSD name, so a whole
// group's duals and special colors come across without hand-listing each key.
//
// Layers a render path does not select yet carry a Condition that the engine's
// conditionMet does not implement, so they stay off until that condition is
// wired in. The color model that maps a card's identity to the dual and
// special keys lives in the engine, not here; this tool only makes the assets
// and names them predictably. Layer order is bottom to top, following the PSD's
// own stacking; the legendary crown sits below the border and name box.
func Normal() extract.Recipe {
	return extract.Recipe{
		Template:   "normal",
		SourceFile: "normal.psd",
		Layers: []extract.LayerRule{
			{
				ManifestName: "background",
				GroupPath:    []string{"Background"},
				AllVariants:  true,
			},
			{
				// Enchantment (Nyx) frame overlay, off until an "nyx" condition exists
				ManifestName: "nyx",
				GroupPath:    []string{"Nyx"},
				Condition:    "nyx",
				AllVariants:  true,
			},
			{
				// Companion banner, off until a "companion" condition exists
				ManifestName: "companion",
				GroupPath:    []string{"Companion"},
				Condition:    "companion",
				AllVariants:  true,
			},
			{
				// A single visible inner shadow that frames every card the same
				ManifestName:  "shadows",
				ColorVariants: map[string]string{"any": "Shadows"},
			},
			{
				// A full-canvas dark overlay that only reads as a shadow through
				// the hollow legendary crown above it. That compositing is not
				// modeled yet, and rendering it flat blacks out the card, so it
				// stays off (asset still extracted) until the engine handles it
				ManifestName:  "hollow_crown_shadow",
				Condition:     "hollow_crown",
				ColorVariants: map[string]string{"any": "Hollow Crown Shadow"},
			},
			{
				ManifestName: "land_pinlines_textbox",
				GroupPath:    []string{"Land Pinlines & Textbox"},
				Condition:    "land",
				AllVariants:  true,
			},
			{
				ManifestName: "pinlines_textbox",
				GroupPath:    []string{"Pinlines & Textbox"},
				Condition:    "nonland",
				AllVariants:  true,
			},
			{
				// Below the border so the notched legendary border frames it and
				// the name box above stays uncovered
				ManifestName: "legendary_crown",
				GroupPath:    []string{"Legendary Crown"},
				Condition:    "legendary",
				AllVariants:  true,
			},
			{
				ManifestName:  "border",
				GroupPath:     []string{"Border"},
				Condition:     "nonlegendary",
				ColorVariants: map[string]string{"any": "Normal Border"},
			},
			{
				ManifestName:  "border_legendary",
				GroupPath:     []string{"Border"},
				Condition:     "legendary",
				ColorVariants: map[string]string{"any": "Legendary Border"},
			},
			{
				// Full-art border, off until a "fullart" condition exists
				ManifestName:  "border_fullart",
				GroupPath:     []string{"Border"},
				Condition:     "fullart",
				ColorVariants: map[string]string{"any": "Full Art Border (leave on)"},
			},
			{
				ManifestName: "name_title_boxes",
				GroupPath:    []string{"Name & Title Boxes"},
				AllVariants:  true,
			},
			{
				// The color-identity pip, off until a "color_indicator" condition exists
				ManifestName: "color_indicator",
				GroupPath:    []string{"Color Indicator"},
				Condition:    "color_indicator",
				AllVariants:  true,
			},
			{
				// The flavor-text divider, off until a "divider" condition exists
				ManifestName:  "divider",
				GroupPath:     []string{"Text and Icons"},
				Condition:     "divider",
				ColorVariants: map[string]string{"any": "Divider"},
			},
			{
				// The P/T box belongs only on creatures. "creature" matches
				// nothing until the engine adds that condition, so it stays off
				ManifestName: "pt_box",
				GroupPath:    []string{"PT Box"},
				Condition:    "creature",
				AllVariants:  true,
			},
			{
				// The darker P/T box variant, off until a "pt_dark" condition exists
				ManifestName: "pt_box_dark",
				GroupPath:    []string{"PT Box Dark"},
				Condition:    "pt_dark",
				AllVariants:  true,
			},
		},
		// The art placeholder is a top-level reference layer; the engine slots
		// the card art directly above the background
		ArtSlot: extract.ArtSlotRule{
			LayerName: "Art Frame",
			After:     "background",
		},
		// Geometry comes from reference boxes, not the dynamic text layers. Font
		// size, alignment and color have no PSD equivalent the engine uses, so
		// they are set here and tuned against test renders
		TextBoxes: map[string]extract.TextBoxRule{
			"title": {
				GroupPath: []string{"Overlay"}, LayerName: "Name",
				FontSize: 150, Align: "left", Color: "#000000",
			},
			"mana": {
				GroupPath: []string{"Text and Icons"}, LayerName: "Mana Cost",
				FontSize: 120, Align: "right", Color: "#000000",
			},
			"type": {
				GroupPath: []string{"Overlay"}, LayerName: "Typeline",
				FontSize: 110, Align: "left", Color: "#000000",
			},
			"oracle": {
				GroupPath: []string{"Text and Icons"}, LayerName: "Textbox Reference",
				FontSize: 90, Align: "left", Color: "#000000",
			},
			"pt": {
				GroupPath: []string{"Text and Icons"}, LayerName: "PT Reference",
				FontSize: 150, Align: "center", Color: "#000000",
			},
		},
	}
}
