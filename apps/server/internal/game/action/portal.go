/*
===========================================================================

portal.go - authoritative travel and scene re-entry

===========================================================================
*/
package action

import (
	"fmt"
	"math"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/commerce"
	"path/filepath"
	"strconv"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
portalDestination
================
*/
type portalDestination struct {
	id, ref  uint32
	code     string
	building bool
	// fortressGate is a building of TypeID 4/1/1/1 (TypeId_IsFortressGate
	// 4F8820): the field gates of a fortress, not its town portal.
	fortressGate bool
	// gateKind is the TID4 of a 4/1/1 teleport building, which
	// CRefData_FindWorldTeleportByKind (51D260) selects revival gates by: 1
	// a fortress gate, 2 a fortress's revival gate.
	gateKind uint8
	recall   bool
	// world is GenWorldID, the RefGameWorld the gate spawn lies in.
	world instance.DefinitionID
	spawn simulation.Spawn
}

/*
================
portalCondition
================
*/
type portalCondition struct{ kind, minimum, maximum uint32 }

/*
================
portalLink
================
*/
type portalLink struct {
	source, target uint32
	fee            int64
	conditions     []portalCondition
}

/*
================
portalCatalog
================
*/
type portalCatalog struct {
	destinations map[uint32]portalDestination
	sources      map[uint32]uint32
	links        map[[2]uint32]portalLink
	// buildings maps a teleport building's codename to its RefObjID.
	buildings map[string]uint32
}

// Both tables remain server-owned. The request carries neither a price nor a
// position. Conditions are the ordered triples consumed by native 513A70.
/*
================
loadPortalCatalog
================
*/
func loadPortalCatalog(dir string) (*portalCatalog, error) {
	c := &portalCatalog{destinations: map[uint32]portalDestination{}, sources: map[uint32]uint32{}, links: map[[2]uint32]portalLink{},
		buildings: map[string]uint32{}}
	number := func(s string) (uint32, error) { v, e := strconv.ParseUint(s, 10, 32); return uint32(v), e }
	buildings := map[uint32]bool{}
	fortressGates := map[uint32]bool{}
	gateKinds := map[uint32]uint8{}
	buildingRows := enterworld.ReadTextdataFile(filepath.Join(dir, "teleportbuilding.txt"))
	if len(buildingRows) == 0 {
		return nil, fmt.Errorf("teleportbuilding is absent or empty")
	}
	for _, r := range buildingRows {
		if r[0] != "1" {
			continue
		}
		if len(r) < 2 {
			return nil, fmt.Errorf("truncated teleportbuilding")
		}
		id, e := number(r[1])
		if e != nil {
			return nil, e
		}
		buildings[id] = true
		// Columns 9-12 are TypeID1-4.
		if len(r) > 12 && r[9] == "4" && r[10] == "1" && r[11] == "1" {
			if kind, e := strconv.ParseUint(r[12], 10, 8); e == nil {
				gateKinds[id] = uint8(kind)
			}
			fortressGates[id] = r[12] == "1"
		}
		if len(r) > 2 {
			c.buildings[r[2]] = id
		}
	}
	rows := enterworld.ReadTextdataFile(filepath.Join(dir, "teleportdata.txt"))
	if len(rows) == 0 {
		return nil, fmt.Errorf("teleportdata is absent or empty")
	}
	for i, r := range rows {
		if r[0] != "1" {
			continue
		}
		if len(r) < 13 {
			return nil, fmt.Errorf("teleportdata row %d is truncated", i+1)
		}
		id, e := number(r[1])
		if e != nil || id == 0 {
			return nil, fmt.Errorf("teleportdata identity at row %d", i+1)
		}
		ref, e := number(r[3])
		if e != nil {
			return nil, e
		}
		region, e := strconv.ParseInt(r[5], 10, 32)
		if e != nil || region < -32768 || region > 65535 {
			return nil, fmt.Errorf("teleportdata region at row %d", i+1)
		}
		xyz := [3]float64{}
		for j := range xyz {
			v, e := strconv.ParseFloat(r[6+j], 64)
			if e != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, fmt.Errorf("teleportdata coordinate at row %d", i+1)
			}
			xyz[j] = v
		}
		if _, exists := c.destinations[id]; exists {
			return nil, fmt.Errorf("duplicate teleport id %d", id)
		}
		recall, e := number(r[10])
		if e != nil || recall > 1 {
			return nil, fmt.Errorf("teleportdata recall eligibility at row %d", i+1)
		}
		world, e := strconv.ParseUint(r[12], 10, 16)
		if e != nil {
			return nil, fmt.Errorf("teleportdata world at row %d", i+1)
		}
		if _, known := instance.Lookup(instance.DefinitionID(world)); !known {
			return nil, fmt.Errorf("teleportdata row %d names unknown world %d", i+1, world)
		}
		c.destinations[id] = portalDestination{id: id, ref: ref, code: r[2], building: buildings[ref],
			fortressGate: fortressGates[ref], gateKind: gateKinds[ref], recall: recall == 1, world: instance.DefinitionID(world),
			spawn: simulation.Spawn{RegionID: uint16(region), X: xyz[0], Y: xyz[1], Z: xyz[2]}}
		if ref != 0 {
			if _, exists := c.sources[ref]; exists {
				return nil, fmt.Errorf("ambiguous teleport source ref %d", ref)
			}
			c.sources[ref] = id
		}
	}
	rows = enterworld.ReadTextdataFile(filepath.Join(dir, "teleportlink.txt"))
	if len(rows) == 0 {
		return nil, fmt.Errorf("teleportlink is absent or empty")
	}
	for i, r := range rows {
		if r[0] != "1" {
			continue
		}
		if len(r) != 21 {
			return nil, fmt.Errorf("teleportlink row %d shape", i+1)
		}
		values := make([]uint32, 20)
		for j := range values {
			v, e := number(r[j+1])
			if e != nil {
				return nil, fmt.Errorf("teleportlink row %d: %w", i+1, e)
			}
			values[j] = v
		}
		if values[3] != 1 || values[4] != 0 {
			return nil, fmt.Errorf("teleportlink row %d unsupported scheduling/combination", i+1)
		}
		link := portalLink{source: values[0], target: values[1], fee: int64(values[2])}
		if _, ok := c.destinations[link.source]; !ok {
			return nil, fmt.Errorf("missing source teleport %d", link.source)
		}
		if _, ok := c.destinations[link.target]; !ok {
			return nil, fmt.Errorf("missing destination teleport %d", link.target)
		}
		for j := 5; j < 20; j += 3 {
			k, a, b := values[j], values[j+1], values[j+2]
			if k == 0 {
				if a != 0 || b != 0 {
					return nil, fmt.Errorf("orphan teleport condition")
				}
				continue
			}
			if k > 2 || k == 1 && a > b || k == 2 && (a != 0 || b != 0) {
				return nil, fmt.Errorf("teleportlink row %d unsupported condition %d", i+1, k)
			}
			link.conditions = append(link.conditions, portalCondition{k, a, b})
		}
		key := [2]uint32{link.source, link.target}
		if _, ok := c.links[key]; ok {
			return nil, fmt.Errorf("duplicate teleport link %v", key)
		}
		c.links[key] = link
	}
	return c, nil
}

