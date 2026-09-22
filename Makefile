# The manifest is the source of truth and the layer PNGs are stable, so a
# routine release carries the PNGs over from the previous bundle and pairs them
# with the edited manifest (repack). That path needs no PSD and runs in CI. The
# PSD is needed only to bootstrap a new template or re-cut its art.
#
#   make repack TEMPLATE=normal    build a bundle from the tracked manifest
#   make extract TEMPLATE=normal   seed a new template from its PSD (once)
#   make assets  TEMPLATE=normal   re-cut just the PNGs from a changed PSD
#
# TEMPLATE selects the template; PSD_DIR is where the source PSDs live locally.

TEMPLATE ?= normal
PSD_DIR ?= psd
VERSION = $(shell jq -r .version templates/$(TEMPLATE)/bundle.json)
LATEST_URL = $(shell jq -r --arg n "$(TEMPLATE)" '.templates[]|select(.name==$$n)|.versions[0].url // empty' index.json 2>/dev/null)

.PHONY: extract assets pack repack release

# extract seeds a brand-new template from its PSD, writing both manifest.json
# and the layer PNGs into templates/$(TEMPLATE)/. The manifest is a starting
# point to hand-tune, and this overwrites any existing one, so use it only when
# first adding a template.
extract:
	go run ./tools/psdextract -template $(TEMPLATE) -assets templates -psd $(PSD_DIR)/$(TEMPLATE).psd

# assets re-cuts only the layer PNGs from the PSD, leaving the tuned manifest
# untouched. Use it when the source art changes.
assets:
	go run ./tools/psdextract -template $(TEMPLATE) -assets templates -psd $(PSD_DIR)/$(TEMPLATE).psd -png-only

# pack builds a full bundle from the local templates/$(TEMPLATE)/ directory
# (tuned manifest plus local PNGs). Used for a template's first release, or after
# re-cutting assets.
pack:
	mkdir -p build
	go run ./tools/pack templates/$(TEMPLATE) -version $(VERSION) -o build/$(TEMPLATE).mimic

# repack builds a bundle by carrying the PNGs over from the latest published
# bundle and pairing them with the tracked manifest. This is what CI does on a
# release; run it locally to test a manifest change without the PSD.
repack:
	mkdir -p build
	@test -n "$(LATEST_URL)" || { echo "no published bundle for $(TEMPLATE) in index.json; use 'make pack' to bootstrap the first release"; exit 1; }
	go run ./tools/pack -from-bundle "$(LATEST_URL)" -manifest templates/$(TEMPLATE)/manifest.json -version $(VERSION) -o build/$(TEMPLATE).mimic

# release uploads a locally built bundle to the tag release-please created and
# triggers the catalog rebuild. Build the bundle first with pack or repack.
# Routine releases run in CI; this is the manual path, for a first release or a
# release cut from your machine.
release:
	gh release upload $(TEMPLATE)/$(VERSION) build/$(TEMPLATE).mimic --clobber
	gh workflow run index.yml
