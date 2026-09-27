package extract

import (
	"slices"
	"testing"
)

func TestSourceNames(t *testing.T) {
	single := Recipe{SourceFile: "normal.psd", Layers: []LayerRule{{ManifestName: "background"}}}
	if got, err := single.sourceNames(); err != nil || !slices.Equal(got, []string{""}) {
		t.Errorf("single source = %v, %v, want [\"\"]", got, err)
	}

	two := Recipe{
		Sources:   map[string]string{"front": "f.psd", "back": "b.psd"},
		Layers:    []LayerRule{{ManifestName: "bg", Source: "front"}, {ManifestName: "bg_back", Source: "back"}},
		TextBoxes: map[string]TextBoxRule{"title": {Source: "back"}},
		ArtSlot:   ArtSlotRule{Source: "front"},
	}
	if got, err := two.sourceNames(); err != nil || !slices.Equal(got, []string{"back", "front"}) {
		t.Errorf("two sources = %v, %v, want [back front]", got, err)
	}

	two.Layers = append(two.Layers, LayerRule{ManifestName: "stray"})
	if _, err := two.sourceNames(); err == nil {
		t.Error("a layer naming no source in a multi-source recipe should fail")
	}
}
