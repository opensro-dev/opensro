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

Focused tests exercise summon debit, duplicate refusal, group dismissal,
expiry polling, signed expired entry, store reopen, count tables, master-transfer
precedence, attribute purchase/reset/refusal, initial HP order, area damage,
Cold status application, preparation/release identity, timer origin and companion
displacement. Client tests cover wire decoding, flags, submenu lifetime,
representative-group dismissal and the owner cooldown display.

The runtime uses the existing companion following and navigation implementation.
Exact native formation slots, steering and the complete AI callback graph are
not established by these tests. Flag-event worlds and the original free-PVP
transition owner are broader port dependencies, not proved by the normal-world
and fortress-world enemy predicates. No live original-client/original-server
comparison or whole-program machine-equivalence claim is made.
