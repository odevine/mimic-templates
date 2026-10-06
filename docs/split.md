# Split template spec

The `split` template draws the classic split card: one portrait frame holding two halves, each with its own name, mana cost, art, type line, and rules text, printed rotated a quarter turn so the card is read sideways. This document covers what the source PSD contains, what the engine cannot do yet, and the order to build it in.

The engine today classifies a Scryfall `split` layout as `RoleSplit` and `KindStandard`, but no template registers it, so every split card reports an `*UnsupportedError`. The frame itself is two copies of the normal frame laid side by side, which means most of the manifest work is familiar. The engine work is the part that is new.

## The source PSD

`split.psd` is a Proxyshop split template at 4440x3264. That is the card's 3264x4440 canvas turned on its side, so the PSD is the reading view: legal text runs vertically down the left edge, and the card is shown rotated a quarter turn clockwise from how it sits upright. The finished PNG is the PSD rotated a quarter turn counterclockwise, which lands at 3264x4440, the same size as `normal` and `transform`. At 3264 pixels across, the manifest's native resolution is 1200 DPI.

The `Left` and `Right` groups hold the two halves. Their geometry is identical apart from a constant 1912 pixel shift in X, so one half layout serves both.

| Piece           | Size and position in `Left`  | `Right` is at |
|-----------------|------------------------------|---------------|
| Art frame       | 1623x1127 at 547,601         | x + 1912      |
| Card name       | 779x140 at 576,395           | x + 1913      |
| Mana cost       | 841x124 at 1339,380          | x + 1912      |
| Type line       | 462x98 at 578,1794           | x + 1912      |
| Rules textbox   | 1629x1045 at 544,1939        | x + 1912      |
| Rules (fuse)    | 1629x870 at 544,1939         | x + 1912      |
| Expansion mark  | 116x108 at 2040,1780         | x + 1912      |

The colored frame layers live once at the top level and are not duplicated per half. `Background` carries 16 variants (gold, the five mono colors, and the ten dual pairs), each a half-sized cut at 427,239. `Textbox` carries the same 16. `Name & Title Boxes` carries gold, land, colorless, artifact, and the five mono colors. The `Left` and `Right` groups only hold a hidden reference layer for each of these, and a mask that clips the half.

Three things in the PSD are not plain per-color art. The `Pinlines` shapes (`Textbox`, `Middle Lines`, `Twins`) are vector layers with layer effects, and their pixels are black, so the PSD has no per-color pinline art to cut. The color variants in `Name & Title Boxes` and in the fuse bar are clipping layers over a base shape, so they only read correctly once clipped. And the `Fuse` group, its `Border Fuse` strip, and the fuse text are hidden by default: they are the bar that spans both halves along the bottom of a fuse card.

## What Scryfall sends

Scryfall has 137 cards in the `split` layout, and they are four different frames. Fifty-eight are classic splits, 22 carry the Fuse keyword, 27 carry Aftermath, and 30 are Rooms. Only the first two are this template. Aftermath prints its second half rotated the opposite way, and a Room is an enchantment frame with a door on each half, so both would render wrongly in this frame while still classifying as `split`.

Within the classic and fuse cards there are no lands, artifacts, creatures, legendary cards, or colorless halves, so the missing artifact, land, and colorless variants in the PSD's background and textbox groups do not matter. Twenty-five of them are hybrid, such as Assure // Assemble, and the PSD's dual variants cover those.

