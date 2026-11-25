# Optional AI caravans and thieves

Port-only, not native. The switch below is off by default, and off leaves the shard native. When on, AI caravan
and thief agents travel the Jangan - Donwhang - Hotan trade highway, remember the players they meet and decide
what to do next.

This is epic #3704 (#3650), landed in slices. The agents run inside the GameWorld, appear as peers, fight and
talk (slices 2-4). Slice 5 wires the money, below; the reward and fee amounts are the sketch's, and the caps
are placeholders for the owner's call.

## Switches

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_AI_CARAVANS` | GameWorld, job variable `ai_caravans` | The agent roster and its tick; memories persist in store layout 14 (`caravan_agent_memories`, `caravan_route_heat`), the daily escort cap in layout 17 (`caravan_escort_days`, schema 60) |

## Slice 5: the economy, abuse-proof

The escort contract's reward, the hire fee and the parley's cargo drop move real value, under these rules:

- An AI-trader escort pays only when the Hunter stayed within escort range (`escortRangeUnits`, 150 world
  units) at every step of the run and the trader survived it; a trader killed on the road voids the run, and
  an abandoned or void run records no escort trust either.
- One Hunter holds one AI-trader escort at a time; `!accept` refuses a second until the first completes.
- Paid escorts are capped per Hunter per UTC day at `MaxPaidEscortsPerDay` (3, a placeholder for the owner's
  call), counted in `caravan_escort_days` as each payment is made, so a restart cannot reset the day.
- Hiring an AI Hunter debits `BaseHunterHireFeeGold` (15,000) from the trader's purse before the hire binds;
  a purse that cannot pay hires nobody.
- A parley tribute is the trader's own cargo: that many units of its origin town's real specialty trade
  goods (Jangan CH, Donwhang WC, Hotan AM - see `caravanai/tradegoods.go` for the data trail) dropped on the
  ground where it stands, never goods conjured for the occasion.
- Gold moves through the character's own mutation door, so the store ledger records it (doors
  `caravan-escort-reward` and `caravan-hire-fee`); an escort reward is created gold by design of the epic.

## The decision brain

The agents use the hardcoded brain: deterministic utility and state-machine rules.

The code also has an LLM brain (`SRO_AGENT_BRAIN=llm` or `hybrid`, with `SRO_AGENT_LLM_URL`). Its prompt carries
nearby players' character names and stats, so by owner decision it stays off, and these settings are not job
variables: no deployment sets them. On a developer machine the GameWorld still refuses any `SRO_AGENT_LLM_URL`
that is not a loopback address (`localhost`, `127.0.0.0/8`, `::1`), logs the refusal and uses the hardcoded brain.
