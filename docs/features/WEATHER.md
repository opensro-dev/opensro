# Region weather

Native. Every region follows its climate: spells of rain and, in the cold lands, snow come and go, and each change of
weather fades in over about half a minute. The switch is on by default, as the original server is.

## Switch

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_WEATHER` | Server | `on` (the default, native): every climate runs its weather cycle. `off`: no weather is sent and every client stays clear |
| `SRO_GM_WEATHER` | Server | Port-only, not native (owner, 2026-11-26). `off` (the default, native): `/WEATHER` answers that it is off. `on` (this shard, `config/shard-settings.json`): a GM with world control types `/WEATHER clear\|rain\|snow [amount] [minutes]` |
| `SRO_CITY_SNOW` | Server | Port-only, not native (owner, 2026-11-28). `off` (the default, native): towns take their climate's weather. `on` (this shard): every town region (one that forbids player combat) snows at a steady strength 60 |

`SRO_WEATHER` unset reads as on. The ops GameWorld job carries it as the Nomad variable `weather` (default `"on"`).

With `SRO_GM_WEATHER` on, `/WEATHER` sets the weather on the climate of the region the GM stands in. The climate fades
to it the native way, holds it for the minutes (default 10, 1 to 120), then goes back to its own cycle. An amount of 0
or none takes the climate's own rain or snow base. It needs `SRO_WEATHER` on too. The Nomad variable is `gm_weather`
(default `"off"`).

## Seasonal skies

Port-only, not native (`internal/game/action/seasonal_sky.go`):

- With `SRO_CHRISTMAS_EVENT` on in mode `decoration`, the winter event's snowfall timetable (one hour every two, on
  even UTC hours, none starting at 03, 04 or 05) snows in every region. Each snowfall draws its own strength, 40 to 200,
  from its start hour, so a late entry or a restart sees the same snowfall.
- With `SRO_CITY_SNOW` on, the towns snow all the time outside the snowfalls. The Nomad variable is `city_snow`.
- A GM's `/WEATHER` on a climate wins over both while its spell runs. Sending it also releases the client's
  console weather hold (a debug `/rain` or `/snow` that ignores the server until `/clear`).

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

  A region without a climate is clear: a crossing into it sends the zero weather (kind 0, drawn as clear), as
  natively, and a scene that starts in it sends nothing, since the client has just reset its weather.

The package holds no clock or random source of its own; the caller passes both, so its tests are deterministic.

## Native evidence

The cycle's constants and rules come from the v1.188 shard manager's disassembly. Each constant's banner in
`world/climate/climate.go` names its rule. One port-only bound: a drawn amount is capped at 255, where the native
8-bit add would wrap. No shipped climate reaches it (the largest is 150 + 40 = 190).
