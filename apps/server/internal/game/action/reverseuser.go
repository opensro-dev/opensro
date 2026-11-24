/*
===========================================================================

reverseuser.go - the reverse return scroll's two later destinations: move
to a party member, and the user-registered location

Later-native (v1.696 client, #3903). The v1.696 client (sro_client.exe,
SHA-256 8af7afc2a7c8c940133d0755b75a4b511cf95978536f97c57c2c348fdb95277a)
adds two rows to the reverse return scroll's choice box beside the last
recall and last death points (textuisystem ids 4200..4221): move to a
party member (CNIFReversePortalParty, interface 0x2ca, res_ui\ui_return.2dt)
and move to the user's set location (CNIFReversePortalUserSave, interface
0x2cb, res_ui\ui_return_message.2dt). The user window holds one registered
location: its enroll button records where the player stands, its move
button returns there, and the refusals are the native lines (party member
left, member in an unmovable location, no location set, location cannot
be set here). A stored location is per character (world.userReversePoint,
schema 52).

The later client's request bytes for these two modes were not isolated,
so the port's wire is port-shaped, the same class as choice 7's gating in
reversemap.go: the bag use 0x75BD carries choice 4 with a u32 member gid,
choice 5 (move to the saved location) or choice 6 (enroll here, which
spends nothing and starts no cast) as a one-byte tail, and the refusals
ride the 0xB5BD channel on port bytes just past the reverse family's
native 0xf5 (the port's client maps them to the native text rows). The
owner is told its saved location at world entry and after every enroll
on the port-only OpReverseUserPoint frame, so its window can say whether
a location exists (ids 4214/4218 read that way natively).

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// OpReverseUserPoint is the port-only S->C saved location (above):
	// u8 present, then u16 region id and f32 x/y/z when present.
	OpReverseUserPoint uint16 = 0x3FCE

	// The port-shaped 0x75BD choices of the two later destinations.
	reverseReturnPartyChoice  uint8 = 4
	reverseReturnUserChoice   uint8 = 5
	reverseReturnEnrollChoice uint8 = 6

	// Port refusal bytes on the item-use channel, beside the reverse
	// family's native codes (0x85, 0x86, 0xf5; 0xf7 is the Hwan scroll's).
	errCodeReversePartyLeft        uint8 = 0xf6
	errCodeReversePartyUnreachable uint8 = 0xf8
	errCodeReverseUserMoveFail     uint8 = 0xfa
	errCodeReverseUserSetBarred    uint8 = 0xfb
)

/*
================
ReverseUserPointFrame

The owner's saved location as one port-only frame (OpReverseUserPoint).
================
*/
func ReverseUserPointFrame(c *enterworld.Character) wire.Frame {
	writer := wire.NewWriter(15)
	var point *domain.WorldPoint
	if c != nil && c.World != nil {
		point = c.World.UserReversePoint
	}
	if point == nil || point.RegionID == nil || *point.RegionID == 0 {
		return wire.Frame{Opcode: OpReverseUserPoint, Payload: writer.U8(0).Payload()}
	}
	writer.U8(1).U16(uint16(*point.RegionID))
	for _, value := range []*float64{point.X, point.Y, point.Z} {
		coordinate := float64(0)
		if value != nil {
			coordinate = *value
		}
		writer.F32(float32(coordinate))
	}
	return wire.Frame{Opcode: OpReverseUserPoint, Payload: writer.Payload()}
}

/*
================
reverseUserPointEntryFrames

The saved-location frame a world entry owes its owner; none while no
location is enrolled, as a character who never saved one is told
nothing (the Hwan level's entry discipline). The enroll answer always
carries the frame, so the point frame's absent form never needs to
arrive at entry.
================
*/
func (rt *Runtime) reverseUserPointEntryFrames(c *enterworld.Character) []enterworld.Packet {
	if c == nil || c.World == nil || c.World.UserReversePoint == nil {
		return nil
	}
	return []enterworld.Packet{enterworld.NewPacket(OpReverseUserPoint, ReverseUserPointFrame(c).Payload)}
}

/*
================
recordedTravelPoint

A recorded point as a travel destination, or the refusal for a missing
one. The point keeps its GameWorldID; a recorded world is type 0,
entered on layer 1. Absent means the field.
================
*/
func recordedTravelPoint(point *domain.WorldPoint, refusal uint8) (travelPoint, uint8) {
	if point == nil || point.RegionID == nil || *point.RegionID == 0 {
		return travelPoint{}, refusal
	}
	world := instance.ID(domain.DefaultWorldInstance)
	if point.World != 0 {
		world = instance.Pack(instance.DefinitionID(point.World), portalWorldLayer)
	}
	return travelPoint{spawn: missionSpawnFromWorld(&point.WorldSpawn, simulation.Spawn{}), world: world}, 0
}

