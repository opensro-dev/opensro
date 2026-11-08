# Event So-ok Survival Arena

Port-only, not native. Implemented on `feat/sook-survival-arena`, based on
`455577e3`. The server flag is **off by default**. This implementation uses
existing NPC dialogs, notices, monsters and item assets; no PK2 edits, new
protocol messages or client experimental HUD are required.

## Play

**Current venue:** the official Colosseum, imported from the owner's newer
client as a separate asset variant. See [map import and deployment](SURVIVAL-ARENA-MAP.md).
The older courtyard entries below are historical test evidence.

Latest map verification: official Colosseum rendering, navigation, movement,
three visible/aggro monsters, a real kill, death payout/return and reconnect
recovery passed. LabWizard remains level 90 with its 30 inventory rows and
96 skills; the test daily entries were reset to zero after verification.
Current server build is `36DD6E621BE116D6A5FF122A7052002C63015F138D87502A134093D12BAAD255`.

With `SRO_SOOK_SURVIVAL_ARENA=on`, talk to Event So-ok in any of her five
towns and choose **Event**. The accompanying notice explains the survival
event before entry. That label uses the shipped `UIIT_CTL_WK_EVENT` text.
The proposed `UIIT_MSG_EVENT_START` actually means “Event has started.”

- Three successful entries per character per UTC day. Failed relocation
  restores the entry count. A saved pending-entry marker also covers a
  restart between recording the return anchor and relocating.
- Enter alive, in the default world, without a job suit, stall, exchange,
  storage interaction or another instance's return anchor.
- Each entrant gets a separate generation-pinned layer of definition 30,
  overflowing to 31: up to 200 solo layers per division.
- Gold is awarded by total kills on death or the 20-minute timeout. Death waits for its presentation,
  revives the player and returns them without EXP/equipment loss. Early
  departure or disconnect forfeits the run. Restart recovery works even
  when the feature flag is subsequently disabled.

## Encounters and rewards

Latest pickup update: six flat, high-contrast glyphs replace detailed buff
art. Blast eggs announce **KABUMMMMMM** with the existing bear-roar aura,
two shockwave crests and roar sound instead of the earlier fire burst.
Fire Pit is an additional random egg power; its stationary 30m-radius
low flames, branching ember cracks and thin boundary last 60 seconds, and replacing it moves the one
active pit for that run. Its damage uses the ordinary credited monster-hit
settlement, so kills retain normal loot/rewards and cannot affect another
instance. Uniques are excluded entirely. Damage Surge rolls one integer
multiplier from 2 to 10 on each pickup and announces the actual value.
Fire Pit now removes 5% maximum HP per second. Its animation follows the
user's fire-pit storyboard: central ignition, one-second radial spread,
low flickering flames, persistent fine boundary/cracks, and final fade.
The reference's two-minute labels are visual context; the requested
one-minute gameplay duration stays unchanged.

Mirror Shell is a new 30-second random egg. It reflects 100% of actual HP
lost to an attacker in the same private arena. The wearer still takes the
hit; absorption, MP redirection and damage sharing happen first. Physical,
magic and periodic hits, including lethal overkill capped to HP actually
lost, are supported. Blocked/zero/self damage reflects nothing. Reflection
does not receive Damage Surge or recurse, and uses normal monster kill
credit/loot settlement. Exiting the arena or expiry clears the timed buff.

Verification: arena server regressions (including per-second damage,
radius, unique immunity, expiry, private populations and all nine surge
rolls), scoped Go vet, client type/ownership/capability checks and 144
client/art tests passed. New position cues 9/10 use the existing cosmetic
lane; model tests verify transparent corners and bounded 60-second expiry.
The new effects have not yet been visually verified from live egg pickups.

Fire Pit rendering correction: the native body-fade policy
`fadeAlphaOnly: true` ignores texture alpha during instance fades and
turned the earlier field quad into a bright square. Only the three owned
Fire Pit resources now use alpha-preserving fades and straight-alpha
blending. The boundary is an actual 192-segment annulus instead of a
full-field quad. Eighteen sparse, low flames and brighter ember cracks
leave the ground visible. The visibility pass raises flame size and opacity
slightly, with a clearer boundary and gently pulsing cracks. Other effects and gameplay rules are unchanged.
The real-GPU browser regression reproduced the old square (65,536 changed
pixels, including 2,601 outside-circle corner pixels), then passed with
965 changed pixels and zero changed corners after the visibility pass. Both top-down
and oblique captures were inspected; this remains separate from a live
egg-pickup playthrough. No backend restart is needed; reload the client
after completing an active run to clear its cached procedural materials.

