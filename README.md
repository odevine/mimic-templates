<picture>
  <source media="(prefers-color-scheme: dark)" srcset=".github/templates-banner-dark.svg">
  <img src=".github/templates-banner-light.svg" alt="mimic">
</picture>

This repo holds the frame templates the [mimic](https://github.com/odevine/mimic)
app renders cards with. Each template is kept here in its source form and ships as
one versioned `.mimic` bundle, published as a GitHub Release asset. The app reads
`index.json` to discover templates and downloads a bundle over HTTPS.

The rendering engine lives in the mimic repo, and so do the commands that check a
template and build its bundle. This repo holds the templates, the workflows that
release them, and the tool that builds the catalog.

## A template

A template is a folder under `templates/` with a `template.json`, a file for each
layout in `layouts/`, and a folder of PNGs for each part of the frame. A part is
named for what it is, and each PNG in its folder is named for the material it
draws for, such as `w.png` for white or `gold.png` for a card of three or more
colors. A card of two colors is blended from the two single color images.

```
templates/m15/
├── template.json      id, version, credits, features, and the parts
├── layouts/           normal.json, split.json, transform.json
├── normal/background/ w.png u.png b.png r.png g.png gold.png ...
└── ...                one folder for each part
```

The PNGs are tracked in git. `templates/m15` is the only template today, with
three layouts: `normal` for standard cards, `split` for split cards, and
`transform` for both faces of a transform card. Its README covers its credits.

`validate -schema template` and `validate -schema layout`, from the commands
below, print the JSON Schema of `template.json` and of a layout file, for an editor
that checks them as they are written.

## Checking and building

The engine provides the commands, and the Makefile runs them at the engine release
named by `ENGINE_VERSION`, which is the one CI uses.

```
make validate TEMPLATE=m15
make bundle TEMPLATE=m15
```

`validate` reports every problem it finds in one pass, and is stricter than the app
is. `bundle` writes `build/m15.mimic`, with the version in `template.json`, and the
same folder always builds the same bytes.

## The `.mimic` bundle

A `.mimic` file is a zip archive with a custom extension, the same construction as
`.jar`, `.docx`, and `.usdz`. A zip gives random access to any one entry through its
central directory, so the engine can open one image without reading the rest.
`unzip m15.mimic` inspects one by hand.

```
m15.mimic  (zip)
├── bundle.json        format, id, version, and minEngine, read first
├── manifest.json      the layouts, parts, and features, with nothing left to resolve
└── assets/<hash>.png  every image, named by the hash of its content
```

`bundle.json` is small and read before the manifest, so the app can check
compatibility before parsing anything larger:

```json
{
  "format": 2,
  "id": "mimic.m15",
  "version": "0.1.0",
  "minEngine": "0.18.0"
}
```

`format` is the container version, which changes only when the bundle structure
does, and the app lists only bundles in a format it reads. `id` is the template's
name, in the form `owner.name`, and `version` is the template's own version.
`minEngine` is the oldest engine that renders it.

The packer cuts each part to the pixels it uses, and names each image by the hash
of its content, so two identical images are stored once. PNG entries are stored
uncompressed, since a PNG is compressed already, and the two JSON files are
deflated.

## Tools

`tools/reindex` rebuilds `index.json` from the `.mimic` assets attached to this
repo's releases. For each bundle it records the download URL, size, SHA-256,
container format, and the `minEngine` it declares. For each template it records the
description and the card shapes read from the newest bundle's manifest, so the app
can describe a template before it is installed. It runs in CI on release.

## Repository layout

```
templates/<name>/             a template in the source form
index.json                    the catalog the app fetches
tools/reindex/                the catalog builder
.github/workflows/            CI, the template checks, releases, and the reindex
```

## Releasing a template

release-please owns versioning and tagging. Each template is a component, so a
conventional-commit history produces a per-template release pull request and a tag
like `m15/v0.2.0`.

A release starts from a change to the template, such as a new image or an edited
layout. Commit it with a conventional message and open a PR. The `templates` workflow
validates every template and packs it twice to check the bundle is the same bytes.
When the release PR release-please opens is merged, CI updates the `version` in
`template.json`, creates the tag and the Release, packs the template from its
folder, uploads the bundle, and rebuilds `index.json`.

Adding a template is a folder under `templates/` and one package block in
`release-please-config.json`, with a `component` and an `extra-files` entry that
points at the `version` in its `template.json`. Nothing needs to be built locally.

## App download and update flow

The app treats `index.json` as the catalog and a local cache directory as its
installed set. It fetches the index, shows the versions in a format it reads and
whose `minEngine` the running engine satisfies, downloads a chosen bundle once into
the cache, verifies its SHA-256, and renders from the cache. Rendering never reads
from the network.

```json
{
  "schema": 2,
  "templates": [
    {
      "name": "mimic.m15",
      "description": "The modern card frame, with single color art blended for pairs",
      "shapes": [{ "role": "single", "kind": "standard" }],
      "latest": "0.1.0",
      "versions": [
        {
          "version": "0.1.0",
          "format": 2,
          "minEngine": "0.18.0",
          "url": "https://github.com/odevine/mimic-templates/releases/download/m15/v0.1.0/m15.mimic",
          "size": 234515263,
          "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
        }
      ]
    }
  ]
}
```

Bundles of the earlier templates, `normal`, `split`, and `transform`, are still
listed with a format of 1. Earlier versions of the app read them, and the current
one leaves them out.
