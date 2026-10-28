/*
===========================================================================

pickup.go - picking up ground items, including the approach walk

===========================================================================
*/

package action

import (
	"math"
	"time"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
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
func (rt *Runtime) armApproach(divisionID, worldKey, pendingKey string, character *enterworld.Character, groundItem grounditem.Item, approach grounditem.Approach, now time.Time, doors characterDoors) OpResult {
	// The reference server's use-item auto command, state 1,
	// walks to an out-of-range drop via
	// its pursuit start, which refuses
	// when the attack lock reports an open cast
	// (char+C08). The port has no persistent auto-command actor to retry
	// the walk on later ticks: armApproach is the single-shot scheduling
	// step for one 7074 interact, so refusing to arm while
	// PlayerAttackLocked (the port's char+C08 cast fold plus its char+C0C
	// standing-wall fold) is the equivalent of refusing that walk.
	if rt.PlayerAttackLocked(divisionID, character.Name) {
		rt.Pending.Clear(pendingKey)
		return OpResult{}
	}
	// The approach is a real move: live plane + record goal write commit as
	// one unit (ADR-1 S5; WorldStore.mu nests under store.mu per the lock
	// table).
	var ackPayload []byte
	var constrained bool
	var movementRevision uint64
	var movementState simulation.WorldState
	// The walk's own arrival: the character's live speed (scrolls, buffs,
	// walk mode) along the committed path. A fixed run-speed estimate made a
	// faster character stand on the drop until the estimate ran out.
	arrivesAtMs := now.Add(approach.Travel).UnixMilli()
	if !doors.update(character, "pickup-approach", func() bool {
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
				committed, walk, refusal := rt.admitGroundWalk(character.Name, live, world.LiveOwnerAt(now.UnixMilli()), goal)
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
		movementRevision = state.GroundRevision()
		movementState = state
		return true
	}) {
		rt.Pending.Clear(pendingKey)
		if constrained {
			return pickupRefusal(wire.ErrCodeCannotBePicked)
		}
		return pickupRefusal(wire.ErrCodeInvalidRequest)
	}

	arrivesAt := time.UnixMilli(arrivesAtMs)
	rt.Pending.ArmGround(grounditem.Pending{
		Key: pendingKey, DivisionID: divisionID, CharacterName: character.Name,
		ItemGid: groundItem.Gid, ArrivesAt: arrivesAt, MovementRevision: movementRevision,
	})

	current := func() bool { return rt.Worlds.GroundPathCurrent(worldKey, movementState) }
	arm := wire.PickupApproachArmFrame()
	arm.Current = current
	return OpResult{
		Pending: &PendingPickup{ItemGid: groundItem.Gid, Eta: arrivesAt.Sub(now)},
		Frames: []wire.Frame{
			arm,
			{Opcode: simulation.OpMovementAck, Payload: ackPayload, Current: current},
		},
	}
}

/*
==================
grantPickup

grantPickup executes the pickup: gold credits the balance, items merge or
occupy a bag slot with the over-cap remainder left on the ground, and trade
goods go to the transport. The despawn rides the SAME burst as the scoop
(bug B, verified native).
==================
*/
func (rt *Runtime) grantPickup(
	divisionID, worldKey string,
	character, characterSnapshot *enterworld.Character,
	groundItem grounditem.Item,
	doors characterDoors,
) OpResult {
	// Natively trade goods leave the party's item share for the picker's
	// transport (tradepickup.go). Trade goods keep their own immediate
	// door on both lanes (grantTradeGoodsPickup predates the batch).
	if isPickupTradeGoods(groundItem.TypeFlags) {
		return rt.grantTradeGoodsPickup(pickupGrant{division: divisionID, worldKey: worldKey, picker: character,
			recipient: character, snapshot: characterSnapshot, item: groundItem})
	}
	recipient := rt.partyPickupRecipient(divisionID, character, groundItem, rt.Now().UnixMilli())
	return rt.grantPickupTo(pickupGrant{division: divisionID, worldKey: worldKey, picker: character,
		recipient: recipient, snapshot: characterSnapshot, item: groundItem}, doors)
}

/*
==================
pickupGrant

One pickup the party rotation has already resolved (it advances once per
pickup, so the caller asks it once): the picker, the chosen recipient,
the picker's snapshot and the heap. actor names the object that plays the
pickup animation when it is not the picker (a pet's pickup).
==================
*/
type pickupGrant struct {
	division, worldKey string
	picker, recipient  *enterworld.Character
	snapshot           *enterworld.Character
	item               grounditem.Item
	actor              *wire.PickupAnim
}

/*
==================
grantPickupTo

grantPickup's grant for a recipient the rotation already chose.
==================
*/
func (rt *Runtime) grantPickupTo(g pickupGrant, doors characterDoors) (result OpResult) {
	divisionID, worldKey, groundItem, characterSnapshot := g.division, g.worldKey, g.item, g.snapshot
	picker, character := g.picker, g.recipient
	defer func() {
		result = rt.routeSharedPickup(result, g)
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
	if g.actor != nil {
		anim = *g.actor
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
		doors.updateMany(credited, "pickup-gold", func() bool {
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
			return pickupRefusal(wire.ErrCodeTargetGone)
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
	stack := groundStack(groundItem)

	// TWO-PLANE COMMIT (ADR-1 S7): ground despawn (or over-cap remainder
	// write-back) + bag grant in one unit. The Remove cannot miss under the
	// division lock; the refusal arm stays defensive.
	removed := true
	var grant inventory.PickupGrant
	var grantedItem inventory.Item
	var questFrames []wire.Frame
	result = pickupRefusal(wire.ErrCodeInvalidRequest)
	if !doors.update(character, "pickup-item", func() bool {
		if character.DeletePending {
			return false
		}
		inv := bagOf(character)
		var fault *inventory.Fault
		grant, fault = inv.GrantStack(groundInventoryItem(groundItem, stack), stackCap)
		if fault != nil {
			if fault.Code == wire.ErrCodeStorageFull {
				// B-11: a full-bag refusal is honest only with the bag
				// state in the log; a repeat then separates "genuinely
				// full" from a client that lost earlier grant answers.
				log.WithField("character", character.Name).
					WithField("occupied", inv.Len()).
					WithField("capacity", inv.BagEnd()).
					Warn("pickup: ground item refused, bag full")
			}
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
			return pickupRefusal(wire.ErrCodeTargetGone)
		}
		return result
	}

	body := wire.ItemBody{
		TradeOwner:        grantedItem.TradeOwner,
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
		Frames:    wire.PickupItemGrantFrames(anim, grant.DestSlot, body, groundItem.Gid, grant.GroundRemainder, questFrames),
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

/*
================
groundStack

How many a ground item holds: its stack count, or one for a single item.
================
*/
func groundStack(g grounditem.Item) uint16 {
	if g.StackCount == 0 {
		return 1
	}
	return g.StackCount
}

/*
================
groundInventoryItem

The bag item a pickup grants for quantity of a ground item. The drop
notice links this same form, so its tooltip shows what the bag will hold.
================
*/
func groundInventoryItem(g grounditem.Item, quantity uint16) inventory.Item {
	return inventory.Item{
		RecordID:          g.RecordID,
		TradeOwner:        g.TradeOwner,
		RefObjID:          g.RefObjID,
		Codename:          g.Codename,
		TypeFlags:         g.TypeFlags,
		Plus:              g.Plus,
		VarianceBits:      g.VarianceBits,
		Durability:        g.Durability,
		Quantity:          quantity,
		MagicOptions:      g.MagicOptions,
		TransformRefObjID: g.TransformRefObjID,
		Summon:            domain.CloneCOS(g.Summon),
	}
}
