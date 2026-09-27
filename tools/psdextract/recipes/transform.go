package recipes

import (
	"github.com/odevine/mimic-templates/tools/psdextract/extract"
	"github.com/odevine/mimic/engine/template"
)

// Transform is the recipe for the transform frame, which draws both faces of a
// transform double-faced card from two Proxyshop-style PSDs at 3264x4440, the
// same canvas as normal. It follows Normal, less the groups these PSDs lack
// (Nyx, Companion, PT Box Dark), and adds the transform icon in the title bar.
//
// Groups whose PNGs differ between the two files are cut from each with a
// front or back condition. The back is darker throughout, and the front's
// textbox has a notch on its right edge for the flipside power and toughness.
// Groups that come out byte-identical (the legendary crown, the color
// indicator, the icon's backing circle, and the divider) are cut once, from
// the front, with no face condition, which keeps the bundle smaller.
//
// Each face-specific layer carries a _front or _back suffix, so the manifest's
// layer names stay unique for the text boxes that avoid or duck under them.
//
// A triangle back face prints its icon at the right end of the title bar, which
// neither PSD lays out, so the back's title bar layers mirror under icon_right.
// The pinlines and name box flip only the title bar band, keeping the type
// line's indicator on the left. The background flips whole, since a band would
// leave seams in its texture. The shadows stay put, as both ends of the bar
// share one outline and the light still falls from the upper left
func Transform() extract.Recipe {
	titleBand := &template.LayerMirror{Condition: "icon_right", Y: 260, Width: 1632, Height: 360}
	whole := &template.LayerMirror{Condition: "icon_right"}
	return extract.Recipe{
		Template: "transform",
		Sources:  map[string]string{"front": "tf-front.psd", "back": "tf-back.psd"},
		Layers: []extract.LayerRule{
			{Source: "front", ManifestName: "background_front", GroupPath: []string{"Background"}, Condition: "front", ColorSlot: "background", AllVariants: true},
			{Source: "back", ManifestName: "background_back", GroupPath: []string{"Background"}, Condition: "back", ColorSlot: "background", AllVariants: true, Mirror: whole},
			{Source: "front", ManifestName: "shadows_front", Condition: "front", ColorVariants: map[string]string{"any": "Shadows"}},
			{Source: "back", ManifestName: "shadows_back", Condition: "back", ColorVariants: map[string]string{"any": "Shadows"}},
			{
				// Off for the same reason as in Normal: it only reads as a shadow
				// through the hollow crown, which the engine does not composite
				Source: "front", ManifestName: "hollow_crown_shadow", Condition: "hollow_crown",
				ColorVariants: map[string]string{"any": "Hollow Crown Shadow"},
			},
			{
				// The front file has only the colorless land textbox, the back one
				// every color, for backs such as Azcanta, the Sunken Ruin
				Source: "front", ManifestName: "land_pinlines_textbox_front", GroupPath: []string{"Land Pinlines & Textbox"},
				Condition: "front,land", ColorSlot: "pinlines", AllVariants: true,
			},
			{
				Source: "back", ManifestName: "land_pinlines_textbox_back", GroupPath: []string{"Land Pinlines & Textbox"},
				Condition: "back,land", ColorSlot: "pinlines", AllVariants: true, Mirror: titleBand,
			},
			{
				Source: "front", ManifestName: "pinlines_textbox_front", GroupPath: []string{"Pinlines & Textbox"},
				Condition: "front,nonland", ColorSlot: "pinlines", AllVariants: true,
			},
			{
				Source: "back", ManifestName: "pinlines_textbox_back", GroupPath: []string{"Pinlines & Textbox"},
				Condition: "back,nonland", ColorSlot: "pinlines", AllVariants: true, Mirror: titleBand,
			},
			{
				Source: "front", ManifestName: "legendary_crown", GroupPath: []string{"Legendary Crown"},
				Condition: "legendary", ColorSlot: "crown", AllVariants: true,
			},
			{Source: "front", ManifestName: "border_front", GroupPath: []string{"Border"}, Condition: "front,nonlegendary", ColorVariants: map[string]string{"any": "Normal Border"}},
			{Source: "back", ManifestName: "border_back", GroupPath: []string{"Border"}, Condition: "back,nonlegendary", ColorVariants: map[string]string{"any": "Normal Border"}},
			{Source: "front", ManifestName: "border_legendary_front", GroupPath: []string{"Border"}, Condition: "front,legendary", ColorVariants: map[string]string{"any": "Legendary Border"}},
			{Source: "back", ManifestName: "border_legendary_back", GroupPath: []string{"Border"}, Condition: "back,legendary", ColorVariants: map[string]string{"any": "Legendary Border"}},
			{
				// The bottom edge behind the legal lines, drawn on every card
				Source: "front", ManifestName: "border_fullart", GroupPath: []string{"Border"},
				ColorVariants: map[string]string{"any": "Full Art Border (leave on)"},
			},
			{
				Source: "front", ManifestName: "name_title_boxes_front", GroupPath: []string{"Name & Title Boxes"},
				Condition: "front", ColorSlot: "twins", AllVariants: true,
			},
			{
				Source: "back", ManifestName: "name_title_boxes_back", GroupPath: []string{"Name & Title Boxes"},
				Condition: "back", ColorSlot: "twins", AllVariants: true, Mirror: titleBand,
			},
			{
				Source: "front", ManifestName: "transform_circle", GroupPath: []string{"Text and Icons", "Transform", "Circle"},
				ColorVariants: map[string]string{"any": "Backing Circle"}, Mirror: whole,
			},
			{
				// Clipped to the backing circle in the PSD and blended linear light
				// at 15% opacity. The engine models neither clipping nor that mode,
				// so it stays off behind a condition never met
				Source: "front", ManifestName: "artifact_overlay", GroupPath: []string{"Text and Icons", "Transform", "Circle"},
				Condition:     "clipped",
				ColorVariants: map[string]string{"any": "artifactoverlay"},
			},
			{
				Source: "front", ManifestName: "transform_icon_front", GroupPath: []string{"Text and Icons", "Transform", "Front"},
				Condition: "front", ColorSlot: "transform_icon", AllVariants: true,
			},
			{
				Source: "back", ManifestName: "transform_icon_back", GroupPath: []string{"Text and Icons", "Transform", "Back"},
				Condition: "back", ColorSlot: "transform_icon", AllVariants: true, Mirror: whole,
			},
			{
				Source: "front", ManifestName: "color_indicator", GroupPath: []string{"Color Indicator"},
				Condition: "color_indicator", ColorSlot: "indicator", AllVariants: true,
			},
			{
				Source: "front", ManifestName: "divider", GroupPath: []string{"Text and Icons"},
				Condition: "divider", ColorVariants: map[string]string{"any": "Divider"},
			},
			{
				Source: "front", ManifestName: "pt_box_front", GroupPath: []string{"PT Box"},
				Condition: "front,creature", ColorSlot: "ptBox", AllVariants: true,
			},
			{
				Source: "back", ManifestName: "pt_box_back", GroupPath: []string{"PT Box"},
				Condition: "back,creature", ColorSlot: "ptBox", AllVariants: true,
			},
		},
		ArtSlot: extract.ArtSlotRule{Source: "front", LayerName: "Art Frame", After: "background_back"},
		// The title comes from Card Name Shift rather than Card Name, so the
		// name clears the icon. The back's title, type, and P/T are white
		TextBoxes: map[string]extract.TextBoxRule{
			"title": {
				Source: "front", GroupPath: []string{"Text and Icons"}, LayerName: "Card Name Shift",
				FontSize: 156, Align: "left", Color: "#000000", Condition: "front",
			},
			"title_back": {
				Source: "back", GroupPath: []string{"Text and Icons"}, LayerName: "Card Name Shift",
				FontSize: 156, Align: "left", Color: "#FFFFFF", Box: "title", Condition: "back",
			},
			// With the icon on the right the name starts where a plain title does
			"title_back_right": {
				Source: "back", GroupPath: []string{"Text and Icons"}, LayerName: "Card Name",
				FontSize: 156, Align: "left", Color: "#FFFFFF", Box: "title", Condition: "back,icon_right",
			},
			"mana": {
				Source: "front", GroupPath: []string{"Text and Icons"}, LayerName: "Mana Cost",
				FontSize: 179, Align: "right", Color: "#000000",
			},
			"type": {
				Source: "front", GroupPath: []string{"Text and Icons"}, LayerName: "Typeline",
				FontSize: 132, Align: "left", Color: "#000000",
			},
			"type_shift": {
				Source: "front", GroupPath: []string{"Text and Icons"}, LayerName: "Typeline Shift",
				FontSize: 132, Align: "left", Color: "#000000", Box: "type", Condition: "color_indicator",
			},
			"type_back": {
				Source: "back", GroupPath: []string{"Text and Icons"}, LayerName: "Typeline",
				FontSize: 132, Align: "left", Color: "#FFFFFF", Box: "type", Condition: "back",
			},
			"type_back_shift": {
				Source: "back", GroupPath: []string{"Text and Icons"}, LayerName: "Typeline Shift",
				FontSize: 132, Align: "left", Color: "#FFFFFF", Box: "type", Condition: "back,color_indicator",
			},
			"oracle": {
				Source: "front", GroupPath: []string{"Text and Icons"}, LayerName: "Textbox Reference",
				FontSize: 150, Align: "left", Color: "#000000", Condition: "front",
			},
			"oracle_back": {
				Source: "back", GroupPath: []string{"Text and Icons"}, LayerName: "Textbox Reference",
				FontSize: 150, Align: "left", Color: "#000000", Box: "oracle", Condition: "back",
			},
			"pt": {
				Source: "front", GroupPath: []string{"Text and Icons"}, LayerName: "PT Reference",
				FontSize: 159, Align: "center", Color: "#000000",
			},
			"pt_back": {
				Source: "back", GroupPath: []string{"Text and Icons"}, LayerName: "PT Reference",
				FontSize: 159, Align: "center", Color: "#FFFFFF", Box: "pt", Condition: "back",
			},
			"flipside_pt": {
				Source: "front", GroupPath: []string{"Text and Icons"}, LayerName: "Flipside Power / Toughness",
				FontSize: 116, Align: "right", Color: "#494949",
			},
		},
	}
}