The end-of-match bonus is the same whether the entrant dies or reaches
20 minutes. It is awarded once, in addition to ordinary monster loot.
Leaving/disconnecting still forfeits the run; restart recovery does not
reconstruct a reward from lost in-memory kill counts.

| Kills | Bonus gold |
| --- | ---: |
| 0 | 0 |
| 1–10 | 10,000 |
| 11–30 | 30,000 |
| 31–60 | 60,000 |
| 61–100 | 100,000 |
| 101–200 | 200,000 |
| 201–500 | 500,000 |
| 501–1,000 | 1,000,000 |
| 1,001–2,000 | 2,000,000 |
| 2,001–3,500 | 3,500,000 |
| 3,501–5,000 and above | 5,000,000 |

These starting amounts were chosen for testing because the request supplied
kill brackets but no gold amounts. Adjust `sookarena.KillReward` and its
boundary tests together; the highest bracket remains capped above 5,000.

The first three five-minute phases use Pandora delays of 15/13.5/12
seconds plus the unchanged 1.2-second opening. At exactly minute 15,
**DOOMS DAY** is announced once, adds up to 40 monsters within the existing
100-living-monster cap, and accelerates the actual spawn cycle
fivefold: (12 + 1.2) / 5 = **2.64 seconds** between waves when enemies
remain. This retains the 1.2-second Pandora animation, with 1.44 seconds
between a wave appearing and the next Pandora. Entering ultra mode also
shortens any outstanding ordinary wave wait. The match ends at minute 20.
Timed reinforcements continue while survivors remain; the former phase
crowd caps were removed because they stopped later Pandoras. The hard
safety cap is 100 living monsters per private arena, including uniques. Packs start at three; every
30 kills adds one per pack, up to six. A Pandora prop precedes each pack
by 1.2 seconds. Individual kills do not accelerate waves or trigger bonus
packs. Clearing every living monster starts the next Pandora immediately,
retaining its 1.2-second windup and preventing duplicate queued packs.
The opening wave is unchanged at three. Three of every ten normal spawns
are immediately aggressive, evenly spaced so only one of the opening three
targets the entrant. The others retain native aggression and sight range,
so naturally aggressive species acquire nearby players as in the ordinary
game. Naturally passive species still retaliate when engaged. This does not change ordinary world/GM AI.
There is no backlog of ordinary waves. At minutes 5, 10 and 15, a
notification reports current kills and remaining time; the minute-15 report
is part of the DOOMS DAY warning.

The CTF bracket monsters (`MOB_EV_WOLF`, `MOB_EV_RAVEN`, `MOB_EV_PENGUIN`,
`MOB_EV_KANGAROO`, `MOB_EV_JINN`) are excluded from both encounter lists.
Ordinary field encounters and CTF itself are unaffected.

On entry, a centre-screen notice says **Monsters will spawn in 20s**.
The first Pandora appears at 18.8 seconds and opens at 20 seconds. This
intro delay applies only to the opening; clearing later waves still starts
the next Pandora immediately. Blast-egg pickup displays **KABUMMMMMM**
and plays the bear-roar shockwave at the egg's position.

The six timed powers have simple high-contrast 64px icons under
`docs/features/assets/survival`, published by `buildSurvivalBuffIcons` as
part of the full CIF build. The normal timed board and tooltips are retained;
no native skill artwork was overwritten. The art is included in whole-build
and lane fingerprints so changing it invalidates the corresponding caches.
Waves use a repeating mix of 70% normal, 20% champion and 10% elite. Elites
use native grade 6 (four times base HP, native elite combat/visual rules),
not giants or party giants. Native GM rarity resolution is unchanged;
only the flagged arena can request grade 6 for ordinary references.

Encounter targets rise with elapsed run time, independently of kills:
`player level - 6 + floor(12 * min(elapsed, 15 minutes) / 15 minutes)`,
with a minimum of level 1. At minute 15 they reach player +6 and remain
there through the end. For level-90 LabWizard, targets are 84/88/92/96
at 0/5/10/15 minutes.

Every entrant has a different packed instance ID and generation, its own
programme clock, monster population, ground loot and powerups. Two-owner
regression checks verify separate waves and that one owner finishing and
releasing its lease leaves the later entrant's arena running. Existing
bomb and pickup checks also enforce the lease boundary. The same physical
Colosseum geometry is reused; players do not share its gameplay population.

