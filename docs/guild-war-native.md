# Guild-war authority and client workflow

The v1.150 client defines the protocol and UI. GameServer v1.188 and its
ShardManager define server mechanics within that client feature set. This branch
builds on the committed fortress battle work in PR #200 and trader/fortress
services in PR #189. It does not use another agent's uncommitted source.

## Native branch map

| Behavior | Native evidence | Port owner |
| --- | --- | --- |
| Declaration, eight score options, packed duration, personal stake | Client 704390, 617730, 617970, 618A60; server 5C6B60 | `guild-war.ts`, `guild-war-hud.ts`, social guild runtime |
| Target master online and within 300, master/funds/50-enemy/union/fortress checks | GameServer 5C6B60; fortress world lookup 648220 | Existing presence, movement, guild, union and fortress owners |
| Consent and refusal; repeat master and funds checks, no second range test | GameServer 5C8510, 46CE20 | Shared invitation dispatcher, session-bound pending proposal |
| Atomic stakes and durable active war | ShardManager 43BE90, 435A10 | Guild-war store transaction and immutable published authority snapshot |
| Player/COS kill score and member totals | GameServer 5C7000; ShardManager 43C320 | Post-transaction fatal publication; actual striker level and owner guild |
| Aggression/death relation | GameServer 4E23E0, 4EB390, 4E6590 | Existing attack, aggression, PK and death owners |
| Score-limit/deadline precedence, ties | ShardManager 43C320, 438430 | Authority combat and tick; tied expiry alone does not end a war |
| Surrender delayed by 60 seconds | GameServer 5C7390, 5C8050; ShardManager 43CB50, 4217F0, 43CCF0 | Requester-owned pending surrender jobs and real world tick |
| Duplicate surrender jobs | ShardManager 4220F0, 421830, 42ABC0, 4289F0 | One pending job per requester; both masters can surrender; earliest queued settlement wins |
| Settlement and compensation | ShardManager 43C130 | Atomic compensation credit and active-row deletion; existing guild-manager claim owner |
| Entry, begin, scores, end | Client 82A870, 61A150, 61A2F0, 762040 | 32BB seed and 3B29 deltas, authoritative removal |
| Single surrender countdown | Client 681740, 6875F0 | One 60-second notice owner; no zero notice; end clears it |
| Requester/recipient timeout and suggestion dialog | Client 75CD90, 76309C..7632F5 | Pending mode, shared consent dialog, two-line result modal |
| Hostile guild rows and contribution sorting/ranks | Client 5FFCC0, 5FFFC0, 616990, 82B9A0 | Native authored layouts, fixed header ordering and dense ranks |
| Disband and fortress master-transfer refusal | GameServer 5C6870, 5C6900, 5C5760 | Existing guild operations; active-war disband refusal and fortress participation gate |

Kill credit is a byte: victim level minus actual striker level below -5 gives 1;
-5 gives 25; equal level gives 100; +10 gives 250; above +10 gives 251. A COS
credits its owner but uses its own level. Fatal abnormal effects resolve live
companions through the existing companion owner. A repeated hit on a corpse does
not create a second credit.

The declaration, proposal and surrender opcodes are v1.150 771B/B71B, 3393 and
7465/B465. The port does not expose later Battle Arena world functionality.

## Approved parity exception

The user explicitly approved requiring a surrender requester's guild to be one
of the two participants. GameServer 5C7390 and ShardManager 43CB50 require a guild
master but omit the participant check. The port rejects an unrelated master with
code 2. No other intentional gameplay exception is introduced by this change.

## Persistence and lifecycle

Authority store layout 7 adds `guild_wars`; existing guild-member fields hold
score, kills and deaths. Both personal stake debits and war insertion commit in
one transaction. Settlement credits the guild's existing compensation field and
removes the war in one transaction. Publication follows commit. Readers use an
immutable snapshot to avoid a character-store/guild-war lock inversion.

Use the existing offline `sro-authority-upgrade -authority-dir PATH` validation
and then `-commit` during a stopped-server upgrade. Layout 6 fortress records,
including holder and staff flags, and character JSON are preserved. Startup does
not silently migrate or repair an incompatible authority store.

Active wars and compensation survive restart. Unanswered invitations and delayed
surrender jobs are transient, as in the native process owners. Proposal sessions
are pinned so a reconnect cannot accept an older session's proposal. Stale
teardown does not erase a newer session's invitation.

## Verification and limits

Tests cover transactional rollback and durable reopen, schema/layout upgrade,
all score boundaries, deadline and score-limit precedence, tied expiry, failed
settlement retry, fatal player and companion/status credit, duplicate corpse
attacks, party protection and PK suppression. The real WebSocket test covers
proposal acceptance/refusal/timeout, both stake debits, oriented score receipts,
entry seed, unrelated-guild surrender rejection, simultaneous opposing surrender
requests, the 59,999/60,000 ms boundary and exactly-once compensation.

The browser test uses the production UI, renderer and assets with a deterministic
world snapshot. It verifies declaration terms, confirmation, surrender,
agreement/refusal and result modal dismissal. It is not a live original-client /
original-server comparison, or a connected browser-to-server guild battle. The
wire test and browser test establish separate boundaries; they do not prove
whole-program machine equivalence.

### Adjacent gap discovered during this audit

GameServer 5C6900 also refuses master transfer with 4C55 while owner timed job
(type 2, ID 4) exists. Raw instructions at 411560/411440 prove those helpers test
for a timed-job type/ID; their former title-destructor labels were wrong.
4FB086..4FB0FC creates the job for 1,200 seconds after a successful guild-soldier
summon. The current server has no mercenary summon producer. That full
mercenary lifecycle, including this cooldown and its dependent refusal, remains
unported; a fabricated fortress flag or a permanently unreachable boolean would
not close it. Staff hiring, which buys fortress service flags, is distinct from
summoning guild soldiers.


Final validation on this branch: all 13 source/server gates and all 11 client
gates passed. The expanded production-UI browser test passed. Focused race tests
covered guild transport, store and action packages. The first final client gate
attempt failed because `unzip` was absent from PATH; the corrected rerun passed.
The full asset rebuild was not run.

Client labels were read back from Binary Ninja snapshot 401. The new ShardManager
job labels were saved and read back in snapshot 2 of the isolated
`.state/native/SR_ShardManager.guildwar.bndb` copy. Each ShardManager address was
checked against original executable bytes and then the pinned Binary Ninja view;
the shared executor's active view can change when another agent uses it.
GameServer label-save/readback work is tracked separately from runtime test
results; a timed-out executor call is not evidence of a completed readback.
