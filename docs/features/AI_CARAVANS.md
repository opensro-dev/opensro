# Optional AI caravans and thieves

Port-only, not native. The switch below is off by default, and off leaves the shard native. When on, AI caravan
and thief agents travel the Jangan - Donwhang - Hotan trade highway, remember the players they meet and decide
what to do next.

The nine agents are transient server-owned characters with shipped job suits, weapons and trader
transport COS. They share normal player targeting, skill damage, region protection and transport
cargo scattering. They never become account-owned or persisted player characters.

Agents use the configured level cap (native 90, or 110 with the matching expansion data), full
level-appropriate armour/accessories, and alternating ordinary and Seal of Sun sets. Three supported
weapon skills are trained at their highest eligible ranks; casts honor native reuse/cost rules and
fall back to a base attack while those skills cool down. Low mana triggers the paced AI potion lane.
Traders mount through normal ride admission and publish the mount after its spawn for late viewers.
They dismount to fight; hunters acquire and attack opposing native job NPCs as well as player thieves.
Loaded AI transports register with the normal caravan ambush timer. Native thieves spawn only in
battlefield regions, after the ordinary randomized timer; they can kill a transport and scatter cargo.
AI characters enter the population and monster target census without becoming socket viewers.

Routes load specialty traders, gate guards and ferry links from shipped data. Each walking leg uses
the authoritative navigation geometry; bounded incremental road searches find detours around blocked
terrain. Ferries require source proximity, admission and the native fare. Convoy hunters follow their
trader, including admitted ferry crossings. Route heat selects alternate crossings.

Escort commands require the correct worn job suit, a living nearby player and an available recruiter
in town. `!hire <hunter>` charges 15,000 gold once. `!accept <trader>` requires a Hunter to remain within
320 units for at least 90% of a surviving run (minimum 30 seconds; observation gaps invalidate it).
Rewards are limited to three per character per UTC day and persist with the gold credit. Schema 48
adds these optional counters; the local launcher retains a backup and runs the offline upgrade.
Commands are limited to one per character per second. Chat cannot deal damage: use ordinary combat.
Dialogue and model notes are filtered as plain, bounded text.

## Local testing

Start the launcher from this worktree so it builds this implementation. The feature is off by default;
the standard `apps/server/config/local.env` does not enable it. In PowerShell, set the switch in the same
terminal that launches the game:

```powershell
$env:SRO_AI_CARAVANS = "on"
$env:SRO_AGENT_BRAIN = "hardcoded"
node scripts/local/run.mjs
```

The GameWorld log must contain `AI caravan & thief agents ON`. In Jangan, Bao and Kwan stand beside
Adventurer Flora: region **25000**, approximately **X=1681/1691, Y=11, Z=261**. The stable is a different
landmark. GM testing can use `/warp 25000 1675 11 260` while dismounted. Other agents travel or stage
at Donwhang and Hotan. Only the default outdoor world receives them. Use `!caravans` for status;
admin world-map markers show current positions. Use full agent names, e.g. `!hire [AI]Hunter_Kwan`.

## Verification checkpoint (2026-11-17)

Automated checks exercise all six directed highway routes and six alternate routes against shipped
navmesh/ferry data, real actor skill damage, cargo drops, transient actor persistence, command/job
validation, contract survival/proximity and persisted daily reward limits. Feature-off boundaries
remain covered. Hands-off browser checks use a separate copy of the local world; test mutations never
provision or move the owner's original character.

The hardcoded brain is the tested mode. Remote model integration remains loopback-only and disabled
for this run. The Windows race gate requires a cgo-capable C compiler; the host currently has no
configured compatible compiler, so ordinary tests/static checks run but race verification is incomplete.

## Switches

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_AI_CARAVANS` | GameWorld, job variable `ai_caravans` | The agent roster and its tick; memories persist in store layout 14 (`caravan_agent_memories`, `caravan_route_heat`) |

## The decision brain

The agents use the hardcoded brain: deterministic utility and state-machine rules.

The code also has an LLM brain (`SRO_AGENT_BRAIN=llm` or `hybrid`, with `SRO_AGENT_LLM_URL`). Its prompt carries
nearby players' character names and stats, so by owner decision it stays off, and these settings are not job
variables: no deployment sets them. On a developer machine the GameWorld still refuses any `SRO_AGENT_LLM_URL`
that is not a loopback address (`localhost`, `127.0.0.0/8`, `::1`), logs the refusal and uses the hardcoded brain.

## Follow-up checkpoint: mounted caravans and native NPCs

Latest main was merged at `945340ea` (main `cb43b671`); no PR was opened. The native mount,
ambush registration and damage owners are reused rather than duplicating their protocol/formulas.
Regression checks cover AI-only monster acquisition without viewers, actual ambush NPC creation,
mount/dismount admission, all highway legs, maximum-level builds and advanced hunter skill damage
against a shipped level-90 thief reference. Hands-off browser testing confirms attached riders and
visible equipped armour; test-world logs also record native NPC transport kills and real cargo drops. Defeat now marks the actor for a fresh loaded transport and resets observer
appearance before respawning; this is covered by a transport-recovery regression.

Public implementation references consulted:

- [JadeRunner](https://github.com/B1Q/JadeRunner): compatible learned skill rotations, reuse-aware
  retries and server-owned approach/cast outcomes; client automation, not a server AI replacement.
- [phBot xCaravan](https://github.com/Vette1123/phbot-plugins#xcaravan): explicit role routes, job
  equipment, ferry travel and settlement states; its later job system is not copied into v1.150.
- [iSRO AI-player developer report](https://www.reddit.com/r/silkroadonline/comments/1xrm1x2/i_spent_months_rebuilding_a_silkroad_online/):
  full characters, trader/hunter/thief roles and mounted travel; implementation is not public, so its
  claims are treated as a comparison rather than code or performance evidence.

Fresh native investigation attempted the released revtool seed. Its checksum is valid, but this
host's client executable hashes to `f6580bf65213c0c074efe312514c55c73040667e7a9ad1aae9b8595d48ccd3a2`,
not the seed's `375e8682...`; the mismatched labels were not applied. Existing verified native owners
and regression fixtures provide the mount/ambush contracts used here. No new native-default rule changes.
