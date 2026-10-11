/*
===========================================================================

cospickup_test.go - COS command completion across inventory and pet motion

===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestCosPickupCommandCompletesThroughInventoryAuthority

The native pet arrives at 2200; the port-only pacing policy slows it to 2500.
================
*/
func TestCosPickupCommandCompletesThroughInventoryAuthority(t *testing.T) {
	for _, paced := range []bool{false, true} {
		for _, distance := range []float32{0, 100} {
			t.Run(map[bool]string{false: "native", true: "paced"}[paced]+"/"+time.Duration(distance).String(), func(t *testing.T) {
				arrivalMs := int64(2200)
				if paced {
					arrivalMs = 2500
				}
				c := testCharacter()
				refs := testCosSource(testItems())
				refs.characters["PET"] = &enterworld.CharacterRef{Codename: "PET", RefObjID: 9, TidWord: 0x21c6, RunSpeed: 100}
				rt, _ := newTestRuntime(c, refs)
				rt.PetPolicies.Pacing = paced
				rt.Now = func() time.Time { return time.UnixMilli(1000) }
				gid, _ := enterworld.CosObjectIDForCharacter(c)
				c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true,
					Container: &domain.COSContainer{Capacity: 2}}
				rt.ConstrainMovement = func(_ string, _, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return to, nil }
				rt.BindPetSession(testDivision, c, 1)
				rt.TickHook()(1000)
				pose := rt.PetPresentation(testDivision, c.Name).World.LiveSpawnAt(1000)
				item := rt.Ground.Add(testDivision, grounditem.Item{GoldAmount: 50, Position: grounditem.Point{
					RegionID: pose.RegionID, X: float32(pose.X) + distance, Z: float32(pose.Z)}})
				body := wire.NewWriter(9).U32(gid).U8(wire.CosCommandPickupTag).U32(item.Gid).Payload()
				before := goldOf(c)
				result := rt.HandleCosCommand(testDivision, c, body)
				frames := result.Frames
				if distance > 0 {
					if len(frames) != 0 || goldOf(c) != before {
						t.Fatal("approach acknowledged before arrival", result)
					}
					rt.TickHook()(1100)
					if paced {
						rt.TickHook()(2200)
						if goldOf(c) != before {
							t.Fatal("slower pet granted gold before arrival")
						}
					}
					for _, batch := range rt.TickHook()(arrivalMs) {
						for _, frame := range batch.Frames {
							frames = append(frames, wire.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
						}
					}
				}
				if goldOf(c) != before+50 || len(rt.Ground.All(testDivision)) != 0 {
					t.Fatal("command failed to commit exactly one pickup", frames)
				}
				assertCosPickupAck(t, frames, gid, item.Gid, true)
				assertCosPickupAck(t, rt.HandleCosCommand(testDivision, c, body).Frames, gid, item.Gid, false)
				if goldOf(c) != before+50 {
					t.Fatal("replay duplicated gold")
				}
			})
		}
	}
}

/*
================
assertCosPickupAck
================
*/
func assertCosPickupAck(t *testing.T, frames []wire.Frame, gid, item uint32, success bool) {
	t.Helper()
	count := 0
	for _, frame := range frames {
		if frame.Opcode != wire.OpCosCommandResult {
			continue
		}
		count++
		p := frame.Payload
		offset, subtype := 3, byte(2)
		if success {
			offset, subtype = 2, 1
		}
		if len(p) != offset+8 || p[0] != subtype || p[1] != wire.CosCommandPickupTag ||
			binary.LittleEndian.Uint32(p[offset:]) != gid || binary.LittleEndian.Uint32(p[offset+4:]) != item {
			t.Fatalf("incorrect pickup acknowledgement: %x", p)
		}
	}
	if count != 1 {
		t.Fatalf("expected one terminal pickup acknowledgement, got %d: %+v", count, frames)
	}
}

