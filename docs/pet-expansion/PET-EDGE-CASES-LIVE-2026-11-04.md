# Pet edge-case live checks — 4 November 2026

Tested build: `58c6ce11`, protocol **13**, authority schema **35**.
Temporary actors used the running local Agent/GameWorld through normal,
authenticated game-protocol commands and the current client decoder. No mock
clock or injected damage events were used. No gameplay source fix was needed.

## Results

| Case | Observed result |
| --- | --- |
| Retaliation I | Ten incoming bear hits triggered a stun (native status `16384`). |
| Retaliation II | Independent bear and owner counters triggered stuns on their attackers. |
| Retaliation III | Bear- and owner-centered ripple cues appeared. Server diagnostics recorded 5 nearby targets / 2 stunned, then 7 / 5. The attacker was stunned; nearby targets used independent 50% rolls. |
| Owner resurrection, rank 1 | HP returned from 0 to 1,322 / 13,226. Death lost 694,900 EXP; resurrection refunded 69,490. The saved cooldown was 600,000 ms. A second death remained blocked during reuse. |
| Owner resurrection, rank 10 | Full HP restored, with no additional net EXP loss after revival. |
| Party resurrection III | HP returned from 0 to 13,490 (full), and EXP returned to 100,000,000. Both the revived member and bear owner received revival packets. |
| Execute I | Pet executed low-HP target `468510`; execute cue and fatal hit recorded. |
| Execute II | Owner's nonfatal 12,377-damage action left target `466158` below threshold; Execute then fired and HP became 0. |
| Execute III | Pet executed target `461564`; two seventh-attack sweep cues were also observed. |
| Clone expiry | Hatch at `1793800644816`, expiry at `1793801244909`: **600,093 ms** alive. Wind opcode `0x3FBE` emitted; clone removed. Dummy `462471` took 17,253 damage and a nearby ordinary target took 17,050. |

The local assertions in `verified-edge-results.json` passed for every category
above. These are functional checks, not statistical validation of the 50% stun
probability. An isolated off-target seventh-hit execution kill, the resurrection
pet-level +10 rejection, and audiovisual polish were not repeated in this run.

## Fixtures and corrections

The initial owner Execute fixture had starter gear, dealt too little damage
and died to poison. A later temporary STR 5,000 / D9+8 setup bounded that
scenario. A full-health 3,121-HP enemy was normally one-shot without an execute
cue (negative control); the 93,629-HP enemy supplied the threshold execution.
This fixture does not establish normal player balance.

Native battle state lasts **20 seconds**, and hits on a pet refresh it. Clear
remaining test aggressors, stop attacks, then wait at least 21 seconds before
dismissing. Refused early swaps were correct behavior. Dead actors correctly
refused GM spawn/warp; normal return-to-town was used to continue. GM scene
transitions can end the minimal protocol probe, requiring reconnection.

## Preservation and handoff

All **five complete character records** matched the original pre-test backup
byte for byte after cleanup, including stats, EXP, inventory, pets and gear.
Original ground-item rows also matched; SQLite integrity check passed.
Temporary GM privileges were removed by restoring the exact original job
environments. Agent, GameWorld and client readiness returned HTTP 200.

Machine-local evidence pointer: `.state/protocol13-edges-current.txt`.
Evidence includes `verified-edge-results.json`, `rank1-proof.json`,
`cooldown-denial.json`, `party-revival-proof.json`, `clone-expiry-proof.json`,
`cleanup-records.json` and `cleanup-deployment.json`. Logs, database backups,
job snapshots containing secrets, and recordings must never be committed.

See also [all-tier live checks](PET-ALL-TIERS-LIVE-2026-11-04.md) for earlier
coverage and remaining isolated scenarios.
