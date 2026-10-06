package extract

import (
	"image/color"
	"math"
)

// Bevel is a Photoshop inner bevel and emboss, as the PSD states it. The engine
// draws no layer effects, so extraction bakes the shading into a layer's pixels
type Bevel struct {
	// Size is how far the bevel reaches into the shape, in pixels
	Size int
	// Depth is the strength as a fraction, 1.2 for the PSD's 120 percent
	Depth float64
	// Angle is the light's direction in degrees counterclockwise from the right,
	// and Altitude its height above the surface
	Angle, Altitude float64
	// Highlight and Shadow are how opaque the lit and unlit faces are, as fractions
	Highlight, Shadow float64
	// Down presses the bevel in instead of raising it, which swaps the lit and
	// unlit sides
	Down bool
}

// InnerShadow darkens the inside of a shape along the edges that face the
// light, as Photoshop's inner shadow does. The shape's outside is moved away from
// the light by Distance, blurred, and laid over the inside
type InnerShadow struct {
	// Distance is how far the shadow moves, in pixels, and Sigma the blur radius
	Distance, Sigma float64
	// Angle is the light's direction in degrees counterclockwise from the right
	Angle float64
	// Opacity is the darkest the shadow gets, as a fraction
	Opacity float64
}

// Outline is a stroke around the outside of a shape
type Outline struct {
	Width int
	Color string
}

// Effects are the layer effects of a shape that a layer's color variants take
// on, read from the shape's own layer in the PSD
type Effects struct {
	// Shape names the layer in the rule's group whose alpha the effects follow
	Shape       string
	Bevel       *Bevel
	InnerShadow *InnerShadow
	Outline     *Outline
}

// effectPlanes are the shading and outline of one shape, ready to lay onto each
// of its color variants
type effectPlanes struct {
	w, h      int
	highlight []uint8
	shadow    []uint8
	inner     []uint8
	ring      []uint8
	ringColor color.NRGBA
}

// prepareEffects measures the shape once, so every variant shares the work
func prepareEffects(shape []uint8, w, h int, fx *Effects) (*effectPlanes, error) {
	p := &effectPlanes{w: w, h: h}
	if b := fx.Bevel; b != nil {
		p.highlight, p.shadow = bevelPlanes(shape, w, h, *b)
	}
	if s := fx.InnerShadow; s != nil {
		p.inner = innerShadowPlane(shape, w, h, *s)
	}
	if o := fx.Outline; o != nil {
		c, err := parseHex(o.Color)
		if err != nil {
			return nil, err
		}
		p.ring, p.ringColor = strokePlane(shape, w, h, o.Width, true), c
	}
	return p, nil
}

// bevelPlanes shades the band just inside a shape's edge. A face that turns
// toward the light is lighter than a flat surface and one that turns away is
// darker, and a bevel pressed in (Down) turns each of them the other way
func bevelPlanes(shape []uint8, w, h int, b Bevel) (highlight, shadow []uint8) {
	inShape := make([]bool, w*h)
	for i, a := range shape {
		inShape[i] = a >= 128
	}
	dist := distanceTo(inShape, w, h, false)

	az, alt := b.Angle*math.Pi/180, b.Altitude*math.Pi/180
	lx, ly := math.Cos(az), -math.Sin(az)
	flat := math.Sin(alt)
	slope := b.Depth
	// A face turned fully toward the light, which is the most the shading moves
	best := (slope*math.Cos(alt)+flat)/math.Sqrt(1+slope*slope) - flat

	highlight, shadow = make([]uint8, w*h), make([]uint8, w*h)
	at := func(x, y int) float64 {
		x, y = min(max(x, 0), w-1), min(max(y, 0), h-1)
		return float64(dist[y*w+x])
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			d := float64(dist[i])
			if !inShape[i] || d > float64(b.Size) {
				continue
			}
			// The distance grows into the shape, so its gradient points inward
			gx, gy := at(x+1, y)-at(x-1, y), at(x, y+1)-at(x, y-1)
			norm := math.Hypot(gx, gy)
			if norm == 0 {
				continue
			}
			// The face looks outward, away from the shape
			facing := -(gx*lx + gy*ly) / norm
			if b.Down {
				facing = -facing
			}
			delta := (slope*facing*math.Cos(alt)+flat)/math.Sqrt(1+slope*slope) - flat
			// The shape's own edge pixels carry its antialiasing
			edge := float64(shape[i]) / 255
			if delta > 0 {
				highlight[i] = uint8(min(delta/best, 1)*b.Highlight*edge*255 + 0.5)
			} else {
				shadow[i] = uint8(min(-delta/best, 1)*b.Shadow*edge*255 + 0.5)
			}
		}
	}
	return highlight, shadow
}

