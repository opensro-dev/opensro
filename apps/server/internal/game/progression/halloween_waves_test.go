/*
===========================================================================

halloween_waves_test.go - the Gourd wave timetable

===========================================================================
*/
package progression

import (
	"testing"
	"time"
)

func waveTestEvent(t *testing.T, days int) (HalloweenEvent, time.Time) {
	t.Helper()
	start := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	t.Setenv(EnvHalloweenEvent, "on")
	t.Setenv(EnvHalloweenEventWindow,
		start.Format(time.RFC3339)+"/"+start.Add(time.Duration(days)*24*time.Hour).Format(time.RFC3339))
	return HalloweenEventFromEnv(), start
}

/*
================
TestWavesRunTwiceADayClearOfTheResetAndBackup
================
*/
func TestWavesRunTwiceADayClearOfTheResetAndBackup(t *testing.T) {
	event, start := waveTestEvent(t, 14)
	moments := event.WaveMomentsBetween(start.Add(-time.Hour), start.Add(15*24*time.Hour))
	waves := 0
	for _, moment := range moments {
		if moment.Warning {
			continue
		}
		waves++
		end := moment.Wave.At.Add(HalloweenWaveSpan())
		for _, at := range []time.Time{moment.Wave.At, end} {
			if hour := at.Hour(); hour >= 3 && hour < 6 {
				t.Fatalf("wave at %v runs into the reset or the backup", moment.Wave.At)
			}
		}
		seen := map[string]bool{}
		for _, field := range moment.Wave.Fields {
			if field.Codename == "" || field.Area == "" || seen[field.Codename] {
				t.Fatalf("wave lacks its three distinct named fields: %+v", moment.Wave)
			}
			seen[field.Codename] = true
		}
	}
	if waves != 14*halloweenWavesPerDay {
		t.Fatalf("waves = %d, want two a day", waves)
	}
	if len(moments) != 2*waves {
		t.Fatalf("moments = %d, want one warning per wave", len(moments))
	}
}

/*
================
TestWaveTrioAndRotatingHourMove
================
*/
func TestWaveTrioAndRotatingHourMove(t *testing.T) {
	event, start := waveTestEvent(t, 3)
	var waves []HalloweenWave
	for _, moment := range event.WaveMomentsBetween(start, start.Add(3*24*time.Hour)) {
		if !moment.Warning {
			waves = append(waves, moment.Wave)
		}
	}
	for i := 1; i < len(waves); i++ {
		if !waves[i].At.After(waves[i-1].At) {
			t.Fatalf("not oldest first: %v", waves)
		}
		if waves[i].Fields == waves[i-1].Fields {
			t.Fatalf("the same trio twice running: %+v then %+v", waves[i-1], waves[i])
		}
	}
	if waves[0].At.Hour() == waves[2].At.Hour() {
		t.Fatalf("the rotating wave did not move: %v", waves)
	}
}

/*
================
TestWaveMomentsFireOnce

Consecutive asks split the timeline, so one wave lands once.
================
*/
func TestWaveMomentsFireOnce(t *testing.T) {
	event, start := waveTestEvent(t, 2)
	count := 0
	for at := start; at.Before(start.Add(48 * time.Hour)); at = at.Add(time.Minute) {
		count += len(event.WaveMomentsBetween(at, at.Add(time.Minute)))
	}
	if want := len(event.WaveMomentsBetween(start, start.Add(48*time.Hour))); count != want || count == 0 {
		t.Fatalf("stepped %d, whole %d", count, want)
	}
}

/*
================
TestNoWaveOutsideTheEvent
================
*/
func TestNoWaveOutsideTheEvent(t *testing.T) {
	event, start := waveTestEvent(t, 1)
	if moments := event.WaveMomentsBetween(start.Add(24*time.Hour), start.Add(72*time.Hour)); len(moments) != 0 {
		t.Fatalf("moments after the window: %+v", moments)
	}
	t.Setenv(EnvHalloweenEvent, "off")
	if moments := HalloweenEventFromEnv().WaveMomentsBetween(start, start.Add(24*time.Hour)); len(moments) != 0 {
		t.Fatalf("moments with the event off: %+v", moments)
	}
	t.Setenv(EnvHalloweenEvent, "on")
	t.Setenv(EnvHalloweenEventWindow, "")
	if moments := HalloweenEventFromEnv().WaveMomentsBetween(start, start.Add(24*time.Hour)); len(moments) != 0 {
		t.Fatalf("moments without a window: %+v", moments)
	}
}

