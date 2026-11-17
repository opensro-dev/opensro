/*
===========================================================================

serveractor.go - port-only caravan actors using the normal action authorities

The composition root owns AI intent; this lane owns character mutations,
companion positions, actual goods on the ground and player gold settlement.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"fmt"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"strings"
)

/*
================
NewServerActorTransport
================
*/
func (rt *Runtime) NewServerActorTransport(gid uint32) (*enterworld.CharacterCOS, error) {
	if gid < domain.COSGIDBase || gid > domain.COSGIDLimit {
		return nil, fmt.Errorf("caravan transport gid outside COS identity range")
	}
	items := rt.deps.ItemReferences()
	refs, ok := items.(enterworld.CharacterRefSource)
	if !ok {
		return nil, fmt.Errorf("caravan transport references unavailable")
	}
	// Higher armored tiers name BSRs absent from the v1.150 client extraction.
	summon, ok := items.ItemRefByCodename("ITEM_COS_T_DHORSE1")
	if !ok {
		return nil, fmt.Errorf("caravan transport summoner unavailable")
	}
	ref, ok := refs.CharacterRefByCodename(summon.AssociatedCharacterCodename)
	if !ok {
		return nil, fmt.Errorf("caravan transport %s unavailable", summon.AssociatedCharacterCodename)
	}
	return &enterworld.CharacterCOS{GID: gid, RefObjID: ref.RefObjID, Codename: ref.Codename, Summoned: true, CurrentHP: ref.MaxHP, CurrentMP: ref.MaxMP, Name: "Caravan horse", Container: &domain.COSContainer{Capacity: uint8(ref.InventoryCapacity)}}, nil
}

/*
================
SyncServerActor
================
*/
func (rt *Runtime) SyncServerActor(division string, c *enterworld.Character, at simulation.Spawn, respawn bool, stacks int, westernGoods bool) *enterworld.Character {
	if c == nil || !c.RuntimeOnly {
		return nil
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	var restored *enterworld.CharacterCOS
	if respawn && c.RuntimeTransportGID != 0 {
		var err error
		restored, err = rt.NewServerActorTransport(c.RuntimeTransportGID)
		if err != nil {
			return rt.characterSnapshot(division, c)
		}
	}
	rt.deps.MutateDeferred(c, "caravan actor tick", func() {
		// The transient operating float pays ferry fares; it is never player-owned or lootable.
		if c.Gold == nil || *c.Gold < 5000 {
			gold := int64(1_000_000)
			c.Gold = &gold
		}
		if respawn && !enterworld.CharacterAlive(c) {
			hp, mp := enterworld.DerivedMaxHP(c), enterworld.DerivedMaxMP(c)
			c.CurrentHP, c.CurrentMP = &hp, &mp
		}
		if restored != nil {
			c.ActiveCOS = restored
		}
		if c.ActiveCOS != nil {
			pet := c.ActiveCOS
			if pet.Container != nil && len(pet.Container.Rows) > max(stacks, 0) {
				pet.Container.Rows = pet.Container.Rows[:max(stacks, 0)]
			}
			if pet.Container != nil && len(pet.Container.Rows) < stacks {
				code := "ITEM_ETC_TRADE_CH_01"
				if westernGoods {
					code = "ITEM_ETC_TRADE_WC_01"
				}
				if ref, found := rt.deps.ItemReferences().ItemRefByCodename(code); found {
					for len(pet.Container.Rows) < min(stacks, 30) {
						pet.Container.Rows = append(pet.Container.Rows, enterworld.InventoryRow{Slot: int64(len(pet.Container.Rows)), RefObjID: ref.RefObjID, Codename: code, TypeFlags: ref.TypeFlags(), StackCount: int64(rt.maxStackFor(ref.TypeFlags(), code)), TradeOwner: c.Job.Alias})
					}
				}
			}
		}
	})
	if restored != nil {
		rt.petMu.Lock()
		parent := rt.petSessions[petOwnerKey{division: division, name: strings.ToLower(c.Name)}]
		rt.petMu.Unlock()
		if parent != nil {
			rt.bindCompanionSession(division, parent, restored)
		}
	}
	rt.Worlds.Update(simulation.WorldKey(division, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(state *simulation.WorldState) {
		state.Spawn, state.SpawnSet, state.MoveSegment = at, true, nil
	})
	rt.petMu.Lock()
	for key, pet := range rt.petSessions {
		if key.division == division && pet.character == c {
			if c.ActiveCOS != nil {
				pet.mover.Park(c.ActiveCOS.GID, at)
			}
		}
	}
	rt.petMu.Unlock()
	rt.registerCaravan(division, c)
	return rt.characterSnapshot(division, c)
}

/*
================
HealServerActor
================
*/
func (rt *Runtime) HealServerActor(division, name string, amount int64) int64 {
	c := rt.findCharacter(division, name)
	if c == nil || !c.RuntimeOnly || amount <= 0 {
		return 0
	}
	var current int64
	rt.deps.MutateDeferred(c, "caravan potion", func() {
		current = enterworld.CurrentHP(c)
		if current <= 0 {
			return
		}
		current = min(current+amount, enterworld.DerivedMaxHP(c))
		c.CurrentHP = &current
		mp := min(enterworld.CurrentMP(c)+enterworld.DerivedMaxMP(c)*35/100, enterworld.DerivedMaxMP(c))
		c.CurrentMP = &mp
	})
	return current
}

/*
================
DropServerActorCargo
================
*/
func (rt *Runtime) DropServerActorCargo(division, name string, stacks int) []wire.Frame {
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil || !c.RuntimeOnly || stacks <= 0 {
		return nil
	}
	var frames []wire.Frame
	rt.deps.Mutate(c, "caravan parley goods", func() {
		pet := c.ActiveCOS
		if pet == nil || pet.Container == nil {
			return
		}
		count := min(stacks, len(pet.Container.Rows))
		part := *pet
		container := *pet.Container
		part.Container = &container
		container.Rows = append([]enterworld.InventoryRow(nil), pet.Container.Rows[:count]...)
		frames = rt.scatterTransportCargo(division, c, &part, rt.Now().UnixMilli(), cargoLoss{cause: "caravan-parley"})
		pet.Container.Rows = pet.Container.Rows[count:]
		for i := range pet.Container.Rows {
			pet.Container.Rows[i].Slot = int64(i)
		}
	})
	return frames
}

/*
================
ServerActorRide

AI uses the ordinary mount admission and state transition.
================
*/
func (rt *Runtime) ServerActorRide(division, name string, mounted bool) OpResult {
	c := rt.findCharacter(division, name)
	snapshot := rt.characterSnapshot(division, c)
	if snapshot == nil || !snapshot.RuntimeOnly || snapshot.ActiveCOS == nil || snapshot.ActiveCOS.Mounted == mounted {
		return OpResult{}
	}
	payload := make([]byte, 5)
	if mounted {
		payload[0] = 1
	}
	binary.LittleEndian.PutUint32(payload[1:], snapshot.ActiveCOS.GID)
	return rt.HandleCosRide(division, c, payload)
}

/*
================
AdmitServerActor
================
*/
func (rt *Runtime) AdmitServerActor(division, name string) error {
	c := rt.findCharacter(division, name)
	if c == nil || !c.RuntimeOnly {
		return fmt.Errorf("not a server actor")
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	return rt.admitPopulationSession(division, name, uint64(c.ID))
}
