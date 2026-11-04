/*
===========================================================================

skillstone_test.go - the stone curse parameter block

===========================================================================
*/

package enterworld

import (
	"strconv"
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
TestStoneCurseParams

snts is projected as the stone curse (delay, maximum-HP percent,
period) with its lifetime from the row's dura block, and never as a
roll source: a curse-only skill still reports no roll presence.
================
*/
func TestStoneCurseParams(t *testing.T) {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	tail := []uint32{
		0x617474, 1, 100, 10, 20, 0, // att
		0x64757261, 20000, // dura
		0x73746e73, 5000, 12, 5000, // snts
		0x73736f75, // ssou
	}
	for i, v := range tail {
		fields[69+i] = strconv.FormatUint(uint64(v), 10)
	}
	params := encodedAbnormalParams(fields)
	want := abnormal.StoneCurse{Present: true, DelayMs: 5000, Percent: 12, PeriodMs: 5000, DurationMs: 20000}
	if params.Stone != want {
		t.Fatalf("stone %+v, want %+v", params.Stone, want)
	}
	if params.Present() {
		t.Fatal("the stone curse leaked into the roll presence")
	}
	if _, found := abnormal.SourceIndex(0x73746e73); found {
		t.Fatal("snts became a roll source")
	}
}

/*
================
TestWhiteSnakeCurseRowCarriesStone

The shipped curse row (MSKILL_TQ_WHITESNAKE_ATTACK03, id 10712) parses
with its stone curse: five seconds to the first pulse, 12% of maximum
HP per pulse every five seconds, twenty seconds of stone.
================
*/
func TestWhiteSnakeCurseRowCarriesStone(t *testing.T) {
	tier.Long(t, "the shipped White Snake curse row parses with its stone curse")
	clientdata.RequireGameData(t)
	dir := clientdata.RetailTextdataDir(t)
	t.Setenv(monster.EnvQinShiTomb, "on")
	row, ok := NewTextdataSkills(dir).SkillByID(10712)
	if !ok {
		t.Fatal("tomb skill 10712 missing")
	}
	want := abnormal.StoneCurse{Present: true, DelayMs: 5000, Percent: 12, PeriodMs: 5000, DurationMs: 20000}
	if row.Abnormal.Stone != want || row.EffectDurationMs != 20000 {
		t.Fatalf("curse row stone %+v effect duration %d", row.Abnormal.Stone, row.EffectDurationMs)
	}
}
