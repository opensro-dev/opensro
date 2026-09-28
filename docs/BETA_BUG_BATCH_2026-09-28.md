# Beta bug batch, 2026-09-28

This batch starts at `99a3a0d` and integrates the merchant-quantity branch
(`1dfe9ab`, PR #24) and level-up recovery branch (`1c203a9`, PR #23). It adds
the fixes below. These are code and local validation results, not a claim
that production has received the batch or that reporters have retested it.

| Report | Root cause and repair | Native evidence / regression coverage |
| --- | --- | --- |
| BUG-006 | A draggable region prevents the browser's default focus transfer. The shared DOM bridge now retires the previous control's focus on a new primary-pointer gesture. | Real Chrome test fails without the listener and passes with it; covers map drag, world click, text selection and keyboard navigation. |
| BUG-009 | Drop notices asked the display-name catalog for system-message symbols. Notices and confirmation now share the HUD system catalog. | Client `68D430`; integrated UI test checks readable warnings and absence of raw keys. Equipped-item refusal uses the same correction. |
| BUG-010 | Item-property tooltips omitted transaction context. Append the active offer or buyback quote outside the item-property memo. | Client `5592B0`, package branch `559AF6`; exact decimal-string gold, one price per package, buyback stack, free offer, changed quote and missing/error contexts. Inventory sale quotes are outside this change. |
| BUG-011 | An empty social snapshot's zero options replaced the local party-formation draft. A shared resolver distinguishes an active party from an empty snapshot. | Client `63B7F0`; all eight sharing-bit combinations, join/leave behavior, and UI registration command. Invitation and matching consumers use the same rule. |
| BUG-024 | All generic window paths admitted the pet window without an active pet. Admission now checks owned living COS records, and loss of the last active record closes the window. | Client `6A2350` to `69D920`; hotkey/menu admission, dead roster, multiple pets, disappearance and generic window placement tests. |
| BUG-030 | The equipment projection omitted the harp family's intrinsic maximum-MP contribution. The shared keeper receives item-owned +50 percent in the percentage-sum channel. | Server `497C20..497C4D`, float constant at `B4603C`; every weapon family, bag/equip/break/repair lifecycle, independent modifier composition and repeat projection. |
| BUG-031 | The generated native font atlas had ASCII and selected punctuation only. The GDI generator now includes Latin-1 and Latin Extended-A for every font/style. | Client `A17690` font setup; 331 glyphs per style, existing glyph coordinates/metrics unchanged, Turkish rendering and Unicode chat request/receipt/broadcast tests. This is not full localization or a change to character-name policy. |
| BUG-034 | The auxiliary party-monster spawn flag was parsed but never painted above the monster. Add the native 16-pixel icon beside the measured name. | Client `862060`, `781940`, `85F4A0`, icon table initialized at `BBEAD0`; all base grades, ordinary monsters, other entity kinds, and integrated resource/publication/removal tests. |

## Shared ownership decisions

- Harp MP belongs to equipment projection, not login, one skill, or one packet.
  Rebuilding the keeper naturally removes the contribution with its item.
- Commerce prices belong to the current transaction quote. They do not belong
  in an item cache or a JavaScript floating-point conversion of 64-bit gold.
- Party formation has a local draft until membership exists. Every consumer
  must use the same choice between draft and live-party rules.
- Focus belongs to the DOM bridge. Fixing the map button's paint alone would
  leave the same stale-focus failure in other windows and world clicks.
- Glyph coverage belongs to the generator and asset publication pipeline.
  No hand-edited generated atlas or development-only resource path is used.

## Investigation findings that are not fixed here

BUG-025 (Trace) has a confirmed contract defect. Client action `1003` is
`UIIT_CTL_AUTOTRACE_TT`; native `695420` emits `[01 03 01 gid]` on `72CD`.
The port does not dispatch that action, while the server calls that wire
family `TargetActionActionPaneAttack` and routes it into basic attack.
Native server `4AE3D0` establishes following with inner/outer distances of
both body radii plus 50/80; `4B0490` owns pursuit updates and target loss.
The native database label was corrected to
`CGCharAutoCommandActor_Handler_FollowTarget` and saved. Enabling the UI
command alone would be wrong: a complete repair must separate follow from
attack and cover cancellation, despawn, death, world transitions and pursuit.

BUG-029 (European voices) requires identifying the reported model and cue.
The shipped animation catalog already assigns model-specific profiles;
for example `CHAR_EU_MAN_ADVENTURER` uses `PCM_ADVENTURER`, whose authored
voice rows point to `vcm_at_*` assets. Hearing a shared voice does not alone
prove that the port selected the wrong profile. No speculative race-based
replacement was added.

Equipment investigation also found broader reinforcement/set contributions
outside the harp resource rule. They are not silently included in this fix
and need their own native arithmetic and lifecycle coverage.

Other reports remain open where this batch has not reproduced and verified
their complete behavior. In particular, do not mark mobile rendering,
distant ground picking, stance selection, repeated pickup, or beta starting
inventory policy fixed based on these changes. Loot work in another active
checkout was left intact.

## Validation

- `pnpm task check:server`: passed tidy, formatting, vet, pinned lint, tests,
  race subset and vulnerability checks.
- `pnpm --filter @sro/client-next check`: passed all eleven gates, including
  runtime/architecture tests, types, ownership, capabilities and delivery.
- `pnpm check source`: passed all eight tasks; its server task reused the
  already-passed unchanged server inputs.
- New UI integration and Latin chat tests also passed directly after their
  final additions; the test-type gate reports no new debt.
- `ui-focus-lifetime.test.mjs`: real Chrome baseline failure and repaired
  pass using the same fixture and gestures.
- Authenticated Chrome smoke: `PASS SUCCESS` with scratch character `asd2`,
  no rewritten application sources or responses, no browser errors, world
  and navigation ready, zero pending/failed UI images, and no pet window
  after pressing W with an empty pet roster. Screenshot and trace captured
  after the loading fade finished.
- `refresh_native_font_asset_packs.mjs`: passed; regenerated the font pair,
  affected packs, web manifest and compressed sidecars. Existing glyph
  coordinates and metrics were compared with the previous publication.

Native function labels were saved with Binary Ninja's database save API and
checked in the saved snapshot symbol table. Local check logs, font artifacts,
and browser evidence live under `.state/bug-batch/` in the batch worktree.
