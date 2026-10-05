/*
===========================================================================

skillpassive_damage_test.go - passive damage programs

Whole-program admission of passive damage and the authored two-hand power
ranks with their consumers.

===========================================================================
*/
package enterworld

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestPassiveDamageWholeProgramAdmission
================
*/
func TestPassiveDamageWholeProgramAdmission(t *testing.T) {
	base := func() []string {
		f := criticalFields("1936028790", "1160926017", "27", "0", "0")
		for len(f) < 118 { // a shipped row's full width
			f = append(f, "0")
		}
		f[68] = "4"
		return f
	}
	for _, tc := range []struct {
		name   string
		change func([]string) []string
		want   bool
	}{
		{"authored", func(f []string) []string { return f }, true},
		{"active", func(f []string) []string { f[8] = "1"; return f }, false},
		{"wrong kind", func(f []string) []string { f[68] = "0"; return f }, false},
		{"chain", func(f []string) []string { f[9] = "1"; return f }, false},
		{"unknown channel", func(f []string) []string { f[70] = "1160926018"; return f }, false},
		{"auxiliary value", func(f []string) []string { f[72] = "1"; return f }, false},
		{"negative", func(f []string) []string { f[71] = "-1"; return f }, false},
		{"overflow", func(f []string) []string { f[71] = "4294967296"; return f }, false},
		{"truncated", func(f []string) []string { return f[:72] }, false},
		// A reqi gate is part of a Praise passive; combat evaluates it (59F0E0).
		{"reqi gate", func(f []string) []string { copy(f[73:], []string{"1919250793", "6", "8"}); return f }, true},
		{"duplicate setv", func(f []string) []string { copy(f[73:], []string{"1936028790", "1160926017", "27", "0"}); return f }, true},
		{"unported tag", func(f []string) []string { copy(f[73:], []string{"1685418593", "1000"}); return f }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := encodedPassiveParameters(tc.change(base())); got.Pinned != tc.want {
				t.Fatalf("%+v", got)
			}
		})
	}
	if encodedAttackParameters(criticalFields("6386804", "1734702198", "1160926017", "0", "0", "0")).Has(ParameterTwoHandPower) {
		t.Fatal("attack arguments became getv")
	}
}

/*
================
TestAuthoredTwoHandPowerRanksAndConsumers
================
*/
func TestAuthoredTwoHandPowerRanksAndConsumers(t *testing.T) {
	licensed.RequireGameData(t)
	s := NewTextdataSkills(licensed.RetailTextdataDir(t))
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	count, consumers := 0, 0
	for _, r := range s.rows.values() {
		if r.PassiveParameters.Pinned && r.PassiveParameters.Mask.Has(ParameterTwoHandPower) {
			count++
			if r.Group != 433 || r.PassiveParameters.Values[ParameterTwoHandPower] != uint32(r.Masteries[0].Level) || r.Level < 1 || r.Level > 22 || r.DirectOffensePinned {
				t.Fatalf("unexpected admission %+v", r)
			}
		}
		if r.Attack.Parameters.Has(ParameterTwoHandPower) {
			consumers++
			if r.RequiredWeaponKinds[0] != 8 {
				t.Fatalf("unexpected consumer %s", r.Codename)
			}
		}
	}
	if count != 22 || consumers == 0 {
		t.Fatalf("ranks=%d consumers=%d", count, consumers)
	}
	base, ok := s.SkillByID(7128)
	if !ok || !base.Attack.Parameters.Has(ParameterTwoHandPower) || !base.DirectOffensePinned {
		t.Fatal("ordinary two-hand attack lost its channel")
	}
}

/*
================
passiveProgramFields

A full-width passive row (col 68 = 4, activity and chain 0) whose program
is tail, starting at the first encoded column.
================
*/
func passiveProgramFields(tail ...string) []string {
	f := criticalFields(tail...)
	for len(f) < 118 {
		f = append(f, "0")
	}
	f[68] = "4"
	return f
}

