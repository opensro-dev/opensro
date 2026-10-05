package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

const (
	// STORE_CH_FORT_GATE1 (teleport 40, field), STORE_JA_FORT_GATE1
	// (teleport 43, INS_FORT_JA) and STORE_JA_CHARGE_GATE (teleport 46, a
	// portal stone inside the fortress).
	testFieldFortGate  uint32 = 23285
	testInnerFortGate  uint32 = 22574
	testFortChargeGate uint32 = 22577
)

/*
================
fortressPortalFixture

A runtime with the shipped portal catalog, an admitted session and the
named gate standing at the player.
================
*/
func fortressPortalFixture(t *testing.T, gateRef uint32) (*Runtime, *enterworld.Character) {
	t.Helper()
	licensed.RequireGameData(t)
	rt, c, _, _ := returnFixture(t, 30000)
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(nil, nil))
	rt.NpcSpawn.Enabled, rt.NpcSpawn.AtPlayer = true, true
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 900000 + gateRef, RefObjID: gateRef, Codename: "GATE",
		Teleport: &simulation.TeleportGateBounds{Radius: 10, Height: 25}}}
	if err := rt.ConfigurePortals(gamedatatest.TextdataDir(t)); err != nil {
		t.Fatal(err)
	}
	if err := rt.AdmitCharacterSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	return rt, c
}

/*
================
usePortal
================
*/
func usePortal(rt *Runtime, c *enterworld.Character, gateRef, target uint32) OpResult {
	gid := 900000 + gateRef
	rt.NpcRoster[0].ObjectID, rt.NpcRoster[0].RefObjID = gid, gateRef
	rt.Selected.Set(testDivision, c.Name, gid)
	return rt.HandlePortal(testDivision, c, wire.NewWriter(9).U32(gid).U8(2).U32(target).Payload())
}

/*
================
TestFortressGateMovesThePlayerIntoTheFortressWorld

teleportdata's GenWorldID puts GATE_JA_FORT_GATE1 in INS_FORT_JA: the
character's world and its population membership both follow the gate,
and the way back returns both to the field.
================
*/
func TestFortressGateMovesThePlayerIntoTheFortressWorld(t *testing.T) {
	rt, c := fortressPortalFixture(t, testFieldFortGate)
	out := usePortal(rt, c, testFieldFortGate, 43)
	if len(out.Frames) == 0 || out.Frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("fortress gate did not enter: %+v", out)
	}
	fortressWorld := instance.Pack(2, 1)
	if instance.ID(domain.CharacterWorldInstance(c)) != fortressWorld {
		t.Fatalf("character world %08x", domain.CharacterWorldInstance(c))
	}
	if lease, ok := rt.EntryPopulationLease(testDivision, c.Name); !ok || lease.ID != fortressWorld {
		t.Fatalf("population membership did not follow: %+v %v", lease, ok)
	}
	out = usePortal(rt, c, testInnerFortGate, 40)
	if len(out.Frames) == 0 || out.Frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("return gate did not enter: %+v", out)
	}
	if c.World.PackedInstance != nil {
		t.Fatal("the field is stored as the absent default world")
	}
	if lease, ok := rt.EntryPopulationLease(testDivision, c.Name); !ok || uint32(lease.ID) != domain.DefaultWorldInstance {
		t.Fatalf("membership did not return to the field: %+v", lease)
	}
}

/*
================
TestFortressGateRefusesAJobSuit

4F2B50 -> 61D3D0: job state 4 (no job) is required at a fortress gate.
================
*/
func TestFortressGateRefusesAJobSuit(t *testing.T) {
	rt, c := fortressPortalFixture(t, testFieldFortGate)
	dressTrader(c)
	out := usePortal(rt, c, testFieldFortGate, 43)
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, []byte{2, portalFortressJobSuit}) {
		t.Fatalf("job suit entered the fortress: %+v", out)
	}
	if c.World.PackedInstance != nil {
		t.Fatal("refused gate changed the world")
	}
}

/*
================
TestFortressPortalStoneServesOnlyTheOwningGuild

The portal stones inside a fortress are not fortress gates (TypeID4 3): an
unoccupied fortress has no owner, so every player is refused 0x1C1D.
================
*/
func TestFortressPortalStoneServesOnlyTheOwningGuild(t *testing.T) {
	rt, c := fortressPortalFixture(t, testFieldFortGate)
	if out := usePortal(rt, c, testFieldFortGate, 43); len(out.Frames) == 0 || out.Frames[0].Opcode != enterworld.OpcodeResetClient {
		t.Fatalf("fortress gate did not enter: %+v", out)
	}
	guild := int64(7)
	c.GuildID = &guild
	out := usePortal(rt, c, testFortChargeGate, 47)
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, []byte{2, portalFortressOwnersOnly}) {
		t.Fatalf("portal stone served a non-owner: %+v", out)
	}
}
