# Fortress battle records — native evidence and boundaries

This follow-up is stacked on PR #197, revision `1cbcf61941a2d78c7a5f978768dd4e9d7380989d`. The original v1.150 client defines the wire and UI; the v1.188 GameServer supplies server rules within that feature set.

## Branch map

| Branch | Native instructions / data | Port |
| --- | --- | --- |
| Fatal player hit in an active siege world, excluding self and already-dead target | 52D460 → 635290 → 61F2D0; COS owner resolution at 52D860 | Shared post-transaction fatal publication |
| Victim death, killer kill, then other party members | 61F2D0 / 61F380 / 61F3C0 | Existing fortress authority; private 0x3887 score receipts |
| Party eligibility | 61F3C0: not killer, alive, identical packed world, killer-centered planar distance ≤1000; no share-option or guild filter | Existing party and planar-distance owners |
| First record and unsigned counts | 622120 creates rank zero; 61F580 increments uint32 | Battle record keyed by fortress and character |
| One rank at a time | 61F580 compares the next authored threshold, cancels the old skill, starts the next indirect skill, then 61CA70 updates rank/date | Common effect installer, no rank skip |
| Rank notices | Server 61F7D5 and client 76C870 case 0x0E / 7BB8C0 | Local name for ranks 1–5; explicit commander name to the guild/union; client banner and system chat |
| Record checkpoints | 622380: forced promotion, otherwise kills%10==1 or deaths%10==1; below-top-rank kill path returns early without periodic checkpoint | Per-character checkpoint images; unrelated saves do not flush live counters |
| Startup and world entry | 629590 loads stored rows; 4DF9E0 / 4E0750 / 620110 send a score only when the player has a record and war is active | Durable fortress JSON, real OnWorldBound hook |
| War end | 601170 mode 2 → 6204E0 cancels resident rank buffs, preserves the holder side, then 61EE00 / 628D50 clear side sets/live records | Existing war phase and effect retirement owners |
| Durable versus live record release | 628D50 → 6283D0 → 628440 releases memory; no battle-record DELETE query is enqueued on that path | Release live rows without rewriting checkpoint rows |
| Death classification | 4E6590 first tests active war and world vtable +0x7C; siege vtable B00FDC points at 5ECFD0 (true) | Prepared siege death context before character transaction |
| Siege death cost and reward | 4E6D60: ordinary player/non-player EXP rate, signed cap product × widened 0.2f, then premium reduction; no SP, drops or PK relief; player killer gets undoubled PvP EXP | Existing progression and reward owners, including fatal abnormal pulses |
| Commander compound effect | Authored 20515: cbuf, nbuf, dura, hpi, hst3, dru, odar; native movement writes at 59642C..59659E alongside other modifier blocks | Complete timed-effect compiler, shared independent movement lane |

All six enabled `siegefortressbattlerank.txt` rows are checked against the licensed data: 15/25/45/70/100/150 kills, skills 20510–20515. Every rank must have an executable persistent program. Pure movement consumables retain their existing compiler path.

The original v1.188 opcode 0x385F maps to the v1.150 client opcode 0x3887. Score payload: subtype 0x11, fortress u32, kills u32, deaths u32. Rank payload: subtype 0x0E, local flag u8, optional length-prefixed name, rank u8.

## Ownership and persistence

The existing fortress authority owns both live battle rows and the last per-character checkpoint images. The existing `fortresses.record` JSON stores those child rows, avoiding another fortress authority or a parallel siege state. Old rows omit the optional child list. Stores receive sorted value copies, not live map aliases.

Combat prepares world/death classification before entering the character-store transaction. Score persistence and rank installation occur after that transaction under the action division owner. This avoids taking the fortress persistence lock while holding the character-store write lock.

## Verification scope

Focused tests exercise actual fatal attacks, duplicate attacks against a corpse, fatal burn ticks, nearby living party credit, packed-world and distance boundaries, all rank transitions, native notices, durable record reopen, independent checkpoints, write failure, unsigned wrap, entry score synchronization, rank replacement and war-end retirement. Native decisions were checked against instructions, not merely database labels.

Nineteen newly encountered server helpers were named and their symbols verified in saved Binary Ninja snapshot 444. The native review parent has separate client/server snapshot evidence and original-x86 resistance execution evidence.

This is a bounded subsystem port. It is not a live original-client/original-server comparison or whole-program machine-equivalence proof. Fortress staff hiring/holder flags and the existing guild-war owner gap remain separate full-game work; they are not silently counted as complete here. No renderer, onboarding, password-manager or other non-native feature is included.


## Staff and holder flags follow-up

Staff hiring uses the existing fortress authority and persistence record. Server
632100 admits requests in this order: war inactive, fortress exists, exact holder
guild, master, no overlapping requested flags, 30,000 personal gold, 3,000 guild
points. Query 632020 has no guild gate after manager admission. Request byte zero
and combined flags retain the native behavior; the normal client offers bits
1, 2 and 4. The store commits flags, personal gold and guild points atomically.

Client 5D7AD0, 5D8930 and 5D26F0 supply the three native staff rows, fee labels and
confirmation. The port uses the existing NPC conversation and modal lifecycle.
Client 754A40 applies query/hire receipts; 76C870 applies subtype 0x12 holder flags.
The real world-entry hook sends the current fortress's flags to its exact holder
regardless of war activity (4DF9E0, 620090). Capture and temporary-holder settlement
clear staff flags (6232C0, 625DB8, 625F29).

Focused tests cover refusal order, duplicate/combined/zero requests, transaction
failure with no debit, durable reopen, selected-manager wire admission, capture,
private holder delivery, target retirement and confirmation invalidation. The
13 source gates and 11 client gates passed during this follow-up; the transaction
abort test and manager wire test were additionally run after that full pass.
Guild-war ownership is being implemented separately and is not covered by these
staff checks.
