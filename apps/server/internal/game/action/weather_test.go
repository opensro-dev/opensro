/*
===========================================================================

weather_test.go - who receives the climate cycle's weather, and when

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// Regions of three climates (world/climate's generated table): Jangan and
// its neighbour run 1001 (clear 180), 0x52d9 the snowy 2004 (clear 100,
// the first to roll).
const (
	weatherJangan   uint16 = 0x62a8
	weatherJanganE  uint16 = 0x62a9
	weatherSnowy    uint16 = 0x52d9
	snowyFirstRoll  int64  = 12_600_000/7 + 1
	weatherTestUser        = "weather2"
)

/*
================
newWeatherTestRuntime

Two characters admitted to the open world, each placed in a region; on
starts the climate cycle at time zero with every rand() draw zero.
================
*/
func newWeatherTestRuntime(t *testing.T, on bool, regions [2]uint16) (*Runtime, [2]*enterworld.Character) {
	t.Helper()
	first, second := testCharacter(), testCharacter()
	second.ID, second.Name = 4, weatherTestUser
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {first, second}},
		Items:      testItems(),
	}
	rt := NewRuntime(deps, nil)
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(nil, nil))
	if on {
		rt.StartWeather(0, func() int { return 0 })
	}
	characters := [2]*enterworld.Character{first, second}
	for i, c := range characters {
		placeWeatherCharacter(rt, c, regions[i])
		if err := rt.AdmitCharacterSession(testDivision, c.Name, uint64(i+1)); err != nil {
			t.Fatal(err)
		}
	}
	return rt, characters
}

/*
================
placeWeatherCharacter
================
*/
func placeWeatherCharacter(rt *Runtime, c *enterworld.Character, region uint16) {
	key := simulation.WorldKey(testDivision, c.Name)
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
		w.Spawn = simulation.Spawn{RegionID: region, X: 900, Y: 20, Z: 900}
		w.MoveSegment = nil
	})
}

/*
================
weatherDeliveries

The 0x3BDE frames among a tick's output, by recipient character id.
================
*/
func weatherDeliveries(t *testing.T, out []simulation.DivisionFrames) map[int64][]byte {
	t.Helper()
	got := map[int64][]byte{}
	for _, batch := range out {
		for _, frame := range batch.Frames {
			if frame.Opcode != weatherOpcode {
				continue
			}
			if batch.OnlyCharacterID == 0 || batch.DivisionID != testDivision {
				t.Fatalf("weather not private: %+v", batch)
			}
			if _, twice := got[batch.OnlyCharacterID]; twice {
				t.Fatalf("character %d got two weather frames in one tick", batch.OnlyCharacterID)
			}
			got[batch.OnlyCharacterID] = frame.Payload
		}
	}
	return got
}

/*
================
TestSceneStartsWithItsRegionWeather

The game-ready answer carries the live region's weather; the entry's own
region bind is no crossing, so the tick sends nothing more.
================
*/
func TestSceneStartsWithItsRegionWeather(t *testing.T) {
	rt, characters := newWeatherTestRuntime(t, true, [2]uint16{weatherJangan, weatherSnowy})
	for i, want := range [][]byte{{1, 180}, {1, 100}} {
		frames := rt.SceneWeatherFrames(testDivision, characters[i])
		if len(frames) != 1 || frames[0].Opcode != weatherOpcode || !bytes.Equal(frames[0].Payload, want) {
			t.Fatalf("character %d scene weather %+v, want % X", i, frames, want)
		}
	}
	if out := rt.advanceWeather(1); len(out) != 0 {
		t.Fatalf("an entry sent tick weather: %+v", out)
	}
	placeWeatherCharacter(rt, characters[0], 0)
	if frames := rt.SceneWeatherFrames(testDivision, characters[0]); len(frames) != 0 {
		t.Fatalf("a region without a climate sent %+v", frames)
	}
}