/*
================
TestCosPickupFullBagUsesNativeDisableClassWithoutConsumingDrop
================
*/
func TestCosPickupFullBagUsesNativeDisableClassWithoutConsumingDrop(t *testing.T) {
	c := testCharacter()
	refs := testCosSource(testItems())
	refs.characters["PET"] = &enterworld.CharacterRef{Codename: "PET", RefObjID: 9, TidWord: 0x21c6, RunSpeed: 100}
	rt, _ := newTestRuntime(c, refs)
	rt.Now = func() time.Time { return time.UnixMilli(1000) }
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	row := c.MissionInventory[0]
	row.Slot = 0
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true,
		Container: &domain.COSContainer{Capacity: 1, Rows: []domain.InventoryRow{row}}}
	rt.BindPetSession(testDivision, c, 1)
	rt.TickHook()(1000)
	pose := rt.PetPresentation(testDivision, c.Name).World.Spawn
	item := rt.Ground.Add(testDivision, grounditem.Item{RefObjID: row.RefObjID, Codename: row.Codename, TypeFlags: row.TypeFlags,
		StackCount: 1, Position: grounditem.Point{RegionID: pose.RegionID, X: float32(pose.X), Z: float32(pose.Z)}, Y: float32(pose.Y)})
	result := rt.HandleCosCommand(testDivision, c, wire.NewWriter(9).U32(gid).U8(wire.CosCommandPickupTag).U32(item.Gid).Payload())
	assertCosPickupAck(t, result.Frames, gid, item.Gid, false)
	if len(result.Frames) != 1 || result.Frames[0].Payload[2] != cosPickupBagFull ||
		len(rt.Ground.All(testDivision)) != 1 || len(c.ActiveCOS.Container.Rows) != 1 {
		t.Fatal("full bag did not preserve the drop and request native pickup disable", result)
	}
}

/*
================
TestCosPickupCommandRetiresOnLifecycleFailure
================
*/
func TestCosPickupCommandRetiresOnLifecycleFailure(t *testing.T) {
	for _, cause := range []string{"death", "desummon", "target-loss", "timeout", "follow", "relocate"} {
		t.Run(cause, func(t *testing.T) {
			c := testCharacter()
			refs := testCosSource(testItems())
			refs.characters["PET"] = &enterworld.CharacterRef{Codename: "PET", RefObjID: 9, TidWord: 0x21c6, RunSpeed: 1}
			rt, _ := newTestRuntime(c, refs)
			rt.Now = func() time.Time { return time.UnixMilli(1000) }
			gid, _ := enterworld.CosObjectIDForCharacter(c)
			c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 9, Codename: "PET", CurrentHP: 100, Summoned: true,
				Container: &domain.COSContainer{Capacity: 2}}
			rt.ConstrainMovement = func(_ string, _, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return to, nil }
			rt.BindPetSession(testDivision, c, 1)
			rt.TickHook()(1000)
			pose := rt.PetPresentation(testDivision, c.Name).World.LiveSpawnAt(1000)
			item := rt.Ground.Add(testDivision, grounditem.Item{GoldAmount: 50, Position: grounditem.Point{
				RegionID: pose.RegionID, X: float32(pose.X) + 100, Z: float32(pose.Z)}})
			body := wire.NewWriter(9).U32(gid).U8(wire.CosCommandPickupTag).U32(item.Gid).Payload()
			before := goldOf(c)
			if result := rt.HandleCosCommand(testDivision, c, body); len(result.Frames) != 0 {
				t.Fatal("approach rejected", result)
			}
			now := int64(1100)
			var frames []wire.Frame
			switch cause {
			case "follow":
				follow := wire.NewWriter(5).U32(gid).U8(wire.CosCommandFollowTag).Payload()
				frames = rt.HandleCosCommand(testDivision, c, follow).Frames
			case "death":
				c.ActiveCOS.CurrentHP = 0
			case "desummon":
				c.ActiveCOS.Summoned = false
			case "target-loss":
				rt.Ground.Remove(testDivision, item.Gid)
			case "timeout":
				now = 1000 + cosPickupApproachTimeoutMs
			case "relocate":
				// The owner travelled: the pet is placed beside them and leaves
				// PICKITEM, whose exit (55AD60) answers the pending pickup.
				rt.relocateReturningPet(testDivision, c, pose)
			}
			for _, batch := range rt.TickHook()(now) {
				for _, frame := range batch.Frames {
					frames = append(frames, wire.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
				}
			}
			assertCosPickupAck(t, frames, gid, item.Gid, false)
			for _, frame := range frames {
				if frame.Opcode == wire.OpItemMoveResponse {
					t.Fatal("COS command failure leaked into manual inventory lane")
				}
			}
			if goldOf(c) != before {
				t.Fatal("failed pickup granted gold")
			}
		})
	}
}
