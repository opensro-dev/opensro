/*
===========================================================================

weather.go - delivers the climate cycle's weather to players (0x3BDE)

The reference shard runs one weather state per climate
(world/climate) and the game server tells each player the weather of the
region it stands in. This file is that delivery, behind SRO_WEATHER (on
by default, as natively; off sends no frame at all and every client
stays clear):

  - a climate's current weather changes: every player whose region
    belongs to that climate gets the new weather (advanceWeather);
  - a player crosses into another region: it gets the new region's
    weather, always, even when it equals the old one (the native region
    change does the same); the crossing is noted where the region is
    bound (world_return.go) and sent by the next tick;
  - a loading scene ends (first entry, teleport, a resumed transport):
    the game-ready answer carries the region's weather
    (SceneWeatherFrames), because the client drops the server's weather
    at every world reset and entry (event-environment.ts).

A crossing into a region without a climate sends the zero weather (kind
0, which the client draws as clear), as natively: every region's weather
word starts at zero (the v1.188 region constructor) and every crossing
sends the new region's word (the PR's native-claims table cites both). A scene that
starts in one sends nothing, since the client already reset its weather.
The Christmas snow needs no
case here: while event group 1 is on, the client draws its own snow
whatever 0x3BDE says.

===========================================================================
*/
package action

import (
	"sync"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/climate"
	"opensro.online/server/internal/game/world/simulation"
)

// weatherOpcode is the weather packet (client 7775A0): u8 kind (1 clear,
// 2 rain, 3 snow), u8 amount.
const weatherOpcode uint16 = 0x3bde

/*
================
weatherCrossing

A region crossing waiting for the next tick's weather frame.
================
*/
type weatherCrossing struct {
	division, name string
	region         uint16
}

/*
================
weatherState

The climate cycle and the crossings waiting for delivery. The tick
updates the cycle while handlers (game-ready, a movement's region bind)
read it and note crossings, so one mutex guards both. A nil world is
SRO_WEATHER off. spare is the crossing buffer the tick last delivered,
reused as the next queue.
================
*/
type weatherState struct {
	mu               sync.Mutex
	world            *climate.World
	crossings, spare []weatherCrossing
}

/*
================
Runtime.StartWeather

Starts every climate at nowMs (SRO_WEATHER on). Nil random is the native
rand() range; tests force their own draws.
================
*/
func (rt *Runtime) StartWeather(nowMs int64, random climate.Rand) {
	rt.weather.mu.Lock()
	defer rt.weather.mu.Unlock()
	rt.weather.world = climate.NewWorld(nowMs, random)
}

/*
================
WeatherPayload
================
*/
func WeatherPayload(weather climate.Weather) []byte {
	return []byte{weather.Kind, weather.Amount}
}

/*
================
Runtime.SceneWeatherFrames

The weather a finished loading scene starts with: the live region's
current weather, or nothing while SRO_WEATHER is off or the region has
no climate.
================
*/
func (rt *Runtime) SceneWeatherFrames(divisionID string, character *enterworld.Character) []wire.Frame {
	if character == nil || !rt.weatherOn() {
		return nil
	}
	region := rt.LiveRegionFor(divisionID, character)
	rt.weather.mu.Lock()
	defer rt.weather.mu.Unlock()
	if rt.weather.world == nil {
		return nil
	}
	weather, ok := rt.weather.world.RegionWeather(region)
	if !ok {
		return nil
	}
	return []wire.Frame{{Opcode: weatherOpcode, Payload: WeatherPayload(weather)}}
}

/*
================
Runtime.weatherOn
================
*/
func (rt *Runtime) weatherOn() bool {
	rt.weather.mu.Lock()
	defer rt.weather.mu.Unlock()
	return rt.weather.world != nil
}

/*
================
Runtime.noteWeatherCrossing

Called where a character's region is bound to a new region; the next
tick sends the new region's weather.
================
*/
func (rt *Runtime) noteWeatherCrossing(division, name string, region uint16) {
	rt.weather.mu.Lock()
	defer rt.weather.mu.Unlock()
	if rt.weather.world == nil {
		return
	}
	rt.weather.crossings = append(rt.weather.crossings, weatherCrossing{division: division, name: name, region: region})
}

/*
================
Runtime.stepWeather

The locked half of a weather tick: every climate's update, and the
crossings noted since the last tick swapped out for delivery (two
buffers trade places, so a quiet tick allocates nothing). The world is
written only here, on the tick goroutine, so the tick reads it unlocked
afterwards; handlers read it under the lock.
================
*/
func (rt *Runtime) stepWeather(nowMs int64) (*climate.World, uint32, []weatherCrossing) {
	rt.weather.mu.Lock()
	defer rt.weather.mu.Unlock()
	world := rt.weather.world
	if world == nil {
		return nil, 0, nil
	}
	crossings := rt.weather.crossings
	rt.weather.crossings, rt.weather.spare = rt.weather.spare[:0], crossings
	return world, world.Update(nowMs), crossings
}

/*
================
Runtime.advanceWeather

One tick of every climate, then the frames: the players of each climate
that changed, and every crossing noted since the last tick, a crossing
into a climate-less region included. The store
lookups run outside the weather lock. With nothing changed and nothing
crossed it allocates nothing.
================
*/
func (rt *Runtime) advanceWeather(nowMs int64) []simulation.DivisionFrames {
	world, changed, crossings := rt.stepWeather(nowMs)
	if world == nil || changed == 0 && len(crossings) == 0 {
		return nil
	}
	var out []simulation.DivisionFrames
	if changed != 0 {
		rt.characterAdmissions.Range(func(_, value any) bool {
			owner := value.(populationAdmission)
			if !owner.regionBound {
				return true
			}
			index, ok := climate.RegionClimate(owner.region)
			if !ok || changed&(1<<index) == 0 {
				return true
			}
			out = rt.appendWeatherFrame(out, owner.division, owner.name, world.Current(index))
			return true
		})
	}
	for _, crossing := range crossings {
		// A region without a climate keeps the zero weather word (see the
		// file banner), so the crossing still sends it.
		weather, ok := world.RegionWeather(crossing.region)
		if !ok {
			weather = climate.Weather{}
		}
		out = rt.appendWeatherFrame(out, crossing.division, crossing.name, weather)
	}
	// Drop the names before the buffer is reused.
	clear(crossings)
	return out
}

/*
================
Runtime.appendWeatherFrame

One character's private weather frame; a character gone from the store
gets none.
================
*/
func (rt *Runtime) appendWeatherFrame(out []simulation.DivisionFrames, division, name string, weather climate.Weather) []simulation.DivisionFrames {
	c := rt.findCharacter(division, name)
	if c == nil {
		return out
	}
	return append(out, simulation.DivisionFrames{DivisionID: division, OnlyCharacterID: c.ID,
		Frames: []simulation.Frame{{Opcode: weatherOpcode, Payload: WeatherPayload(weather)}}})
}
