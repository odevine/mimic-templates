// Package recipes holds one hand-authored Recipe per source template. main
// selects one by name. There is no dynamic discovery: the set of templates is
// small, known and finite, so a plain map is the right amount of structure.
package recipes

import (
	"sort"

	"github.com/odevine/mimic-templates/tools/psdextract/extract"
)

// registry maps a template name to its recipe constructor
var registry = map[string]func() extract.Recipe{
	"normal": Normal,
}

// Get returns the recipe for name, and whether it exists
func Get(name string) (extract.Recipe, bool) {
	build, ok := registry[name]
	if !ok {
		return extract.Recipe{}, false
	}
	return build(), true
}

// Names lists the known template names, sorted
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