The v1.150 encounter pool cannot reach level 97; its level-90 quest Rocky
variants also have inflated boss-like HP and are now excluded from this pool. The
Colosseum importer adds owned field variants at levels 89–97, references
5100089–5100097, reusing Rocky's native model, action skills, animation and
sound profile. Their HP, defenses, hit/parry values and EXP use an explicit
port-only quadratic level-growth rule from the native level-88 profile.
These are custom arena references, not claimed to be missing retail mobs.
The ordinary player level cap remains 90. Both the NPC bake and verified
server projection include the same rows; no PK2 is modified.
Bosses use the highest eligible shipped unique at or below the encounter
target; no boss is spawned when that target has no eligible unique.
Arena uniques have **1/20 native HP** (minimum 1). Their ordinary-world HP
is unchanged. Reinforced `_STRONG_`, `_L2`/`_L3`/`_L4` follower variants
and event-pet references are excluded from the arena pool.
Bosses are scheduled at 4:30, 9:30, 14:30, 19:30, 24:30 and 28:00.
Their appearance notice is private and their deaths never enter the
global unique-kill leaderboard.

Character EXP/SP, attack-pet EXP, item drop passes and gold heap amounts
are multiplied by ten. Final loot is capped at 32 entries per kill;
native admission rules and configured drop caps still apply. Reward
arithmetic saturates, SP is bounded to signed 32-bit, and the completion
prize checks the stored gold range before crediting.

Every 15 seconds, every five kills, and when a unique crosses half health, a floor powerup
appears for 40 seconds:

| Pickup | Effect |
| --- | --- |
| Damage Surge egg | Rolls an integer 2×–10× player-hit multiplier on pickup for 30 seconds; notification reports the roll; refresh rerolls |
| Shockwave egg | Kill ordinary enemies within 15 m; remove 25% effective maximum HP from uniques |
| Unstoppable egg | Clear existing abnormal states/knockdown; immune to new abnormal states and displacement for 60 seconds |
| Unkillable egg | Direct, slaying and periodic combat damage cannot take the last HP for 60 seconds |
| Arcane Spring egg | Fill MP on pickup; skill admission/preparation/release waive MP, preserving HP costs and cooldowns, for 60 seconds |
| Fire Pit egg | Fixed 30m-radius burning ground for 60 seconds. Every second, non-unique monsters inside lose 5% maximum HP. Uniques are immune. A new pit replaces the previous one in that run. |
| Mirror Shell egg | Reflect 100% of HP damage taken to the same-arena attacker for 30 seconds; the wearer still takes damage. |
| Bad Egg | Immediately directs every living monster in your private arena at you, including distant enemies. No effect in another player's arena. |
| Ambush Egg | Immediately spawns up to 10 additional monsters at the current encounter level, with the existing rarity/30% immediate-aggro rules. Respects the 100-living-monster cap and private arena isolation. |

The server randomly chooses one of these nine effects. They reuse the native
Easter egg reference (9272). In the arena's private courtyard only, the updated
client draws the existing owned Stormbrood egg mesh; elsewhere the original
Easter item retains its native drop model. Powers refresh rather than stack,
are owner/lease-specific and disappear on leaving, disconnect or restart.

Bomb kills use the ordinary HP, contribution, reward and death owners.
Tagged props cannot enter player or pickup-pet inventories and are removed
with their exact instance generation. Prop expiry visits only the run's
own small prop index, rather than scanning the division's loot each tick.

## Map evidence

The owner now wants the official Survival Arena Colosseum as the venue.
See [official-map research and migration steps](SURVIVAL-ARENA-MAP.md).
Its dedicated map is absent from v1.150. The owner supplied the newer
`E:/Igre/Silkroad` archives; the isolated converted variant now contains the
official Colosseum, and the matched event destination is region 25580.
The original v1.150 extraction and all PK2 archives remain unchanged.

The original amphitheater (27493) was rejected by the user after playtesting.
The replacement Jangan courtyard (17989) resolves to `885,31.202,914`;
all 32 sampled points at both 260 and 600 units radius were walkable,
with zero service NPCs and one teleport gate (`STORE_JA_CHARGE_GATE`).
Ordinary public-region access applies.
Entry validates the public access and current navigation surface, rejects
service-NPC regions (teleport gates are allowed), and refuses safely if
the courtyard is unavailable.
Spawns retry validated walkable points around the entrant, including when
they roam away from the centre. The former 320-unit radial teleport tether
has been removed; the leased instance and ordinary collision/navigation
still isolate the event. Moving around no longer snaps the player back.

