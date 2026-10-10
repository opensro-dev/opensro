/*
===========================================================================

fortress_dismantle_test.go - dismissing summoned objects and demolishing
structures (0x71E1 actions 0x16 and 0x17)

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/world/instance"
)

/*
================
dismantleFixture

Jangan held by guild 77, whose member c holds role; the fixture's stone
stands on zone 84 and its guard tower on zone 85.
================
*/
func dismantleFixture(t *testing.T, role uint8) (*Runtime, *enterworld.Character, *structureRowStore, uint32, *fakeClock) {
	t.Helper()
	rt, c, clock, _ := captureFixture(t)
	jangan := uint32(0)
	for _, record := range rt.Fortresses.Records(testDivision) {
		if record.CodeName == "FORTRESS_JANGAN" {
			jangan = record.ID
		}
	}
	rt.Fortresses.Capture(testDivision, jangan, 77, 0)
	rt.Guilds = fortressGuilds{guild: domain.GuildRecord{ID: 77}, members: []domain.GuildMemberRecord{{CharID: c.ID, FortressRole: role}}}
	store := &structureRowStore{rows: map[uint32]domain.FortressStructureRecord{}}
	rt.FortressStore = store
	return rt, c, store, jangan, clock
}

/*
================
dismantle

One 0x71E1 request through the dispatcher: target, action, fortress.
================
*/
func dismantle(rt *Runtime, c *enterworld.Character, action uint8, target, fortressID uint32) []byte {
	out := rt.HandleFortressInteraction(testDivision, c, wire.NewWriter(9).U32(target).U8(action).U32(fortressID).Payload())
	if len(out.Frames) == 0 {
		return nil
	}
	return out.Frames[len(out.Frames)-1].Payload
}

/*
================
TestFortressDismissRefusesWithoutASummonedObject

633EC0: no summoned siege object stands in the port yet (#483), so any
target, a structure included, answers 0x282A.
================
*/
func TestFortressDismissRefusesWithoutASummonedObject(t *testing.T) {
	rt, c, _, jangan, _ := dismantleFixture(t, fortressRoleCommander)
	tower := structureByRef(t, rt, 19536)
	if got := dismantle(rt, c, siege.ActionDismiss, tower.Gid, jangan); !bytes.Equal(got, []byte{siege.ActionDismiss, 2, fortressErrNotSiegeObject}) {
		t.Fatalf("dismissal answered % x", got)
	}
}

/*
================
TestFortressDemolitionVacatesTheZone

634020 in order: not a structure 0x282B, another guild 0x282C, a role
that is none of 1, 2, 4 0x282F, another fortress 3, a destroyed structure
3. A sub-commander's demolition empties zone 85, stores RefObjID 0 for it
and answers {0x17, 1, 85}; the reinstall after a war and a restart both
leave the zone empty.
================
*/
func TestFortressDemolitionVacatesTheZone(t *testing.T) {
	rt, c, store, jangan, clock := dismantleFixture(t, 8)
	tower, stone := structureByRef(t, rt, 19536), structureByRef(t, rt, 19553)
	refused := func(code uint8) []byte { return []byte{siege.ActionDemolish, 2, code} }
	if got := dismantle(rt, c, siege.ActionDemolish, 999999, jangan); !bytes.Equal(got, refused(fortressErrNotStructure)) {
		t.Fatalf("not a structure: % x", got)
	}
	other := int64(78)
	c.GuildID = &other
	if got := dismantle(rt, c, siege.ActionDemolish, tower.Gid, jangan); !bytes.Equal(got, refused(fortressErrNotOwnGuild)) {
		t.Fatalf("other guild: % x", got)
	}
	own := int64(77)
	c.GuildID = &own
	if got := dismantle(rt, c, siege.ActionDemolish, tower.Gid, jangan); !bytes.Equal(got, refused(fortressErrDemolishRole)) {
		t.Fatalf("production role: % x", got)
	}
	rt.Guilds = fortressGuilds{guild: domain.GuildRecord{ID: 77}, members: []domain.GuildMemberRecord{{CharID: c.ID, FortressRole: fortressRoleSubCommander}}}
	if got := dismantle(rt, c, siege.ActionDemolish, tower.Gid, jangan+1); !bytes.Equal(got, refused(fortressErrInvalid)) {
		t.Fatalf("other fortress: % x", got)
	}
	rt.Monsters.RestoreStructure(testDivision, stone.Gid, 0, structureDestroyedMask)
	if got := dismantle(rt, c, siege.ActionDemolish, stone.Gid, jangan); !bytes.Equal(got, refused(fortressErrInvalid)) {
		t.Fatalf("destroyed structure: % x", got)
	}

	want := wire.NewWriter(6).U8(siege.ActionDemolish).U8(1).U32(85).Payload()
	if got := dismantle(rt, c, siege.ActionDemolish, tower.Gid, jangan); !bytes.Equal(got, want) {
		t.Fatalf("demolition answered % x, want % x", got, want)
	}
	world := instance.Pack(2, 1)
	if _, stands := rt.structureOnZone(testDivision, world, 85); stands {
		t.Fatal("the demolished tower still stands")
	}
	if row, ok := store.rows[85]; !ok || row.RefObjID != 0 || row.FortressID != jangan {
		t.Fatalf("vacancy not stored: %+v", store.rows)
	}
	rt.Monsters.ReinstallStructures(testDivision, world, clock.NowMs())
	rt.Monsters.AdvancePopulation(clock.NowMs() + 10*60*1000)
	if _, stands := rt.structureOnZone(testDivision, world, 85); stands {
		t.Fatal("the reinstall or the hive rebuilt a demolished zone")
	}
	if _, stands := rt.structureOnZone(testDivision, world, 84); !stands {
		t.Fatal("the reinstall lost the standing stone")
	}

	// A restart: fresh populations, then the stored rows.
	restarted, _, _, _, restartClock := dismantleFixture(t, fortressRoleCommander)
	restarted.FortressStore = store
	restarted.advanceFortressStructures(restartClock.NowMs())
	if _, stands := restarted.structureOnZone(testDivision, world, 85); stands {
		t.Fatal("a restart rebuilt a demolished zone")
	}
}

/*
================
TestStoredOccupantReplacesTheNestDefault

A row whose RefObjID differs from the nest's authored structure stands
that structure on the zone (construction, upgrade) with the stored hit
points, rather than being skipped as a mismatch.
================
*/
func TestStoredOccupantReplacesTheNestDefault(t *testing.T) {
	rt, _, store, jangan, clock := dismantleFixture(t, fortressRoleCommander)
	store.rows[85] = domain.FortressStructureRecord{FortressID: jangan, EventStructID: 85, RefObjID: 19553, HP: 400}
	rt.advanceFortressStructures(clock.NowMs())
	row, stands := rt.structureOnZone(testDivision, instance.Pack(2, 1), 85)
	if !stands || row.Ref.RefObjID != 19553 || row.CurrentHP != 400 {
		t.Fatalf("zone 85 holds %+v", row)
	}
}
