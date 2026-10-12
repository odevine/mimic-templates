# A template is a folder under templates/ in the source form, with a template.json
# and a file for each layout beside the PNGs of its parts. The engine's validate
# and pack commands check it and build its bundle, at the engine release named by
# ENGINE_VERSION, which is the one CI uses.
#
#   make validate TEMPLATE=m15     check a source folder and report every problem
#   make bundle   TEMPLATE=m15     build build/m15.mimic from the source folder
#   make release  TEMPLATE=m15     upload that bundle to the release release-please made
#
# TEMPLATE selects the template.

TEMPLATE ?= m15
ENGINE_VERSION ?= v0.18.0
VERSION = $(shell jq -r .version templates/$(TEMPLATE)/template.json)

.PHONY: validate bundle release

# validate checks a template in the source form, which is stricter than the app is
validate:
	GOWORK=off go run github.com/odevine/mimic/engine/cmd/validate@$(ENGINE_VERSION) templates/$(TEMPLATE)

# bundle builds a template into a bundle, with the version in its template.json.
# The same folder always builds the same bytes
bundle:
	mkdir -p build
	GOWORK=off go run github.com/odevine/mimic/engine/cmd/pack@$(ENGINE_VERSION) templates/$(TEMPLATE) -o build/$(TEMPLATE).mimic

# release uploads a locally built bundle to the tag release-please created and
# triggers the catalog rebuild. Routine releases run in CI, and this is the manual
# path for one cut from your machine. Build the bundle first with bundle
release:
	gh release upload $(TEMPLATE)/v$(VERSION) build/$(TEMPLATE).mimic --clobber
	gh workflow run index.yml
