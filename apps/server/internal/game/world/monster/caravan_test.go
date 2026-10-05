/*
===========================================================================

caravan_test.go - caravan bandit tactics and the native bandit tables

===========================================================================
*/

package monster

import (
	"encoding/json"
	"testing"

	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestCaravanTacticsRejectUnboundNumbers

Eight rows alone do not close the native IDs: 2005 must not substitute for
one of 2001..2004 or 2011..2014, even with otherwise valid controls.
================
*/
func TestCaravanTacticsRejectUnboundNumbers(t *testing.T) {
	var document struct {
		Source  string
		Tactics map[string]TacticsControls
	}
	if err := json.Unmarshal(caravanTacticsJSON, &document); err != nil {
		t.Fatal(err)
	}
	row := document.Tactics["2004"]
	delete(document.Tactics, "2004")
	row.ID = 2005
	document.Tactics["2005"] = row
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("unbound tactics number replaced a required native row")
		}
	}()
	loadCaravanTactics(data)
}

/*
================
TestCaravanTacticsProjectTheBackupRows

2001..2004 (flags 542) and 2011..2014 (flags 666); the target policy is
the row's ChangeTarget byte.
================
*/
func TestCaravanTacticsProjectTheBackupRows(t *testing.T) {
	for id, want := range map[uint32]struct {
		flags  uint32
		policy uint8
	}{2001: {542, 0}, 2003: {542, 1}, 2004: {542, 2}, 2011: {666, 0}, 2014: {666, 2}} {
		tactics, ok := CaravanTactics(id)
		if !ok || tactics.NativeFlags != want.flags || tactics.TargetPolicy != want.policy || !tactics.HasControls ||
			tactics.SightRange != 250 || tactics.Controls.TraceData != 500 || tactics.Controls.DiversionBasisData[5] != 30 {
			t.Fatalf("tactics %d: %+v", id, tactics)
		}
	}
	if _, ok := CaravanTactics(2005); ok {
		t.Fatal("unknown caravan tactics resolved")
	}
}

/*
================
bandit
================
*/
func bandit(id uint32, codename string, typeID4, level uint8) MonsterRef {
	return MonsterRef{RefObjID: id, Codename: codename, TidWord: 0x00c6, TypeID4: typeID4, Level: level, MaxHP: 100}
}

/*
================
TestBanditTablesFileByPrefixAndWalkDownLevels
================
*/
func TestBanditTablesFileByPrefixAndWalkDownLevels(t *testing.T) {
	tables := NewBanditTables([]MonsterRef{
		bandit(3, "MOB_THIEF_NPC_0003", 2, 10),
		bandit(1, "MOB_THIEF_NPC_0001", 2, 10),
		bandit(2, "MOB_EU_THIEF_NPC_0001", 2, 10),
		bandit(4, "MOB_HUNTER_NPC_0001", 3, 5),
		bandit(5, "MOB_THIEF_NPC_0005", 2, 1),
		bandit(6, "MOB_CH_TIGER", 0, 10),
	})
	draw := func(value uint32) func() uint32 { return func() uint32 { return value } }
	// Zone 0 level 10 holds 1 and 3 in reference order; draw 1 picks 3.
	if ref, ok := tables.Pick(true, 0, 10, draw(1)); !ok || ref.RefObjID != 3 {
		t.Fatalf("east thief pick %+v/%v", ref, ok)
	}
	if ref, ok := tables.Pick(true, 1, 10, draw(0)); !ok || ref.RefObjID != 2 {
		t.Fatalf("west thief pick %+v/%v", ref, ok)
	}
	// Level 30 has none; the walk descends to level 10.
	if ref, ok := tables.Pick(true, 0, 30, draw(0)); !ok || ref.RefObjID != 1 {
		t.Fatalf("descending pick %+v/%v", ref, ok)
	}
	// The walk stops before level 1, as the native loop does.
	if _, ok := tables.Pick(true, 0, 5, draw(0)); ok {
		t.Fatal("walk reached level 1")
	}
	if ref, ok := tables.Pick(true, 0, 1, draw(0)); !ok || ref.RefObjID != 5 {
		t.Fatalf("level 1 pick %+v/%v", ref, ok)
	}
	if ref, ok := tables.Pick(false, 0, 5, draw(0)); !ok || ref.RefObjID != 4 {
		t.Fatalf("hunter pick %+v/%v", ref, ok)
	}
	if _, ok := tables.Pick(true, 2, 10, draw(0)); ok {
		t.Fatal("zone 2 holds bandits")
	}
}

/*
================
TestShippedJobMonstersFillEveryZoneAndLevel

The v1.150 catalog files 1,400 thieves and 1,400 hunters per continent
across levels 1..140.
================
*/
func TestShippedJobMonstersFillEveryZoneAndLevel(t *testing.T) {
	licensed.RequireGameData(t)
	refs := LoadMonsterRefs(gamedatatest.TextdataDir(t))
	list := make([]MonsterRef, 0, len(refs))
	for _, ref := range refs {
		list = append(list, ref)
	}
	tables := NewBanditTables(list)
	for zone := uint8(0); zone < 2; zone++ {
		for _, thieves := range []bool{true, false} {
			for _, level := range []int{1, 70, 140} {
				ref, ok := tables.Pick(thieves, zone, level, func() uint32 { return 0 })
				if !ok || int(ref.Level) != level {
					t.Fatalf("zone %d thieves %v level %d: %+v/%v", zone, thieves, level, ref, ok)
				}
			}
		}
	}
}
