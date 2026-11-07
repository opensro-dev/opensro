# Companion auto-potions

Port-only and off by default. Enable **Companion auto-potion** under
Experimental → Pets, then press **T**. Turning the feature off keeps the
original player-only recovery dialog and behavior.

## Character tab

The expanded dialog is 414 × 674 pixels. The original HP, MP, abnormal-state
and timing controls keep their existing server settings and save packet.

**Vigor HP** and **Vigor MP** each add an enable checkbox, threshold and
F1–F4 / numbered-slot binding. They accept vigor potions and vigor grains,
not ordinary HP/MP potions. Both may use the same stack: either threshold
can request a vigor, which restores both resources normally. Both rows start
disabled when upgrading older saved preferences.

## Companion tab

This tab is 414 × 524 pixels and keeps the same top-left position when switching
tabs. It provides independent HP thresholds for **Transport** and **Attack pet**.
Riding mounts and trader transports are supported; pickup pets are excluded.
Attack pets use their effective server-provided maximum HP, including gear.

Two shared hotbar bindings serve both companion types:

- **Pet HP potion:** bind a pet/transport HP potion. Each enabled HP channel
  uses it at or below its threshold.
- **Universal pill:** bind a pet universal pill and enable its checkbox.
  Any server-published negative status triggers a use, even at full HP.
  Ordinary player pills are not accepted.

**Consumption does not guarantee cleansing.** Native pet universal pills cover
freeze, frostbite, shock, burn, poison and zombie according to their cure level.
Standalone Slow, Root and Stun are outside that cure list. As requested, they
still trigger consumption: pills can be spent repeatedly until the effect
expires or the checkbox is disabled. Cleansing behavior is unchanged.

## Saving and item safety

**OK** saves both tabs. Cancel or closing discards unsaved changes; switching
tabs preserves them. Extra settings are browser-local per server and character
and do not synchronize between PCs. Native player settings still save normally.
No database migration or server packet extension is required.

Bindings use the real numbered hotbar slots 1–40; slot 0 is the separate common
M slot. Missing bindings, empty stacks and wrong item families never fall back
to another inventory item.

The extra recovery channels share one request gate and a 1.2-second retry guard.
They wait for world/frontend admission and respect item cooldowns, pending
inventory transactions, item dragging, NPC/shop/storage/stall interactions and
the Item Mall. Cures take priority over companion HP recovery. A healthy
companion with no negative status does not consume items. The server retains
its normal validation and consumption rules.

## Verification

Run the companion-potions, transport-auto-potion, auto-potion and
experimental-options runtime suites, plus client verification. The focused
suites currently contain 95 passing tests, including all 40 numbered hotbar
slots, independent vigor thresholds, flag-off behavior, native save bytes,
reconnect admission, tab layout, Cancel and item-use guards.

Live checks on disposable authority copies verified independent HP-only and
MP-only vigor use, pet/trader transport healing, and shock/burn/poison cleansing.
Freeze/frostbite cleansing passed on a wolf; transport-specific frostbite
cleansing was not independently observed.

The final Slow consumption test used an actual NPC thief Earth spell. While
server-published Slow was active, the trader transport consumed 16 pills and
the wolf consumed 9: the shared stack decreased from 50 to 25 with zero pill
rejections. Slow expired naturally. One unrelated HP reuse-delay rejection
was recorded and left unchanged. All original character records, inventory,
pets, gear, EXP and ground items were verified unchanged after cleanup.

These are local live-test results, not verification of a public server release.
Machine-specific backups, packet captures and detailed handoff notes remain
outside the repository.

The broad local checks are not fully green: seven generated-data client test
failures and the Fortress map action-snapshot mismatch also reproduce on the
unchanged branch base. The server gate likewise fails on missing native dungeon
route tables. These unrelated asset issues were left unchanged.