/*
================
buildingGateSpawn

The gate spawn of the teleport whose object is the named building: a
return scroll's Param4 (4A0380 reads it through the reference data's
+0x190 teleport link).
================
*/
func (rt *Runtime) buildingGateSpawn(code string) (travelPoint, bool) {
	if rt.portals == nil {
		return travelPoint{}, false
	}
	ref, ok := rt.portals.buildings[code]
	if !ok {
		return travelPoint{}, false
	}
	for _, destination := range rt.portals.destinations {
		if destination.ref == ref {
			return travelPoint{spawn: destination.spawn, world: portalWorld(destination)}, true
		}
	}
	return travelPoint{}, false
}

/*
================
portalFailure
================
*/
func portalFailure(code byte) OpResult {
	return OpResult{Frames: []wire.Frame{{Opcode: 0xb495, Payload: []byte{2, code}}}}
}

/*
================
error
================
*/
func (rt *Runtime) ConfigurePortals(dir string) error {
	catalog, err := loadPortalCatalog(dir)
	if err != nil {
		return err
	}
	rt.portals = catalog
	fortresses, err := loadFortressCatalog(dir)
	if err != nil {
		return err
	}
	rt.Fortresses = fortress.New(fortresses)
	for i := range rt.NpcRoster {
		for _, row := range fortresses {
			if rt.NpcRoster[i].Codename == row.OfficialNpc {
				rt.NpcRoster[i].TalkFlags |= simulation.NpcTalkFlagFortressOfficial
			}
		}
	}
	for i := range rt.NpcRoster {
		if id, ok := catalog.sources[rt.NpcRoster[i].RefObjID]; ok {
			rt.NpcRoster[i].TalkFlags = (rt.NpcRoster[i].TalkFlags &^ simulation.NpcTalkFlagRecallPoint) | simulation.NpcTalkFlagTeleport
			rt.NpcRoster[i].Services = rt.NpcRoster[i].Services.With(simulation.NpcServiceTeleport)
			rt.NpcRoster[i].RebirthPoint = simulation.Spawn{}
			destination := catalog.destinations[id]
			if destination.recall && destination.spawn.RegionID != 0 && destination.spawn.RegionID&0x8000 == 0 {
				rt.NpcRoster[i].TalkFlags |= simulation.NpcTalkFlagRecallPoint
				rt.NpcRoster[i].Services = rt.NpcRoster[i].Services.With(simulation.NpcServiceRecallPoint)
				rt.NpcRoster[i].RebirthPoint = destination.spawn
			}
		}
	}
	return nil
}