/*
================
TestCrossingSendsTheNewRegionWeather

The tick that binds a new region sends its weather to that player alone,
even into a region of the same climate; a region without a climate sends
the zero weather, which the client draws as clear, as natively.
================
*/
func TestCrossingSendsTheNewRegionWeather(t *testing.T) {
	rt, characters := newWeatherTestRuntime(t, true, [2]uint16{weatherJangan, weatherJangan})
	placeWeatherCharacter(rt, characters[0], weatherSnowy)
	got := weatherDeliveries(t, rt.TickHook()(1000))
	if len(got) != 1 || !bytes.Equal(got[characters[0].ID], []byte{1, 100}) {
		t.Fatalf("crossing into 2004 delivered %v, want only % X to %d", got, []byte{1, 100}, characters[0].ID)
	}
	placeWeatherCharacter(rt, characters[1], weatherJanganE)
	got = weatherDeliveries(t, rt.TickHook()(2000))
	if len(got) != 1 || !bytes.Equal(got[characters[1].ID], []byte{1, 180}) {
		t.Fatalf("crossing within 1001 delivered %v, want only % X to %d", got, []byte{1, 180}, characters[1].ID)
	}
	if got = weatherDeliveries(t, rt.TickHook()(3000)); len(got) != 0 {
		t.Fatalf("a quiet tick delivered %v", got)
	}
	placeWeatherCharacter(rt, characters[0], 0)
	got = weatherDeliveries(t, rt.TickHook()(4000))
	if len(got) != 1 || !bytes.Equal(got[characters[0].ID], []byte{0, 0}) {
		t.Fatalf("crossing into a climate-less region delivered %v, want only % X to %d", got, []byte{0, 0}, characters[0].ID)
	}
}

/*
================
TestClimateChangeReachesOnlyItsRegions

Climate 2004 rolls first (every draw zero: snow 3); its fade's first step
goes to the player in its region, not to the one in Jangan.
================
*/
func TestClimateChangeReachesOnlyItsRegions(t *testing.T) {
	rt, characters := newWeatherTestRuntime(t, true, [2]uint16{weatherJangan, weatherSnowy})
	rt.advanceResidentRegions(1)
	got := weatherDeliveries(t, rt.advanceWeather(snowyFirstRoll))
	// Clear 100 toward snow 3 steps by (100+3)/10 = 10.
	if len(got) != 1 || !bytes.Equal(got[characters[1].ID], []byte{1, 90}) {
		t.Fatalf("2004's change delivered %v, want only 01 5A to %d", got, characters[1].ID)
	}
	if got = weatherDeliveries(t, rt.advanceWeather(snowyFirstRoll+1)); len(got) != 0 {
		t.Fatalf("the fade stepped inside three seconds: %v", got)
	}
}

/*
================
TestWeatherOffSendsNothing
================
*/
func TestWeatherOffSendsNothing(t *testing.T) {
	rt, characters := newWeatherTestRuntime(t, false, [2]uint16{weatherJangan, weatherSnowy})
	if frames := rt.SceneWeatherFrames(testDivision, characters[0]); len(frames) != 0 {
		t.Fatalf("scene weather while off: %+v", frames)
	}
	placeWeatherCharacter(rt, characters[0], weatherSnowy)
	for _, now := range []int64{1000, snowyFirstRoll, snowyFirstRoll + 3000} {
		if got := weatherDeliveries(t, rt.TickHook()(now)); len(got) != 0 {
			t.Fatalf("weather while off at %d: %v", now, got)
		}
	}
}

/*
================
TestQuietWeatherTickAllocatesNothing
================
*/
func TestQuietWeatherTickAllocatesNothing(t *testing.T) {
	rt, _ := newWeatherTestRuntime(t, true, [2]uint16{weatherJangan, weatherSnowy})
	rt.advanceWeather(1)
	if allocs := testing.AllocsPerRun(100, func() { rt.advanceWeather(2) }); allocs != 0 {
		t.Fatalf("a quiet weather tick allocated %.1f times", allocs)
	}
}
