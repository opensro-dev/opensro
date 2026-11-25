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
	// Prefer the max-level Lv.90 armored transport horse (COS_T_DHORSE3: 101 slots, 87,829 HP);
	// fall back to ITEM_COS_T_DHORSE1 only in minimal test fixtures lacking DHORSE3.
	summon, ok := items.ItemRefByCodename("ITEM_COS_T_DHORSE3")
	if !ok {
		summon, ok = items.ItemRefByCodename("ITEM_COS_T_DHORSE1")
	}
	if !ok {
		return nil, fmt.Errorf("caravan transport summoner unavailable")
	}
	ref, ok := refs.CharacterRefByCodename(summon.AssociatedCharacterCodename)
	if !ok {
		return nil, fmt.Errorf("caravan transport %s unavailable", summon.AssociatedCharacterCodename)
	}
	capacity := rt.PetInventoryPages.TransportCapacity(ref.InventoryCapacity)
	if capacity == 0 {
		capacity = uint8(ref.InventoryCapacity)
	}
	return &enterworld.CharacterCOS{GID: gid, RefObjID: ref.RefObjID, Codename: ref.Codename, Summoned: true, CurrentHP: ref.MaxHP, CurrentMP: ref.MaxMP, Name: "Caravan horse", Container: &domain.COSContainer{Capacity: capacity}}, nil
}

/*
================
SyncServerActor
================
*/
func (rt *Runtime) SyncServerActor(division string, c *enterworld.Character, at simulation.Spawn, respawn bool, stacks int, westernGoods bool) *enterworld.Character {
	return rt.SyncServerActorCargo(division, c, at, respawn, stacks, 0, westernGoods)
}

