# Pet skill UI

The tier artwork and equipment follow-ups are documented below.

- Sixty original painted icons match the native character skill style:
  basic I, intermediate II and unchanged III for each of20families. Tree,
  numbered slots and drag preview select the same book variant. Source, cell
  layout and publishing: `assets/README.md`.
- Removed row-side captions/status. Family symbols on the left expose
  controls, unlock rules, shared recharge, restrictions and separate hit counts.
- All twenty families use compact effect/stat lines with native gold diamond
  sections. Requirements stay at the bottom, individually red when unmet.
  Unlearned books retain effects. General controls no longer fill skill help.
- Top-right native-style badges show Resolve, bear retaliation hits, crow
  burst progress and Mark Prey's attacks remaining. Owner retaliation hits
  remain separate and are shown in the row information tooltip.
- Port-only private 0x3FB6: LE u32 pet GID, then u8 attacks modulo40, bear
  hits0..9, owner hits0..9, Resolve0..10. The existing pet tick sends only
  changed values; opening the tree requests a fresh snapshot. Flag off sends
  none. Decoder rejects malformed packets, ignores other pets and preserves
  cooldown receipt time. No client prediction, persistence or global GID lookup.
- Fixed existing next-rank preview: include worn +skills, so learning no longer
  appears to reduce a skill's current effect. Covered with actual worn gear.

## Layout follow-up

The three books now occupy adjacent native cells at36px pitch. Five unused
cells use the character tree's `skl_mastery_nothing.png` gray placeholders;
locked books use `skl_mastery_disable.png`, revealed unlearned books use its
128/255 preview alpha. Locked skill descriptions remain available. Restored
character skill cells to their original36px pitch as well: the first icon UI
change had accidentally changed their spacing through a shared text replacement.

## Tier artwork follow-up

Each book now has distinct art: simple I, intermediate II, original III.
The original `pet-skills.png` remains byte-identical; two additional20-cell
atlases provide the simpler variants. Tree, quickslot and dragged icon select
the same book variant. The left row-help icon uses the family symbol instead
of a question mark; it retains the same accessible name and tooltip.

## Gear artwork and skill heading follow-up

Removed the pet-level label above skill rows; skill points and the actual
learning requirements remain. Replaced procedural pet gear placeholders with
27 original painted inventory icons resembling native character equipment.
Three3x3 source sheets (`96x96`) contain distinctD1..D9 collar/harness/charm designs.
The existing exporter/publisher keeps every item path, name, stat and drop
binding unchanged. No backend restart, migration or balance changes needed.
The asset test verifies27 unique32px RGBA exports; focused publication passed.