The card data has three properties that shape the engine work. The faces in `card_faces` have no `colors` array (it is null for Fire // Ice), and no `image_uris`, so a half's frame color and art both have to come from somewhere else. The top level carries a single `art_crop` for the whole card. And on a fuse card the Fuse reminder line is repeated at the end of each face's `oracle_text`, though the card prints it once.

## Engine gaps

Each gap below is a change in the `mimic` repo. They are listed in the order they depend on each other.

**The engine must render in one orientation and deliver in another.** A manifest is a single canvas, and the render loop draws text upright into it. The split frame is authored in the landscape reading view, with half text upright, then turned for delivery. A manifest `rotate` field (degrees clockwise, multiples of 90, default 0) turns the finished composite after the last layer, and `render.Template.Render` applies it before returning. `Manifest.Resolution` and `Presets` must report the rotated size, since the UI and `mpcfill` read the output dimensions from them, and `NativeDPI` must be stated in the manifest (`dpi: 1200`) because inferring it from the 4440 pixel width would give 1632. Scaling still happens on the authored canvas, so a preview at a lower DPI lays out the same way.

**A layer and a text box must be able to belong to one half.** `LayerSpec` and `TextBoxSpec` gain a `half` field, 0 for the whole card (today's behavior), 1 for the first face, and 2 for the second. A layer or box with a half evaluates its `condition`, its `colorSlot`, and its text against that face, not the card. In practice this means `render.Render` derives `frame.Keys` once per half (`frame.DeriveFace` on `Data.Face(i)`), `layerNode` and `AvoidRect` take the keys for the layer's half, and `ResolveTextBoxes` dedupes on the pair of logical box and half so `title` can resolve once per half. The text boxes in the manifest then differ only by `half` and X, and `card.TextFor` already fills each from the face's own fields.

The face data needs one fix first. `Data.Face(i)` copies the face's `Colors`, which are empty for a split, so every half would frame as colorless. `toData` should derive a split face's colors from its mana cost when Scryfall omits them, counting each hybrid and Phyrexian symbol toward both of its colors and leaving generic and X cost alone.

**One half-sized cut should be placeable at either half.** Frame layers are authored document-sized and always placed at the origin (`LoadLayer`). Split would otherwise ship every colored layer twice, once per half, and the two copies would only differ in position. A layer `x` and `y` offset places the PNG at that point, so the extractor cuts each layer to its bounds and the manifest places it at the left half's origin for `half: 1` and 1912 pixels over for `half: 2`. `Scaled` and `LoadLayer` need to carry the offset through the DPI scale. The template would work with document-sized PNGs per half at roughly twice the bundle size, so this is an economy and the first pass can fall back to that if it needs to.

**Art is one image drawn across two slots.** Scryfall's split `art_crop` is a single landscape image holding both halves side by side, already upright in the reading view (Fire on the left, Ice on the right). The engine needs no second download and no second art field. `Manifest.Art` becomes a list of slots, one per half, and the renderer cuts the one image into a left and right half and fits each into its slot with `FitArt`. Each half of the crop has the same aspect ratio as an art frame (about 1.44), so the cut is clean. The art is composited in the reading view before `rotate` turns it with the rest of the frame, so it needs no rotation of its own. The draw order is unchanged: each slot goes in directly above the layer it names. A manifest with a single `art` slot behaves as it does today.

**Fuse needs a bar, a condition, and a text move.** A fuse card draws the bar from the `Fuse` group across the bottom of both halves, shortens each half's rules box from 1045 to 870 pixels, and prints the Fuse reminder text once in the bar. That needs a `fuse` condition in `frame.Keys.ConditionMet`, derived from the card's keywords, which `card.Data` does not carry. Adding a `Keywords []string` field to `Data`, mapped from Scryfall's `keywords`, serves this and the Aftermath check below, and the bulk path picks it up through `FromScryfallJSON`. The text boxes then use `condition: "fuse"` to offer the shorter rules box, which `ResolveTextBoxes` already prefers over an unconditional one. The bar blends the pinline colors of its two halves, each half's letters in order, so a red half beside a white one is the key `rw`. Two slots carry it: `fuse` for the textbox, which is gold once the blend passes three colors, and `fuse_pinlines` for the pinline, which blends up to four. A layer marked `colorBlend` with no variant for the whole key draws each letter's variant across the bar, with the seams of the printed card, so the PSD's gold and five single colors cover every pairing. Last, a half's `oracle` text must drop its Fuse reminder line when the card is a fuse card, and a `fuse` text box draws it once at the card level.

**The legal line is drawn after the rotation.** The set, artist, collector, and copyright lines run vertically in the reading view, and the text renderer only lays out horizontal text. Rather than add rotated text, a text box can be marked `"space": "output"`, which makes the renderer composite it after the `rotate` step in the delivered canvas's own coordinates. The legal boxes then reuse the geometry `normal` already has for them, which keeps the three templates' bottom edges consistent. The general alternative, a per-box `rotate` that turns a box in place before the composite, would also serve Aftermath later, and is worth choosing instead if that template is planned next.

**Aftermath and Rooms must not classify as `split`.** `Classify` returns `RoleSplit` for every `split` layout, so a Room or an Aftermath card would pass `Supports` and render in the wrong frame. The classifier should read the new `Keywords` for Aftermath and the type line for Room, and return a distinct `RoleAftermath` and `KindRoom`. Neither is claimed by any template yet, so they fall out as unsupported the way a saga does today. `knownRoles` and `knownKinds` gain the two entries.

**The placeholder assets and tests need to cover the new paths.** `render/placeholder.go` generates a manifest and layers for the no-assets case, and `render/face_test.go` and `template/mirror_test.go` show the pattern for testing a face-scoped feature. The rotation, half scoping, the layer offset, and the output-space boxes each get a test against placeholder art, and the face-colors derivation gets cases for mono, gold, and hybrid halves.

## Template repo work

None of this blocks on the engine except the final render check, so it can proceed in parallel.

**Seed the recipe.** Add `tools/psdextract/recipes/split.go` reading `psd/split.psd`, with layer rules for `Background`, `Textbox`, `Name & Title Boxes`, the fuse bar, `Border`, `Art Outline`, and the shadows, in the style of `transform.go`. `Art Outline` is the visible frame line around each half's art. It is cut once from `Left` as a half-sized layer and placed above the art slot for both halves (`half: 1` and `half: 2`), so it is listed after the layer the art slot follows, which puts it on top of the art. The art slot and text box bounds come from the reference layers (`Art Frame`, `Textbox Reference`, and the text layers), taken from the `Left` half only.

**Teach the extractor to clip.** `extract` does not apply `Clipping` today (the transform recipe leaves its one clipped layer off for that reason). The `Name & Title Boxes` colors and the fuse bar colors are clipped to a base shape, so each needs its alpha multiplied by the base layer's before it is written.

**Synthesize the pinline colors.** The pinline shapes carry no color, so each variant is the black shape recolored with a palette. The palette can be sampled from `templates/normal/pinlines_textbox`, which already has the same 16 variants and so keeps split's lines consistent with the normal frame. Hybrid halves use the dual variants, which the palette needs to blend left to right the way normal's do.

**Add the template.** A `templates/split/bundle.json` at `0.1.0` with the new `minEngine`, an entry in `.release-please-manifest.json`, and a package block in `release-please-config.json` with a `last-release-sha`, as `transform` has. The first bundle is built locally (`make extract`, tune, `make pack`, `gh release create`) because CI has no previous bundle to repack from. `bundle.json` is in place with `minEngine` set to `0.13.0`, the engine minor the changes above are expected to ship in, so correct it if the release number differs. The release-please entry and config block wait until that engine is released and the `go.mod` pin moves to it, because registering the template earlier lets the next `feat(split)` commit open a release pull request for a bundle the pinned engine cannot draw.

## Manifest format

The recipe seeds `templates/split/manifest.json`, which is the contract the engine changes above are built to read. The engine's `template.Manifest` ignores the fields it does not know, so the manifest already packs and validates against the current engine, and renders correctly only once these are implemented.

**The authored canvas is the reading view.** `width` and `height` are 4440 and 3264, `dpi` is 1200, and `rotate` is 270, the degrees clockwise that stand the finished composite upright. The first half is at the bottom of the delivered card.

**Half layers come in pairs.** Each half layer is cut once at the first half's frame, a 1864x2781 rectangle at 425,237, and the manifest lists it twice, as `background_1` and `background_2`, with `half` set to 1 and 2 and `x` and `y` placing the cut at the half (the second is 1912 pixels right). Both entries point at the same PNGs. A layer with no `half` is document-sized and draws as it does today. The layers are, bottom to top, `background`, `pinlines_boxes`, `pinlines_twins`, `textbox`, `name_title_boxes`, `art_outline`, then the whole-card `border` and the fuse layers.

**Art is an `arts` list.** The manifest has `arts`, two slots sized to each half's art window, both with `after` naming `pinlines_boxes_2`, so the art sits above the background and below the plates. A single-faced manifest keeps its `art` object, and a manifest has one or the other.

**Half text boxes come in pairs too.** Each is keyed `title_1` and `title_2`, with `box` naming the logical box (`title`) and `half` the face it fills. The second differs from the first only by its `x`. `clearOf` names a logical box, and resolves to the same half's box, so `title_1` keeps clear of `mana_1`. The rules box has a second spec, `oracle_fuse`, with `condition: "fuse"` and the shorter height. The fuse reminder is one whole-card box, `fuse`, with the same condition, and the legal boxes (`artist`, `set`, `copyright`) carry `space: "output"`.

**The fuse layers blend.** `fuse_textbox` and `fuse_pinlines` have `colorSlot` set to `fuse` and `fuse_pinlines`, `colorBlend: true`, and variants for `gold` and the five single colors. They are cut to the bar and placed with `x` and `y` rather than drawn document-sized, since the blend works at the size of the bar.

## What the recipe made of the PSD

A few things in the PSD differ from the first reading of its layer tree, and the recipe follows the PSD's own settings and Proxyshop's stacking. The pinline shapes are unfilled, with a stroke effect that draws the visible ring: `Textbox` and `Middle Lines` strokes are 24 pixels inside their shapes, `Twins` is 30 pixels outside the title and type bars, and the fuse bar's is 24 pixels outside. The recipe reads those sizes from the PSD and draws the rings itself, as `pinlines_boxes` and `pinlines_twins`. Proxyshop stacks each half as background, textbox color, pinlines, then the name and title plates, so the plates sit on top of the pinlines and leave only the ring showing around them. The shapes are black in the PSD, so the recipe colors the rings from the normal frame's pinline colors, and a dual blends its two colors across the middle third of the half, left to right in the reading view, which matches the direction the PSD's own dual backgrounds run.

A pure hybrid half, such as Assure, takes the engine's colorless plate key, and a printed hybrid card draws that plate in a warm silver. The PSD's `Colorless` layer is a cooler blue-gray, and its `Land` layer matches the printed color, so the recipe files the `Land` layer under the `colorless` key. Sampling the type bars of the real Assure // Assemble against a render put the difference at under 2 on each channel afterward.

The plates carry Photoshop layer effects that are not pixels in any layer: an 8 pixel black outline outside the shape and an inner bevel (14 pixels, 120 percent, pressed in, lit from the lower left), and the fuse plate has its own bevel at 160 percent. The engine draws no layer effects, so the extractor bakes both into each plate variant, reading the sizes and angle from the PSD. The shading inside the textbox edge on a printed card is not in the PSD, so the recipe adds an inner shadow to the textbox fitted to a scan of the real Assure // Assemble: it falls on the edges facing the light, which are the top and right of the authored canvas, starting about 17 pixels in with a 3 pixel blur at 44 percent. Sampled across that edge, the render and the scan differ by less than 30 on a 0 to 255 scale at each point. The hidden `Shadows` layer is an outer drop shadow of the whole half and is also left off.

The legal line follows the PSD's visible `Set` and `Artist` layers, which sit on separate rows at the same left margin, with the copyright line on the set row. The PSD's `Collector` group is hidden, so the manifest has no collector box. The `Shadows` layer exists only in the right half and is left off.

The text boxes were measured from the PSD's text layers, and their sizes are the layers' sizes at the 1.113 scale the PSD applies (151 for the name, 174 for the cost, 102 for the type line, 150 for rules, 113 for the fuse line). They will want a pass against real renders once the engine can draw halves.

## Sequencing

The engine changes ship as one minor release, since the template needs every one of them and the classifier change is safe on its own. Within the engine the order is: the keywords and face-colors data changes, then classification, then `rotate` and `space: "output"`, then `half` scoping and the layer offset, then art, then fuse. Each step is testable against placeholder assets without the real PNGs. After the release the `go.mod` pin moves to it, the manifest is tuned against real renders, and `pack` derives `minEngine` from that pin.

Aftermath and Rooms are separate templates and out of scope here. Flip and meld cards are unclassified and stay that way.

## Open questions

Which half is face 0 should be checked against a physical card or a scan. The legal text sitting on the left of the PSD puts the reading view's left half at the bottom of the finished card, and this spec assumes the PSD's `Left` is Scryfall's first face. If a scan disagrees, only the `half` numbering in the manifest changes.

The layer offset (half-sized cuts placed at either half) and the legal line drawn in output space are both included in the plan above. The offset keeps the two halves pixel-identical and the bundle smaller, and output-space boxes are the smaller change now. If Aftermath becomes the next template, the per-box `rotate` is worth revisiting then, and both choices are cheap to change after the first release.