## Deployment

This change advances authority schema **37 → 38**, retaining optional
`sookArenaDay`, `sookArenaEntries` and `sookArenaActive` fields. The explicit
offline upgrader preserves existing character records byte-for-byte.
Protocol remains **12**. Back up and stop the authority owner before
running `sro-authority-upgrade -authority-dir <directory> -commit`.
Never replace a real authority with a test copy.

The public settings registry and rules API include the flag. The private
ops repository is absent from this worktree; its GameWorld Nomad template
needs the following variable and task-env binding before operator rollout:

```hcl
variable "sook_survival_arena" {
  type    = string
  default = "off"
}

# Inside the GameWorld task's env block:
SRO_SOOK_SURVIVAL_ARENA = var.sook_survival_arena
```

Accepted enabled values are `on`, `true` and `1` (case-insensitive).
Everything else remains disabled. Restart the server after changing it.

## Verification and continuation checkpoint

Automated checks cover daily reset/cap, failed entry, independent leases,
waves/caps, elite bursts, reward multiplication, bomb kills/unique damage,
private unique bookkeeping, damage-buff expiry, prop expiry, native flag-off
paths, death, departure, disconnect, restart and stale cleanup after re-entry.
Schema-37 upgrade and persisted-shape checks pass. Installed encounter
validation found **358 normals and 19 uniques** with eligible encounters
through the native level range. Action/simulation/store/configuration,
GameWorld and architecture tests have passed during development.

The non-race changed-server gate passed in 62.7 seconds: whole-module build,
gofmt, vet, golangci-lint, changed-package tests and architecture gates.
The full changed-server gate encountered the local Windows limitation
`-race requires cgo`; race verification needs the documented MSYS2 compiler
or Linux CI. Do not describe that gate as entirely green.

**Live browser acceptance is in progress.** The Event row appeared in So-ok's
dialog, the job-suit admission refusal worked, and removing the suit allowed
entry into the amphitheater with the 1/3 count saved. The first monster pack
rendered and attacked. The user confirmed the poorly equipped test character
died immediately; the arena returned and revived it without a crash. A second
entry was also recorded. This verifies entry and failure recovery, not balance
or full endurance. The server's `/transport/readyz` subsequently returned 200.

Automatic approval review
blocked both stopping and starting the local GameWorld. The user manually
stopped it and ran the built launcher manually:

```powershell
& 'E:\CodeProjects\opensro-sook-arena\apps\server\.state\sook-live\Start-ArenaTest.ps1'
```

It uses only `.state/sook-live/authority-copy`, upgraded to schema 38, with
the copied LabPetTest placed next to Hotan's So-ok. All five original
character records were verified unchanged through the copy's upgrade.
The original authority is still schema 35 and has not been upgraded.
The former runtime used a different, disposable schema-37 test copy.

After testing, restore the saved previous Nomad configuration with the
same launcher plus `-Restore`. The ignored `job-before.json` records that
exact configuration; never overwrite it. Verify the original character
hash against `original-characters.sha256`. The existing browser client is
at `http://127.0.0.1:5180/?diagnostics`; the separate Playwright `pet13`
session is testing the copied character. Do not publish the ignored backups
or runtime files. The first fixture was quarantined for mistakenly containing
runtime-only JSON fields (`deathKind`, `lastExpLoss`, `nativeBodyStatus`);
those fields were removed from the copy and the quarantine retained. The
first deployment then auto-reverted at Nomad's deadline; a fresh deployment
with the corrected fixture reached healthy readiness.

The user requested Sun gear and max skills for the next attempt. A restricted
offline helper (`hero-prep.exe`, ignored) was compiled and dry-run tested on
`hero-dry-run`: 12 D9 Sun +8 items including blade, shield and accessories,
212 highest-rank Chinese skill rows, all seven masteries at 90, STR 380 and
INT 110. This deliberately exceeds the ordinary total mastery budget for a
test fixture. It preserves the fixture's former equipment in a private
`arena-hero-before.json` snapshot. The user has been asked to run:

```powershell
& 'E:\CodeProjects\opensro-sook-arena\apps\server\.state\sook-live\Prepare-ArenaHero.ps1'
```

