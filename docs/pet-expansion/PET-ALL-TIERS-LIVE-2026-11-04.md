# Pet tier checks — 4 November 2026

Build: `58c6ce11`, protocol **13**, authority schema **35**.

## Live method

Twelve temporary level-90 pet profiles covered four pets and all three books
of each of their five rows. Previous books were maxed; book III used rank 1
(the legitimate level-90 requirement). Test-only operator bonus points made
each complete profile possible without changing normal point progression.

Two normal authenticated native-protocol sessions formed a party. Five
stationary GM training dummies supplied targets; a third, real Chrome/WebGPU
client observed. Casts, hits, buffs, profiles and counters were decoded using
the current client code. No direct browser-state injection or game-time jump.

## Observed results

| Pet | Books I / II / III | Live evidence |
| --- | --- | --- |
| Wolf | Passed exercised paths | Howl: pet only / owner / party. Rend: 1 / 3 / 5 targets. Bloodscent: 5 / 8 / 10 wolf stacks; separate owner pool reached 10. Blood Hunt marked prey at four hits; owner and wolf seventh-hit counters progressed independently. |
| Bear | Passed exercised paths | Roar casts and cooldowns; Iron Hide protects pet + owner, then party; three tier-III shields expired with detonation flags. Basic attacks committed damage. |
| Penguin | Passed exercised paths | All Ice Lance / Blizzard books cast; all profiles reached five Resonance and Hourglass charges. Area damage and status/counter updates continued without client errors. |
| Crow | Passed exercised paths | All Plume / Stormbrood books cast; eggs hatched after real 30-second waits. All books reached ten Tailwind and five Guiding Feathers stacks. Tier III shared support with the party. |

Additional owner/party check: learned Fire imbue spent MP, then crow attacks
restored MP to both recipients. Brood's recharge deadline shortened by about
5.5 seconds beyond elapsed wall time, demonstrating Mana Spring III recovery.
Repeated premature skill casts were refused by the shared row cooldown.

Both supplemental Go gameplay runs passed: pet/companion selection and
Guardian/Resolve/Retribution/crow/penguin/wolf conditional regression selection.
These are automated gameplay checks, separate from the live evidence above.

## Still not proven by these dummy tests

Training dummies never attack and start with 100 million HP. Consequently this
run does **not** establish all 60 books' conditional effects end to end.
Remaining live scenarios include:

- Unbroken Spirit full-stack slow/knockback resistance and empowered cast
  still need isolated live checks. Retaliation and owner/party resurrection
  were subsequently covered in the edge-case report linked below.
- Off-target seventh-hit execution kills, Shatter death bursts and an isolated
  numerical Blood Hunt bonus/splash comparison. Execute I/II/III threshold
  kills were subsequently covered in the edge-case report.
- Exact echo balance comparison and extended status durations. Owner magical
  stacks, a committed two-batch echo and tier-III freeze were observed in the
  follow-up below.
- Plume poison/burn cleanse and the ten-clone cap were not repeated. Natural
  ten-minute clone expiry/wind damage subsequently passed; see the edge-case
  report. Owner/party Tailwind stun and Guidance consumption passed above.
- Frame-by-frame review and audible verification of every animation/sound.

Earlier handoffs contain older evidence for some of these; it is not relabelled
as a fresh protocol-13 live pass. No production fault was found in the paths
exercised here, so no gameplay source change was warranted.

## Preservation and repeatability

Machine-local backup/evidence pointer: `.state/protocol13-alltiers-current.txt`.
It contains the pre-test SQLite backup, original Nomad jobs (secrets: never
commit), twelve per-profile logs, `tier-matrix.json` and
`owner-party-focus.json`. The local handoff records final restoration and
readiness. All five original full character records must match the backup
before test cleanup is declared complete. Restore original GM scope, then
restart to clear temporary dummies/clones; preserve assets and source version.

Final cleanup passed: all five full records restored exactly, database integrity
OK, temporary GM removed, original service environments restored, and Agent,
GameWorld and client all returned HTTP 200. No gameplay source changes.

## Focused live follow-up

Fresh reversible fixture: `.state/protocol13-focused-current.txt`. The two
test characters temporarily learned native Sword Smash and magic skills and
wore a Copper Sword. Production progression and original characters were
unchanged outside the fixture.

- Plume I restored owner HP immediately, then five healing ticks (~248 HP
  each) at one-second intervals; ordinary regeneration was separate.
- Tailwind II reached ten stacks, stunned the dummy on the owner's next hit
  and consumed travel stacks. Guidance II reached five stacks and the next
  committed Sword Smash consumed them.
- Penguin owner magic and pet spell pools both reached five. A charged Ice
  Lance produced two separate three-target damage batches in the same tick
  (primary 16,132 and 25,881 damage), with charge consumption. This establishes
  an echo occurred, not an isolated balance comparison.
- Blood Hunt reached four marks, then Rend hit five dummies and left one
  newly earned mark. Separate numerical bonus/splash isolation remains open.

The first party follow-up was invalid: pathing near the town boundary and
GM teleport scene transitions interrupted the native test client. Its events
are retained, not counted as party passes. A short refusal packet also exposed
a bounds assumption in the ignored probe logger; the real client handled it.
Only the probe was corrected. No gameplay source change was justified.

Final party repeat passed in the clear Jangan field, after reconnecting the
native probe following the GM scene transition: both owner and party member
reached ten Tailwind stacks and stunned different dummies (469313/469312).
Both independent Guidance pools reached five and became zero after each
player used Sword Smash. `party-final.json` preserves the signal sequence.
The apparent party failure was a test-session/position problem, not a
production fault. All five full character records were restored exactly
again after this second fixture; original GM scope and jobs were reinstated.

Subsequent checks: [retaliation, resurrection, Execute and natural clone expiry](PET-EDGE-CASES-LIVE-2026-11-04.md).
