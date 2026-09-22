# mimic-templates

This repo produces the frame templates the [mimic](https://github.com/odevine/mimic)
app renders cards with. Each template ships as one versioned `.mimic` bundle,
built from a source PSD and published as a GitHub Release asset. The app reads
`index.json` to discover templates and downloads a bundle over HTTPS.

The rendering engine lives in the mimic repo. This repo holds only the producer
pipeline: the extraction tool, the packer, the per-template manifests, and the
release configuration.

## The `.mimic` bundle

A `.mimic` file is a zip archive with a custom extension, the same construction
as `.jar`, `.docx`, and `.usdz`. Using a zip gives random access to any single
entry through the central directory, so the engine can open one layer PNG
without reading the rest, and it needs no dependency beyond the Go standard
library's `archive/zip`. `unzip normal.mimic` inspects one by hand.

The layout mirrors the loose extraction directory, with a header added:

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

`tools/psdextract` converts a Proxyshop-style PSD into `manifest.json` and the
layer PNGs, driven by a per-template recipe under `tools/psdextract/recipes`. It
is a local dev tool: run it once per template against a local PSD.

`tools/pack` wraps a loose extracted directory into a `.mimic` bundle. It
imports the engine's `template` package and validates the manifest the same way
the running engine will, so a bundle that packs is one the engine accepts.

`tools/reindex` rebuilds `index.json` from the `.mimic` assets attached to this
repo's releases, recording each bundle's download URL, size, SHA-256, and the
`minEngine` it declares. It runs in CI on release.

## Repository layout

```
templates/<name>/bundle.json     tracked: format, version, minEngine
templates/<name>/manifest.json    tracked: layout and geometry
templates/<name>/<group>/*.png    gitignored: rebuilt from the PSD
psd/<name>.psd                    gitignored: the source PSD
index.json                        the catalog the app fetches
```

The source PSDs stay out of git, as they did in the engine repo, because they
are large binaries and not the shipped artifact. The manifests are tracked so
the geometry is reviewable and diffable; only the PNGs and the PSDs are ignored.

## Releasing a template

release-please owns versioning and tagging. Each template is a component, so a
conventional-commit history produces a per-template release pull request and a
tag like `normal/1.2.0`. Because the source PSDs are not in git, CI cannot build
bundles; the version bump and tag happen in CI, and the bundle is built locally
and uploaded to the release.

1. Land conventional commits describing the change to a template.
2. Merge the release pull request release-please opens. That creates the tag and
   an empty GitHub Release, and bumps `templates/<name>/bundle.json`.
3. With the source PSD in `psd/<name>.psd`, run `make release TEMPLATE=<name>`.
   It extracts, packs, uploads the bundle to the tag, and triggers the index
   rebuild.

Adding a template later is one entry in `.release-please-manifest.json`, one
package block in `release-please-config.json`, one recipe under
`tools/psdextract/recipes`, and a `templates/<name>/bundle.json`.

## App download and update flow

The app treats `index.json` as the catalog and a local cache directory as its
installed set. It fetches the index, shows the templates whose `minEngine` the
running engine satisfies, downloads a chosen bundle once into the cache, verifies
its SHA-256, and renders from the cache. Rendering never reads from the network.

`index.json` lists every template, its available versions, and each version's
download URL, size, checksum, and `minEngine`:

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
