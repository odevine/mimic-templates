package extract

import (
	"fmt"
	"io"
	"strings"

	"github.com/oov/psd"
)

// Inspect prints the PSD's group and layer tree with each layer's kind and
// bounds. It skips pixel decoding, so it is fast and cheap to run repeatedly
// while authoring a recipe against the real layer names
func Inspect(psdPath string, w io.Writer) error {
	doc, err := decodePSD(psdPath, true)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "canvas %dx%d\n", doc.Config.Rect.Dx(), doc.Config.Rect.Dy())
	printLayers(w, doc.Layer, 0)
	return nil
}

// printLayers writes one line per layer, indented by depth, recursing into
// groups. The kind and rectangle are what a recipe author needs to fill in
// GroupPath, ColorVariants and shape-layer bounds
func printLayers(w io.Writer, layers []psd.Layer, depth int) {
	indent := strings.Repeat("  ", depth)
	for i := range layers {
		l := &layers[i]
		kind := "layer"
		switch {
		case l.Folder():
			kind = "group"
		case !l.HasImage():
			kind = "meta"
		}
		vis := ""
		if !l.Visible() {
			vis = " (hidden)"
		}
		mask := ""
		if l.Mask.Enabled() && !l.Mask.Rect.Empty() {
			_, hasCh := l.Channel[-2]
			mask = fmt.Sprintf(" mask=%dx%d(ch=%t)", l.Mask.Rect.Dx(), l.Mask.Rect.Dy(), hasCh)
		}
		if l.Clipping {
			mask += " clip"
		}
		r := l.Rect
		fmt.Fprintf(w, "%s[%s] %q  %d,%d %dx%d  blend=%q op=%d%s%s\n",
			indent, kind, l.Name, r.Min.X, r.Min.Y, r.Dx(), r.Dy(),
			strings.TrimSpace(string(l.BlendMode)), l.Opacity, mask, vis)
		if l.Folder() {
			printLayers(w, l.Layer, depth+1)
		}
	}
}
