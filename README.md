# mimic-templates

This repo produces the frame templates the [mimic](https://github.com/odevine/mimic)
app renders cards with. Each template ships as one versioned `.mimic` bundle,
published as a GitHub Release asset. The app reads `index.json` to discover
templates and downloads a bundle over HTTPS.

The rendering engine lives in the mimic repo. This repo holds the producer
pipeline: the per-template manifests, the packer, the catalog builder, and the
one-time extraction tool.

## The manifest is the source of truth

A template is two things: a `manifest.json` that describes the layout and
geometry, and a set of layer PNGs. The PNGs come out of a source PSD once and
then stay stable. The manifest is edited by hand: the PSD's layer bounds are
only a starting point, and the app needs exact bounds to center text, so the
real values are tuned in the manifest itself.

So `manifest.json` is tracked and reviewable, and a routine change to a template
is a manifest edit. The layer PNGs are not regenerated on release; they are
carried over from the previous bundle. The extraction tool runs only to seed a
new template or to re-cut art from a changed PSD.

## The `.mimic` bundle

A `.mimic` file is a zip archive with a custom extension, the same construction
as `.jar`, `.docx`, and `.usdz`. Using a zip gives random access to any single
entry through the central directory, so the engine can open one layer PNG
without reading the rest, and it needs no dependency beyond the Go standard
library's `archive/zip`. `unzip normal.mimic` inspects one by hand.

```
normal.mimic  (zip)
├── bundle.json        format + version header, read first
├── manifest.json      layout and geometry
├── background/*.png
├── pinlines_textbox/*.png
└── ...                one directory per layer group
```

`bundle.json` is small and read before the manifest, so the app can check
compatibility before parsing anything larger:

```json
{
  "format": 1,
  "template": "normal",
  "version": "1.2.0",
  "minEngine": "0.3.0"
}
```

`format` is the container schema version, bumped only when the bundle structure
changes incompatibly. `version` is the template's own semver, independent of the
engine and of other templates. `minEngine` lets a bundle refuse to load in an
engine too old to render it, and `pack` derives it from the engine version this
repo pins in `go.mod`.

PNG entries are stored uncompressed. PNG pixel data is already DEFLATE-compressed
inside the file, so zipping it again spends CPU on every read for almost no size
change, and a stored entry stays cheaply seekable. The two JSON files are
deflated.

## Tools

`tools/pack` builds a `.mimic` bundle, in one of two modes. Repack mode
(`-from-bundle`) carries the layer PNGs over from a previous bundle, verbatim,
and pairs them with the tracked manifest. This is the routine path: it needs no
PSD and runs in CI. Bootstrap mode (a loose directory argument) reads the
manifest and PNGs from a directory the extraction tool wrote, for a template's
first bundle or an art re-cut. In both modes it imports the engine's `template`
package and validates the manifest the way the running engine will, and repack
also checks that every layer the manifest points at is present in the carried
PNGs.

`tools/reindex` rebuilds `index.json` from the `.mimic` assets attached to this
repo's releases, recording each bundle's download URL, size, SHA-256, and the
`minEngine` it declares. It runs in CI on release.

`tools/psdextract` converts a Proxyshop-style PSD into `manifest.json` and the
layer PNGs, driven by a per-template recipe under `tools/psdextract/recipes`. It
is a bootstrap tool: run it once to seed a template, or with `-png-only` to
re-cut the art while leaving the tuned manifest alone.

## Repository layout

```
templates/<name>/bundle.json     tracked: format, version, minEngine
templates/<name>/manifest.json    tracked: layout and geometry (the edit surface)
templates/<name>/<group>/*.png    gitignored: seeded by psdextract, carried in bundles
psd/<name>.psd                    gitignored: the source PSD (bootstrap only)
index.json                        the catalog the app fetches
```

The source PSDs and the layer PNGs stay out of git because they are large
binaries and not the shipped artifact. The manifests are tracked so the geometry
is reviewable and diffable. The durable home for the PNGs is each published
bundle, which is where a repack reads them from.

## Releasing a template

release-please owns versioning and tagging. Each template is a component, so a
conventional-commit history produces a per-template release pull request and a
tag like `normal/1.2.0`.

**Routine release (a manifest change).** Edit `templates/<name>/manifest.json`,
commit with a conventional message, and open a PR. When the release PR
release-please opens is merged, CI creates the tag and Release, repacks a bundle
from the previous release's PNGs plus the edited manifest, uploads it, and
rebuilds `index.json`. No PSD or local asset set is involved.

**First release of a new template (bootstrap).** CI has no previous bundle to
carry PNGs from, so the first bundle is built locally from the PSD:

1. Put the source PSD at `psd/<name>.psd` and add a recipe under
   `tools/psdextract/recipes`.
2. `make extract TEMPLATE=<name>` seeds `manifest.json` and the PNGs, then tune
   the manifest.
3. `make pack TEMPLATE=<name>` builds the bundle.
4. `gh release create <name>/<version> build/<name>.mimic` publishes it, then run
   the `index` workflow to catalog it.

After that first release, the template follows the routine path.

**Re-cutting art.** If the source PSD's pixels change, `make assets
TEMPLATE=<name>` rewrites the PNGs without touching the tuned manifest, then
`make pack` builds a bundle to release.

Adding a template is one entry in `.release-please-manifest.json`, one package
block in `release-please-config.json`, one recipe, and a
`templates/<name>/bundle.json`.

## App download and update flow

The app treats `index.json` as the catalog and a local cache directory as its
installed set. It fetches the index, shows the templates whose `minEngine` the
running engine satisfies, downloads a chosen bundle once into the cache, verifies
its SHA-256, and renders from the cache. Rendering never reads from the network.

```json
{
  "schema": 1,
  "templates": [
    {
      "name": "normal",
      "latest": "1.2.0",
      "versions": [
        {
          "version": "1.2.0",
          "minEngine": "0.3.0",
          "url": "https://github.com/odevine/mimic-templates/releases/download/normal/1.2.0/normal.mimic",
          "size": 208412672,
          "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
        }
      ]
    }
  ]
}
```
