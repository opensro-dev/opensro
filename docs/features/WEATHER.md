# Region weather

Native. Every region follows its climate: spells of rain and, in the cold lands, snow come and go, and each change of
weather fades in over about half a minute. The switch is on by default, as the original server is.

## Switch

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_WEATHER` | Server | `on` (the default, native): every climate runs its weather cycle. `off`: no weather is sent and every client stays clear |

Unset reads as on. The ops GameWorld job carries it as the Nomad variable `weather` (default `"on"`).

## How it works

- **Data.** `scripts/build/import_region_climate.py` reads the reference shard's `_RefClimate` rows (each climate's
  initial weather, roll division, spell duration, and rain and snow amounts and chances) and the climate each
  `_RefRegion` row names. It writes the immutable Go table `world/climate/climate_generated.go`. The backup is read
  page by page and never restored.
- **Cycle.** `world/climate` runs one state machine per climate, a port of the v1.188 shard manager's climate update.
  A periodic roll (12,600,000 ms divided by the climate's division) picks rain, snow or clear from its chances, the
  spell lasts its duration, then the climate returns to its initial weather. The weather fades toward its target, at
  most one step every 3 s, and a change of kind fades the old kind out before the new one rises.
- **Delivery.** `action/weather.go` sends each player their region's weather (opcode `0x3BDE`):
  - when the climate's weather changes;
  - on every region crossing, even into the same weather, as natively;
  - when a loading scene ends (first entry, teleport, resumed transport), because the client drops the server's
    weather at each world reset.

  A region without a climate sends nothing.

The package holds no clock or random source of its own; the caller passes both, so its tests are deterministic.

## Native evidence

The cycle's constants and rules come from the v1.188 shard manager's disassembly. Each constant's banner in
`world/climate/climate.go` names its rule. One port-only bound: a drawn amount is capped at 255, where the native
8-bit add would wrap. No shipped climate reaches it (the largest is 150 + 40 = 190).