// apply lays the shading over a color variant that fills the shape, then the
// outline beneath it where the variant leaves the pixel empty
func (p *effectPlanes) apply(img *nrgba) {
	for y := 0; y < p.h; y++ {
		for x := 0; x < p.w; x++ {
			i := y*p.w + x
			c := img.NRGBAAt(x, y)
			if c.A > 0 {
				if p.inner != nil && p.inner[i] > 0 {
					m := 1 - float64(p.inner[i])/255
					c.R, c.G, c.B = uint8(float64(c.R)*m+0.5), uint8(float64(c.G)*m+0.5), uint8(float64(c.B)*m+0.5)
				}
				if p.shadow != nil && p.shadow[i] > 0 {
					m := 1 - float64(p.shadow[i])/255
					c.R, c.G, c.B = uint8(float64(c.R)*m+0.5), uint8(float64(c.G)*m+0.5), uint8(float64(c.B)*m+0.5)
				}
				if p.highlight != nil && p.highlight[i] > 0 {
					s := float64(p.highlight[i]) / 255
					c.R, c.G, c.B = uint8(float64(c.R)+(255-float64(c.R))*s+0.5), uint8(float64(c.G)+(255-float64(c.G))*s+0.5), uint8(float64(c.B)+(255-float64(c.B))*s+0.5)
				}
				img.SetNRGBA(x, y, c)
			}
			if p.ring != nil && p.ring[i] > 0 && c.A < 255 {
				ring := p.ringColor
				ring.A = p.ring[i]
				img.SetNRGBA(x, y, over(ring, c))
			}
		}
	}
}

// over draws the top color over the bottom one, both not premultiplied
func over(bottom, top color.NRGBA) color.NRGBA {
	ta, ba := float64(top.A)/255, float64(bottom.A)/255
	a := ta + ba*(1-ta)
	if a == 0 {
		return color.NRGBA{}
	}
	mix := func(t, b uint8) uint8 {
		return uint8((float64(t)*ta+float64(b)*ba*(1-ta))/a + 0.5)
	}
	return color.NRGBA{R: mix(top.R, bottom.R), G: mix(top.G, bottom.G), B: mix(top.B, bottom.B), A: uint8(a*255 + 0.5)}
}

// innerShadowPlane is the shadow's opacity at each pixel of a shape. The area
// outside the shape is slid away from the light, blurred, and kept where it
// reaches into the shape, so only the edges facing the light are darkened
func innerShadowPlane(shape []uint8, w, h int, s InnerShadow) []uint8 {
	az := s.Angle * math.Pi / 180
	// The shadow falls away from the light, and the light's y runs up the page
	ox := int(math.Round(-math.Cos(az) * s.Distance))
	oy := int(math.Round(math.Sin(az) * s.Distance))

	outside := make([]float32, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx, sy := x-ox, y-oy
			// Beyond the crop is outside the shape
			v := float32(1)
			if sx >= 0 && sx < w && sy >= 0 && sy < h {
				v = 1 - float32(shape[sy*w+sx])/255
			}
			outside[y*w+x] = v
		}
	}
	blurPlane(outside, w, h, s.Sigma)

	out := make([]uint8, w*h)
	for i, a := range shape {
		if a == 0 {
			continue
		}
		out[i] = uint8(float64(outside[i])*s.Opacity*float64(a) + 0.5)
	}
	return out
}

// blurPlane blurs a plane in place with a Gaussian of the given sigma, one axis
// at a time
func blurPlane(p []float32, w, h int, sigma float64) {
	if sigma <= 0 {
		return
	}
	radius := int(math.Ceil(sigma * 3))
	kernel := make([]float32, 2*radius+1)
	var sum float32
	for i := range kernel {
		d := float64(i - radius)
		kernel[i] = float32(math.Exp(-d * d / (2 * sigma * sigma)))
		sum += kernel[i]
	}
	for i := range kernel {
		kernel[i] /= sum
	}
	tmp := make([]float32, len(p))
	pass := func(src, dst []float32, n, stride, lines, lineStride int) {
		for l := 0; l < lines; l++ {
			base := l * lineStride
			for i := 0; i < n; i++ {
				var v float32
				for k, wt := range kernel {
					// The edge repeats, so a shape at the frame's edge is not blurred
					// toward darkness
					j := min(max(i+k-radius, 0), n-1)
					v += wt * src[base+j*stride]
				}
				dst[base+i*stride] = v
			}
		}
	}
	pass(p, tmp, w, 1, h, w)
	pass(tmp, p, h, w, w, 1)
}
