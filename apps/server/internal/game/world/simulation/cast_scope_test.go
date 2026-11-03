package simulation

import (
	"opensro.online/server/internal/game/item/wire"
	"testing"
)

func TestCastControlUsesCurrentSourceScope(t *testing.T) {
	a := peerSession("a", "A", 1, "A")
	b := peerSession("b", "A", 2, "B")
	foreign := peerSession("c", "A", 3, "C")
	a.WorldInstance, b.WorldInstance, foreign.WorldInstance = 0x10001, 0x10001, 0x20001
	source := &fakeSource{sessions: []SessionSnapshot{a, b, foreign}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)
	ticker.Hooks = []NamedTickHook{{Hook: func(int64) []DivisionFrames {
		return []DivisionFrames{{DivisionID: "A", SourceGID: PlayerObjectID(1), Frames: []Frame{{Opcode: wire.OpSkillEffectControl}}}}
	}}}
	ticker.RunTick(100)
	if len(peerFramesTo(push, "a", wire.OpSkillEffectControl)) != 1 || len(peerFramesTo(push, "b", wire.OpSkillEffectControl)) != 1 || len(peerFramesTo(push, "c", wire.OpSkillEffectControl)) != 0 || len(push.toDivision) != 0 {
		t.Fatal("cast scope leaked or omitted source")
	}
	push.toSession = nil
	source.sessions[1].World.Spawn.X += 2000
	ticker.RunTick(200)
	if len(peerFramesTo(push, "b", wire.OpObjectDespawn)) != 1 || len(peerFramesTo(push, "b", wire.OpSkillEffectControl)) != 0 {
		t.Fatal("departed observer received cast control")
	}
	push.toSession = nil
	source.sessions[1].World.Spawn = b.World.Spawn
	ticker.RunTick(300)
	if len(peerFramesTo(push, "b", wire.OpSingleObjectSpawn)) != 1 || len(peerFramesTo(push, "b", wire.OpSkillEffectControl)) != 1 {
		t.Fatal("reentry did not restore current scope")
	}
}

/*
================
TestOwnPetCastControlReachesItsOwner

The peer lane never shows a pet to its own owner, but the owner sees the
pet and got its cast open: the pet's close must reach the owner too, or
the owner's client keeps every pet cast open for the whole summon.
================
*/
func TestOwnPetCastControlReachesItsOwner(t *testing.T) {
	owner := peerSession("owner", "A", 1, "Owner")
	near := peerSession("near", "A", 2, "Near")
	far := peerSession("far", "A", 3, "Far")
	far.World.Spawn.X += 2000
	const petGID = 100
	owner.COS = &PeerCOS{Row: wire.CosSpawnBand2{Band: 4, RefObjID: 9, Gid: petGID, Name: "Pet", OwnerName: "Owner", OwnerGid: PlayerObjectID(1), Run: 100},
		World: owner.World, Session: 1, Generation: 1}
	source := &fakeSource{sessions: []SessionSnapshot{owner, near, far}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)
	ticker.Hooks = []NamedTickHook{{Hook: func(int64) []DivisionFrames {
		return []DivisionFrames{{DivisionID: "A", SourceGID: petGID, Frames: []Frame{{Opcode: wire.OpSkillEffectControl}}}}
	}}}
	ticker.RunTick(100)
	ticker.RunTick(200)
	if len(peerFramesTo(push, "owner", wire.OpSkillEffectControl)) != 2 {
		t.Fatal("the owner missed its own pet's cast control")
	}
	if len(peerFramesTo(push, "near", wire.OpSkillEffectControl)) != 2 || len(peerFramesTo(push, "far", wire.OpSkillEffectControl)) != 0 {
		t.Fatal("pet cast control left its observers")
	}
}

/*
================
TestPetPublicEventsFollowThePetsScope

#804: a pet's public events reach the pet's viewers, the audience of its
cast closes, and skip its owner (who has them privately). A viewer who sees
the owner but not the pet gets neither the open nor the close.
================
*/
func TestPetPublicEventsFollowThePetsScope(t *testing.T) {
	owner := peerSession("owner", "A", 1, "Owner")
	byOwner := peerSession("by-owner", "A", 2, "ByOwner")
	byPet := peerSession("by-pet", "A", 3, "ByPet")
	const petGID = 100
	petWorld := owner.World
	petWorld.Spawn.X += 1500
	byPet.World.Spawn.X += 1500
	owner.COS = &PeerCOS{Row: wire.CosSpawnBand2{Band: 4, RefObjID: 9, Gid: petGID, Name: "Pet", OwnerName: "Owner", OwnerGid: PlayerObjectID(1), Run: 100},
		World: petWorld, Session: 1, Generation: 1}
	source := &fakeSource{sessions: []SessionSnapshot{owner, byOwner, byPet}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)
	ticker.Hooks = []NamedTickHook{{Hook: func(int64) []DivisionFrames {
		return []DivisionFrames{{DivisionID: "A", SourceGID: petGID, ExceptCharacterID: 1, Frames: []Frame{{Opcode: wire.OpSkillCastResult}}}}
	}}}
	ticker.RunTick(100)
	if len(peerFramesTo(push, "by-owner", wire.OpSingleObjectSpawn)) == 0 {
		t.Fatal("fixture: the owner's neighbour must see the owner")
	}
	if len(peerFramesTo(push, "by-pet", wire.OpSkillCastResult)) != 1 {
		t.Fatal("the pet's viewer missed its public event")
	}
	if len(peerFramesTo(push, "by-owner", wire.OpSkillCastResult)) != 0 || len(peerFramesTo(push, "owner", wire.OpSkillCastResult)) != 0 {
		t.Fatal("a pet's public event reached a session outside its scope, or its owner twice")
	}
}
