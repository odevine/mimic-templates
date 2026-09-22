// Command textmeta is a read-only probe: it walks a PSD and prints, for every
// live text layer, its bounds, transform, and the Photoshop type-tool
// EngineData (fonts, sizes, leading, tracking, color, justification). It also
// prints the document resolution and the Art Frame bounds. It measures, it
// never writes.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/oov/psd"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: textmeta <file.psd>")
		os.Exit(1)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	doc, _, err := psd.Decode(f, &psd.DecodeOptions{SkipLayerImage: true, SkipMergedImage: true})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	filter := ""
	if len(os.Args) >= 3 {
		filter = os.Args[2]
	}
	fmt.Printf("canvas: %dx%d\n", doc.Config.Rect.Dx(), doc.Config.Rect.Dy())
	printResolution(doc)
	fmt.Println()
	walk(doc.Layer, nil, filter)
}

// printResolution decodes ResolutionInfo (image resource 1005): hRes and vRes
// are 16.16 fixed-point pixels per inch
func printResolution(doc *psd.PSD) {
	res, ok := doc.Config.Res[1005]
	if !ok || len(res.Data) < 16 {
		fmt.Println("resolution: (resource 1005 absent)")
		return
	}
	d := res.Data
	hRes := float64(uint32(d[0])<<24|uint32(d[1])<<16|uint32(d[2])<<8|uint32(d[3])) / 65536.0
	vRes := float64(uint32(d[8])<<24|uint32(d[9])<<16|uint32(d[10])<<8|uint32(d[11])) / 65536.0
	fmt.Printf("resolution: %.3f x %.3f ppi\n", hRes, vRes)
}

func walk(layers []psd.Layer, path []string, filter string) {
	for i := range layers {
		l := &layers[i]
		here := append(append([]string{}, path...), l.Name)
		if tysh, ok := l.AdditionalLayerInfo["TySh"]; ok {
			joined := strings.Join(here, " > ")
			if filter == "" {
				printText(joined, l, tysh)
			} else if strings.Contains(strings.ToLower(joined), strings.ToLower(filter)) {
				printFull(joined, l, tysh)
			}
		}
		if l.Folder() {
			walk(l.Layer, here, filter)
		}
	}
}

// printFull dumps a layer's transform, bounds, and the whole EngineData so the
// applied style run can be read in context
func printFull(path string, l *psd.Layer, tysh []byte) {
	r := l.Rect
	fmt.Printf("==== %s\n  rendered bounds: x=%d y=%d w=%d h=%d\n", path, r.Min.X, r.Min.Y, r.Dx(), r.Dy())
	if len(tysh) >= 50 {
		fmt.Printf("  transform: scaleX=%.4f scaleY=%.4f tx=%.2f ty=%.2f\n",
			f64(tysh, 2), f64(tysh, 2+8*3), f64(tysh, 2+8*4), f64(tysh, 2+8*5))
	}
	if start := bytes.Index(tysh, []byte("/EngineDict")); start >= 0 {
		if open := bytes.LastIndex(tysh[:start], []byte("<<")); open >= 0 {
			start = open
		}
		fmt.Println(printable(tysh[start:]))
	}
	fmt.Println()
}

func printText(path string, l *psd.Layer, tysh []byte) {
	r := l.Rect
	fmt.Printf("==== %s\n", path)
	fmt.Printf("  rendered bounds: x=%d y=%d w=%d h=%d\n", r.Min.X, r.Min.Y, r.Dx(), r.Dy())

	// TySh: 2-byte version, then 6 float64 transform (xx, xy, yx, yy, tx, ty)
	if len(tysh) >= 50 {
		xx := f64(tysh, 2)
		yy := f64(tysh, 2+8*3)
		tx := f64(tysh, 2+8*4)
		ty := f64(tysh, 2+8*5)
		fmt.Printf("  transform: scaleX=%.4f scaleY=%.4f tx=%.2f ty=%.2f\n", xx, yy, tx, ty)
	}

	// EngineData is ASCII. Read the applied style (the region before
	// ResourceDict) and resolve the font index against the ResourceDict FontSet
	engStart := bytes.Index(tysh, []byte("/EngineDict"))
	if engStart < 0 {
		fmt.Println("  (no EngineData: legacy or non-text layer)")
		fmt.Println()
		return
	}
	eng := printable(tysh[engStart:])
	applied := eng
	if i := strings.Index(eng, "/ResourceDict"); i >= 0 {
		applied = eng[:i]
	}
	shape := "area/box"
	if firstToken(applied, "/ShapeType") == "0" {
		shape = "point"
	}
	fontIdx := firstToken(applied, "/Font")
	fmt.Printf("  anchor: %s | font[%s]=%s | size=%s | autoLeading=%s | leading=%s | tracking=%s | justification=%s\n",
		shape, fontIdx, fontName(eng, fontIdx),
		firstToken(applied, "/FontSize"), firstToken(applied, "/AutoLeading"),
		firstToken(applied, "/Leading"), firstToken(applied, "/Tracking"),
		firstToken(applied, "/Justification"))
	fmt.Printf("  fillColor (A,R,G,B): %s\n", firstValues(applied))
	fmt.Println()
}

// firstToken returns the value after the first occurrence of key, e.g.
// firstToken(s, "/FontSize") on "/FontSize 157.52" returns "157.52"
func firstToken(s, key string) string {
	i := strings.Index(s, key+" ")
	if i < 0 {
		return "-"
	}
	rest := strings.TrimSpace(s[i+len(key):])
	if nl := strings.IndexAny(rest, "\n\r"); nl >= 0 {
		rest = rest[:nl]
	}
	return strings.TrimSpace(rest)
}

// firstValues returns the first "/Values [ ... ]" array contents
func firstValues(s string) string {
	i := strings.Index(s, "/Values [")
	if i < 0 {
		return "-"
	}
	rest := s[i+len("/Values ["):]
	if j := strings.Index(rest, "]"); j >= 0 {
		return strings.TrimSpace(rest[:j])
	}
	return "-"
}

// fontName resolves a FontSet index to its font name from the ResourceDict
func fontName(eng, idx string) string {
	fs := strings.Index(eng, "/FontSet [")
	if fs < 0 || idx == "-" {
		return "?"
	}
	names := []string{}
	for _, part := range strings.Split(eng[fs:], "/Name (")[1:] {
		if j := strings.Index(part, ")"); j >= 0 {
			names = append(names, part[:j])
		}
	}
	var n int
	fmt.Sscanf(idx, "%d", &n)
	if n >= 0 && n < len(names) {
		return names[n]
	}
	return "?"
}

func printable(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		if c == '\n' || c == '\r' || c == '\t' || (c >= 0x20 && c <= 0x7e) {
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

func f64(b []byte, off int) float64 {
	return math.Float64frombits(binary.BigEndian.Uint64(b[off : off+8]))
}