/*
================
TestSetvlessPassiveProgramsPin

A passive needs no setv to install: reat, real or br alone pins it, br's
lane mask is normalized like the timed buff's (587630), and a program that
holds nothing but its reqi gate still has nothing to install.
================
*/
func TestSetvlessPassiveProgramsPin(t *testing.T) {
	const (
		reat = "1919246708"
		real = "1919246700"
		reqi = "1919250793"
		br   = "25202"
	)
	for _, tc := range []struct {
		name string
		tail []string
		want bool
		br   SkillPassiveBlockRate
	}{
		{"reat only", []string{reat, "63", "40"}, true, SkillPassiveBlockRate{}},
		{"real only", []string{real, "25145280", "50", "3"}, true, SkillPassiveBlockRate{}},
		{"protection", []string{reat, "63", "40", real, "25145280", "50", "3", reqi, "6", "9"}, true, SkillPassiveBlockRate{}},
		{"br only", []string{br, "15", "2"}, true, SkillPassiveBlockRate{Mask: 15, Value: 2}},
		{"blockade", []string{br, "15", "9", reqi, "6", "7"}, true, SkillPassiveBlockRate{Mask: 15, Value: 9}},
		{"br physical lane", []string{br, "4", "5"}, true, SkillPassiveBlockRate{Mask: 7, Value: 5}},
		{"br zero mask", []string{br, "0", "5", reqi, "6", "7"}, false, SkillPassiveBlockRate{}},
		{"br at the ceiling", []string{br, "15", "100"}, true, SkillPassiveBlockRate{Mask: 15, Value: 100}},
		{"br above the ceiling", []string{br, "15", "101", reqi, "6", "7"}, false, SkillPassiveBlockRate{}},
		{"duplicate br", []string{br, "15", "2", br, "15", "3"}, false, SkillPassiveBlockRate{}},
		{"reqi only", []string{reqi, "6", "7"}, false, SkillPassiveBlockRate{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := encodedPassiveParameters(passiveProgramFields(tc.tail...))
			if got.Pinned != tc.want || got.Br != tc.br {
				t.Fatalf("%+v", got)
			}
		})
	}
	if SkillParameterCount > 64 {
		t.Fatalf("%d parameter slots overflow SkillParameterMask", SkillParameterCount)
	}
}

/*
================
TestAuthoredSetvlessPassiveRanks

The shipped ranks this compiler admits: Blockade (8 ranks, br 15 2..9 under
reqi 6 7), Protection (7 ranks, reat 63 40..100 and real 0x17FAFC0 50 3..9
under reqi 6 9) and the Chinese
sword passive (9 ranks, br 15 2..10 under reqi 4 1). The active timed rows
that author the same reat/real pair (Holy Word, Poison Circle, Vein Circle)
are not passives and stay unpinned.
================
*/
func TestAuthoredSetvlessPassiveRanks(t *testing.T) {
	s := NewTextdataSkills(gamedatatest.TextdataDir(t))
	rank := func(line string, i int) SkillRow {
		t.Helper()
		row, ok := s.SkillByCodename(fmt.Sprintf("%s_%02d", line, i))
		if !ok {
			t.Fatalf("missing %s_%02d", line, i)
		}
		return row
	}
	reqiPair := func(row SkillRow, kind, value uint32) bool {
		return row.Reqi.Present && row.Reqi.Count == 1 && row.Reqi.Pairs[0] == SkillReqiPair{Kind: kind, Value: value}
	}
	for i := 1; i <= 8; i++ {
		row := rank("SKILL_EU_WARRIOR_SHIELDP_BLOCK_A", i)
		p := row.PassiveParameters
		if !p.Pinned || p.Br != (SkillPassiveBlockRate{Mask: 15, Value: uint32(i + 1)}) || p.Mask != 0 || !reqiPair(row, 6, 7) {
			t.Errorf("Blockade %d: %+v reqi %+v", i, p, row.Reqi)
		}
	}
	for i := 1; i <= 7; i++ {
		row := rank("SKILL_EU_WARRIOR_DUALP_ABNORMAL_A", i)
		p := row.PassiveParameters
		if !p.Pinned || p.Reat != (SkillPassiveReat{Mask: 63, Value: uint32(30 + 10*i)}) ||
			p.Real != (SkillPassiveReal{Mask: 0x17FAFC0, Flat: 50, Grade: uint32(i + 2)}) || !reqiPair(row, 6, 9) {
			t.Errorf("Protection %d: %+v reqi %+v", i, p, row.Reqi)
		}
	}
	for i := 1; i <= 9; i++ {
		row := rank("SKILL_CH_SWORD_PASSIVE_A", i)
		if p := row.PassiveParameters; !p.Pinned || p.Br != (SkillPassiveBlockRate{Mask: 15, Value: uint32(i + 1)}) || !reqiPair(row, 4, 1) {
			t.Errorf("Chinese sword passive %d: %+v reqi %+v", i, p, row.Reqi)
		}
	}
	for _, line := range []struct {
		name  string
		ranks int
	}{{"SKILL_EU_CLERIC_SAINTA_ABNORMAL_A", 8}, {"SKILL_EU_ROG_POISONA_GUARD_A", 5}, {"SKILL_EU_ROG_POISONA_GUARD_B", 2}} {
		for i := 1; i <= line.ranks; i++ {
			if p := rank(line.name, i).PassiveParameters; p.Pinned {
				t.Errorf("active %s_%02d pinned as a passive: %+v", line.name, i, p)
			}
		}
	}
}
