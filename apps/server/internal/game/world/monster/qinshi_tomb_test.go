/*
===========================================================================

qinshi_tomb_test.go - verification of Qin-Shi Emperor Tomb Floor 1-6 monster tables

Tests both SRO_QINSHI_TOMB off (native default) and on: 79 characterdata rows
(78 monster species passing monsterTidGate plus 1 COS mount row MOB_QT_TQ_THIEF_01_COS,
spanning 61 MOB_TQ_* in 14791..14851 and 18 MOB_QT_TQ_* in 22638..22655),
1,042 Floor 1-6 nests across regions 0x8002..0x8007, hive caps, tactics
controls, remapped DefaultSkillIDs, and uncontaminated retail unique stats.

===========================================================================
*/

package monster

import (
	"testing"

	"opensro.online/server/internal/testsupport/clientdata"
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
TestQinShiTombEmbeddedOverlay
================
*/
func TestQinShiTombEmbeddedOverlay(t *testing.T) {
	seed := TemplateFromParts(
		map[uint32]MonsterRef{1933: {RefObjID: 1933, Codename: "MOB_CH_MANGNYANG", Level: 1, MaxHP: 59}},
		nil,
	)
	withTomb, err := seed.WithQinShiTomb("")
	if err != nil {
		t.Fatalf("WithQinShiTomb failed: %v", err)
	}
	if got, want := len(withTomb.Refs)-len(seed.Refs), 78; got != want {
		t.Fatalf("added monster refs = %d, want %d", got, want)
	}
	if got, want := len(withTomb.Nests), 1042; got != want {
		t.Fatalf("added tomb nests = %d, want %d", got, want)
	}
	regionCounts := map[uint16]int{}
	for _, nest := range withTomb.Nests {
		if nest.RegionID < 0x8002 || nest.RegionID > 0x8007 {
			t.Fatalf("unexpected tomb nest region 0x%x for ref %d", nest.RegionID, nest.RefObjID)
		}
		if !nest.RetailEvidence || !nest.PolicyPinned || !nest.HasControls || len(nest.HiveKey) != 64 {
			t.Fatalf("tomb nest %+v missing evidence/controls/hive", nest)
		}
		if nest.InstanceLimit() <= 0 {
			t.Fatalf("tomb nest %+v has non-positive instance limit %d", nest, nest.InstanceLimit())
		}
		regionCounts[nest.RegionID]++
	}
	if len(regionCounts) != 6 {
		t.Fatalf("expected nests across all 6 tomb floors (0x8002..0x8007), got %v", regionCounts)
	}
	if regionCounts[0x8003] != 5 || regionCounts[0x8002] != 4 {
		t.Fatalf("expected 5 Floor 5 nests and 4 Floor 6 nests, got F5=%d F6=%d", regionCounts[0x8003], regionCounts[0x8002])
	}

	// Verify 120-column characterdata parsing: rewardActionPinned, ExpToGive, and remapped DefaultSkillIDs.
	for id, ref := range withTomb.Refs {
		if id == 1933 {
			continue
		}
		if !ref.RewardActionPinned {
			t.Fatalf("tomb ref %d (%s) RewardActionPinned=false (column shift)", id, ref.Codename)
		}
		if id < 20000 && ref.ExpToGive <= 0 {
			t.Fatalf("tomb ref %d (%s) ExpToGive=%d, want > 0", id, ref.Codename, ref.ExpToGive)
		}
		for _, skillID := range ref.DefaultSkillIDs {
			if skillID >= 12131 && skillID <= 12300 {
				t.Fatalf("tomb ref %d (%s) retains unremapped v1.188 skill ID %d", id, ref.Codename, skillID)
			}
		}
	}

	for _, code := range []string{
		"MOB_TQ_NORTHGUARDIAN",
		"MOB_TQ_SOUTHGUARDIAN",
		"MOB_TQ_WESTGUARDIAN",
		"MOB_TQ_EASTGUARDIAN",
		"MOB_TQ_BLACKSNAKE",
		"MOB_TQ_WHITESNAKE",
	} {
		if got := UniqueSummonPolicy(code); got != SummonByHealthBand {
			t.Fatalf("UniqueSummonPolicy(%q) = %v, want SummonByHealthBand", code, got)
		}
	}

	whitesnake, ok := withTomb.Refs[14839]
	if !ok || whitesnake.Codename != "MOB_TQ_WHITESNAKE" {
		t.Fatalf("ref 14839 = %+v, ok=%v; want MOB_TQ_WHITESNAKE", whitesnake, ok)
	}
	if whitesnake.Level != 100 || whitesnake.MaxHP != 250008449 || whitesnake.ExpToGive != 81063918 {
		t.Fatalf("MOB_TQ_WHITESNAKE stats = Lvl %d HP %d Exp %d, want Lvl 100 HP 250008449 Exp 81063918", whitesnake.Level, whitesnake.MaxHP, whitesnake.ExpToGive)
	}
}

/*
================
TestWithQinShiTombAppendsRefsAndFloor1To6Nests
================
*/
func TestWithQinShiTombAppendsRefsAndFloor1To6Nests(t *testing.T) {
	clientdata.RequireGameData(t)
	dir := clientdata.RetailTextdataDir(t)
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
	if got, want := len(withTomb.Nests)-len(base.Nests), 1042; got != want {
		t.Fatalf("added tomb nests = %d, want %d", got, want)
	}

	// Verify representative Floor 1-6 monsters and quest variants.
	first, ok := withTomb.Refs[14791]
	if !ok || first.Codename != "MOB_TQ_TOMBSOLDIER_CLON2" {
		t.Fatalf("ref 14791 = %+v, ok=%v; want MOB_TQ_TOMBSOLDIER_CLON2", first, ok)
	}
	questMob, ok := withTomb.Refs[22638]
	if !ok || questMob.Codename != "MOB_QT_TQ_THIEF_01" {
		t.Fatalf("ref 22638 = %+v, ok=%v; want MOB_QT_TQ_THIEF_01", questMob, ok)
	}
}