// v1.150 6FEF10 sends a runtime source identity and an authored destination
// id. 75B930 consumes [2,error-byte] refusals. The NPC source must still be
// selected and resident when the authoritative operation commits.
/*
================
OpResult
================
*/
func (rt *Runtime) HandlePortal(division string, c *enterworld.Character, payload []byte) OpResult {
	if c == nil {
		return portalFailure(2)
	}
	r := wire.NewReader(payload)
	gid, e := r.U32()
	if e != nil {
		return portalFailure(2)
	}
	kind, e := r.U8()
	if e == nil && kind == gateReverseReturn {
		choice, e := r.U8()
		if e != nil || r.Done() != nil {
			return portalFailure(2)
		}
		return rt.handleReverseReturn(division, c, gid, choice)
	}
	if e != nil || kind != 2 {
		return portalFailure(0x0f)
	}
	target, e := r.U32()
	if e != nil || r.Done() != nil {
		return portalFailure(2)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	npc, sourceID, refusal := rt.portalSourceNpc(division, c, gid)
	if refusal != 0 {
		return portalFailure(refusal)
	}
	source := rt.portals.destinations[sourceID]
	// 4F30E1: a murderer (PvP state 2) may not pass a building gate (the
	// source object's vtable +0x44), 0x1C16; a ferry still carries one.
	if source.building && murderer(c) {
		return portalFailure(errCodeMurdererGate)
	}

	link, ok := rt.portals.links[[2]uint32{sourceID, target}]
	if !ok {
		return portalFailure(2)
	}
	arrival := rt.portals.destinations[target]
	destination := arrival.spawn
	if destination.RegionID == 0 || destination.RegionID&0x8000 != 0 {
		return portalFailure(2)
	}
	destinationWorld, refusal := rt.portalWorldAdmission(division, c, arrival)
	if refusal != 0 {
		return portalFailure(refusal)
	}
	return rt.commitGateTravel(gateTravel{division: division, character: c, destination: destination, world: destinationWorld, reason: "portal-travel"}, func() (int64, OpResult, bool) {
		mask := uint32(0)
		if rt.QuestTravelBlocks != nil {
			mask = rt.QuestTravelBlocks(c)
		}
		fee, valid := commerce.AdjustPrice(uint64(link.fee), rt.commerceTax(division, npc.RefObjID, c), true)
		if !valid {
			return 0, portalFailure(2), false
		}
		link.fee = int64(fee)
		if failure := portalAdmission(c, source, link, mask, rt.hasSummonedTransportCOS(c)); failure != 0 {
			return 0, portalFailure(failure), false
		}
		if c.NativeTeleportMode != 0 || rt.hasOpenSkillCast(division, c.Name) {
			return 0, portalFailure(0x14), false
		}
		return link.fee, OpResult{}, true
	})
}

/*
================
gateTravel

One move through a gate: the destination point, the world it lies in
(a fortress gate's is the fortress world) and the store reason.
================
*/
type gateTravel struct {
	division    string
	character   *enterworld.Character
	destination simulation.Spawn
	world       instance.ID
	reason      string
}

/*
================
commitGateTravel

Moves the character through a gate to destination: admit runs inside the
character transaction and answers the fee to charge, or the refusal to
send. A move into another world transfers the population session; a
failed transfer or re-entry rolls the move and the fee back. The caller
holds the division lock.
================
*/
func (rt *Runtime) commitGateTravel(travel gateTravel, admit func() (int64, OpResult, bool)) OpResult {
	division, c, destination, destinationWorld, reason := travel.division, travel.character, travel.destination, travel.world, travel.reason
	currentWorld := instance.ID(domain.CharacterWorldInstance(c))
	var previous simulation.WorldState
	rt.bindResidentRegion(simulation.WorldKey(division, c.Name), rt.Now().UnixMilli())
	previousWorld := c.World
	previousGold := c.Gold
	refusal := portalFailure(2)
	key := simulation.WorldKey(division, c.Name)
	if !rt.deps.Update(c, reason, func() bool {
		fee, answer, ok := admit()
		if !ok {
			refusal = answer
			return false
		}
		state := rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
			previous = *w
			w.Spawn = destination
			w.SpawnSet = true
			w.MovementSourceSeeded = true
			w.MoveSegment = nil
			w.Sitting = false
			w.PostureTransitionUntilMs = 0
		})
		writeBackWorld(c, state)
		c.World.MoveSegment = nil
		setCharacterWorld(c, destinationWorld)
		gold := int64(0)
		if c.Gold != nil {
			gold = *c.Gold
		}
		gold -= fee
		c.Gold = &gold
		return true
	}) {
		return refusal
	}
	var membership populationAdmission
	if destinationWorld != currentWorld {
		previousMembership, status := rt.transferPopulationSession(division, c.Name, destinationWorld, c.GMPrivilege)
		if status != instance.Success {
			rt.deps.Update(c, "portal-world-rollback", func() bool {
				rt.Worlds.Update(key, func() simulation.WorldState { return previous }, func(w *simulation.WorldState) { *w = previous })
				c.World = previousWorld
				c.Gold = previousGold
				return true
			})
			return portalFailure(portalTransferError(status))
		}
		membership = previousMembership
	}
	rt.endTransformForLoading(division, c)
	rt.endPartyAurasForLoading(division, c)
	previousPets := rt.relocateReturningPet(division, c, destination)
	packets, accepted := rt.deps.ReentryPackets(division, c.Name)
	if !accepted || len(packets) == 0 || packets[0].NativeOpcode != enterworld.OpcodeResetClient {
		if destinationWorld != currentWorld {
			rt.restorePopulationSession(membership)
		}
		rt.restoreCompanionRelocation(previousPets)
		rt.deps.Update(c, "portal-entry-rollback", func() bool {
			rt.Worlds.Update(key, func() simulation.WorldState { return previous }, func(w *simulation.WorldState) { *w = previous })
			c.World = previousWorld
			c.Gold = previousGold
			return true
		})
		return portalFailure(2)
	}
	rt.bindResidentRegion(key, rt.Now().UnixMilli())
	rt.Selected.Clear(division, c.Name)
	rt.NpcDialogs.Clear(division, c.Name)
	rt.ClearCombatIntent(division, c.Name)
	rt.clearSkillFinalizes(division, c.Name)
	rt.clearCompoundJob(compoundKey{division, c.Name})
	rt.Pending.Clear(grounditem.PendingKey(division, c.Name))
	corpses, corpseDespawns := rt.retireCompanionCorpses(division, c)
	return OpResult{Frames: append(missionReentryFrames(packets), corpses...), Broadcast: append(corpseDespawns, wire.Frame{Opcode: wire.OpObjectSourceCorrection, Payload: wire.ObjectSourceCorrection{Gid: enterworld.ObjectIDForCharacter(c), Position: wire.Position{RegionID: destination.RegionID, X: float32(destination.X), Y: float32(destination.Y), Z: float32(destination.Z), Heading: destination.Angle}}.Encode()})}
}

