# Building a template bundle needs the source PSD, which is large and not in
# git, so bundles are built here on the maintainer's machine rather than in CI.
# release-please owns the version and the tag; this builds the asset for the tag
# it created and triggers the catalog rebuild.
#
#   make release TEMPLATE=normal
#
# TEMPLATE selects the template; PSD_DIR is where the source PSDs live locally.

TEMPLATE ?= normal
PSD_DIR ?= psd
VERSION = $(shell jq -r .version templates/$(TEMPLATE)/bundle.json)

.PHONY: extract pack release

# extract writes manifest.json and the layer PNGs into templates/$(TEMPLATE)/.
# The manifest is tracked and reviewable; the PNGs are gitignored and rebuilt
# from the PSD on demand.
extract:
	go run ./tools/psdextract -template $(TEMPLATE) -assets templates -psd $(PSD_DIR)/$(TEMPLATE).psd

# pack wraps the extracted directory into a versioned .mimic bundle under build/.
pack:
	mkdir -p build
	go run ./tools/pack templates/$(TEMPLATE) -version $(VERSION) -o build/$(TEMPLATE).mimic

# release builds the bundle and attaches it to the tag release-please created,
# then asks CI to rebuild index.json now that the asset exists. Run it after the
# release pull request for this template has merged.
release: extract pack
	gh release upload $(TEMPLATE)/$(VERSION) build/$(TEMPLATE).mimic --clobber
	gh workflow run index.yml
