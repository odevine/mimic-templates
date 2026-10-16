# pw-borderless-family

A full-bleed, borderless planeswalker frame, converted from Proxyshop's own
borderless planeswalker PSDs. Three members: `pw_extended` for a
single-faced planeswalker, and `proxyshop-pw-mdfc-extended` and
`proxyshop-pw-transform-extended` for both faces of a planeswalker modal
double-faced or transform card. Kept apart from the bordered planeswalker
in `m15-family`, since "no border, full bleed art" reads as a distinctly
different look, the same way Extended/Borderless normal cards are kept
separate from plain normal.

Converted, not hand-kept: the engine's `psdconvert` command read each
member's own Proxyshop PSD into a template of its own, and `tplmerge`
combined the three into the one folder here. The recipe for each is
checked in at `recipes/pw-borderless-family/`, named by the id
`psdconvert` was given (`proxyshop.pw-extended`,
`proxyshop.pw-mdfc-extended`, `proxyshop.pw-transform-extended`).

`template.json` names the parts and what they are for, and the files in
`layouts/` place them. `version` stays at 0.0.0 and the template is not yet
registered with release-please: a planeswalker's loyalty abilities need
sections and `template.PlacedRow.FirstLineCenter`, neither in a released
engine yet (odevine/mimic#134). That lands first; registering this
template follows once an engine release carries it, the same two-step
`split` went through (see `6ffa8fc` in this repository's history).

## Checking and building

Once registered, from the root of this repository, with the engine release
that covers the needed additions:

```
make validate TEMPLATE=pw-borderless-family
make bundle TEMPLATE=pw-borderless-family
```

For now, with a local engine checkout that has them:

```
GOWORK=off go run github.com/odevine/mimic/engine/cmd/validate templates/pw-borderless-family
GOWORK=off go run github.com/odevine/mimic/engine/cmd/pack templates/pw-borderless-family
```

## Credits

The frame art is cut from the borderless planeswalker templates that
[Proxyshop](https://github.com/Investigamer/Proxyshop) distributes. They remain the
work of their original designers and are redistributed with attribution. The cuts,
blends, layouts, and manifest are licensed under CC BY-NC 4.0.