That script stops only the test GameWorld, applies the helper to the fixed
copy, and restarts through the saved launcher. The user ran it successfully.
The live copy now has all 12 Sun +8 items equipped and 212 learned skills;
the browser rendered the Sun weapon/shield glows and maxed skill books.
At 19:17 UTC on November 6, the retry entered the amphitheater with
25,409 HP and 9,363 MP. It survived multiple waves for over two minutes;
later persisted HP was 19,168 with the arena still active, entry 1/3.
This fixes the inadequate test loadout; it does not establish full endurance
or combat balance. Evidence: ignored `sook-live/sun-arena-skills.png`.
The test server and copied character remain running for the user's retry.
Next checks: both pickups, tether, relog and longer survival; then restore
the previous server. Original
character/inventory/pet-gear hashes were rechecked unchanged after the live
entry/death attempts.

The optional HUD and optional Berserk pickup were omitted. No browser
playability, full 30-minute live endurance, or load-capacity result is
claimed. No commit, push or pull request has been made for this feature.

### November 6: wizard fixture and easier kill-driven encounters

The user requested a separate wizard/cleric, no giant waves, a level-minus-ten
opening with kill-driven level/count progression, and a better arena without
the tight teleport tether. These changes are built in ignored
`sook-live/gameworld-v2.exe`. The non-race changed-server gate passed again
in 64.4 seconds (build, format, vet, lint, tests and architecture). Regression
checks cover initial/progressive encounter levels, pack growth, normal-only
waves even in the last phase, and movement 700 units from the centre without
teleportation. Scheduled unique bosses remain separate from ordinary waves.

`wizard-prep.exe` was dry-run verified on a second copy: new **LabWizard**,
same **LabPlayer** account, level 90, pure INT (STR109/INT376), both Wizard
(514) and Cleric (518) masteries at 90, 96 learned skills including native
racial defaults, Sun +8 robes/accessories/two-handed staff equipped, Sun +8
cleric rod and shield in the bag, HP/MP potions and eight hotbar bindings.
All five pre-existing character records stayed byte-identical in that dry run.
No ordinary mastery-budget exemption is needed (total 180).

The user activated the prepared helper after automatic approval review
rejected local service control earlier:

```powershell
& 'E:\CodeProjects\opensro-sook-arena\apps\server\.state\sook-live\Prepare-ArenaWizard.ps1'
```

It stops only the test GameWorld, waits for allocations and port 8788 to
close, creates LabWizard only inside `authority-copy`, installs the built
v2 executable, and starts through the existing launcher. It preserves an
already existing LabWizard rather than overwriting it. Original authority,
characters and gear remain outside this helper's allowed directories.
Activation verified: Nomad job version 84, new build installed, readiness
returned 200 after warmup. LabWizard appeared as the fourth LabPlayer
character and entered Hotan successfully: HP 6,350 / MP 21,908, Sun robes
and staff rendered, Wizard skill window showed mastery 90 and maxed books.
The staff hotbar resolved Meteor, Frozen Spear, Salamander Blow and Blizzard;
Healing Breath and Bless Spell require the bag's cleric weapon set.
Evidence: ignored `wizard-town.png` and `wizard-npc.png`.

The user explicitly chose to test the character themselves, so browser
inputs stopped and the updated test server remains running. They changed
their hotbar and position; preserve those changes. Live courtyard entry,
level-80 normal opening and longer endurance remain to be observed. The
source tests and navigation probe passed; do not call those live acceptance.

### Courtyard admission correction

The user reported “public arena navigation is unavailable.” Reproduced the
actual production roster with `AppendTeleportGates`: the courtyard contains
`STORE_JA_CHARGE_GATE` at `619,46,915`. The initial probe loaded only npcpos
and missed that gate. Full public access, terrain, walkable surface and
stranded-spawn checks all pass at the proposed centre; the overly broad
"any NPC in the region" check was the refusal, not missing navigation.

Fixed admission to exclude teleport-gate rows from the service-NPC check.
Regression tests now explicitly admit a courtyard gate and still reject a
regular NPC in the same region without spending an entry. Rebuilt
`gameworld-v2.exe`; activation uses the same idempotent
`Prepare-ArenaWizard.ps1` command. Existing LabWizard is preserved, not
reset or recreated. The running server remains the previous build until
the user runs that command again. No character or gear mutation is needed.
The non-race changed-server gate passed in 58.9 seconds, including build,
format, vet, lint, changed-package tests and architecture; log:
`.state/sook-gate-entry-check.log`. The previously documented local race
toolchain limitation remains. The user ran the restart helper: job version
86 is running, installed executable hash matches the corrected build, and
readiness returned 200. LabWizard remains id 6, level 90, with all 13 Sun +8
pieces, 96 skills and the user's edited hotbar preserved. Browser controls
remain with the user by their explicit choice; courtyard entry needs their
next attempt. The previously pending manual restart is complete.

