/*
===========================================================================

pickup.go - picking up ground items, including the approach walk

===========================================================================
*/

package action

import (
	"math"
	"opensro.online/server/internal/domain"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
armApproach

armApproach walks the character to the drop: the approach is a real move
on the live plane (a drop issued while walking lands underfoot too), the
pending matures at the travel time, and the client gets the latch arm plus
the movement ack toward the item.
==================
*/
func (rt *Runtime) armApproach(divisionID, worldKey, pendingKey string, character *enterworld.Character, groundItem grounditem.Item, approach grounditem.Approach, now time.Time) OpResult {
	// The approach is a real move: live plane + record goal write commit as
	// one unit (ADR-1 S5; WorldStore.mu nests under store.mu per the lock
	// table).
	var ackPayload []byte
	var constrained bool
	// The walk's own arrival: the character's live speed (scrolls, buffs,
	// walk mode) along the committed path. A fixed run-speed estimate made a
	// faster character stand on the drop until the estimate ran out.
	arrivesAtMs := now.Add(approach.Travel).UnixMilli()
	if !rt.deps.Update(character, "pickup-approach", func() bool {
		if character.DeletePending {
			return false
		}
		state := rt.Worlds.Update(worldKey,
			func() simulation.WorldState { return simulation.SeedWorldState(character) },
			func(world *simulation.WorldState) {
				request := simulation.MovementRequest{
					Mode:     simulation.MovementAckDestinationMode,
					RegionID: groundItem.Position.RegionID,
					X:        float64(groundItem.Position.X),
					Y:        float64(groundItem.Y),
					Z:        float64(groundItem.Position.Z),
				}
				live := world.LiveSpawnAt(now.UnixMilli())
				goal := simulation.SpawnFromMovement(request, live)
				committed, walk, refusal := rt.constrainWalk(character.Name, live, world.LiveOwnerAt(now.UnixMilli()), goal)
				if refusal != nil || !samePlacement(committed, goal) {
					constrained = true
					return
				}
				request.Y = committed.Y
				result := simulation.ApplyMove(world, enterworld.ObjectIDForCharacter(character), request, world.MovementMode, now.UnixMilli())
				world.CommitWalk(walk.Spans, walk.Rest)
				ackPayload = result.AckPayload
				if result.Segment != nil && result.Segment.Valid() {
					arrivesAtMs = result.Segment.ArrivesAtMs
				}
			})
		if constrained {
			return false
		}
		writeBackWorld(character, state)
		return true
	}) {
		rt.Pending.Clear(pendingKey)
		if constrained {
			return pickupRefusal(wire.ErrCodeCannotBePicked)
		}
		return pickupRefusal(wire.ErrCodeInvalidRequest)
	}

	arrivesAt := time.UnixMilli(arrivesAtMs)
	rt.Pending.ArmOwned(
		pendingKey,
		divisionID,
		character.Name,
		groundItem.Gid,
		arrivesAt,
	)

	return OpResult{
		Pending: &PendingPickup{ItemGid: groundItem.Gid, Eta: arrivesAt.Sub(now)},
		Frames: []wire.Frame{
			wire.PickupApproachArmFrame(),
			{Opcode: simulation.OpMovementAck, Payload: ackPayload},
		},
	}
}

/*
==================
grantPickup

grantPickup executes the pickup: gold credits the balance, items merge or
occupy a bag slot with the over-cap remainder left on the ground. The
despawn rides the SAME burst as the scoop (bug B, verified native).
==================
*/
func (rt *Runtime) grantPickup(
	divisionID, worldKey string,
	character, characterSnapshot *enterworld.Character,
	groundItem grounditem.Item,
) (result OpResult) {
	picker := character
	character = rt.partyPickupRecipient(divisionID, picker, groundItem, rt.Now().UnixMilli())
	defer func() {
		result = routeSharedPickup(result, picker, character)
		// A granted pickup publishes its scoop; only then does the party learn of it.
		if len(result.Broadcast) > 0 {
			result.Recipients = append(result.Recipients, rt.partyLootNotice(divisionID, picker, character, groundItem, rt.Now().UnixMilli())...)
		}
	}()
	snapshot := rt.Worlds.Snapshot(worldKey, func() simulation.WorldState {
		return simulation.SeedWorldState(characterSnapshot)
	})
	anim := wire.PickupAnim{
		Gid:     enterworld.ObjectIDForCharacter(picker),
		Heading: wire.HeadingByteFromAngle(snapshot.Spawn.Angle),
	}

	if groundItem.IsGold() {
		// TWO-PLANE COMMIT (ADR-1 S6): heap despawn + balance credit in one
		// unit. The Remove cannot miss in practice (the division lock covers
		// the Get in HandleTargetInteract through to here and the TTL sweep
		// owns the maintenance barrier), but the refusal arm stays defensive; its
		// commit is a no-op by content.
		// The rotation chose character; its party may split the heap.
		shares := rt.partyGoldShares(divisionID, character, groundItem.GoldAmount, rt.Now().UnixMilli())
		split := shares != nil
		if !split {
			shares = []goldShare{{character, groundItem.GoldAmount}}
		}
		credited := make([]*enterworld.Character, len(shares))
		for i, s := range shares {
			credited[i] = s.character
		}
		removed := false
		balances := make([]uint64, len(shares))
		rt.deps.UpdateMany(credited, "pickup-gold", func() bool {
			for _, s := range shares {
				// The durable character stores signed-64 gold. Refuse before removing
				// the heap rather than publishing a balance that persistence clamps.
				if s.character.DeletePending || goldOf(s.character) > uint64(math.MaxInt64)-uint64(s.amount) {
					return false
				}
			}
			if _, removed = rt.Ground.Remove(divisionID, groundItem.Gid); !removed {
				return false
			}
			for i, s := range shares {
				balances[i] = inventory.PickupGold(goldOf(s.character), s.amount)
				setGold(s.character, balances[i])
			}
			return true
		})
		if !removed {
			return pickupRefusal(wire.ErrCodeCannotBePicked)
		}
		result := OpResult{Broadcast: wire.PickupBroadcastFrames(anim, groundItem.Gid, 0)}
		for i, s := range shares {
			if s.character == character {
				result.Frames = wire.PickupGoldGrantFrames(anim, groundItem.GoldAmount, balances[i], groundItem.Gid, split)
				continue
			}
			// Each other share: AddGold(share, reason 0x17, send 1, notify 1).
			result.Recipients = append(result.Recipients, RecipientFrames{CharacterID: s.character.ID, Frames: []wire.Frame{{
				Opcode:  wire.OpPointsUpdate,
				Payload: wire.GoldRefresh{Balance: balances[i], Notify: true}.Encode(),
			}}})
		}
		return result
	}

	groundItem.Summon.RefreshRentalTimes(rt.Now().Unix())
	stackCap := rt.maxStackFor(groundItem.TypeFlags, groundItem.Codename)
	stack := groundItem.StackCount
	if stack == 0 {
		stack = 1
	}

	// TWO-PLANE COMMIT (ADR-1 S7): ground despawn (or over-cap remainder
	// write-back) + bag grant in one unit. The Remove cannot miss under the
	// division lock; the refusal arm stays defensive.
	removed := true
	var grant inventory.PickupGrant
	var grantedItem inventory.Item
	var questFrames []wire.Frame
	result = pickupRefusal(wire.ErrCodeInvalidRequest)
	if !rt.deps.Update(character, "pickup-item", func() bool {
		if character.DeletePending {
			return false
		}
		inv := inventory.New(invItemsFromRows(character.MissionInventory))
		var fault *inventory.Fault
		grant, fault = inv.GrantStack(inventory.Item{
			RecordID:          groundItem.RecordID,
			TradeOwner:        groundItem.TradeOwner,
			RefObjID:          groundItem.RefObjID,
			Codename:          groundItem.Codename,
			TypeFlags:         groundItem.TypeFlags,
			Plus:              groundItem.Plus,
			VarianceBits:      groundItem.VarianceBits,
			Durability:        groundItem.Durability,
			Quantity:          stack,
			MagicOptions:      groundItem.MagicOptions,
			TransformRefObjID: groundItem.TransformRefObjID, Summon: domain.CloneCOS(groundItem.Summon),
		}, stackCap)
		if fault != nil {
			result = pickupRefusal(fault.Code)
			return false
		}
		var destinationPresent bool
		grantedItem, destinationPresent = inv.At(grant.DestSlot)
		if !destinationPresent {
			return false
		}
		if grant.GroundRemainder > 0 {
			// Over-cap pickup leaves the REMAINDER on the ground: the heap
			// keeps its gid and its rendered entity, so the despawn is
			// withheld.
			rt.Ground.SetStackCount(divisionID, groundItem.Gid, grant.GroundRemainder)
		} else if _, ok := rt.Ground.Remove(divisionID, groundItem.Gid); !ok {
			removed = false
			return false
		}
		character.MissionInventory = rowsFromInvItems(inv.Items())
		questFrames = rt.updateQuestInventory(character)
		return true
	}) {
		if !removed {
			return pickupRefusal(wire.ErrCodeCannotBePicked)
		}
		return result
	}

	body := wire.ItemBody{
		TypeFlags:         grantedItem.TypeFlags,
		Quantity:          grantedItem.Quantity,
		RefObjID:          grantedItem.RefObjID,
		Plus:              grantedItem.Plus,
		VarianceBits:      grantedItem.VarianceBits,
		Durability:        grantedItem.Durability,
		MagicOptions:      grantedItem.MagicOptions,
		TransformRefObjID: grantedItem.TransformRefObjID, Summon: domain.CloneCOS(grantedItem.Summon),
	}
	return OpResult{
		Frames:    append(wire.PickupItemGrantFrames(anim, grant.DestSlot, body, groundItem.Gid, grant.GroundRemainder), questFrames...),
		Broadcast: wire.PickupBroadcastFrames(anim, groundItem.Gid, grant.GroundRemainder),
	}
}

/*
================
updateQuestInventory
================
*/
func (rt *Runtime) updateQuestInventory(character *enterworld.Character) []wire.Frame {
	if rt.UpdateQuestInventory == nil {
		return nil
	}
	frames, _ := rt.UpdateQuestInventory(character)
	return frames
}
