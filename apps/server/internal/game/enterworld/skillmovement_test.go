/*
===========================================================================

skillmovement_test.go - movement skill admission

Native branch precedence, descriptor admission and the envelope boundaries
of the shipped direct movement skills.

===========================================================================
*/
package enterworld

import (
	"strconv"
	"testing"

	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestMovementNativeBranchPrecedence
================
*/
func TestMovementNativeBranchPrecedence(t *testing.T) {
	for _, tc := range []struct {
		tail      []uint32
		kind      statuseffect.MovementKind
		percent   uint32
		supported bool
	}{
		{[]uint32{0x68737432, 40}, statuseffect.MovementOverride, 40, true},
		{[]uint32{0x68737433, 60}, statuseffect.MovementIndependent, 60, true},
		{[]uint32{0x68737433, 60, 0x68737432, 40, 0x68737465, 20}, statuseffect.MovementHaste, 20, true},
		{[]uint32{0x68737465, 0, 0x68737433, 60, 0x68737432, 40}, statuseffect.MovementOverride, 40, true},
		{[]uint32{0x68737432, 0, 0x68737433, 60}, statuseffect.MovementIndependent, 60, true},
		{[]uint32{0x68737465, 0}, statuseffect.MovementHaste, 0, false},
		{[]uint32{0x68737432, 40, 0x68737432, 60}, statuseffect.MovementOverride, 40, false},
		{[]uint32{0x68737433, 60, 0x61626364}, statuseffect.MovementIndependent, 60, false},
	} {
		fields := make([]string, 69)
		for _, n := range append([]uint32{0x64757261, 1000}, tc.tail...) {
			fields = append(fields, strconv.FormatUint(uint64(n), 10))
		}
		got := encodedMovementModifier(fields)
		if !got.Present || got.Kind != tc.kind || got.Percent != tc.percent || got.Supported != tc.supported {
			t.Fatalf("%v: %+v", tc.tail, got)
		}
	}
}

/*
================
TestMovementDescriptorAdmission
================
*/
func TestMovementDescriptorAdmission(t *testing.T) {
	for _, tc := range []struct {
		tail                  []uint32
		supported, persistent bool
	}{
		{[]uint32{0x63627566, 0x64757261, 3600000, 0x68737465, 100, 0}, true, true},
		{[]uint32{0x65667461, 0x64757261, 100, 0x68737465, 50, 0}, true, false},
		{[]uint32{0x63627566, 0x64757261, 100, 0x68737465, 50, 0x61626364, 0}, false, true},
		{[]uint32{0x64757261, 100, 0x68737465}, false, false},
		{[]uint32{0x64757261, 0, 0x68737465, 100, 0}, false, false},
		{[]uint32{0x64757261, 100, 0x68737465, 100, 0, 0x61626364}, false, false},
	} {
		fields := make([]string, 69)
		for _, n := range tc.tail {
			fields = append(fields, strconv.FormatUint(uint64(n), 10))
		}
		got := encodedMovementModifier(fields)
		if got.Supported != tc.supported || got.Persistent != tc.persistent {
			t.Fatalf("%v: %+v", tc.tail, got)
		}
	}
}

/*
================
TestShippedDirectMovementAdmissionAndEnvelopeBoundaries
================
*/
func TestShippedDirectMovementAdmissionAndEnvelopeBoundaries(t *testing.T) {
	licensed.RequireGameData(t)
	source := NewTextdataSkills(licensed.RetailTextdataDir(t))
	if err := source.Load(); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, r := range source.rows.values() {
		if r.InstantSelfEffectPinned && !r.Imbue.Pinned {
			count++
			if !r.MovementModifier.Supported || r.MovementModifier.Persistent || r.ChainSub || r.EffectDurationMs == 0 {
				t.Fatal("partial effect admitted", r.ID)
			}
		}
	}
	// 24 Chinese GYEONGGONG ranks and the 9 Scud ranks.
	if count != 33 {
		t.Fatalf("direct movement ranks %d", count)
	}
	f := make([]string, 118)
	for i := range f {
		f[i] = "0"
	}
	f[0] = "1"
	f[8] = "1"
	f[18] = "6"
	f[68] = "3"
	f[50] = "255"
	f[51] = "255"
	row := SkillRow{TimingPinned: true, Consumption: SkillConsumption{Pinned: true}, MovementModifier: SkillMovementModifier{Present: true, Supported: true, Percent: 20}}
	if !instantMovementSkill(f, row) {
		t.Fatal("valid envelope refused")
	}
	for _, i := range []int{9, 12, 13, 15, 16, 17, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 56} {
		copyFields := append([]string(nil), f...)
		copyFields[i] = "1"
		if instantMovementSkill(copyFields, row) {
			t.Fatalf("unimplemented envelope %d admitted", i)
		}
	}
	row.MovementModifier.Persistent = true
	if instantMovementSkill(f, row) {
		t.Fatal("item job admitted as ordinary cast")
	}
}

/*
================
TestInstantMovementEnvelopeLeavesPackedStatesAndWeaponsToTheirOwners

Column 18 is the replacement descriptor's PackedStates word and columns
50/51 are the weapon kinds 58D480 checks at execution; neither selects the
activation shape. The weapon columns must still be bytes, or the row would
keep the compiler's 0xFF/0xFF default and admit any weapon.
================
*/
func TestInstantMovementEnvelopeLeavesPackedStatesAndWeaponsToTheirOwners(t *testing.T) {
	f := make([]string, 118)
	for i := range f {
		f[i] = "0"
	}
	f[0] = "1"
	f[8] = "1"
	f[18] = "78"
	f[68] = "3"
	f[50] = "13"
	f[51] = "255"
	row := SkillRow{TimingPinned: true, Consumption: SkillConsumption{Pinned: true}, MovementModifier: SkillMovementModifier{Present: true, Supported: true, Kind: statuseffect.MovementOverride, Percent: 48}}
	if !instantMovementSkill(f, row) {
		t.Fatal("dagger-only envelope with packed states 78 refused")
	}
	for _, bad := range []struct {
		column int
		value  string
	}{{50, "256"}, {51, "-1"}, {50, "x"}, {51, ""}} {
		copyFields := append([]string(nil), f...)
		copyFields[bad.column] = bad.value
		if instantMovementSkill(copyFields, row) {
			t.Fatalf("weapon column %d=%q admitted", bad.column, bad.value)
		}
	}
}

/*
================
TestShippedScudIsAnInstantMovementOverride

SKILL_EU_ROG_DAGGERA_SPEED_A (Scud): dura(15000) hst2(N), dagger only
(13/255), packed states 78. hst2 is the override branch (5964B3/59652F).
================
*/
func TestShippedScudIsAnInstantMovementOverride(t *testing.T) {
	source := NewTextdataSkills(gamedatatest.TextdataDir(t))
	if err := source.Load(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id      uint32
		code    string
		percent uint32
	}{
		{20060, "SKILL_EU_ROG_DAGGERA_SPEED_A_01", 48},
		{20068, "SKILL_EU_ROG_DAGGERA_SPEED_A_09", 144},
	} {
		r, ok := source.SkillByID(tc.id)
		if !ok || r.Codename != tc.code {
			t.Fatalf("missing %d %s", tc.id, tc.code)
		}
		m := r.MovementModifier
		if !r.InstantSelfEffectPinned || r.Imbue.Pinned || r.ChainSub || !m.Supported || m.Persistent ||
			m.Kind != statuseffect.MovementOverride || m.Percent != tc.percent {
			t.Fatalf("%s not an instant override: instant %v imbue %v movement %+v", tc.code, r.InstantSelfEffectPinned, r.Imbue.Pinned, m)
		}
		if r.EffectDurationMs != 15000 || r.RequiredWeaponKinds != [2]uint8{13, 255} || r.Reqi.Present ||
			!r.ReplacementPinned || r.Replacement.PackedStates != 78 {
			t.Fatalf("%s envelope: dura %d weapons %v reqi %+v packed %d", tc.code, r.EffectDurationMs,
				r.RequiredWeaponKinds, r.Reqi, r.Replacement.PackedStates)
		}
	}
}
