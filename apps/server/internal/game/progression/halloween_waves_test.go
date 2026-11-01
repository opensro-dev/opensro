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
		end := moment.Wave.At.Add(HalloweenWaveLifetime)
		for _, at := range []time.Time{moment.Wave.At, end} {
			if hour := at.Hour(); hour >= 3 && hour < 6 {
				t.Fatalf("wave at %v runs into the reset or the backup", moment.Wave.At)
			}
		}
		if moment.Wave.Low == "" || moment.Wave.High == "" || moment.Wave.Low == moment.Wave.High {
			t.Fatalf("wave lacks its two fields: %+v", moment.Wave)
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
TestWavePairAndRotatingHourMove
================
*/
func TestWavePairAndRotatingHourMove(t *testing.T) {
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
		if waves[i].Low == waves[i-1].Low {
			t.Fatalf("the same pair twice running: %+v then %+v", waves[i-1], waves[i])
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
