# m15

The modern card frame, in the source form the engine's `pack` command builds a
bundle from. It has three layouts: `normal` for standard cards, `split` for
split cards, and `transform` for both faces of a transform card.

Each folder under `normal/`, `split/`, and `transform/` is one part of a frame.
A file's name is the material it draws for, such as `w.png` for white or `gold.png`
for a card of three or more colors. A card of two colors is blended from the two
single color images, so the folders hold no pair images.

`template.json` names the parts and what they are for, and the files in `layouts/`
place them. Release-please keeps the `version` in `template.json` up to date, and
the version here stays at 0.0.0 until the first release.

## Checking and building

From the root of this repository, with the engine release that CI uses:

```
make validate TEMPLATE=m15
make bundle TEMPLATE=m15
```

`validate` reports every problem it finds, and `bundle` writes `build/m15.mimic`,
the same bytes every time it is run on the same folder. The Makefile runs the
engine's `validate` and `pack` commands at the release named by `ENGINE_VERSION`.

## Credits

The frame art is cut from the M15 templates that
[Proxyshop](https://github.com/Investigamer/Proxyshop) distributes. They remain the
work of their original designers and are redistributed with attribution. The cuts,
blends, layouts, and manifest are licensed under CC BY-NC 4.0.
