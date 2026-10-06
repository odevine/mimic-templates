package extract

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/oov/psd"
)

// solid builds a pixel layer filled with one opaque color
func solid(name string, r image.Rectangle, c color.NRGBA) psd.Layer {
	img := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := 0; y < r.Dy(); y++ {
		for x := 0; x < r.Dx(); x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return psd.Layer{Name: name, Rect: r, Picker: img}
}

var red = color.NRGBA{R: 255, A: 255}

func TestClipBase(t *testing.T) {
	group := []psd.Layer{
		{Name: "Shape"},
		{Name: "Gold", Clipping: true},
		{Name: "Land", Clipping: true},
		{Name: "Other"},
	}
	if got := clipBase(group, 2); got == nil || got.Name != "Shape" {
		t.Errorf("clipBase(Land) = %v, want Shape, the nearest layer below that is not clipped", got)
	}
	if got := clipBase(group, 3); got != nil {
		t.Errorf("clipBase(Other) = %v, want nil for a layer that is not clipped", got.Name)
	}
	if got := clipBase([]psd.Layer{{Name: "Orphan", Clipping: true}}, 0); got != nil {
		t.Errorf("clipBase(Orphan) = %v, want nil when nothing is below", got.Name)
	}
}

func TestRenderLayerCropsAndClips(t *testing.T) {
	doc := &psd.PSD{}
	doc.Config.Rect = image.Rect(0, 0, 40, 40)
	layer := solid("Color", image.Rect(10, 10, 20, 20), red)
	base := solid("Base", image.Rect(15, 10, 25, 20), color.NRGBA{A: 255})

	out, err := renderLayer(doc, &layer, image.Rect(10, 10, 30, 30), &base)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Bounds(); got != image.Rect(0, 0, 20, 20) {
		t.Fatalf("cut is %v, want the 20x20 crop", got)
	}
	// The layer spans crop x 0..9 and the base crop x 5..14, so only 5..9 shows
	for _, c := range []struct {
		x, y int
		want uint8
	}{{2, 5, 0}, {5, 5, 255}, {9, 5, 255}, {12, 5, 0}, {7, 15, 0}} {
		if got := out.NRGBAAt(c.x, c.y).A; got != c.want {
			t.Errorf("alpha at crop (%d,%d) = %d, want %d", c.x, c.y, got, c.want)
		}
	}

	whole, err := renderLayer(doc, &layer, image.Rectangle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := whole.Bounds(); got != image.Rect(0, 0, 40, 40) {
		t.Fatalf("an empty crop cut %v, want the whole canvas", got)
	}
	if whole.NRGBAAt(12, 12).A != 255 || whole.NRGBAAt(2, 2).A != 0 {
		t.Error("the whole-canvas cut did not keep the layer at its own offset")
	}
}

func TestTint(t *testing.T) {
	a := solid("A", image.Rect(0, 0, 10, 4), red)
	b := solid("B", image.Rect(8, 0, 20, 4), red)
	crop := image.Rect(0, 0, 20, 4)
	plane := unionAlpha([]*psd.Layer{&a, &b}, crop)
	if plane[0] != 255 || plane[19] != 255 || plane[9] != 255 {
		t.Error("the union should cover both shapes and where they overlap")
	}

	flat, err := tintImage(plane, 20, 4, "#112233")
	if err != nil {
		t.Fatal(err)
	}
	if got := flat.NRGBAAt(3, 1); got != (color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 255}) {
		t.Errorf("flat tint = %v", got)
	}

	grad, err := tintImage(plane, 20, 4, "#000000>#ffffff")
	if err != nil {
		t.Fatal(err)
	}
	if left, right := grad.NRGBAAt(0, 0), grad.NRGBAAt(19, 0); left.R != 0 || right.R != 255 {
		t.Errorf("gradient ends = %v and %v, want black then white", left, right)
	}
	if mid := grad.NRGBAAt(10, 0).R; mid <= 0 || mid >= 255 {
		t.Errorf("gradient middle = %d, want a blend", mid)
	}

	for _, bad := range []string{"", "red", "#12345", "#000000>nope"} {
		if _, err := tintImage(plane, 20, 4, bad); err == nil {
			t.Errorf("tint %q should fail", bad)
		}
	}
}

func TestMeasureHalves(t *testing.T) {
	doc := &psd.PSD{Layer: []psd.Layer{
		folder("Left", leaf("Frame", image.Rect(100, 50, 300, 450))),
		folder("Right", leaf("Frame", image.Rect(700, 50, 900, 450))),
		folder("Wide", leaf("Frame", image.Rect(700, 50, 950, 450))),
	}}
	rule := func(second string) HalvesRule {
		return HalvesRule{
			First:  LayerRef{GroupPath: []string{"Left"}, LayerName: "Frame"},
			Second: LayerRef{GroupPath: []string{second}, LayerName: "Frame"},
		}
	}
	got, err := measureHalves(doc, rule("Right"))
	if err != nil {
		t.Fatal(err)
	}
	if got.frame != image.Rect(100, 50, 300, 450) || got.shift != 600 {
		t.Errorf("halves = %+v, want the left frame and a shift of 600", got)
	}
	if _, err := measureHalves(doc, rule("Wide")); err == nil || !strings.Contains(err.Error(), "differ") {
		t.Errorf("halves of different sizes should fail, got %v", err)
	}
}

func TestStrokePlane(t *testing.T) {
	const w, h = 30, 30
	shape := make([]uint8, w*h)
	for y := 10; y < 20; y++ {
		for x := 10; x < 20; x++ {
			shape[y*w+x] = 255
		}
	}
	at := func(p []uint8, x, y int) uint8 { return p[y*w+x] }

	inside := strokePlane(shape, w, h, 2, false)
	for _, c := range []struct {
		x, y int
		want uint8
	}{{10, 15, 255}, {11, 15, 255}, {12, 15, 0}, {15, 15, 0}, {9, 15, 0}} {
		if got := at(inside, c.x, c.y); got != c.want {
			t.Errorf("inside stroke at (%d,%d) = %d, want %d", c.x, c.y, got, c.want)
		}
	}

	outside := strokePlane(shape, w, h, 3, true)
	for _, c := range []struct {
		x, y int
		want uint8
	}{{9, 15, 255}, {7, 15, 255}, {6, 15, 0}, {15, 15, 0}, {15, 7, 255}} {
		if got := at(outside, c.x, c.y); got != c.want {
			t.Errorf("outside stroke at (%d,%d) = %d, want %d", c.x, c.y, got, c.want)
		}
	}

	// A corner is rounded, since the stroke is a distance from the edge
	if at(outside, 7, 7) != 0 {
		t.Error("the outside corner should be rounded off, not square")
	}
}

func TestBevelShadesTheSideFacingTheLight(t *testing.T) {
	const w, h = 60, 60
	shape := make([]uint8, w*h)
	for y := 10; y < 50; y++ {
		for x := 10; x < 50; x++ {
			shape[y*w+x] = 255
		}
	}
	at := func(p []uint8, x, y int) uint8 { return p[y*w+x] }
	// Light from the lower left, as in the PSD's plates, with the bevel raised
	up := Bevel{Size: 8, Depth: 1.2, Angle: -135, Altitude: 30, Highlight: 0.8, Shadow: 0.6}
	hi, sh := bevelPlanes(shape, w, h, up)
	if at(hi, 11, 30) == 0 || at(sh, 11, 30) != 0 {
		t.Error("the left edge faces the light, so a raised bevel should light it")
	}
	if at(sh, 48, 30) == 0 || at(hi, 48, 30) != 0 {
		t.Error("the right edge faces away, so a raised bevel should shade it")
	}
	if at(hi, 30, 30) != 0 || at(sh, 30, 30) != 0 {
		t.Error("the middle of the shape is flat and should stay as it is")
	}

	// Pressed in, the same edges swap
	down := up
	down.Down = true
	hi, sh = bevelPlanes(shape, w, h, down)
	if at(sh, 11, 30) == 0 || at(hi, 48, 30) == 0 {
		t.Error("a bevel pressed in should shade the edge facing the light and light the other")
	}
}

func TestEffectsLayOntoAVariant(t *testing.T) {
	const w, h = 40, 40
	shape := make([]uint8, w*h)
	for y := 10; y < 30; y++ {
		for x := 10; x < 30; x++ {
			shape[y*w+x] = 255
		}
	}
	p, err := prepareEffects(shape, w, h, &Effects{
		Bevel:   &Bevel{Size: 4, Depth: 1.2, Angle: -135, Altitude: 30, Highlight: 0.8, Shadow: 0.6, Down: true},
		Outline: &Outline{Width: 3, Color: "#000000"},
	})
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 10; y < 30; y++ {
		for x := 10; x < 30; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 200, G: 200, B: 200, A: 255})
		}
	}
	p.apply(img)
	if got := img.NRGBAAt(9, 20); got.A != 255 || got.R != 0 {
		t.Errorf("outline pixel = %v, want opaque black", got)
	}
	if got := img.NRGBAAt(5, 20); got.A != 0 {
		t.Errorf("a pixel beyond the outline = %v, want empty", got)
	}
	if got := img.NRGBAAt(20, 20); got != (color.NRGBA{R: 200, G: 200, B: 200, A: 255}) {
		t.Errorf("the flat middle = %v, want it untouched", got)
	}
	if got := img.NRGBAAt(10, 20); got.R >= 200 {
		t.Errorf("the lit edge pressed in = %v, want it darker", got)
	}

	if _, err := prepareEffects(shape, w, h, &Effects{Outline: &Outline{Width: 3, Color: "red"}}); err == nil {
		t.Error("a bad outline color should fail")
	}
}

