/*
===========================================================================

seasonal_sky.go - the seasonal snow laid over the climate weather

Port-only, not native. Two skies the original never had, both delivered
as ordinary weather (0x3BDE) through weather.go:

  - the Christmas decoration's snowfalls (progression/christmas_snow.go
    timetable, SRO_CHRISTMAS_EVENT in mode decoration): every region
    snows for the snowfall's hour, each snowfall at its own strength.
    The event group the winter event uses draws one fixed strength in
    the client (event-environment.ts: group 1 is (3, 20)), and the owner
    asked for the strength to vary (2026-11-28), so the decoration's
    snow is weather instead;
  - SRO_CITY_SNOW (default off; owner 2026-11-28, on for our shard): the
    towns (the regions that forbid player combat) snow all the time.

A GM's /WEATHER on a climate wins over both while its spell runs, so a
GM can still show rain or a clear sky anywhere. The skies hold no state
but the snowfall a tick last delivered (weather.go): a snowfall's
strength comes from its start time, so a restart keeps it.

===========================================================================
*/
package action

import (
	"os"
	"strings"
	"time"

	"opensro.online/server/internal/game/world/climate"
)

// EnvCitySnow keeps the towns snowing (port-only, not native; owner
// 2026-11-28): "on", "1" or "true". Off, the default, is the native sky.
const EnvCitySnow = "SRO_CITY_SNOW"

const (
	// citySnowAmount is the towns' steady snow strength: a clear, steady
	// fall, well under a heavy snowfall.
	citySnowAmount uint8 = 60
	// A decoration snowfall's strength is drawn from
	// [snowfallMinAmount, snowfallMaxAmount]: from a light fall to a
	// heavy one (255 is the client's byte limit; the console's /snow 255
	// is a white-out).
	snowfallMinAmount = 40
	snowfallMaxAmount = 200
)

/*
================
CitySnowFromEnv
================
*/
func CitySnowFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvCitySnow))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
snowfallAmount

A snowfall's strength, drawn from its start: the same snowfall always
has the same strength (a restart or a late entry sees what the others
see), and two snowfalls differ. A splitmix64 finalizer over the start's
hour spreads neighbouring hours across the range.
================
*/
func snowfallAmount(start time.Time) uint8 {
	h := uint64(start.Unix()/3600) + 0x9e3779b97f4a7c15
	h = (h ^ (h >> 30)) * 0xbf58476d1ce4e5b9
	h = (h ^ (h >> 27)) * 0x94d049bb133111eb
	h ^= h >> 31
	return uint8(snowfallMinAmount + h%uint64(snowfallMaxAmount-snowfallMinAmount+1))
}

/*
================
Runtime.snowfallKey

The decoration snowfall falling at now, by its start (Unix seconds), or
zero when none falls. A change of key is when every player's sky must be
sent again.
================
*/
func (rt *Runtime) snowfallKey(now time.Time) int64 {
	start, snowing := rt.ChristmasDecoration.SnowfallAt(now)
	if !snowing {
		return 0
	}
	return start.Unix()
}

/*
================
Runtime.skyWeather

The weather a player in region sees: a GM's spell on its climate first,
then a decoration snowfall, then the town snow, then the climate's own
weather. False when none applies (a climate-less region outside every
seasonal sky), which weather.go treats as natively.
================
*/
func (rt *Runtime) skyWeather(world *climate.World, region uint16, snowfall int64) (climate.Weather, bool) {
	index, hasClimate := climate.RegionClimate(region)
	if hasClimate && world.GMSpell(index) {
		return world.Current(index), true
	}
	if snowfall != 0 {
		return climate.Weather{Kind: climate.Snow, Amount: snowfallAmount(time.Unix(snowfall, 0))}, true
	}
	if rt.CitySnow && townRegion(region) {
		return climate.Weather{Kind: climate.Snow, Amount: citySnowAmount}, true
	}
	if !hasClimate {
		return climate.Weather{}, false
	}
	return world.Current(index), true
}
