package extract

import (
	"image"
	"strings"
	"testing"

	"github.com/oov/psd"
)

// folder builds a group layer holding the given children
func folder(name string, kids ...psd.Layer) psd.Layer {
	l := psd.Layer{Name: name, Layer: kids}
	l.SectionDividerSetting.Type = 1 // open folder
	return l
}

// leaf builds a pixel layer with the given bounds. Type 0 makes HasImage true
func leaf(name string, r image.Rectangle) psd.Layer {
	return psd.Layer{Name: name, Rect: r}
}

func tree() []psd.Layer {
	return []psd.Layer{
		folder("Background", leaf("W", image.Rect(0, 0, 10, 10)), leaf("U", image.Rect(1, 1, 5, 5))),
		folder("Outer", folder("Inner", leaf("target", image.Rect(2, 3, 8, 9)))),
		leaf("Art Frame", image.Rect(10, 20, 110, 220)),
	}
}

func TestResolveGroup(t *testing.T) {
	root := tree()

	got, err := resolveGroup(root, []string{"Background"})
	if err != nil {
		t.Fatalf("resolveGroup(Background): %v", err)
	}
	if findLayer(got, "W") == nil || findLayer(got, "U") == nil {
		t.Fatalf("Background is missing expected child layers")
	}

	got, err = resolveGroup(root, []string{"Outer", "Inner"})
	if err != nil {
		t.Fatalf("resolveGroup(Outer>Inner): %v", err)
	}
	if findLayer(got, "target") == nil {
		t.Fatalf("Outer>Inner is missing target")
	}

	empty, err := resolveGroup(root, nil)
	if err != nil {
		t.Fatalf("resolveGroup(root): %v", err)
	}
	if findLayer(empty, "Art Frame") == nil {
		t.Fatalf("empty path should resolve to the document root")
	}
}

func TestResolveGroupMissingFailsLoud(t *testing.T) {
	_, err := resolveGroup(tree(), []string{"Outer", "Nope"})
	if err == nil {
		t.Fatal("expected an error for a missing group")
	}
	// The message must name the segment that did not match and where it stopped
	if !strings.Contains(err.Error(), `"Nope"`) || !strings.Contains(err.Error(), "Outer") {
		t.Fatalf("error should name the missing segment and path, got: %v", err)
	}
}

func TestFindLayer(t *testing.T) {
	root := tree()
	if l := findLayer(root, "Art Frame"); l == nil || l.Rect.Dx() != 100 || l.Rect.Dy() != 200 {
		t.Fatalf("findLayer(Art Frame) returned wrong layer: %+v", l)
	}
	if findLayer(root, "absent") != nil {
		t.Fatal("findLayer should return nil for an absent name")
	}
}

func TestNormalizeKey(t *testing.T) {
	cases := map[string]string{
		"W":                          "w",
		"GW":                         "wg", // reordered into WUBRG order
		"RW":                         "wr",
		"GU":                         "ug",
		"UB":                         "ub", // already in order
		"UBR":                        "ubr",
		"Gold":                       "gold",
		"Colorless":                  "colorless", // has non-color letters, left as is
		"Full Art Border (leave on)": "full_art_border_(leave_on)",
		"  Land  ":                   "land",
	}
	for in, want := range cases {
		if got := normalizeKey(in); got != want {
			t.Errorf("normalizeKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractBounds(t *testing.T) {
	r, err := extractBounds(&psd.PSD{Layer: tree()}, []string{"Outer", "Inner"}, "target")
	if err != nil {
		t.Fatalf("extractBounds: %v", err)
	}
	if r != image.Rect(2, 3, 8, 9) {
		t.Fatalf("extractBounds returned %v, want (2,3)-(8,9)", r)
	}

	if _, err := extractBounds(&psd.PSD{Layer: tree()}, nil, "absent"); err == nil {
		t.Fatal("expected an error for a missing shape layer")
	}
}
