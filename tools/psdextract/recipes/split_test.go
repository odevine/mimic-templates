package recipes

import (
	"strings"
	"testing"
)

func TestPinlineDualsRunTheWayThePSDNamesThem(t *testing.T) {
	colors := pinlineColors()
	// The PSD's own dual layers are GW, RW and GU, so those run from the first
	// letter's color on the left to the second's on the right
	cases := map[string][2]string{
		"wg": {"g", "w"}, "wr": {"r", "w"}, "ug": {"g", "u"},
		"wu": {"w", "u"}, "ub": {"u", "b"}, "rg": {"r", "g"},
	}
	for key, order := range cases {
		want := pinline[order[0]] + ">" + pinline[order[1]]
		if colors[key] != want {
			t.Errorf("%s = %q, want %q", key, colors[key], want)
		}
	}
	if len(colors) != len(pinline)+10 {
		t.Errorf("%d tints, want the flat colors and ten duals", len(colors))
	}
	for key := range colors {
		if len(key) == 2 && strings.Index("wubrg", key[:1]) > strings.Index("wubrg", key[1:]) {
			t.Errorf("key %q is not in WUBRG order", key)
		}
	}
}
