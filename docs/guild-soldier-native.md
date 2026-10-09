# Guild soldier lifecycle

Guild soldier scrolls now create character-owned mercenaries and the timed job
that native master transfer checks. Fortress staff hiring is a separate feature.

## Native authority

| Behavior | GameServer evidence | Port owner |
| --- | --- | --- |
| Scroll admission and reference selection | 49AFE0, 4FCEF0, 4A2CE0 | action/mercenary.go, enterworld/mercenary.go |
| Guild and union-leader counts | 5C56C0, tables ADE900/ADE93C | domain.MercenaryCount |
| Owner reuse job `(2, 4)` | 4FB086, 651B20, 651D30, 652080 | action/mercenary_clock.go |
| Master-transfer refusal order | 5C6900 | action/npcguild.go |
| Initial vitals before attribute modifiers | 4D97A0, 4A8310, 4D9850 | action/mercenary.go |
| Attribute purchase and living-owner refresh | 5C7900, 5D0E00, 5D0FF0, 5C99A0, 4FDC70 | data/store/mercenary.go |
| Public/private soldier records | 4D99F0, 3158 band 5 | enterworld/cosrecord.go, item/wire/coscommand.go |
| Enemy selection | 546CA0, 529AA0, 529AE0, 52B6D0, 52DA50 | action/mercenary_target.go |
| Prepare and release | 58356C, 585BF8, 585F69 | action/petcast.go, petarea.go |
| Strategy timer starts before command submission | 5472A0 | action/petcombat.go |
| Displacement admission | 58E540 (not the old comment's 58E520) | action/playerdisplacement.go, cosdisplacement.go |
| Group dismissal | 517D20, 4FDB00 | action/mercenary.go |

Addresses were checked against instructions, not accepted from database names.
The misleading spawn and reference-group labels were corrected. C++ vector and
exception helpers encountered in the menu path are runtime/container machinery;
the port uses its language's arrays and resource lifetime rather than translating
allocator instructions into gameplay state.

## Rules and transitions

The licensed census contains 15 scroll families, each with 140 actor rows.
Native checks reference-vector count strictly greater than player level, so the
port admits 2,085 family/level combinations (levels 1 through 139) and refuses
level 140. It validates every weighted default skill, including Cold's
zero-damage freeze/frostbite cone. All authored soldier skills have zero HP,
MP, percentage-vital and ammunition consumption.

Ordinary guild levels 3/4/5 summon 1/3/6 soldiers. The leading guild of a union
summons 1/5/10. Each has its own GID, HP, MP, status block, cast token and mover.
One group consumes one scroll. Soldiers expire after the authored 20 minutes.
Death and dismissal remove actors without clearing the owner's 1,200-second job.
That job remains present until its ten-second online poll observes expiry;
re-entry publishes the signed remaining time and starts a fresh poll accumulator.
The cooldown and retained soldiers persist in the existing character store.

Attribute requests use v1.150 7322/B322. Nonzero replies and metadata deltas OR
the flags; zero resets them. Native's four purchase choices are defense, attack,
hit and health. Tolerance can be present in the flags but is not a menu choice
and has no parameter write in 4D9850. The native unknown-byte fee is preserved.
Health adds 35 percent to maximum HP; it does not fill the extra current HP.
Defense writes parameter IDs 84 through 87, which are distinct from the AE
through B1 absorption parameters. The port does not substitute the latter IDs.

The ordinary v1.150 guild-manager menu builder does not add the soldier submenu's
opening row. The port preserves that omission. An attribute reply or guild
metadata update refreshes the submenu when the NPC talk window is visible;
closing the conversation retires it. No new player-facing entry row was added.

Mercenaries share the existing combat/status and victim owners. Area damage uses
one cast token and cumulative reductions. Companion sources can apply statuses
to monsters; periodic damage records the soldier's owner for reward credit.
Companion victims now commit knockdown/knockback through their existing mover,
retire prepared actions, and hold movement until recovery.

## Verification boundaries

### Nearest-target distance boundaries (#219)

The distance-selection tail of selector 6 was checked in v1.188
`SR_GameServer.exe`, SHA-256
`bec2375e2c4c1073e3bf7761571470c430de251de74b452dbb86537348ef5290`.
All addresses here are preferred VAs (image base `0x400000`). The 225-byte
window `[546D71,546E52)` has SHA-256
`91ee8b3f4795a977b3bd30d009c5e5b2822d7ba3292bb07d5376da6770908c5b`.

- `546D76` stores the distance as float32 before comparison.
- `546D90..546D97` compares sight range against distance; `TEST AH,5` / `JPO`
  rejects a distance greater than the sight limit, accepting equality.
- `546DAA..546DB1` distinguishes an empty selection from an existing target.
- `546DF0..546E0B` compares distance against the existing unsigned integer best;
  `TEST AH,5` / `JPE` preserves the first target on equality.
- Both accepted branches truncate the float32 distance to the integer best
  through `FISTP` with round-toward-zero control.

The port previously combined sight and replacement into a single strict
comparison and kept distance as float64. This missed enemies exactly at the
130-unit sight limit and changed a near-tie decision. The acquisition tests
cover ten player-candidate cases and three monster-distance cases, including
integer truncation, equal distances, float32 rounding and the outside control.
They exercise the existing eligibility and population paths. They do not prove
the entire native spatial query, candidate enumeration order, or AI graph.

### Lifecycle and callback coverage

Focused tests exercise summon debit, duplicate refusal, group dismissal,
expiry polling, signed expired entry, store reopen, count tables, master-transfer
precedence, attribute purchase/reset/refusal, initial HP order, area damage,
Cold status application, preparation/release identity, timer origin and companion
displacement. Client tests cover wire decoding, flags, submenu lifetime,
representative-group dismissal and the owner cooldown display.

Guild soldiers now use the owner-follow callback at `549F80`, with owner-local
CPositioner slots (`555340`/`555460`), retained full-formation indices, the
three-body-radius spacing rule (`5400C0`), surface probes and rounding (`55E090`),
and the 30-unit/5-degree/100-unit steering decisions. `548A30` supplies the
80-unit catch-up speed rule through the existing parameter keeper. Follow
entry uses the 100 ms timer and retains the battle movement timer across
re-entry. Death, dismissal and completion release reservations. Distant
relocation replaces visibility with native reason 7; reconnect and peer spawns
read the same movement parameters.

The checked-in native fixtures execute 384 original `549F80` steering cases and
432 original `55E090` placement cases, with bit-exact Go comparisons. The
steering fixture injects timer results, the formation goal and command sinks;
the placement fixture injects effective reach and navigation responses. They
execute the original vector math and CRT, not a reconstructed oracle.
`apps/server/internal/game/world/monster/testdata/generate_owner_follow.py`
reproduces both captures from the licensed executable and records its SHA-256. These
are bounded comparisons; the complete AI callback graph is not established.
Attack and pickup pets use the same slot owner and follow callback, so a
simultaneous summon competes for the same eight reservations.
Flag-event worlds and the original free-PVP transition owner remain broader
port dependencies, not proved by the normal-world and fortress-world enemy
predicates. No live original-client/original-server
comparison or whole-program machine-equivalence claim is made.
