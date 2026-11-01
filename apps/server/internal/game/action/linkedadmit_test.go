/*
===========================================================================

linkedadmit_test.go - per-tick link and abnormal-source checks read facts, not copies

advanceLinkedEffectInDivision runs for every live link every tick and
monsterAbnormalContext.SourceDead for every credited periodic hit.
Both detached whole character records - inventory included - to weigh
a handful of fields: endpoint presence, deletion, GID and life for
the tether; life alone for the source (#1943). The verdicts stay;
the copies go.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
)

/*
================
linkedAdmitParty

A runtime holding a caster and a friend joined by one threat link,
both carrying a full inventory so one detached copy is expensive.
The link is returned unapplied; the caller mutates and applies it.
================
*/
func linkedAdmitParty(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character, *enterworld.Character, statuseffect.Link) {
	t.Helper()
	rt, clock, c, _ := newCombatTestRuntime(t, 10000)
	friend := *c
	friend.ID = 4
	friend.Name = "admit-source"
	friendHP := *friend.CurrentHP
	friend.CurrentHP = &friendHP
	rows := make([]enterworld.InventoryRow, 0, 48)
	for slot := 0; slot < 48; slot++ {
		rows = append(rows, enterworld.InventoryRow{Slot: int64(slot), RefObjID: 62, StackCount: 1000,
			MagicOptions: []uint64{uint64(slot), uint64(slot + 1)}})
	}
	c.MissionInventory = append(c.MissionInventory, rows...)
	friend.MissionInventory = append([]enterworld.InventoryRow(nil), c.MissionInventory...)
	rt.deps.(*enterworld.Deps).Characters = enterworld.StaticCharacterSource{testDivision: {c, &friend}}
	link := statuseffect.Link{DivisionID: testDivision, SourceName: friend.Name, TargetName: c.Name,
		SourceGID: enterworld.ObjectIDForCharacter(&friend), TargetGID: enterworld.ObjectIDForCharacter(c),
		SourceToken: 100, TargetToken: 101, SkillID: 7246, SkillGroup: 418, Group: 3, MaxOutgoing: 2,
		MaxDistance: 1500, ThreatPercent: 36, ExpiresAtMs: clock.NowMs() + 60000, ClientCancelable: true}
	return rt, clock, c, &friend, link
}

/*
================
TestLinkedEffectEndpointVerdictsUnchanged

Every endpoint fact that retired the tether through the copied
records still retires it through the door read, and a healthy pair
still survives.
================
*/
func TestLinkedEffectEndpointVerdictsUnchanged(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(c, friend *enterworld.Character, link *statuseffect.Link)
		survive bool
	}{
		{"both endpoints alive", func(c, friend *enterworld.Character, link *statuseffect.Link) {}, true},
		{"source dead", func(c, friend *enterworld.Character, link *statuseffect.Link) { *friend.CurrentHP = 0 }, false},
		{"target dead", func(c, friend *enterworld.Character, link *statuseffect.Link) { *c.CurrentHP = 0 }, false},
		{"source deletion pending", func(c, friend *enterworld.Character, link *statuseffect.Link) {
			friend.DeletePending = true
		}, false},
		{"target deletion pending", func(c, friend *enterworld.Character, link *statuseffect.Link) {
			c.DeletePending = true
		}, false},
		{"stale source gid", func(c, friend *enterworld.Character, link *statuseffect.Link) { link.SourceGID++ }, false},
		{"source missing", func(c, friend *enterworld.Character, link *statuseffect.Link) {
			link.SourceName = "ghost"
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt, clock, c, friend, link := linkedAdmitParty(t)
			tc.mutate(c, friend, &link)
			if code := rt.effects.ApplyLink(link); code != 0 {
				t.Fatal(code)
			}
			rt.advanceLinkedEffects(clock.NowMs())
			if tc.survive {
				if _, ok := rt.effects.ThreatLink(testDivision, c.Name, clock.NowMs()); !ok {
					t.Fatal("healthy link retired")
				}
				if n := len(rt.effects.DrainStopRequested()); n != 0 {
					t.Fatalf("healthy link requested %d stops", n)
				}
				return
			}
			if n := len(rt.effects.DrainStopRequested()); n != 2 {
				t.Fatalf("retirement requested %d stops, want both halves", n)
			}
			if _, ok := rt.effects.ThreatLink(testDivision, c.Name, clock.NowMs()); ok {
				t.Fatal("retired link remained active")
			}
		})
	}

	// A stale target GID cannot be applied (ApplyLink resolves the
	// target by GID and refuses), so drive the advance directly with
	// the mismatched record the registry would have carried.
	t.Run("stale target gid", func(t *testing.T) {
		rt, clock, c, _, link := linkedAdmitParty(t)
		if code := rt.effects.ApplyLink(link); code != 0 {
			t.Fatal(code)
		}
		link.TargetGID++
		rt.advanceLinkedEffectInDivision(link, clock.NowMs())
		if n := len(rt.effects.DrainStopRequested()); n != 2 {
			t.Fatalf("retirement requested %d stops, want both halves", n)
		}
		if _, ok := rt.effects.ThreatLink(testDivision, c.Name, clock.NowMs()); ok {
			t.Fatal("retired link remained active")
		}
	})
}