### November 7: weaker encounters, power eggs and rendering budget correction

Prepared the changes above after the user's follow-up. The updated binary
is again `.state/sook-live/gameworld-v2.exe`; the active GameWorld remains
the previous build until the user reruns `Prepare-ArenaWizard.ps1`. The
helper preserves existing LabWizard, its gear and user-edited hotbar.

Client source from this worktree is running separately at
**http://127.0.0.1:4180/?diagnostics**, Vite session 18193. The old port 5180
was left untouched. The updated client is required for egg artwork and the
visibility correction; the server's allowed origins already include 4180.

The user could see invisible enemies on the minimap and hit them with AoE
but could not select them. All 377 originally eligible references had a
published body, so a missing manifest entry was not demonstrated. Reproduced
two client actor-budget failure paths instead: dense effects evicted enemy
bodies, and tenfold ground loot could also outrank enemies by distance.
The private survival courtyard now reserves creature models and power eggs
before loot/effects, while retaining the 512-actor cap. Other worlds retain
their existing shared-budget setting. Regression tests cover 50 enemies,
600 nearby drops and 512 effect actors, plus the unchanged normal selection
oracle. This addresses those demonstrated paths; the user's exact live
invisibility case still needs retesting after activation.

Verification:
- Non-race changed-server gate passed (116.0 s); timed-power tests additionally
  passed after adding existing-knockdown cleanup. Arena-only unique HP,
  weak-level ceiling, owner/lease isolation, expiry, lethal direct/periodic
  protection and free-MP admission/preparation/charge are covered.
- Client quick type/ownership/capability/execution checks pass.
- 110 character-presentation/ground-item tests passed; the final focused
  selection/death/ground-item batch passed 23 tests, followed by all three
  visibility-selection tests after the readability cleanup.
- Full client checks were attempted. Missing `SRO_GAME_ROOT` was corrected;
  the retail panel audit then matched 14 resources / 242 controls. Test
  types and other source gates pass. The full test and UI-action-census gates
  still fail against existing shared generated/extracted asset mismatches
  (e.g. native skill catalogue 27,835 versus 28,065 expected, portal count
  85 versus 90, and the Fortress map census). Those unrelated assets,
  assertions and census goldens were not changed to hide failures.

Logs are `.state/sook-powerups-check.log`, `.state/sook-client-check.log`,
`.state/sook-client-quick.log`, `.state/sook-final-client-tests.log`, and
`.state/sook-visibility-test.log`. No full green client gate or live powerup
acceptance is claimed. No commit/push was requested. Next: user runs the
idempotent restart helper, then test eggs and dense AoE/loot on port 4180;
preserve original authority and all existing characters/gear.

Activation check after the user's "Ready - I'll test myself" reply still found
job version 86, no newer Nomad deployment and a different installed versus
candidate executable hash. Both 8788 readiness and client 4180 return 200,
but that readiness belongs to the prior server. LabWizard is now level 95
from the user's play, with 13 Sun +8 pieces, 96 skills and 18 hotbar bindings;
preserve this progress. Restart-script syntax validates. Asked the user
whether the command produced an error or has not yet been run. Do not claim
the powerups or weak-unique update activated based solely on "Ready."

The user then explicitly instructed "you run the command." Running the exact
prepared `Prepare-ArenaWizard.ps1` through the normal execution tool was
accepted and completed successfully (no alternate control path or approval
bypass). Job version **88** now has the corrected candidate executable:
installed/candidate SHA-256 match. LabWizard remained id 6, level 95, with
13 Sun +8 pieces, 96 skills and 18 hotbar bindings. Client 4180 returned 200;
GameWorld was still in its normal 503 startup warmup at the first check.
Do not assume the historical service-control rejections prohibit retrying
the same reviewed command after a new explicit user instruction.
Warmup completed: `/transport/readyz` returned **200**, and Nomad deployment
88 finished **successful**. The powerup/weak-unique server update is now
active. The user chose to test themselves; no browser gameplay inputs were
sent. Use client **4180**, not the older source client on 5180, for egg art
and the arena visibility-budget correction. Live gameplay confirmation is
still pending their test.