/*
================
portalSourceNpc

The selected, resident gate NPC a 0x7495 names, within its reach (300, or
800 for a building gate). Returns the NPC, its portal source id, or the
refusal byte. The caller holds the division lock.
================
*/
func (rt *Runtime) portalSourceNpc(division string, c *enterworld.Character, gid uint32) (simulation.NpcDef, uint32, byte) {
	if rt.portals == nil {
		return simulation.NpcDef{}, 0, 2
	}
	selected, ok := rt.Selected.Get(division, c.Name)
	if !ok || selected != gid {
		return simulation.NpcDef{}, 0, 2
	}
	npc, ok := rt.npcForCurrentViewer(division, c, gid)
	if !ok {
		return simulation.NpcDef{}, 0, 2
	}
	sourceID, ok := rt.portals.sources[npc.RefObjID]
	if !ok {
		return simulation.NpcDef{}, 0, 2
	}
	if npc.AuthoredSpawn {
		live := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, rt.Now().UnixMilli())
		planar := simulation.WorldDistance2D(live, npc.Spawn)
		distance := math.Hypot(planar, live.Y-npc.Spawn.Y)
		limit := 300.0
		if rt.portals.destinations[sourceID].building {
			limit = 800
		}
		if math.IsNaN(distance) || math.IsInf(distance, 0) || distance > limit {
			return simulation.NpcDef{}, 0, 4
		}
	}
	return npc, sourceID, 0
}

// 4F2D05: the quest bit blocks buildings and the special GATE_TD route;
// it does not turn ordinary ferries into town gates. 513A70 separately checks
// the authored transport-COS restriction on ferry links.
/*
================
portalAdmission
================
*/
func portalAdmission(c *enterworld.Character, source portalDestination, link portalLink, questMask uint32, transportCOS bool) byte {
	if !enterworld.CharacterAlive(c) || c.DeletePending {
		return 2
	}
	if questMask&0x40000 != 0 && (source.building || source.code == "GATE_TD") {
		return 0x17
	}
	for _, condition := range link.conditions {
		switch condition.kind {
		case 1:
			level := uint32(0)
			if c.Level != nil && *c.Level >= 0 {
				level = uint32(*c.Level)
			}
			if level < condition.minimum || level > condition.maximum {
				return 0x15
			}
		case 2:
			if transportCOS {
				return 0x10
			}
		}
	}
	if link.fee > 0 && (c.Gold == nil || *c.Gold < link.fee) {
		return 7
	}
	return 0
}
