/*
===========================================================================

seasonal_sky_test.go - the decoration's snowfalls and the town snow

Both skies are port-only (seasonal_sky.go); each test covers the flag
off (the native climate weather) beside on.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
)

// An even UTC hour inside the decoration window: a snowfall falls from
// 00:00 to 01:00, and 01:10 is between snowfalls.
var (
	skySnowfallStart = time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC)
	skyInSnowfall    = skySnowfallStart.Add(10 * time.Minute)
	skyBetween       = skySnowfallStart.Add(70 * time.Minute)
)

/*
================
setDecoration

Reads the Christmas switch in mode decoration (on) or with the switch
off, the way the composition root reads it.
================
*/
func setDecoration(t *testing.T, rt *Runtime, on bool) {
	t.Helper()
	t.Setenv(progression.EnvChristmasEvent, map[bool]string{true: "on", false: "off"}[on])
	t.Setenv(progression.EnvChristmasEventWindow, "2026-12-20T00:00:00Z/2027-01-03T00:00:00Z")
	t.Setenv(progression.EnvChristmasEventMode, string(progression.ChristmasModeDecoration))
	rt.ChristmasDecoration = progression.ChristmasDecorationFromEnv()
}

/*
================
sceneWeather
================
*/
func sceneWeather(t *testing.T, rt *Runtime, i int, characters [2]*enterworld.Character) []byte {
	t.Helper()
	frames := rt.SceneWeatherFrames(testDivision, characters[i])
	if len(frames) != 1 {
		t.Fatalf("character %d scene weather %+v", i, frames)
	}
	return frames[0].Payload
}

/*
================
TestCitySnowSnowsOnlyInTowns

On, the town region snows at the steady strength and the open field
keeps its climate; off, both keep the climate's weather.
================
*/
func TestCitySnowSnowsOnlyInTowns(t *testing.T) {
	if !townRegion(weatherJangan) || townRegion(weatherSnowy) {
		t.Fatalf("fixture: Jangan town %v, snowy field town %v", townRegion(weatherJangan), townRegion(weatherSnowy))
	}
	for _, on := range []bool{false, true} {
		rt, characters := newWeatherTestRuntime(t, true, [2]uint16{weatherJangan, weatherSnowy})
		rt.CitySnow = on
		town := []byte{1, 180}
		if on {
			town = []byte{3, citySnowAmount}
		}
		if got := sceneWeather(t, rt, 0, characters); !bytes.Equal(got, town) {
			t.Fatalf("city snow %v: town weather % X, want % X", on, got, town)
		}
		if got := sceneWeather(t, rt, 1, characters); !bytes.Equal(got, []byte{1, 100}) {
			t.Fatalf("city snow %v: field weather % X, want the climate's 01 64", on, got)
		}
	}
}

/*
================
TestDecorationSnowfallReachesEveryone

On, a snowfall's start sends every player its strength and its end sends
the climate back; off, the tick sends nothing at either time.
================
*/
func TestDecorationSnowfallReachesEveryone(t *testing.T) {
	for _, on := range []bool{false, true} {
		rt, characters := newWeatherTestRuntime(t, true, [2]uint16{weatherJangan, weatherSnowy})
		setDecoration(t, rt, on)
		now := skyInSnowfall
		rt.Now = func() time.Time { return now }
		got := weatherDeliveries(t, rt.advanceWeather(1))
		if !on {
			if len(got) != 0 {
				t.Fatalf("decoration off sent %v", got)
			}
			continue
		}
		snow := []byte{3, snowfallAmount(skySnowfallStart)}
		for _, c := range characters {
			if !bytes.Equal(got[c.ID], snow) {
				t.Fatalf("snowfall start sent %v, want % X to everyone", got, snow)
			}
		}
		if scene := sceneWeather(t, rt, 1, characters); !bytes.Equal(scene, snow) {
			t.Fatalf("a late entry saw % X, want % X", scene, snow)
		}
		if again := rt.advanceWeather(2); len(again) != 0 {
			t.Fatalf("a steady snowfall resent %+v", again)
		}
		now = skyBetween
		got = weatherDeliveries(t, rt.advanceWeather(3))
		if !bytes.Equal(got[characters[0].ID], []byte{1, 180}) || !bytes.Equal(got[characters[1].ID], []byte{1, 100}) {
			t.Fatalf("snowfall end sent %v, want each climate back", got)
		}
	}
}

/*
================
TestSnowfallStrengthsVary

Each snowfall draws its own strength inside the range, and the same
snowfall always draws the same one.
================
*/
func TestSnowfallStrengthsVary(t *testing.T) {
	seen := map[uint8]bool{}
	for i := 0; i < 24; i++ {
		start := skySnowfallStart.Add(time.Duration(2*i) * time.Hour)
		amount := snowfallAmount(start)
		if amount < snowfallMinAmount || amount > snowfallMaxAmount {
			t.Fatalf("snowfall %v strength %d outside [%d, %d]", start, amount, snowfallMinAmount, snowfallMaxAmount)
		}
		if amount != snowfallAmount(start) {
			t.Fatalf("snowfall %v drew two strengths", start)
		}
		seen[amount] = true
	}
	if len(seen) < 12 {
		t.Fatalf("24 snowfalls drew only %d strengths", len(seen))
	}
}

/*
================
TestGMSpellWinsOverTheSeasonalSky

A GM's /WEATHER rain in a snowing town shows the climate's own weather,
fading toward rain, while its spell runs.
================
*/
func TestGMSpellWinsOverTheSeasonalSky(t *testing.T) {
	rt, characters := newWeatherTestRuntime(t, true, [2]uint16{weatherJangan, weatherSnowy})
	rt.CitySnow, rt.GMWeather = true, true
	setDecoration(t, rt, true)
	rt.Now = func() time.Time { return skyInSnowfall }
	rt.advanceResidentRegions(1)
	rt.advanceWeather(1)
	if _, ok := rt.SetWeatherGM(testDivision, characters[0].Name, 2, 200, 0); !ok {
		t.Fatal("SetWeatherGM refused")
	}
	got := weatherDeliveries(t, rt.advanceWeather(3000))
	// The spell fades the clear sky out before its rain falls, as
	// TestGMWeatherFadesItsClimateIn shows: the first step is clear 142.
	if !bytes.Equal(got[characters[0].ID], []byte{1, 142}) {
		t.Fatalf("the GM's rain delivered %v, want the climate's fade 01 8E to %d, not the town snow", got, characters[0].ID)
	}
	if _, other := got[characters[1].ID]; other {
		t.Fatalf("the spell on Jangan's climate reached the field: %v", got)
	}
}
