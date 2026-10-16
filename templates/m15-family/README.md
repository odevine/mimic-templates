# m15-family

The modern M15 card frame across every card shape converted from Proxyshop's
own M15-style PSDs: standard cards, split, transform (front and back),
modal double-faced cards (front and back), planeswalker, planeswalker MDFC
and transform (front and back), adventure, leveler, mutate, prototype,
battle, saga, and class.

Unlike `m15`, which is hand-kept in the source form, this template is
converted: the engine's `psdconvert` command reads each shape's own
Proxyshop PSD into a template of its own, and `tplmerge` combines those into
the one folder here, renaming a part or layout name two sources define
differently instead of letting one silently win. The recipe `psdconvert`
followed for each member PSD is checked in at `recipes/m15-family/`, one
file per member, named by the id `psdconvert` was given
(`proxyshop.normal`, `proxyshop.transform`, `proxyshop.vertical-mode`, and
so on).

This folder is the merged result, not a one-command rebuild yet: turning
the recipes back into this folder today means running `psdconvert` once per
recipe and `tplmerge merge` on the results by hand, the way they were built.
Wiring that into `make convert`/`make check-convert` the way `m15` already
has them for a single recipe is tracked as a follow-up.

`template.json` names the parts and what they are for, and the files in
`layouts/` place them, eighteen of them, one per shape above. `version`
stays at 0.0.0 and the template is not yet registered with release-please:
it needs sections (a saga's chapters, a class's levels, a leveler's tiers,
a planeswalker's loyalty abilities) and the psdconvert/tplmerge tooling
itself, neither of which is in a released engine yet (odevine/mimic#134,
and the PR after it that adds the psdconvert tool). Both land first;
registering this template follows once an engine release carries them, the
same two-step `split` went through (see `6ffa8fc` in this repository's
history).

## Checking and building

Once registered, from the root of this repository, with the engine release
that covers the needed additions:

```
make validate TEMPLATE=m15-family
make bundle TEMPLATE=m15-family
```

For now, with a local engine checkout that has them:

```
GOWORK=off go run github.com/odevine/mimic/engine/cmd/validate templates/m15-family
GOWORK=off go run github.com/odevine/mimic/engine/cmd/pack templates/m15-family
```

## Credits

The frame art is cut from the M15-style templates that
[Proxyshop](https://github.com/Investigamer/Proxyshop) distributes. They remain the
work of their original designers and are redistributed with attribution. The cuts,
blends, layouts, and manifest are licensed under CC BY-NC 4.0.