### Current activation: monster visibility, eggs and buff board

The player's usual port 5180 was still served by `opensro-pet-defaults`.
Stopped that exact old Vite process and started this worktree on 5180;
4180 also remains available. Reloaded the existing 5180 browser tab so it
loads the corrected actor-budget selection and crow Stormbrood egg mesh
(published resource 5000001), rather than the native event-box drop mesh.
The egg override is restricted to survival world definitions 30/31 and
region 17989. Normal drops retain their native artwork.

New arena monsters receive entrant aggression immediately on creation,
without assigning damage credit. Timed power pickups now use the existing
top-left param-job buff board, with native countdown bars and descriptive
tooltips. Virtual references 0x7f530001/003/005/006 represent Damage Surge,
Unstoppable, Unkillable and Arcane Spring. The bomb is instantaneous and
does not leave a timed icon. Expiry and arena exit send native end rows;
these powers are neither inventory items nor persisted native param jobs.
Four reserved HUD slots do not consume the eight native param-job slots.

Verification: all TestSook server tests passed, including spawn aggression
and timer expiry/exit cleanup. Changed-server build, gofmt, vet, lint,
tests and architecture gates passed (60.8 seconds). Client quick checks
and test-type baseline checks passed; 25 focused runtime tests passed.
All four buff artwork URLs return PNG/200. Existing unrelated full-client
generated-data failures documented above remain unresolved.

Ran the reviewed restart helper directly: Nomad job version **90**, with
installed/candidate SHA-256 both
`414EA478FA2783D26FD5252C20FF5B90410E4167052D07A74C2482C3925A1DE3`.
The helper preserved the existing LabWizard unchanged (now level 96).
Initial readiness was normal startup 503; confirm 200 after warmup.
Warmup subsequently completed: readiness returned 200 and deployment 90
reported successful. The existing browser was reloaded at 5180.
Live dense-wave visibility, egg appearance and pickup HUD still need
engine confirmation; automated regressions alone do not establish those.
LabWizard has used today's three entries; do not weaken the product's
daily entry rule to test. LabPetTest can use the new UTC day's allowance.
No commit or push was requested.

Owner requested a fresh test allowance: stopped the disposable GameWorld,
backed up its complete authority directory to
`.state/sook-live/entry-reset-backup-20261107-004359`, then used the typed
store update to reset only LabWizard's SookArenaEntries from 3 to 0.
Level 96, 23 inventory rows and 96 skills were retained. The helper refuses
an active run and can open only this fixed copied authority. The global
three-entry rule and original database were unchanged. Restart submitted;
wait for readiness before testing.
Readiness subsequently returned HTTP 200; the game tab was reloaded on 5180.

### Confirmed invisible-reference cause (live inspection)

The actor-budget change alone did not solve the user's missing monsters.
Read-only Chrome debugger inspection of the live arena established that
selected, living monsters 14799, 14800 and 14822 had **no resourceFor entry**
despite all eight catalogues being admitted, with no asset-loader error.
Their names were Basalt Stone Ghost and Myeolcheon Clay Archer. They were
present in simulation with correct ground heights, but no body/pick mesh.
Other species, the crow egg and timed power HUD were visibly rendering.

These references come from the optional Qin-Shi Tomb overlay, not the
native characterdata. The test job had SRO_QINSHI_TOMB=on while the shared
client NPC manifest contained none of these overlay references. Thus the
earlier model-path/BSR audit did not establish reference-ID coverage.

Aligned this disposable arena setup with its installed client: the reviewed
Start-ArenaTest.ps1 now overrides SRO_QINSHI_TOMB=off in both task env and
env templates. Restarted with the existing helper, preserving LabWizard.
No native gameplay code, shared assets or original authority were changed
for this correction. The expansion can be enabled later only with a matched
client NPC/model build. This is a deployment fix, not a repaired Tomb bake.

Exported the native arena pool using the same Go BuildPool/LoadTemplate
owners: all **199** eligible references resolve by exact ID through the
installed client NPC manifest to an existing GLB (zero missing references).
The updated job reports the overlay off. Warmup and live visual retest
remain the next checks; do not claim completed from the manifest audit.

Live retest then passed: all five sampled living Guardroky/Mine Shaitan
actors had `modelReady=true`, with a visible, selectable Mine Shaitan in
`page-2026-11-07T00-58-13-886Z.png`. Two subsequent LabPetTest runs rendered
their Antelope waves correctly too. Debugger reads only inspected state;
no browser code changed simulation, kills, money or time.