func TestInnerShadowFallsOnTheEdgesFacingTheLight(t *testing.T) {
	const w, h = 120, 120
	shape := make([]uint8, w*h)
	for y := 20; y < 100; y++ {
		for x := 20; x < 100; x++ {
			shape[y*w+x] = 255
		}
	}
	// Light from the upper right, so the top and right edges are shaded
	plane := innerShadowPlane(shape, w, h, InnerShadow{Distance: 8, Sigma: 3, Angle: 45, Opacity: 0.5})
	at := func(x, y int) uint8 { return plane[y*w+x] }

	if at(60, 21) < 60 || at(98, 60) < 60 {
		t.Errorf("the top and right edges are %d and %d, want them shaded", at(60, 21), at(98, 60))
	}
	if at(60, 98) > 8 || at(21, 60) > 8 {
		t.Errorf("the bottom and left edges are %d and %d, want them clear", at(60, 98), at(21, 60))
	}
	if at(60, 60) != 0 {
		t.Errorf("the middle is %d, want it clear", at(60, 60))
	}
	if at(5, 5) != 0 {
		t.Error("the shadow should stay inside the shape")
	}
	var peak uint8
	for _, v := range plane {
		peak = max(peak, v)
	}
	if peak > 128 {
		t.Errorf("the shadow reaches %d, past its half opacity", peak)
	}
	// Fading with depth
	if !(at(60, 21) > at(60, 30) && at(60, 30) > at(60, 40)) {
		t.Errorf("the shadow does not fade inward: %d %d %d", at(60, 21), at(60, 30), at(60, 40))
	}
}

func TestBlurPlanePreservesTotal(t *testing.T) {
	const w, h = 30, 30
	p := make([]float32, w*h)
	p[15*w+15] = 1
	blurPlane(p, w, h, 2)
	var sum float32
	for _, v := range p {
		sum += v
	}
	if sum < 0.99 || sum > 1.01 {
		t.Errorf("blur changed the total to %f", sum)
	}
	if p[15*w+15] >= 1 || p[15*w+15] <= p[15*w+18] {
		t.Error("the blur should spread the point out around its center")
	}
}