/*
================
TestLinkedAdvanceAllocatesLessThanOneEndpointCopy

The healthy advance and the dead-endpoint advance both allocate less
than detaching one endpoint used to.
================
*/
func TestLinkedAdvanceAllocatesLessThanOneEndpointCopy(t *testing.T) {
	rt, clock, c, friend, link := linkedAdmitParty(t)
	if code := rt.effects.ApplyLink(link); code != 0 {
		t.Fatal(code)
	}
	copyAllocs := testing.AllocsPerRun(20, func() { rt.characterSnapshot(testDivision, c) })
	healthyAllocs := testing.AllocsPerRun(20, func() { rt.advanceLinkedEffectInDivision(link, clock.NowMs()) })
	*friend.CurrentHP = 0
	deadAllocs := testing.AllocsPerRun(20, func() { rt.advanceLinkedEffectInDivision(link, clock.NowMs()) })
	t.Logf("allocs: one endpoint copy %.0f, healthy advance %.0f, dead-source advance %.0f",
		copyAllocs, healthyAllocs, deadAllocs)
	if healthyAllocs >= copyAllocs || deadAllocs >= copyAllocs {
		t.Fatalf("link advance still copies endpoints: healthy %.0f, dead %.0f allocs; one copy is %.0f",
			healthyAllocs, deadAllocs, copyAllocs)
	}
}

/*
================
TestMonsterAbnormalSourceDeadVerdictWithoutCopy

A living player source reads not-dead, a dead one reads dead, an
unknown name stays not-dead - and none of them detaches the record.
================
*/
func TestMonsterAbnormalSourceDeadVerdictWithoutCopy(t *testing.T) {
	rt, _, c, _, _ := linkedAdmitParty(t)
	ctx := monsterAbnormalContext{rt}
	gid := enterworld.ObjectIDForCharacter(c)
	if ctx.SourceDead(testDivision, gid, c.Name) {
		t.Fatal("living source read dead")
	}
	if ctx.SourceDead(testDivision, gid, "ghost") {
		t.Fatal("unknown source read dead")
	}
	copyAllocs := testing.AllocsPerRun(20, func() { rt.characterSnapshot(testDivision, c) })
	deadAllocs := testing.AllocsPerRun(20, func() { ctx.SourceDead(testDivision, gid, c.Name) })
	t.Logf("allocs: one source copy %.0f, SourceDead %.0f", copyAllocs, deadAllocs)
	if deadAllocs >= copyAllocs {
		t.Fatalf("SourceDead still copies the source: %.0f allocs; one copy is %.0f", deadAllocs, copyAllocs)
	}
	*c.CurrentHP = 0
	if !ctx.SourceDead(testDivision, gid, c.Name) {
		t.Fatal("dead source read living")
	}
}
