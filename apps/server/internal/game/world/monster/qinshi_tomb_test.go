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
		// Chain-owned nests (Floor 5 #2372, Floor 6 #2373) keep the
		// native limit 0: the TombChain spawns them, the nest lifecycle
		// never does. The zeroing is region-scoped.
		chainOwned := qinshiTombChainOwned(nest.RegionID, withTomb.Refs[nest.RefObjID].Codename)
		if !chainOwned && nest.InstanceLimit() <= 0 {
			t.Fatalf("tomb nest %+v has non-positive instance limit %d", nest, nest.InstanceLimit())
		}
		if chainOwned && nest.InstanceLimit() != 0 {
			t.Fatalf("chain nest %+v has instance limit %d, want 0", nest, nest.InstanceLimit())
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
TestWithQinShiTombFloor5ChainNestsStayZeroed

Slice A of #2362 (#2372): on Floor 5 the four guardians and the Flame
Master are chain-owned. Their nest rows keep the native max count 0 so
the nest lifecycle never spawns them; the TombChain spawns them. Floor 6
rows joined the chain in slice B (#2373); see the Floor 6 test below.
================
*/
func TestWithQinShiTombFloor5ChainNestsStayZeroed(t *testing.T) {
	clientdata.RequireGameData(t)
	dir := clientdata.RetailTextdataDir(t)
	base := LoadTemplate(dir)
	withTomb, err := base.WithQinShiTomb(dir)
	if err != nil {
		t.Fatalf("WithQinShiTomb failed: %v", err)
	}
	chainOwned := map[string]bool{
		"MOB_TQ_EASTGUARDIAN":  true,
		"MOB_TQ_SOUTHGUARDIAN": true,
		"MOB_TQ_WESTGUARDIAN":  true,
		"MOB_TQ_NORTHGUARDIAN": true,
		"MOB_TQ_FLAMEMASTER":   true,
	}
	found := map[string]int{}
	for _, nest := range withTomb.Nests {
		ref, ok := withTomb.Refs[nest.RefObjID]
		if !ok || !chainOwned[ref.Codename] {
			continue
		}
		found[ref.Codename] = nest.MaxCount
	}
	if len(found) != len(chainOwned) {
		t.Fatalf("chain nests found = %v, want all %d Floor 5 chain codenames", found, len(chainOwned))
	}
	for codename, maxCount := range found {
		if maxCount != 0 {
			t.Fatalf("%s nest maxCount = %d, want 0 (chain-owned, #2372)", codename, maxCount)
		}
	}
}

/*
================
TestWithQinShiTombFloor6ChainNestsStayZeroed

Slice B of #2362 (#2373): on Floor 6 (region 0x8002) the four stage
anchor nests are chain-owned and keep max count 0, like Floor 5. The
zeroing is region-scoped: SNAKEGENERAL also nests on Floor 3 (region
0x8005), and that row keeps its restored count - the chain owns the
Floor 6 stage, not the species.
================
*/
func TestWithQinShiTombFloor6ChainNestsStayZeroed(t *testing.T) {
	clientdata.RequireGameData(t)
	dir := clientdata.RetailTextdataDir(t)
	base := LoadTemplate(dir)
	withTomb, err := base.WithQinShiTomb(dir)
	if err != nil {
		t.Fatalf("WithQinShiTomb failed: %v", err)
	}
	chainOwned := map[string]bool{
		"MOB_TQ_BLACKSNAKE":         true,
		"MOB_TQ_SNAKEGENERAL":       true,
		"MOB_TQ_TOMBBUGGHOST_CLON2": true,
		"MOB_TQ_WHITESNAKE":         true,
	}
	found := map[string]int{}
	floor3General := -1
	for _, nest := range withTomb.Nests {
		ref, ok := withTomb.Refs[nest.RefObjID]
		if !ok {
			continue
		}
		if nest.RegionID == 0x8002 && chainOwned[ref.Codename] {
			found[ref.Codename] = nest.MaxCount
		}
		if nest.RegionID == 0x8005 && ref.Codename == "MOB_TQ_SNAKEGENERAL" {
			floor3General = nest.MaxCount
		}
	}
	if len(found) != len(chainOwned) {
		t.Fatalf("Floor 6 chain nests found = %v, want all %d stage anchors", found, len(chainOwned))
	}
	for codename, maxCount := range found {
		if maxCount != 0 {
			t.Fatalf("Floor 6 %s nest maxCount = %d, want 0 (chain-owned, #2373)", codename, maxCount)
		}
	}
	if floor3General <= 0 {
		t.Fatalf("Floor 3 MOB_TQ_SNAKEGENERAL nest maxCount = %d, want its restored count (zeroing is region-scoped)", floor3General)
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