/*
================
TestShippedWaveHours

The shipped window's wave clock, read literally (#2590): over
2026-11-01/2026-11-15 the rotating wave falls at 07, 11, 15, 19
and 01 UTC in a five-day cycle, the peak lands at 22:00 every day,
each wave warns ten minutes ahead, the final day (11-14) still
lands both waves (warnings 18:50 and 21:50, waves 19:00 and
22:00), and nothing exists at or after the 11-15 close. The other
wave tests pin the shape against the constants themselves; this
one pins the constants to the shipped timetable, so a moved hour
fails here instead of on the live shard.
================
*/
func TestShippedWaveHours(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	t.Setenv(EnvHalloweenEvent, "on")
	t.Setenv(EnvHalloweenEventWindow, start.Format(time.RFC3339)+"/"+end.Format(time.RFC3339))
	event := HalloweenEventFromEnv()

	moments := event.WaveMomentsBetween(start.Add(-time.Hour), end.Add(48*time.Hour))
	var waves []HalloweenWave
	warned := map[time.Time]time.Time{}
	for _, moment := range moments {
		if moment.Warning {
			warned[moment.Wave.At] = moment.instant()
			continue
		}
		waves = append(waves, moment.Wave)
	}
	if len(waves) != 28 {
		t.Fatalf("waves = %d, want 14 days of two", len(waves))
	}
	rotating := [...]int{7, 11, 15, 19, 1}
	for i, wave := range waves {
		day := start.Add(time.Duration(i/2) * 24 * time.Hour)
		hour := rotating[(i/2)%len(rotating)]
		if i%2 == 1 {
			hour = 22
		}
		if want := day.Add(time.Duration(hour) * time.Hour); !wave.At.Equal(want) {
			t.Fatalf("wave %d at %v, want %v", i, wave.At, want)
		}
		if instant, ok := warned[wave.At]; !ok || !instant.Equal(wave.At.Add(-10*time.Minute)) {
			t.Fatalf("wave %v warned at %v, want ten minutes ahead", wave.At, instant)
		}
	}
	// The trio walks one step per wave across all three bands alike.
	trios := [...][halloweenBands]string{
		{"MOB_CH_WHITETIGER", "MOB_KK_YETI_CLON", "MOB_RM_WINGTRIBE"},
		{"MOB_EU_KYKLOPES", "MOB_TK_DEATHKARA_CLON", "MOB_RM_ANTELOPE"},
		{"MOB_AM_CRAB", "MOB_TK_DEVILEYE", "MOB_RM_ROCKY"},
	}
	for i := 0; i < 4; i++ {
		for band, codename := range trios[i%len(trios)] {
			if waves[i].Fields[band].Codename != codename {
				t.Fatalf("wave %d band %d = %s, want %s", i, band, waves[i].Fields[band].Codename, codename)
			}
		}
	}
	// The final day by the clock: 19:00 behind an 18:50 warning,
	// then the 22:00 peak behind 21:50.
	if last := waves[26]; last.At.Hour() != 19 || warned[last.At].Hour() != 18 || warned[last.At].Minute() != 50 {
		t.Fatalf("final rotating wave %+v warned %v", last, warned[last.At])
	}
	if peak := waves[27]; peak.At.Hour() != 22 || warned[peak.At].Hour() != 21 || warned[peak.At].Minute() != 50 {
		t.Fatalf("final peak wave %+v warned %v", peak, warned[peak.At])
	}
	if moments := event.WaveMomentsBetween(end, end.Add(72*time.Hour)); len(moments) != 0 {
		t.Fatalf("moments at or after the close: %+v", moments)
	}
}

/*
================
TestWaveAtTheWindowEdgeKeepsItsWarningHonest

A wave the window ends before never warns.
================
*/
func TestWaveAtTheWindowEdgeKeepsItsWarningHonest(t *testing.T) {
	_, start := waveTestEvent(t, 1)
	t.Setenv(EnvHalloweenEventWindow, start.Format(time.RFC3339)+"/"+start.Add(21*time.Hour+55*time.Minute).Format(time.RFC3339))
	event := HalloweenEventFromEnv()
	for _, moment := range event.WaveMomentsBetween(start, start.Add(24*time.Hour)) {
		if moment.Wave.At.Hour() == halloweenPeakHour {
			t.Fatalf("a wave past the window's end appeared: %+v", moment)
		}
	}
}