/*
================
SyncServerActorCargo

Synchronizes a RuntimeOnly caravan actor's position, vitals, and (for Traders)
transport cargo. When targetStars > 0, Traders load enough trade stacks on
their max-level transport horse to genuinely reach that star rating under the
shard's active caravanTier rules (scaled by stacks/10 after partial drops).
Thieves manage their own loot transports and are never trimmed here.
================
*/
func (rt *Runtime) SyncServerActorCargo(division string, c *enterworld.Character, at simulation.Spawn, respawn bool, stacks int, targetStars uint8, westernGoods bool) *enterworld.Character {
	if c == nil || !c.RuntimeOnly {
		return nil
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	var restored *enterworld.CharacterCOS
	needTraderTransport := c.Job.Type == domain.JobTrader && c.RuntimeTransportGID != 0 &&
		(respawn || (c.ActiveCOS == nil && enterworld.CharacterAlive(c) && stacks > 0))
	if needTraderTransport {
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
		if respawn {
			c.BattleUntilMs = 0
			if !enterworld.CharacterAlive(c) {
				hp, mp := enterworld.DerivedMaxHP(c), enterworld.DerivedMaxMP(c)
				c.CurrentHP, c.CurrentMP = &hp, &mp
			}
		}
		if restored != nil {
			c.ActiveCOS = restored
		}
		if c.Job.Type == domain.JobTrader && c.ActiveCOS != nil && c.ActiveCOS.Container != nil {
			pet := c.ActiveCOS
			desired := stacks
			if targetStars > 0 && stacks > 0 {
				desired = rt.serverActorDesiredStacks(c, stacks, targetStars, westernGoods)
			}
			if desired < 0 {
				desired = 0
			}
			capSlots := int(pet.Container.Capacity)
			if capSlots > 0 && desired > capSlots {
				desired = capSlots
			}
			if len(pet.Container.Rows) > desired {
				pet.Container.Rows = pet.Container.Rows[:desired]
			}
			if len(pet.Container.Rows) < desired {
				code := "ITEM_ETC_TRADE_CH_01"
				if westernGoods {
					code = "ITEM_ETC_TRADE_WC_01"
				}
				if ref, found := rt.deps.ItemReferences().ItemRefByCodename(code); found {
					maxStack := int64(rt.maxStackFor(ref.TypeFlags(), code))
					for len(pet.Container.Rows) < desired {
						pet.Container.Rows = append(pet.Container.Rows, enterworld.InventoryRow{Slot: int64(len(pet.Container.Rows)), RefObjID: ref.RefObjID, Codename: code, TypeFlags: ref.TypeFlags(), StackCount: maxStack, TradeOwner: c.Job.Alias})
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

type caravanStacksMemo struct {
	stacks, capacity int
	stars            uint8
	western          bool
	desired          int
}

func (rt *Runtime) serverActorDesiredStacks(c *enterworld.Character, logicalStacks int, targetStars uint8, westernGoods bool) int {
	pet := c.ActiveCOS
	if pet == nil || pet.Container == nil || pet.Container.Capacity == 0 {
		return logicalStacks
	}
	// The answer depends only on these inputs (the rows are always this
	// Trader's own trade goods): probe once per change, not every tick.
	key := strings.ToLower(c.Name)
	in := caravanStacksMemo{stacks: logicalStacks, capacity: int(pet.Container.Capacity), stars: targetStars, western: westernGoods}
	rt.caravanStacksMu.Lock()
	memo, ok := rt.caravanStacks[key]
	rt.caravanStacksMu.Unlock()
	if ok && memo.stacks == in.stacks && memo.capacity == in.capacity && memo.stars == in.stars && memo.western == in.western {
		return memo.desired
	}
	in.desired = rt.probeServerActorStacks(c, logicalStacks, targetStars, westernGoods)
	rt.caravanStacksMu.Lock()
	if rt.caravanStacks == nil {
		rt.caravanStacks = make(map[string]caravanStacksMemo)
	}
	rt.caravanStacks[key] = in
	rt.caravanStacksMu.Unlock()
	return in.desired
}

func (rt *Runtime) probeServerActorStacks(c *enterworld.Character, logicalStacks int, targetStars uint8, westernGoods bool) int {
	pet := c.ActiveCOS
	capSlots := int(pet.Container.Capacity)
	if targetStars > 5 {
		targetStars = 5
	}
	// Fast path: if the transport already holds rows and logicalStacks >= 10,
	// check whether it already matches the target star rating.
	if logicalStacks >= 10 && len(pet.Container.Rows) > 0 {
		tier, stars := rt.caravanTier(c)
		if tier == targetStars && (targetStars != 2 || stars <= 2) {
			return len(pet.Container.Rows)
		}
	}
	// Probe the minimum stack count on a scratch copy so caravanTier reaches targetStars.
	code := "ITEM_ETC_TRADE_CH_01"
	if westernGoods {
		code = "ITEM_ETC_TRADE_WC_01"
	}
	ref, found := rt.deps.ItemReferences().ItemRefByCodename(code)
	if !found {
		return logicalStacks
	}
	maxStack := int64(rt.maxStackFor(ref.TypeFlags(), code))
	probe := *c
	probeCOS := *pet
	probeCont := domain.COSContainer{Capacity: pet.Container.Capacity, Rows: make([]enterworld.InventoryRow, 0, capSlots)}
	probeCOS.Container = &probeCont
	probe.ActiveCOS = &probeCOS
	fullForStars := capSlots
	for n := 1; n <= capSlots; n++ {
		row := enterworld.InventoryRow{Slot: int64(n - 1), RefObjID: ref.RefObjID, Codename: code, TypeFlags: ref.TypeFlags(), StackCount: maxStack, TradeOwner: c.Job.Alias}
		if n <= len(pet.Container.Rows) {
			row = pet.Container.Rows[n-1]
		}
		probeCont.Rows = append(probeCont.Rows, row)
		tier, stars := rt.caravanTier(&probe)
		if targetStars == 2 && (tier > 2 || stars > 2) {
			fullForStars = n - 1
			break
		}
		if tier >= targetStars {
			fullForStars = n
			break
		}
	}
	if logicalStacks >= 10 {
		return fullForStars
	}
	return min(fullForStars, max(1, fullForStars*logicalStacks/10))
}

/*
================
summonServerActorTransportLocked

Summons a transport horse for a RuntimeOnly actor (e.g. an AI Thief looting
dropped trade goods) under the caller's division lock.
================
*/
func (rt *Runtime) summonServerActorTransportLocked(division string, c *enterworld.Character) bool {
	if c == nil || !c.RuntimeOnly || c.RuntimeTransportGID == 0 {
		return false
	}
	if c.ActiveCOS != nil {
		return true
	}
	pet, err := rt.NewServerActorTransport(c.RuntimeTransportGID)
	if err != nil {
		return false
	}
	rt.deps.Mutate(c, "caravan thief transport summon", func() {
		if c.ActiveCOS == nil {
			c.ActiveCOS = pet
		}
	})
	at := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, rt.Now().UnixMilli())
	rt.petMu.Lock()
	parent := rt.petSessions[petOwnerKey{division: division, name: strings.ToLower(c.Name)}]
	rt.petMu.Unlock()
	if parent != nil {
		rt.bindCompanionSession(division, parent, pet)
		parent.mover.Park(pet.GID, at)
	}
	return true
}

/*
================
ServerActorDismissTransport

Dismisses a RuntimeOnly actor's active transport (e.g. after a Thief returns
to Bandit Den with stolen goods).
================
*/
func (rt *Runtime) ServerActorDismissTransport(division, name string) {
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil || !c.RuntimeOnly {
		return
	}
	rt.deps.Mutate(c, "caravan transport dismiss", func() {
		c.ActiveCOS = nil
	})
}

/*
================
ServerActorHasTransportGoods

Reports whether a RuntimeOnly actor currently has trade goods loaded in its
active transport.
================
*/
func (rt *Runtime) ServerActorHasTransportGoods(division, name string) bool {
	c := rt.findCharacter(division, name)
	if c == nil || !c.RuntimeOnly {
		return false
	}
	var has bool
	rt.deps.Read(division, func() {
		has = c.ActiveCOS != nil && c.ActiveCOS.Container != nil && len(c.ActiveCOS.Container.Rows) > 0
	})
	return has
}

/*
================
ServerActorReturnPending

Reports whether a RuntimeOnly actor has an active return-scroll countdown.
================
*/
func (rt *Runtime) ServerActorReturnPending(division, name string) bool {
	c := rt.findCharacter(division, name)
	if c == nil || !c.RuntimeOnly {
		return false
	}
	_, exists := rt.returnCasts.Load(simulation.WorldKey(division, c.Name))
	return exists
}

/*
================
ServerActorCasting
================
*/
func (rt *Runtime) ServerActorCasting(division, name string) bool {
	c := rt.findCharacter(division, name)
	if c == nil || !c.RuntimeOnly {
		return false
	}
	return rt.hasOpenSkillCast(division, c.Name) || rt.PlayerAttackLocked(division, c.Name)
}

func (rt *Runtime) serverActorCOSMaxVitals(cos *enterworld.CharacterCOS) (maxHP, maxMP uint32) {
	if cos == nil {
		return 0, 0
	}
	refs, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if !ok {
		return 0, 0
	}
	ref, ok := refs.CharacterRefByCodename(cos.Codename)
	if !ok {
		return 0, 0
	}
	return ref.MaxHP, ref.MaxMP
}

/*
================
ServerActorTransportVitals
================
*/
func (rt *Runtime) ServerActorTransportVitals(division, name string) (hp, maxHP int64, lost bool) {
	c := rt.findCharacter(division, name)
	if c == nil || !c.RuntimeOnly {
		return 0, 0, false
	}
	rt.deps.Read(division, func() {
		if c.RuntimeTransportGID != 0 && c.ActiveCOS == nil {
			lost = true
			return
		}
		if c.ActiveCOS != nil {
			hp = int64(c.ActiveCOS.CurrentHP)
			mHP, _ := rt.serverActorCOSMaxVitals(c.ActiveCOS)
			maxHP = int64(mHP)
		}
	})
	return hp, maxHP, lost
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
		if c.ActiveCOS != nil && c.ActiveCOS.CurrentHP > 0 {
			cosMaxHP, cosMaxMP := rt.serverActorCOSMaxVitals(c.ActiveCOS)
			if cosMaxHP > 0 {
				c.ActiveCOS.CurrentHP = min(c.ActiveCOS.CurrentHP+max(cosMaxHP*35/100, 1), cosMaxHP)
			}
			if cosMaxMP > 0 {
				c.ActiveCOS.CurrentMP = min(c.ActiveCOS.CurrentMP+max(cosMaxMP*35/100, 1), cosMaxMP)
			}
		}
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
