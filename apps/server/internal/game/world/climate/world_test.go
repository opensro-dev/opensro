/*
===========================================================================

world_test.go - the generated climate table and the region lookup

===========================================================================
*/
package climate

import "testing"

// referenceRegionRows is the _RefRegion row count the importer read; every
// one names a climate.
const referenceRegionRows = 3194

/*
================
TestEveryClimateIsGenerated
================
*/
func TestEveryClimateIsGenerated(t *testing.T) {
	want := []int32{1001, 1005, 2001, 2002, 2004, 3001, 3002, 3003, 3004, 3005, 4001, 4002, 5001}
	if Count != len(want) {
		t.Fatalf("%d climates, want %d", Count, len(want))
	}
	for i, id := range want {
		if RowAt(i).ID != id {
			t.Fatalf("climate %d is %d, want %d", i, RowAt(i).ID, id)
		}
	}
	snowy := RowAt(4)
	if snowy.ProbSnow != 70 || snowy.Snowfall != 3 || snowy.SnowfallVariance != 7 || snowy.Division != 7 {
		t.Fatalf("climate 2004 row %+v", snowy)
	}
	china := RowAt(0)
	if china.InitialAmount != 180 || china.ProbRain != 50 || china.Rainfall != 100 || china.Duration != 240000 {
		t.Fatalf("climate 1001 row %+v", china)
	}
}

/*
================
TestRegionClimate
================
*/
func TestRegionClimate(t *testing.T) {
	for _, tc := range []struct {
		region  uint16
		climate int32
	}{
		{0x62a8, 1001}, // Jangan
		{0x4345, 1001},
		{0x52d9, 2004},
		{0x52e9, 2004},
		{0x5992, 2002},
		{0x4838, 4002},
	} {
		index, ok := RegionClimate(tc.region)
		if !ok || RowAt(index).ID != tc.climate {
			t.Fatalf("region %04x: climate %d (%v), want %d", tc.region, RowAt(index).ID, ok, tc.climate)
		}
	}
	for _, region := range []uint16{0, 0x52e1, 0xffff} {
		if _, ok := RegionClimate(region); ok {
			t.Fatalf("region %04x has a climate", region)
		}
	}
}

/*
================
TestRegionIndexMatchesRanges

The direct index answers as the ranges do for every region id, and covers
exactly the reference region rows.
================
*/
func TestRegionIndexMatchesRanges(t *testing.T) {
	known := 0
	for id := 0; id <= 0xffff; id++ {
		want := int32(0)
		for _, row := range regionClimateRanges {
			if uint16(id) >= row.first && uint16(id) <= row.last {
				want = row.climate
				break
			}
		}
		index, ok := RegionClimate(uint16(id))
		if ok != (want != 0) || ok && RowAt(index).ID != want {
			t.Fatalf("region %04x: index %d ok %v, want climate %d", id, index, ok, want)
		}
		if ok {
			known++
		}
	}
	if known != referenceRegionRows {
		t.Fatalf("%d regions have a climate, want %d", known, referenceRegionRows)
	}
}

/*
================
TestWorldUpdateReportsChangedClimates
================
*/
func TestWorldUpdateReportsChangedClimates(t *testing.T) {
	// Every draw 0: the first percent, so snow where a climate has a snow
	// chance and rain where it has a rain chance.
	w := NewWorld(0, func() int { return 0 })
	if changed := w.Update(1); changed != 0 {
		t.Fatalf("changes before any roll: %b", changed)
	}
	// Climate 2004 (index 4, Division 7) has the shortest period.
	roll := int64(cyclePeriodMs/7) + 1
	changed := w.Update(roll)
	if changed != 1<<4 {
		t.Fatalf("changed %b, want only climate 2004", changed)
	}
	if got, _ := w.RegionWeather(0x52d9); got != (Weather{Clear, 90}) {
		t.Fatalf("2004 region weather %+v, want clear 90 (fading from 100 toward snow 3)", got)
	}
	if got, _ := w.RegionWeather(0x62a8); got != (Weather{Clear, 180}) {
		t.Fatalf("1001 region weather %+v moved", got)
	}
	if _, ok := w.RegionWeather(0); ok {
		t.Fatalf("a region without a climate has weather")
	}
}

/*
================
TestWeatherIsOnUnlessSwitchedOff

The cycle runs by default, as natively; only an explicit off stops it.
================
*/
func TestWeatherIsOnUnlessSwitchedOff(t *testing.T) {
	for _, value := range []string{"", "on", "ON", "1", "true", " on "} {
		if !Enabled(value) {
			t.Fatalf("%q switched the weather off", value)
		}
	}
	for _, value := range []string{"off", "OFF", "0", "false", " off "} {
		if Enabled(value) {
			t.Fatalf("%q left the weather on", value)
		}
	}
}
