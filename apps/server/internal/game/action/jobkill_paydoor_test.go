/*
===========================================================================

jobkill_paydoor_test.go - a suited job kill classifies and counts through
the pay door (BR-261015-0844-66A1)

The live shard (SRO_MOST_WANTED on) never logged 'job kill classified'
(#1859's diagnostic in payPlayerKillInDoor's DeathJob case). These tests
drive a suited hunter's kill of a suited thief through classifyPlayerKill
into payPlayerKillInDoor - the same classification every fatal player hit
uses - and pin the shipped itemdata rows the classification reads.

===========================================================================
*/
package action

import (
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestShippedJobSuitsReadAsTheirJob

The classification reads the worn suit's packed type word. The shipped
itemdata must carry the trader/thief/hunter suits as type 3/1/7 with the
job in the fourth type id, or DressedJob can never answer on live data.
================
*/
func TestShippedJobSuitsReadAsTheirJob(t *testing.T) {
	items := enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	for codename, want := range map[string]uint8{
		"ITEM_CH_M_TRADE_TRADER_04": domain.JobTrader,
		"ITEM_CH_M_TRADE_THIEF_04":  domain.JobThief,
		"ITEM_CH_M_TRADE_HUNTER_04": domain.JobHunter,
	} {
		ref, ok := items.ItemRefByCodename(codename)
		if !ok || ref == nil {
			t.Fatalf("shipped itemdata has no %s", codename)
		}
		if ref.TypeIDs != [4]int64{3, 1, 7, int64(want)} {
			t.Fatalf("%s type ids %v, want [3 1 7 %d]", codename, ref.TypeIDs, want)
		}
		if got := enterworld.JobSuitJob(ref.TypeFlags()); got != want {
			t.Fatalf("%s packed type word %#x reads as job %d, want %d", codename, ref.TypeFlags(), got, want)
		}
	}
	// Every shipped job-suit family (both races, every degree) must read
	// as a job; a family whose type word packs differently would leave
	// its wearers outside job mode on live data.
	suits := 0
	for _, row := range items.ItemCommandReferences() {
		if !strings.Contains(row.Codename, "_TRADE_") ||
			!(strings.Contains(row.Codename, "TRADER") || strings.Contains(row.Codename, "THIEF") || strings.Contains(row.Codename, "HUNTER")) {
			continue
		}
		suits++
		if enterworld.JobSuitJob(row.TypeFlags) == 0 {
			t.Fatalf("shipped suit %s packs type word %#x, which reads as no job", row.Codename, row.TypeFlags)
		}
	}
	if suits == 0 {
		t.Fatal("no shipped job suits enumerated")
	}
	t.Logf("%d shipped job suits all read as their job", suits)
}

/*
================
TestSuitedJobKillCountsThroughThePayDoor

A suited hunter kills a suited thief (strangers, both alive, open field):
classification must yield pk.DeathJob and, with the rule on, the pay door
must count the kill on the killer's streak. Controls: either side
unsuited classifies as a murder; opposing free-battle capes classify as
a team kill (the cape test runs first).
================
*/
func TestSuitedJobKillCountsThroughThePayDoor(t *testing.T) {
	rt, _, hunter, thief := newPvpPair(t)
	rt.MostWanted = true
	rt.deps.(*enterworld.Deps).Levels = enterworld.NewTextdataLevels(gamedatatest.TextdataDir(t))
	hunter.AccountID, thief.AccountID = "hunter-account", "thief-account"
	dressForJob(rt, hunter, 9301, "ITEM_CH_M_TRADE_HUNTER_04", 3)
	dressForJob(rt, thief, 9302, "ITEM_CH_M_TRADE_THIEF_04", 2)

	kill := rt.classifyPlayerKill(testDivision, hunter, thief)
	if kill.kind != pk.DeathJob {
		t.Fatalf("suited hunter killing a suited thief classified %v, want DeathJob", kill.kind)
	}
	if !kill.mostWantedStrangers {
		t.Fatal("strangers on separate accounts refused the count")
	}
	for i := int64(1); i <= 3; i++ {
		rt.payPlayerKillInDoor(testDivision, hunter, thief, kill, i)
		if hunter.MostWanted == nil || hunter.MostWanted.Streak != i {
			t.Fatalf("after %d counted kills the streak is %+v", i, hunter.MostWanted)
		}
	}
	// The anti-farm pair limit: a fourth kill of the same victim in the
	// hour still classifies as a job kill but does not count.
	if again := rt.classifyPlayerKill(testDivision, hunter, thief); again.kind != pk.DeathJob {
		t.Fatalf("the fourth kill classified %v, want DeathJob", again.kind)
	} else {
		rt.payPlayerKillInDoor(testDivision, hunter, thief, again, 4)
	}
	if hunter.MostWanted.Streak != 3 {
		t.Fatalf("the fourth kill counted: streak %d, want 3", hunter.MostWanted.Streak)
	}

	// Controls: the kind is the suits, nothing else.
	unsuitedVictim := *thief
	unsuitedVictim.MissionInventory = nil
	if kind := rt.classifyPlayerKill(testDivision, hunter, &unsuitedVictim).kind; kind != pk.DeathPlayer {
		t.Fatalf("unsuited victim classified %v, want DeathPlayer", kind)
	}
	unsuitedKiller := *hunter
	unsuitedKiller.MissionInventory = nil
	if kind := rt.classifyPlayerKill(testDivision, &unsuitedKiller, thief).kind; kind != pk.DeathPlayer {
		t.Fatalf("unsuited killer classified %v, want DeathPlayer", kind)
	}
	for i, actor := range []*enterworld.Character{hunter, thief} {
		cape := &enterworld.ItemRef{
			RefObjID: uint32(63100 + i), Codename: capeCodename(i),
			TypeIDs:      [4]int64{3, 1, 7, 5},
			NativeFields: enterworld.NewNativeFields(map[string]float64{freeBattleGroupField: float64(i + 1)}),
			Combat:       &enterworld.ItemCombatRef{},
		}
		rt.deps.ItemReferences().(staticItemSource)[cape.Codename] = cape
		actor.MissionInventory = []enterworld.InventoryRow{
			{Slot: int64(jobSuitSlot), RefObjID: cape.RefObjID, Codename: cape.Codename, TypeFlags: cape.TypeFlags(), StackCount: 1},
		}
	}
	if kind := rt.classifyPlayerKill(testDivision, hunter, thief).kind; kind != pk.DeathTeam {
		t.Fatalf("opposing capes classified %v, want DeathTeam", kind)
	}
}

/*
================
capeCodename
================
*/
func capeCodename(i int) string {
	return "TEST_AUDIT_CAPE_" + string(rune('A'+i))
}
