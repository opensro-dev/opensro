/*
===========================================================================

sundrops_test.go - the Sun drop counting hook's tests (#2538)

Port-only, not native, and count-only: with no sink installed nothing
counts, and the count never changes a drop.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/item/grounditem"
)

/*
================
sunNote
================
*/
type sunNote struct {
	characterID int64
	codename    string
	at          time.Time
}

/*
================
recordingSunSink
================
*/
type recordingSunSink struct {
	notes []sunNote
}

/*
================
NoteSunDrop
================
*/
func (s *recordingSunSink) NoteSunDrop(characterID int64, codename string, at time.Time) {
	s.notes = append(s.notes, sunNote{characterID, codename, at})
}

/*
================
TestSunDropCountFromEnv
================
*/
func TestSunDropCountFromEnv(t *testing.T) {
	t.Setenv(EnvSunDropCount, "")
	if SunDropCountFromEnv() {
		t.Fatal("unset must be off (the native default)")
	}
	t.Setenv(EnvSunDropCount, "off")
	if SunDropCountFromEnv() {
		t.Fatal("off must be off")
	}
	t.Setenv(EnvSunDropCount, " On ")
	if !SunDropCountFromEnv() {
		t.Fatal("on must be on")
	}
}

/*
================
TestCountSunDropsCountsOnlyNamedSuns

Only Seal of Sun drops count, each named, for the settlement's winner:
Star, Moon, unsealed and unknown items are skipped, and a repeated Sun
counts once per drop.
================
*/
func TestCountSunDropsCountsOnlyNamedSuns(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), sealTestItems())
	sink := &recordingSunSink{}
	rt.SunDrops = sink
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	rt.countSunDrops(testCharacter(), []grounditem.Item{
		{Codename: "ITEM_CH_BLADE_05_C_RARE"}, // degree-5 Sun
		{Codename: "ITEM_CH_BLADE_05_C_RARE"},
		{Codename: "ITEM_CH_BLADE_06_A_RARE"}, // Star
		{Codename: "ITEM_CH_BLADE_08_B_RARE"}, // Moon
		{Codename: "ITEM_CH_BLADE_09_C"},      // unsealed
		{Codename: "ITEM_NOT_A_REAL_ITEM"},
	}, now.UnixMilli())
	if len(sink.notes) != 2 {
		t.Fatalf("counted %d Sun drops, want 2: %+v", len(sink.notes), sink.notes)
	}
	for _, note := range sink.notes {
		if note.characterID != 3 || note.codename != "ITEM_CH_BLADE_05_C_RARE" || !note.at.Equal(now) {
			t.Fatalf("Sun note = %+v, want character 3, the Sun blade, at %v", note, now)
		}
	}
}

/*
================
TestCountSunDropsWithoutSinkCountsNothing
================
*/
func TestCountSunDropsWithoutSinkCountsNothing(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), sealTestItems())
	// No sink: the native default. Must simply do nothing.
	rt.countSunDrops(testCharacter(), []grounditem.Item{{Codename: "ITEM_CH_BLADE_05_C_RARE"}}, 0)
	rt.countSunDrops(nil, []grounditem.Item{{Codename: "ITEM_CH_BLADE_05_C_RARE"}}, 0)
}
