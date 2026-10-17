/*
===========================================================================

qinshi_tomb_test.go - verification of Qin-Shi Emperor Tomb Floor 1-4 monster tables

Tests both SRO_QINSHI_TOMB off (native default) and on: 79 monster species
(61 MOB_TQ_* in 14791..14851 and 18 MOB_QT_TQ_* in 22638..22655), 1,033 Floor
1-4 nests across regions 0x8004..0x8007, hive caps and tactics controls.

===========================================================================
*/

package monster

import (
	"testing"

	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestQinShiTombFromEnv
================
*/
func TestQinShiTombFromEnv(t *testing.T) {
	t.Setenv(EnvQinShiTomb, "")
	if QinShiTombFromEnv() {
		t.Fatal("unset SRO_QINSHI_TOMB must be false (native default)")
	}
	t.Setenv(EnvQinShiTomb, "off")
	if QinShiTombFromEnv() {
		t.Fatal("SRO_QINSHI_TOMB=off must be false")
	}
	t.Setenv(EnvQinShiTomb, "on")
	if !QinShiTombFromEnv() {
		t.Fatal("SRO_QINSHI_TOMB=on must be true")
	}
}

/*
================
TestWithQinShiTombAppendsRefsAndFloor1To4Nests
================
*/
func TestWithQinShiTombAppendsRefsAndFloor1To4Nests(t *testing.T) {
	dir := licensed.RetailTextdataDir(t)
	base := LoadTemplate(dir)
	if len(base.Refs) == 0 || len(base.Nests) == 0 {
		t.Fatal("expected non-empty base monster template")
	}
	if _, exists := base.Refs[14791]; exists {
		t.Fatal("base v1.150 template must not contain Tomb ID 14791 when gate is off")
	}

	withTomb, err := base.WithQinShiTomb(dir)
	if err != nil {
		t.Fatalf("WithQinShiTomb failed: %v", err)
	}
	if got, want := len(withTomb.Refs)-len(base.Refs), 78; got != want {
		t.Fatalf("added monster refs = %d, want %d", got, want)
	}
	if got, want := len(withTomb.Nests)-len(base.Nests), 1033; got != want {
		t.Fatalf("added tomb nests = %d, want %d", got, want)
	}

	// Verify representative Floor 1-4 monsters and quest variants.
	first, ok := withTomb.Refs[14791]
	if !ok || first.Codename != "MOB_TQ_TOMBSOLDIER_CLON2" {
		t.Fatalf("ref 14791 = %+v, ok=%v; want MOB_TQ_TOMBSOLDIER_CLON2", first, ok)
	}
	questMob, ok := withTomb.Refs[22638]
	if !ok || questMob.Codename != "MOB_QT_TQ_THIEF_01" {
		t.Fatalf("ref 22638 = %+v, ok=%v; want MOB_QT_TQ_THIEF_01", questMob, ok)
	}

	// Verify all 1,033 added nests sit in Tomb Floor 1-4 regions 0x8004..0x8007
	// and carry retail evidence, tactics controls, and hive keys.
	regionCounts := map[uint16]int{}
	for _, nest := range withTomb.Nests[len(base.Nests):] {
		if nest.RegionID < 0x8004 || nest.RegionID > 0x8007 {
			t.Fatalf("unexpected tomb nest region 0x%x for ref %d", nest.RegionID, nest.RefObjID)
		}
		if !nest.RetailEvidence || !nest.PolicyPinned || !nest.HasControls || len(nest.HiveKey) != 64 {
			t.Fatalf("tomb nest %+v missing evidence/controls/hive", nest)
		}
		regionCounts[nest.RegionID]++
	}
	if len(regionCounts) != 4 {
		t.Fatalf("expected nests across all 4 tomb floors (0x8004..0x8007), got %v", regionCounts)
	}
}
