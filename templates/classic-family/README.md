# classic-family

The old rounded, mana-colored border frame, converted from Proxyshop's own
classic-frame PSDs. Two layouts: `normal_classic` for standard cards and
`basic_classic` for basic lands. Most other card shapes (planeswalker,
saga, MDFC, mutate, and so on) didn't exist yet during the old-border era
of Magic, so there is no classic-frame PSD for them to add here.

Converted, not hand-kept: the engine's `psdconvert` command read each
member's own Proxyshop PSD into a template of its own, and `tplmerge`
combined the two into the one folder here. The recipe for each is checked
in at `recipes/classic-family/proxyshop.normal-classic.json` and
`recipes/classic-family/proxyshop.basic-classic.json`.

`template.json` names the parts and what they are for, and the files in
`layouts/` place them. `version` stays at 0.0.0 and the template is not yet
registered with release-please: it draws the classic frame's "Illus. "
artist credit with `TextStyle.Prefix`, which is not in a released engine
yet (odevine/mimic#134). That lands first; registering this template
follows once an engine release carries it, the same two-step `split` went
through (see `6ffa8fc` in this repository's history).

## Checking and building

Once registered, from the root of this repository, with the engine release
that covers the needed additions:

```
make validate TEMPLATE=classic-family
make bundle TEMPLATE=classic-family
```

For now, with a local engine checkout that has them:

```
GOWORK=off go run github.com/odevine/mimic/engine/cmd/validate templates/classic-family
GOWORK=off go run github.com/odevine/mimic/engine/cmd/pack templates/classic-family
```

## Credits

The frame art is cut from the classic-frame templates that
[Proxyshop](https://github.com/Investigamer/Proxyshop) distributes. They remain the
work of their original designers and are redistributed with attribution. The cuts,
blends, layouts, and manifest are licensed under CC BY-NC 4.0.
