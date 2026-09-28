# Scrolls, chat continuity and repeated pickup

This batch extends PR #26 on `codex/beta-bug-batch`. It changes the local
client and server; production publication is a separate release step.

## BUG-037: complete timed stat consumables

The item dispatcher previously admitted speed and detection effects but
refused other associated skills. The repair compiles complete descriptor
programs into the existing active-effect owner. It admits timed damage,
absorption, HP/MP, accuracy/evasion and STR/INT contributions without item-name
exceptions. Unknown instructions reject the whole program before consumption.

One effect registry owns installation, duplicate refusal, parameter writes,
death protection, logout checkpoints, restoration and expiry. Inventory debit
follows successful installation. Derived stat refresh stays private to the
owner; attached effects remain public. Malformed trailing request bytes are
refused without consuming an item.

The shipped catalog audit found 84 skill-consumable references in family
3/3/13/{1..3}. Fifty stat programs are newly admitted, bringing admitted
speed/detection/stat references to 68. Admission still enforces item country,
level, stats and use permissions. Twelve level-gated HP/MP potions correctly
refuse the level-1 test character; their unchanged programs are then exercised
through the producer separately. The other 38 exercise normal inventory use.

Sixteen references remain unsupported, so this report is partial:

```
ITEM_MALL_SKILL_RESTORATION_POTION
ITEM_QNO_RM_OLDWOMAN_2_02
ITEM_ETC_WARENETWORK_EVENT
ITEM_QTUTORIAL_EU_01
ITEM_QTUTORIAL_EU_2_01
ITEM_QNO_EU_CONS_12_02
ITEM_QNO_EU_EASTEU_14_02
ITEM_QNO_EU_EASTEU_17_1_02
ITEM_QNO_EU_GENERAL_1_01
ITEM_QNO_EU_GENERAL_1_03
ITEM_QNO_EU_IVY_2_01
ITEM_QNO_CA_THIEF_5_02
ITEM_QNO_CA_GORIA_6_01
ITEM_QNO_CH_HWAN_1_4_04
ITEM_QNO_CH_WHAN_2_1_01
ITEM_QNO_EU_WHAN_2_1_01
```

## BUG-039: conversation lifetime and authoritative public color

Scene reentry preserves chat for the same character session. Character change
and session reset clear it. The beta server retains ten public messages per
shard in memory and replays them once at world admission. Replay and live
subscription share one lock; whispers, guild and party chat never enter it.
Restarting the shard clears this transient history.

Public beta broadcasts now include the author before the keyed native receipt.
The client uses that authoritative Global channel for color and suppresses the
receipt's duplicate local line. It retains repeated historical text, including
the player's own earlier messages. Native acknowledgement bytes are unchanged.

## BUG-014: duplicate intent before the first reply

Repeated G/direct-pickup commands could enqueue identical requests before the
first response arrived. A successful pickup removed the drop, and stale copies
then received the legitimate missing-item refusal. The worker now coalesces
the current target's repeated intent until server action release. Different
targets replace it; movement, portal approach, cancellation, despawn and world
reset retire it. No guessed timeout or global pickup cooldown is introduced.

This is an intentional port improvement, not an assertion that native had a
per-pickup receipt lock. Native's five-per-second action throttle still permits
the duplicate window. Real ownership, range and inventory refusals remain.
The repair covers that reproduced command/response ordering; a player retest
is still needed to determine whether every occurrence in BUG-014 shares it.

## Native evidence

- Server `49C2B0`: TID4 minus one indexes a dispatcher. The first three table
  entries at `49CC30` all target `49C354`, resolving the associated skill at
  reference offset `+2A0`. Corrected the misleading return-scroll-only label
  to `CGItemExpendable_UseSpecialConsumable`.
- Server `59B8D0`: pointers `+358` (cbuf) and `+280` (dura) select `49A390`
  owner timed jobs before ordinary cast validation. Duration is unsigned
  milliseconds divided by 1,000. `493D52` belongs to monster capsules and
  does not justify cancelling hide on this timed-scroll branch.
- Server `594AC0`: hpi/mpi write parameters 3/4; er/hr write 9/11. Word 1
  enters percent-sum channel 1, then word 0 enters flat channel 0. STR/INT
  use the existing capped-additive implementation; dru/odar retain the
  existing damage/absorption arithmetic.
- Client `752800`: named `ClientChat_PresentChannelMessage`; Global channel
  6 selects `FFFFFF00`, ordinary All selects `FFFFFFFF`. Outgoing status
  does not select a separate public color.
- Client `6955FA`: nearest-item shortcut reaches the native pickup request.
  `71FBA0` supplies burst throttling; the static pending table at `CC9054`
  maps `7338`, not `72CD`. `75BAA0` decodes action state and presents kind-3
  errors under category `0x19`. The port now consumes this action lifecycle.
- Inspected unnamed client `692CB0` and labeled it
  `CGInterface_InteractCharacterTarget`; it is a character-target dispatcher,
  not the nearest-drop shortcut.

Claims were checked against instructions. Client annotations were saved in
snapshot 175; server annotations in snapshot 234. Saved annotation records
were read back through Binary Ninja's existing database connection.

## Validation

- Focused server chat tests use actual WebSocket world admission, author
  echoes, bounded last-ten replay, private exclusion and repeated game-ready.
- All 50 shipped stat programs pass installation, reconnect and expiry tests.
  Additional tests verify exact HP/MP increases, private stat delivery,
  duplicate-use refusal, offline lifetime freezing, gauge clamping and
  malformed/unsupported descriptor refusal.
- Client tests exercise real gameplay bootstrap/reset, pending chat receipts,
  repeated text, public color, repeated pickup input, malformed releases,
  cancellation, movement, replacement and scene lifetimes.
- `pnpm check source` passed all eight tasks, including the complete server
  gate (tidy, gofmt, vet, pinned lint, tests, race subset, vulnerabilities).
- The full client gate passed all eleven tasks. Initial fixture/type errors
  were corrected; no new formatting or test-type debt was added.
- Authenticated Chrome entered `asd2` and `CodexProbe` through the actual
  dock against the updated local server. One All request produced exactly
  one author line on Global channel 6; the newly admitted second character
  received the same line once from history. Both browsers recorded no page
  errors. Artifacts are in the ignored `.state/bug-batch/chat-live.{json,png}`.
  Travel preservation and pickup request timing are covered by behavioral
  tests, not claimed as full browser travel/pickup reproductions.

All changed maintained source was hand-edited in repository id Software style
and formatted with gofmt/dprint. Generated source was not edited. Local Nomad
reported healthy Agent and GameWorld allocations after installing these builds.