/*
================
reverseUserPoint

The saved location a user move (choice 5) names, or the refusal when
none was enrolled (id 4220's line, on its port byte).
================
*/
func reverseUserPoint(c *enterworld.Character) (travelPoint, uint8) {
	var point *domain.WorldPoint
	if c.World != nil {
		point = c.World.UserReversePoint
	}
	return recordedTravelPoint(point, errCodeReverseUserMoveFail)
}

/*
================
enrollReverseUserPoint

The user window's enroll button (choice 6): records where the player
stands as the saved location. It spends no scroll and starts no cast;
the answer is the item-use success with the stack untouched, then the
saved-location frame. A point records only in a type-0 world, as every
recorded point does (recordedPoint); anywhere else the native refusal
is "Cannot set location." (id 4221). The caller holds the character's
update; the door has already refused the dead.
================
*/
func (rt *Runtime) enrollReverseUserPoint(division string, c *enterworld.Character, request wire.ItemUseRequest, remaining uint16, now int64, result *OpResult) bool {
	at := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now)
	point, ok := recordedPoint(c, at)
	if !ok {
		*result = itemUseFailure(errCodeReverseUserSetBarred)
		return false
	}
	c.World.UserReversePoint = point
	*result = OpResult{Frames: []wire.Frame{
		{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)},
		ReverseUserPointFrame(c),
	}}
	return true
}

/*
================
reversePartyMove

A party move the item-use closure admitted: the member it names and the
use's clock. The member's standing point is judged after the door
closes (finishReversePartyMove), so nothing is spent here.
================
*/
type reversePartyMove struct {
	member uint32
	nowMs  int64
}

/*
================
reversePartyDestination

The standing point of the party member a party move (choice 4) names,
or the native refusal on its port byte: the member is not in the
player's party or not online (id 4204, "left the party"), or stands in
a world no scroll may enter - an instanced world, not the type-0 field
(id 4205, "a location you cannot move to").
================
*/
func (rt *Runtime) reversePartyDestination(division string, c *enterworld.Character, member uint32) (travelPoint, uint8) {
	self := enterworld.ObjectIDForCharacter(c)
	party := rt.rewardPartyOf(division, self)
	if party == nil || member == self {
		return travelPoint{}, errCodeReversePartyLeft
	}
	memberListed := false
	for _, gid := range party.Members {
		if gid == member {
			memberListed = true
		}
	}
	if !memberListed {
		return travelPoint{}, errCodeReversePartyLeft
	}
	other := rt.findCharacterByGid(division, member)
	if other == nil {
		return travelPoint{}, errCodeReversePartyLeft
	}
	if here, ok := instance.Lookup(instance.ID(domain.CharacterWorldInstance(other)).Definition()); !ok || here.NativeType != 0 {
		return travelPoint{}, errCodeReversePartyUnreachable
	}
	return rt.standingPoint(division, other), 0
}

/*
================
finishReversePartyMove

Judge the admitted party move against the member's live standing
point, then start the scroll's cast in a fresh character update that
re-finds the scroll row by slot and codename (the guild recall's
shape, guildrecall.go). A refusal spends nothing.
================
*/
func (rt *Runtime) finishReversePartyMove(division string, c *enterworld.Character, request wire.ItemUseRequest, ref *enterworld.ItemRef, use *reversePartyMove, result *OpResult) {
	if ref == nil {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		return
	}
	destination, refusal := rt.reversePartyDestination(division, c, use.member)
	if refusal != 0 {
		*result = itemUseFailure(refusal)
		return
	}
	var used *enterworld.ItemRef
	committed := rt.deps.Update(c, "item-use-reverse-party", func() bool {
		if c.DeletePending {
			return false
		}
		if !enterworld.CharacterAlive(c) {
			*result = itemUseFailure(wire.ErrCodeItemUseDead)
			return false
		}
		row := -1
		for index := range c.MissionInventory {
			if c.MissionInventory[index].Slot == int64(request.Slot) &&
				c.MissionInventory[index].Codename == ref.Codename {
				row = index
				break
			}
		}
		if row < 0 {
			return false
		}
		used = ref
		return rt.beginReverseReturnScroll(division, c, ref, row,
			reverseReturnTarget{choice: reverseReturnPartyChoice, member: use.member, resolved: &destination},
			use.nowMs, result)
	})
	if committed && used != nil {
		rt.publishItemUseVisual(c, used, 0, result)
	}
}
