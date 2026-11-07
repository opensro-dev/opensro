# Penguin Blizzard reference pass — 2026-11-05

## Behavior

This is port-only pet gameplay, not native Silkroad behavior. The
[owner decision recorded on 6 November 2026](https://github.com/opensro-dev/opensro/pull/2625#issuecomment-6675542034)
delegates pet balance to the author and explicitly accepts the five-second,
five-hit design without a separate approval. The existing default-off
`SRO_ATTACK_PET_EXPANSION` setting still gates it.

Blizzard I–III now last five seconds. Damage lands at release and at approximately +1, +2, +3 and +4 seconds. The field fades by +5 seconds. Book III freezes eligible enemies on the fifth pulse only if they remained exposed to every pulse; normal resistance applies. There is no sixth pulse.

Per-pulse damage and the existing 20-second base cooldown are unchanged, so the additional fifth pulse increases total damage by 25%. Book I keeps its 5m radius; Books II/III keep 7m. The storyboard's 8m and 4s labels were not used to change those radii: the explicit duration request was five seconds. Target-centred gameplay is preserved.

## Visuals

The six-panel penguin storyboard supplied by the user guides the low bow, flippers raised, open beak, frost bloom, curved snow gusts and rising ice clusters. The owned cast clip keeps the feet planted on both shipped penguin rigs. A short caster bloom follows the penguin while the full storm stays at its target location.

Four bounded effect actors combine textured frost, six curved wind ribbons with eight low mist wisps, 64 animated snowflakes/motes and 24 faceted ice spikes. The old rectangular ground quad is replaced by a circular frost mesh and genuinely transparent artwork. Effects use the existing resource cache, native texture decoder and particle draw path. No protocol schema change or retail archive edit is required.

This is an in-engine interpretation, not a numerical match to the illustration. Flat ground frost can be partly hidden on uneven terrain; the raised snow, wind and crystals remain visible. Item-proc Blizzard gameplay remains unchanged; its shared cosmetic signal uses the new storm visuals.

## Verification

- Full Go action and companion package tests passed.
- Focused client Penguin, bear and wolf effect/rig tests passed, including both Penguin rigs, transparent atlas gutters, bounded mesh data and the five-second lifecycle.
- Client type/ownership/capability/execution checks and test-type ledger checks passed.
- Live training-dummy test isolated Blizzard III from normal pet attacks: five separate HP decreases about one second apart, freeze on the fifth, no sixth damage pulse. Captured public HP timestamps: 1793825591577, 1793825592574, 1793825593575, 1793825594574, 1793825595576.
- Foreground browser captures reviewed the growing crystal clusters, textured snow and wind, followed by disappearance. Local screenshots remain outside version control.

All five original character records, inventories, pet gear and ground items were exactly restored from the pre-test backup. Temporary test GM authorization and the training dummy were removed. The updated GameWorld and client are left running for user review. Deployment details and local evidence locations are in the local handoff.
