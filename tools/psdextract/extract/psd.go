package extract

import (
	"fmt"
	"image"
	"os"
	"strings"

	"github.com/oov/psd"
)

// maxDimension caps the canvas size. It matches the engine manifest's own cap.
// This is not a security boundary, just a cheap way to turn a walk-logic bug
// into an immediate, obvious failure instead of a hung process
const maxDimension = 20000

// Short aliases keep the extract logic readable without spelling the upstream
// package names on every signature
type (
	psdDoc   = psd.PSD
	psdLayer = psd.Layer
	nrgba    = image.NRGBA
)

// decodePSD reads the file at path. skipLayerImages leaves layer pixel data
// undecoded, which is much faster and lighter and is all inspection needs
func decodePSD(path string, skipLayerImages bool) (*psd.PSD, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening PSD %q: %w", path, err)
	}
	defer f.Close()
	doc, _, err := psd.Decode(f, &psd.DecodeOptions{
		SkipLayerImage:  skipLayerImages,
		SkipMergedImage: true,
	})
	if err != nil {
		return nil, fmt.Errorf("decoding PSD %q: %w", path, err)
	}
	w, h := doc.Config.Rect.Dx(), doc.Config.Rect.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("PSD %q has non-positive canvas %dx%d", path, w, h)
	}
	if w > maxDimension || h > maxDimension {
		return nil, fmt.Errorf("PSD %q canvas %dx%d exceeds %d cap", path, w, h, maxDimension)
	}
	return doc, nil
}

// resolveGroup follows a case-sensitive path of nested group names from the
// document root. An empty path resolves to the root layer slice. It fails
// loudly naming the segment that did not match, since a missing group is
// almost always a recipe bug rather than an expected case
func resolveGroup(root []psd.Layer, path []string) ([]psd.Layer, error) {
	layers := root
	for i, name := range path {
		child := findFolder(layers, name)
		if child == nil {
			return nil, fmt.Errorf("group path %v: no group named %q under %s",
				path, name, pathSoFar(path, i))
		}
		layers = child.Layer
	}
	return layers, nil
}

// pathSoFar describes where in a group path resolution stopped, for errors
func pathSoFar(path []string, i int) string {
	if i == 0 {
		return "document root"
	}
	return strings.Join(path[:i], " > ")
}

// findFolder returns the direct-child group layer with the given name, or nil
func findFolder(layers []psd.Layer, name string) *psd.Layer {
	for i := range layers {
		if layers[i].Folder() && layers[i].Name == name {
			return &layers[i]
		}
	}
	return nil
}

// findLayer returns the direct-child layer with the given name, group or not,
// or nil. Callers that need pixels check HasImage on the result
func findLayer(layers []psd.Layer, name string) *psd.Layer {
	for i := range layers {
		if layers[i].Name == name {
			return &layers[i]
		}
	}
	return nil
}

// extractBounds resolves a group path and returns the named layer's rectangle.
// It reads a shape or reference layer's bounds without touching pixel data, so
// it works whether or not layer images were decoded. A missing group or layer
// is fatal, since these are singular references a recipe author points at a
// specific layer and a miss is a bug
func extractBounds(doc *psd.PSD, groupPath []string, layerName string) (image.Rectangle, error) {
	group, err := resolveGroup(doc.Layer, groupPath)
	if err != nil {
		return image.Rectangle{}, err
	}
	l := findLayer(group, layerName)
	if l == nil {
		return image.Rectangle{}, fmt.Errorf("no layer named %q under %s",
			layerName, pathSoFar(groupPath, len(groupPath)))
	}
	return l.Rect, nil
}

// renderLayer places a layer's pixels onto a full-canvas transparent buffer at
// the layer's own offset. Frame layers are authored document-sized, so the
// engine loads them at the origin, which is what this preserves. The layer's
// stored visibility flag is ignored: PSD automation hides color-variant layers
// and toggles them at render time, so exporting only visible layers would
// leave most variants blank
func renderLayer(doc *psd.PSD, l *psd.Layer) (*image.NRGBA, error) {
	if l.Picker == nil {
		return nil, fmt.Errorf("layer %q has no pixel data", l.Name)
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, doc.Config.Rect.Dx(), doc.Config.Rect.Dy()))
	src := l.Picker
	sb := src.Bounds()
	// Copy the layer's pixels into the canvas at its PSD rectangle, keeping
	// alpha. drawOver is not wanted here: this is a single isolated layer
	for y := 0; y < sb.Dy(); y++ {
		dy := l.Rect.Min.Y + y
		if dy < 0 || dy >= canvas.Rect.Dy() {
			continue
		}
		for x := 0; x < sb.Dx(); x++ {
			dx := l.Rect.Min.X + x
			if dx < 0 || dx >= canvas.Rect.Dx() {
				continue
			}
			canvas.Set(dx, dy, src.At(sb.Min.X+x, sb.Min.Y+y))
		}
	}
	return canvas, nil
}