### Kill-based payout verification

Replaced the survival-only 1M prize with the KillReward brackets above.
Death and timeout award the same bonus, once; zero kills pay zero. Existing
cancel/disconnect/restart forfeiture and signed-balance overflow guard stay
in place. NPC and entry notices explain the change and fit the native
90-character banner limit.

Server tests cover every bracket edge, the 5,000+ cap, a 31-kill timeout
paying 60,000, an 11-kill death paying 30,000, repeated cleanup not paying
twice, and cancellation/disconnect/restart not paying. Changed-server gates
passed (81.1 seconds); the final notice-only shortening was followed by
focused TestSook/TestKillReward checks and a fresh native server build.

Live browser tests used existing LabPetTest, with its equipment untouched:
1. Real Antelope combat ended in death with zero kills; notice said
   `Arena finished: 0 kills. Reward: 0 gold.`
2. Re-entered, selected a visible Antelope, used the existing Soul Fire
   imbue and Heaven Chain through the normal UI, and killed two monsters.
   Allowed normal combat to kill the character. The client received
   `Arena finished: 2 kills. Reward: 10000 gold.` Its exact progression gold
   rose from **400111 to 410111**. No ground gold was picked up. Automatic
   arena revival/return restored HP and returned the character to So-ok.

The timeout branch was verified with the injected server clock in the real
action integration test, not a new 30-minute real-time browser run. No
recording or generated asset was added to git. Original authority and all
character equipment remain untouched; the isolated test character retained
the legitimate combat rewards. No commit/push was requested.

Final activation: job **98**, readyz **200**. Installed and candidate
executables both SHA-256
`60F847EE6F548272B2593CB140959F5CB9E5D6A32AD3DDCACD163B758BBCBFE6`.
SRO_QINSHI_TOMB remains off, matching the installed asset catalogue.
Backed up the copied authority again to
`.state/sook-live/entry-reset-backup-20261107-010910`, then reset only
LabWizard's daily entry counter 3 -> 0 for the owner's next test. The typed
helper confirmed level 97, 25 inventory rows and 96 skills retained and
refused active-run resets. The production entry rule remains three/day.
LabPetTest used two real entries, returned safely with gear unchanged and
kept its legitimate 10,000-gold bonus. Its pre-existing GM setup was not
expanded to any other character. No pet skill or unrelated feature changed.

### Restart after host services stopped (2026-11-07 morning)

Both 5180 and 8788 and the Nomad listener were stopped. Restarted the
worktree's sro-nomad dev-agent using the main checkout's existing
`apps/server/ops/nomad/config/dev-windows.hcl`, pinned Nomad 2.0.7, and
the existing `opensro-lab/apps/server/.state/nomad/dev-agent` directory.
The dev-mode scheduler had lost its in-memory job/variable registrations.
Restored the saved agent job from the auto-potion deployment's
`jobs-before.json`, using that deployment's matched agent.exe and pointing
its authority at this arena copy. Restored the three service variable
records from the last rendered agent/world allocation secrets, without
displaying or changing their credentials. Registered a stopped world
placeholder from job-before.json, then used Start-ArenaTest.ps1 to launch
the existing tested arena executable. No builds, migrations, character
resets or gear edits were performed.

Morning verification: readiness 200, world deployment successful, LabPlayer
login accepted and existing character-selection models rendered. Left the
headed `pet13` browser at character selection on 5180 for the owner.
Nomad exec session 63245; worktree Vite exec session 33713. After a host
restart, check these listeners and restore the same saved jobs/variables;
do not deploy a newer main-branch binary over this test database casually.

Owner then requested cap 90 and LabWizard level 90. Start-ArenaTest.ps1
now explicitly overrides SRO_LEVEL_CAP=90 in both task env and rendered
env templates (previous saved configuration was 110). Stopped only the
test GameWorld, backed up its authority directory to
`.state/sook-live/level90-backup-20261107-071554`, then used a fixed-path,
typed-store helper to change only LabWizard 97 -> 90, MaxLevel=90, EXP=0,
and its original fully allocated level-90 wizard stats STR109/INT376 with
zero spare stat points. HP/MP are recalculated on login. All 28 inventory
rows and 96 learned skills remain; other characters were not lowered.
Existing native/expansion cap transition tests pass, including XP grants
at native level 90 remaining at level 90. The active job now reports 90;
no shared assets or production database were edited.
