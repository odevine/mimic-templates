package extract

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"

	"github.com/oov/psd"
)

// gradientFrom and gradientTo are the fractions of a layer's width a two-color
// tint holds its first color up to and its second color from, so the colors
// blend across the middle third rather than all the way across
const (
	gradientFrom = 0.35
	gradientTo   = 0.65
)

// unionAlpha is the alpha plane of several layers drawn over one another, within
// crop. It is the shape a tint colors, read once and reused for every color
func unionAlpha(layers []*psd.Layer, crop image.Rectangle) []uint8 {
	w, h := crop.Dx(), crop.Dy()
	plane := make([]uint8, w*h)
	for _, l := range layers {
		if l.Picker == nil {
			continue
		}
		r := l.Rect.Intersect(crop)
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				a := alphaAt(l, x, y)
				if a == 0 {
					continue
				}
				i := (y-crop.Min.Y)*w + (x - crop.Min.X)
				// Source-over of two alphas
				plane[i] = uint8(uint32(plane[i]) + uint32(a) - uint32(plane[i])*uint32(a)/255)
			}
		}
	}
	return plane
}

// tintImage colors an alpha plane with a color spec, "#RRGGBB" for a flat color
// or "#RRGGBB>#RRGGBB" for a gradient from the left of the plane to the right
func tintImage(plane []uint8, w, h int, spec string) (*image.NRGBA, error) {
	from, to, err := parseTint(spec)
	if err != nil {
		return nil, err
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		t := (float64(x)+0.5)/float64(w) - gradientFrom
		t = min(max(t/(gradientTo-gradientFrom), 0), 1)
		c := color.NRGBA{
			R: lerp8(from.R, to.R, t), G: lerp8(from.G, to.G, t), B: lerp8(from.B, to.B, t),
		}
		for y := 0; y < h; y++ {
			if a := plane[y*w+x]; a != 0 {
				c.A = a
				out.SetNRGBA(x, y, c)
			}
		}
	}
	return out, nil
}

func lerp8(a, b uint8, t float64) uint8 {
	return uint8(float64(a) + (float64(b)-float64(a))*t + 0.5)
}

// parseTint reads a tint spec. A flat color returns the same color twice
func parseTint(spec string) (from, to color.NRGBA, err error) {
	first, second, gradient := strings.Cut(spec, ">")
	if from, err = parseHex(first); err != nil {
		return from, to, err
	}
	if !gradient {
		return from, from, nil
	}
	to, err = parseHex(second)
	return from, to, err
}

func parseHex(s string) (color.NRGBA, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil || len(s) != 6 {
		return color.NRGBA{}, fmt.Errorf("tint color %q is not #RRGGBB", s)
	}
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}, nil
}

// strokePlane turns a shape's alpha plane into the ring a stroke effect draws
// around its edge, Width pixels thick, inside the shape or outside it. The
// shape itself is left empty, the way a shape with no fill shows only its stroke
func strokePlane(shape []uint8, w, h, width int, outside bool) []uint8 {
	inShape := make([]bool, w*h)
	for i, a := range shape {
		inShape[i] = a >= 128
	}
	// Distance to the nearest pixel on the other side of the edge, so a pixel
	// next to the edge is 1 away
	dist := distanceTo(inShape, w, h, outside)
	out := make([]uint8, w*h)
	for i := range out {
		if inShape[i] == outside {
			continue
		}
		// Coverage falls from full to none across the last pixel of the stroke
		cover := min(max(float64(width)+1-float64(dist[i]), 0), 1)
		if outside {
			cover *= 1 - float64(shape[i])/255
		} else {
			cover *= float64(shape[i]) / 255
		}
		out[i] = uint8(cover*255 + 0.5)
	}
	return out
}

// distanceTo is each pixel's Euclidean distance to the nearest pixel whose
// membership of mask equals target
func distanceTo(mask []bool, w, h int, target bool) []float32 {
	const far = 1e20
	sq := make([]float32, w*h)
	for i, in := range mask {
		if in != target {
			sq[i] = far
		}
	}
	// The exact transform runs along each column and then each row
	col := make([]float32, max(w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			col[y] = sq[y*w+x]
		}
		squaredTransform(col[:h])
		for y := 0; y < h; y++ {
			sq[y*w+x] = col[y]
		}
	}
	for y := 0; y < h; y++ {
		squaredTransform(sq[y*w : (y+1)*w])
	}
	for i, v := range sq {
		sq[i] = float32(math.Sqrt(float64(v)))
	}
	return sq
}

// squaredTransform replaces a row of squared distances with the lower envelope of
// the parabolas they define, which is the squared distance along that row
func squaredTransform(f []float32) {
	n := len(f)
	v := make([]int, n)
	z := make([]float32, n+1)
	k := 0
	z[0], z[1] = float32(math.Inf(-1)), float32(math.Inf(1))
	for q := 1; q < n; q++ {
		var s float32
		for {
			p := v[k]
			s = ((f[q] + float32(q*q)) - (f[p] + float32(p*p))) / float32(2*q-2*p)
			if s > z[k] || k == 0 {
				break
			}
			k--
		}
		k++
		v[k] = q
		z[k], z[k+1] = s, float32(math.Inf(1))
	}
	out := make([]float32, n)
	k = 0
	for q := 0; q < n; q++ {
		for z[k+1] < float32(q) {
			k++
		}
		d := float32(q - v[k])
		out[q] = d*d + f[v[k]]
	}
	copy(f, out)
}
