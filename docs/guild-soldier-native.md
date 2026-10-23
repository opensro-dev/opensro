# Guild soldier lifecycle

Guild soldier scrolls now create character-owned mercenaries and the timed job
that native master transfer checks. Fortress staff hiring is a separate feature.

## Native authority

| Behavior | Native rule | Port owner |
| --- | --- | --- |
| Scroll admission and reference selection | The native admission and reference selection | action/mercenary.go, enterworld/mercenary.go |
| Guild and union-leader counts | The native count tables | domain.MercenaryCount |
| Owner reuse job `(2, 4)` | The native reuse job | action/mercenary_clock.go |
| Master-transfer refusal order | The native refusal order | action/npcguild.go |
| Initial vitals before attribute modifiers | The native initial-vitals computation | action/mercenary.go |
| Attribute purchase and living-owner refresh | The native purchase and refresh rules | data/store/mercenary.go |
| Public/private soldier records | The native record split, 3158 band 5 | enterworld/cosrecord.go, item/wire/coscommand.go |
| Enemy selection | The native selection chain | action/mercenary_target.go |
| Prepare and release | The native prepare and release rules | action/petcast.go, petarea.go |
| Strategy timer starts before command submission | The timer starts before command submission | action/petcombat.go |
| Displacement admission | The native displacement admission | action/playerdisplacement.go, cosdisplacement.go |
| Group dismissal | The native dismissal rules | action/mercenary.go |

The rules were checked against instructions, not accepted from database names.
The misleading spawn and reference-group labels were corrected. The container
and exception helpers encountered in the menu path are runtime machinery;
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
and has no parameter write. The native unknown-byte fee is preserved.
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

The distance-selection tail of selector 6 was checked against the original
machine code:

- The distance is stored as float32 before comparison.
- Sight range is compared against distance; the parity test rejects a
  distance greater than the sight limit, accepting equality.
- An empty selection is distinguished from an existing target.
- Distance is compared against the existing unsigned integer best; the
  parity test preserves the first target on equality.
- Both accepted branches truncate the float32 distance to the integer best
  with round-toward-zero control.

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

Guild soldiers now use the native owner-follow callback, with owner-local
positioner slots, retained full-formation indices, the three-body-radius
spacing rule, surface probes and rounding, and the 30-unit/5-degree/100-unit
steering decisions. The native catch-up rule supplies the 80-unit catch-up
speed through the existing parameter keeper. Follow entry uses the 100 ms
timer and retains the battle movement timer across re-entry. Death,
dismissal and completion release reservations. Distant relocation replaces
visibility with native reason 7; reconnect and peer spawns read the same
movement parameters.

The checked-in native fixtures execute 384 original steering cases and
432 original placement cases, with bit-exact Go comparisons. The
steering fixture injects timer results, the formation goal and command sinks;
the placement fixture injects effective reach and navigation responses. They
execute the original vector math and CRT, not a reconstructed oracle.
`apps/server/internal/game/world/monster/testdata/generate_owner_follow.py`
reproduces both captures from the licensed executable. These are bounded
comparisons; the complete AI callback graph is not established.
Attack and pickup pets use the same slot owner and follow callback, so a
simultaneous summon competes for the same eight reservations.
Flag-event worlds and the original free-PVP transition owner remain broader
port dependencies, not proved by the normal-world and fortress-world enemy
predicates. No live original-client/original-server
comparison or whole-program machine-equivalence claim is made.
