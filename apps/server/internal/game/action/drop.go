/*
===========================================================================

drop.go - owns drop behavior and its checked data boundaries

===========================================================================
*/
// Package action plans the position-dependent item operations: it turns an
// inventory mutation plus the dropper's LIVE position into the ground-item
// registry entry the division sees.
//
// THE BUG-D CONTRACT: every `at` parameter in this package is the dropper's
// live position - simulation.WorldState.LiveSpawnAt(nowMs) - NEVER
// WorldState.Spawn, which is the move GOAL. The wave-9 bug-D signature was
// gold dropped mid-run spawning at the pathing destination instead of
// underfoot; the reference fixture reads the live plane on both drop legs
// (server.mjs moveMissionItem types 0x07 and 0x0A), and so must every Go
// call site. The signatures here take a simulation.Spawn so a handler cannot
// compile without deciding which plane it reads - pass the live one.
package action

import (
	"math"
	"opensro.online/server/internal/domain"
	"time"

	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// clampDropCoordinate mirrors the fixture's clampFiniteNumber(value, -0x8000,
// 0xffff) on the drop legs: a non-finite value reads as 0 first.
/*
================
clampDropCoordinate
================
*/
func clampDropCoordinate(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		value = 0
	}
	if value < -0x8000 {
		return -0x8000
	}
	if value > 0xFFFF {
		return 0xFFFF
	}
	return value
}

// placementFor expands a live spawn into the ground-item placement fields.
/*
================
placementFor
================
*/
func placementFor(at simulation.Spawn) (grounditem.Point, float32, uint16) {
	point := grounditem.Point{
		RegionID: at.RegionID,
		X:        float32(clampDropCoordinate(at.X)),
		Z:        float32(clampDropCoordinate(at.Z)),
	}
	return point, float32(clampDropCoordinate(at.Y)), at.Angle
}

// PlanItemDrop builds the registry entry for a type-0x07 ground drop: the
// dropped row lands where the character IS, keeping its identity fields and
// carrying dropCount so a pickup can restore the stack.
//
// at is the dropper's LIVE position (WorldState.LiveSpawnAt) - see the
// package contract. The returned entry carries no Gid; the registry's Add is
// the sole allocator.
/*
================
PlanItemDrop
================
*/
func PlanItemDrop(dropped inventory.Item, dropCount uint16, at simulation.Spawn, droppedBy string, now time.Time) grounditem.Item {
	if dropCount == 0 {
		dropCount = 1
	}
	point, y, heading := placementFor(at)
	return grounditem.Item{
		RecordID:          dropped.RecordID,
		RefObjID:          dropped.RefObjID,
		Codename:          dropped.Codename,
		TypeFlags:         dropped.TypeFlags,
		Plus:              dropped.Plus,
		VarianceBits:      dropped.VarianceBits,
		Durability:        dropped.Durability,
		MagicOptions:      append([]uint64(nil), dropped.MagicOptions...),
		TransformRefObjID: dropped.TransformRefObjID, Summon: domain.CloneCOS(dropped.Summon),
		StackCount: dropCount,
		Position:   point,
		Y:          y,
		Heading:    heading,
		DroppedBy:  droppedBy,
		DroppedAt:  now,
	}
}

// GoldHeapRef is the itemdata row of a gold-heap tier
// (inventory.GoldHeapTier names the codename; the caller resolves the row
// from its reference data).
/*
================
GoldHeapRef
================
*/
type GoldHeapRef struct {
	RefObjID uint32
	Codename string
	// Tid1..Tid4 are the itemdata TypeID columns; the heap's type word is
	// packed from them exactly like the fixture packs the tier row's
	// typeIds.
	Tid1, Tid2, Tid3, Tid4 uint8
}

// TypeFlags packs the heap's RefItemData type word.
/*
================
TypeFlags
================
*/
func (r GoldHeapRef) TypeFlags() uint16 {
	return wire.PackTypeFlags(r.Tid1, r.Tid2, r.Tid3, r.Tid4)
}

// PlanGoldDrop builds the registry entry for a type-0x0A gold drop: the tier
// heap (chosen by amount via inventory.GoldHeapTier) lands where the
// character IS, carrying the amount.
//
// at is the dropper's LIVE position (WorldState.LiveSpawnAt) - the wave-9
// bug-D repro was exactly this leg reading the goal. The returned entry
// carries no Gid; the registry's Add is the sole allocator.
/*
================
PlanGoldDrop
================
*/
func PlanGoldDrop(ref GoldHeapRef, amount uint32, at simulation.Spawn, droppedBy string, now time.Time) grounditem.Item {
	point, y, heading := placementFor(at)
	return grounditem.Item{
		RefObjID:   ref.RefObjID,
		Codename:   ref.Codename,
		TypeFlags:  ref.TypeFlags(),
		GoldAmount: amount,
		Position:   point,
		Y:          y,
		Heading:    heading,
		DroppedBy:  droppedBy,
		DroppedAt:  now,
	}
}

// scatterMonsterDrop follows CGItem::vtable+0x4dc (server 0048FDF0):
// independent angle, radius and heading draws after admission. The server
// reference is v1.188; it is mechanism evidence, not a v1.150 pixel proof.
// Rejected geometry falls back to the live death position, never the move goal.
/*
================
scatterMonsterDrop
================
*/
func (rt *Runtime) scatterMonsterDrop(item grounditem.Item, at simulation.Spawn, rarity uint8) grounditem.Item {
	minRadius, maxRadius := float32(8), float32(20)
	switch rarity {
	case 3, 8:
		minRadius, maxRadius = 10, 30
	case 4:
		minRadius, maxRadius = 10, 40
	case 5:
		minRadius, maxRadius = 20, 100
	}
	draw := func() (float32, bool) {
		if rt.DropRoll == nil {
			return 0, false
		}
		value, err := rt.DropRoll()
		return float32(float64(value) / 32767), err == nil && value <= 32767
	}
	angleUnit, ok := draw()
	if !ok {
		return item
	}
	radiusUnit, ok := draw()
	if !ok {
		return item
	}
	headingUnit, ok := draw()
	if !ok {
		return item
	}
	const tau = 6.2831854820251465
	angle := float32(float64(angleUnit) * tau)
	radius := float32(float64(minRadius) + float64(maxRadius-minRadius)*float64(radiusUnit))
	to := at
	to.X = float64(float32(float32(at.X) + float32(float32(math.Cos(float64(angle)))*radius)))
	to.Z = float64(float32(float32(at.Z) + float32(float32(math.Sin(float64(angle)))*radius)))
	to = simulation.NormalizeSpawnFrame(to)
	if rt.ConstrainMovement != nil {
		checked, refusal := rt.ConstrainMovement(item.DroppedBy, at, to)
		if refusal != nil || checked.RegionID != to.RegionID || checked.X != to.X || checked.Z != to.Z {
			to = at
			if retry, valid := draw(); valid {
				headingUnit = retry
			}
		} else {
			to = checked
		}
	}
	to.Angle = simulation.HeadingWordFromRadians(float64(float32(float64(headingUnit) * tau)))
	item.Position, item.Y, item.Heading = placementFor(to)
	return item
}
