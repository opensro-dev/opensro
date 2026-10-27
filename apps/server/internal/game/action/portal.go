/*
===========================================================================

portal.go - authoritative travel and scene re-entry

===========================================================================
*/
package action

import (
	_ "embed"
	"fmt"
	"math"
	"opensro.online/server/internal/data/texttable"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/commerce"
	"path/filepath"
	"strconv"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/alexandria"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

//go:embed data/qinshi_tomb_teleportdata.tsv
var qinshiTombTeleportDataTSV string

//go:embed data/qinshi_tomb_teleportlink.tsv
var qinshiTombTeleportLinkTSV string

/*
================
portalDestination
================
*/
type portalDestination struct {
	id, ref  uint32
	code     string
	building bool
	// fortressGate is a building of TypeID 4/1/1/1 (the reference server's
	// fortress gate test): the field gates of a fortress, not its town portal.
	fortressGate bool
	// gateKind is the TID4 of a 4/1/1 teleport building, which
	// the reference server's teleport lookup by kind selects revival gates by: 1
	// a fortress gate, 2 a fortress's revival gate.
	gateKind uint8
	recall   bool
	// returnPoint is CanGotoResurrectPos (column 11): the reference
	// server's teleport row reader gives the object service 9, the talk row
	// that sends the player to their return point (0x7495 type 1).
	returnPoint bool
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

// portalConditionTransport is Restrict kind 2: the reference server's
// teleport link check refuses 0x1C10 while the player's first summoned
// COS is a transport (vt+0x2EC tests TID band 2).
const portalConditionTransport uint32 = 2

// portalConditionThiefSuitTransport is Restrict kind 7 (the Alexandria
// ferry arrivals): the same check refuses 0x1C10 while the item in the
// job-suit socket is a thief suit and the kind-2 transport test stands.
// The operands are never read. The port-only job-goods admission
// (transportAdmitted) belongs to kind 2 and does not reach this kind.
const portalConditionThiefSuitTransport uint32 = 7

/*
================
linkRefusesTransport

Whether a link refuses a summoned transport. The server reads Restrict from
its database, and both research databases (portaltransport_generated.go)
put kind 2 on the town gates and none on the ferries, flyships and tunnels,
as the v1.150 client's own text says (UIIT_MSG_STRGERR_CANT_TELEPORT_WITH_CART:
"... ferries can be used"). The client's teleportlink.txt carries the kind
on the reverse links, so it is used only for a link the databases lack.
================
*/
func linkRefusesTransport(source, target string, clientTransport bool) bool {
	if refuses, known := serverTransportLinks[[2]string{source, target}]; known {
		return refuses
	}
	return clientTransport
}

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
	// walkIns maps a TELEPORT event zone's codename to its teleport's one
	// link (walkin.go).
	walkIns map[string]portalLink
	// questGate is the quest instance exit gate's building row
	// (INS_QUEST_TELEPORT, questgate.go); nil when the data lacks it.
	questGate *questGateBuilding
}

// Both tables remain server-owned. The request carries neither a price nor a
// position. Conditions are the ordered triples the native link check consumes.
/*
================
loadPortalCatalog
================
*/
func loadPortalCatalog(dir string) (*portalCatalog, error) {
	key := texttable.Fingerprint(filepath.Join(dir, "teleportbuilding.txt"),
		filepath.Join(dir, "teleportdata.txt"), filepath.Join(dir, "teleportlink.txt"),
		filepath.Join(dir, alexandria.OverlayFile))
	if monster.QinShiTombFromEnv() {
		key += "+qinshi_tomb"
	}
	if on, _ := alexandria.EnabledFromEnv(); on {
		key += "+alexandria"
	}
	parsed := portalCatalogParses.Get(key, func() *portalCatalogParse {
		catalog, err := parsePortalCatalog(dir)
		return &portalCatalogParse{catalog: catalog, err: err}
	})
	return parsed.catalog, parsed.err
}

// portalCatalogParse is one immutable reading of the teleport tables; every
// runtime over the same files, Qin-Shi and Alexandria settings shares it,
// and nothing writes to a loaded catalog.
type portalCatalogParse struct {
	catalog *portalCatalog
	err     error
}

var portalCatalogParses texttable.SharedParses[portalCatalogParse]

/*
================
parsePortalCatalog
================
*/
func parsePortalCatalog(dir string) (*portalCatalog, error) {
	c := &portalCatalog{destinations: map[uint32]portalDestination{}, sources: map[uint32]uint32{}, links: map[[2]uint32]portalLink{},
		buildings: map[string]uint32{}}
	number := func(s string) (uint32, error) { v, e := strconv.ParseUint(s, 10, 32); return uint32(v), e }
	buildings := map[uint32]bool{}
	fortressGates := map[uint32]bool{}
	gateKinds := map[uint32]uint8{}
	// Port-only, not native: while SRO_ALEXANDRIA is off the catalogue skips
	// Alexandria's appended gates, their buildings and every link to them.
	hidden, err := alexandria.Hidden(dir)
	if err != nil {
		return nil, err
	}
	hiddenIDs := map[uint32]bool{}
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
		if len(r) > 2 && hidden.HidesBuilding(r[2]) {
			continue
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
		if len(r) > 2 && r[2] == questGateCodename {
			gate, e := questGateBuildingRow(r)
			if e != nil {
				return nil, e
			}
			c.questGate = gate
		}
	}
	rows := enterworld.ReadTextdataFile(filepath.Join(dir, "teleportdata.txt"))
	if len(rows) == 0 {
		return nil, fmt.Errorf("teleportdata is absent or empty")
	}
	if monster.QinShiTombFromEnv() {
		// Port-only, not native: append the 13 Qin-Shi Emperor Tomb Floor 5-6
		// walk-in teleport source/destination rows (IDs 136..148).
		rows = append(rows, monster.SplitTabbedText(qinshiTombTeleportDataTSV)...)
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
		if hidden.HidesTeleport(r[2]) {
			hiddenIDs[id] = true
			continue
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
		returnPoint, e := number(r[11])
		if e != nil || returnPoint > 1 {
			return nil, fmt.Errorf("teleportdata return point eligibility at row %d", i+1)
		}
		world, e := strconv.ParseUint(r[12], 10, 16)
		if e != nil {
			return nil, fmt.Errorf("teleportdata world at row %d", i+1)
		}
		if _, known := instance.Lookup(instance.DefinitionID(world)); !known {
			return nil, fmt.Errorf("teleportdata row %d names unknown world %d", i+1, world)
		}
		c.destinations[id] = portalDestination{id: id, ref: ref, code: r[2], building: buildings[ref],
			fortressGate: fortressGates[ref], gateKind: gateKinds[ref], recall: recall == 1, returnPoint: returnPoint == 1, world: instance.DefinitionID(world),
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
	if monster.QinShiTombFromEnv() {
		// Port-only, not native: append the 77 Qin-Shi Emperor Tomb teleport
		// links (72 walk-in links + 5 building links 125 and 131..134).
		rows = append(rows, monster.SplitTabbedText(qinshiTombTeleportLinkTSV)...)
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
		if hiddenIDs[link.source] || hiddenIDs[link.target] {
			continue
		}
		if _, ok := c.destinations[link.source]; !ok {
			return nil, fmt.Errorf("missing source teleport %d", link.source)
		}
		if _, ok := c.destinations[link.target]; !ok {
			return nil, fmt.Errorf("missing destination teleport %d", link.target)
		}
		clientTransport := false
		for j := 5; j < 20; j += 3 {
			k, a, b := values[j], values[j+1], values[j+2]
			if k == 0 {
				if a != 0 || b != 0 {
					return nil, fmt.Errorf("orphan teleport condition")
				}
				continue
			}
			// Only the kinds portalAdmission evaluates load; any other
			// kind refuses the whole catalogue (#1749 PR-C(ii)).
			switch k {
			case 1:
				if a > b {
					return nil, fmt.Errorf("teleportlink row %d unsupported condition %d", i+1, k)
				}
			case portalConditionTransport:
				if a != 0 || b != 0 {
					return nil, fmt.Errorf("teleportlink row %d unsupported condition %d", i+1, k)
				}
				clientTransport = true
				continue
			case portalConditionThiefSuitTransport:
				// The native check never reads the operands.
			default:
				return nil, fmt.Errorf("teleportlink row %d unsupported condition %d", i+1, k)
			}
			link.conditions = append(link.conditions, portalCondition{k, a, b})
		}
		if linkRefusesTransport(c.destinations[link.source].code, c.destinations[link.target].code, clientTransport) {
			link.conditions = append(link.conditions, portalCondition{kind: portalConditionTransport})
		}
		key := [2]uint32{link.source, link.target}
		if _, ok := c.links[key]; ok {
			return nil, fmt.Errorf("duplicate teleport link %v", key)
		}
		c.links[key] = link
	}
	walkIns, err := loadWalkIns(dir, c)
	if err != nil {
		return nil, err
	}
	c.walkIns = walkIns
	return c, nil
}

/*
================
buildingGateSpawn

The gate spawn of the teleport whose object is the named building: a
return scroll's Param4 (the reference server reads it through the reference data's
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
questTeleportFrames

A successful 0x7495 use raises the quest lane's used-a-teleport event
(native event 0x26: the use kind and the gate's gid) once the move has
landed and before the success answer; the native use handler fires it
in the success tail every kind shares. The lane answers with any
frames the event earns. No seam wired, no event.
================
*/
func (rt *Runtime) questTeleportFrames(c *enterworld.Character, kind uint8, gid uint32) []wire.Frame {
	if rt.UpdateQuestTeleport == nil {
		return nil
	}
	return rt.UpdateQuestTeleport(c, kind, gid)
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
	if rt.gatePulleys, err = loadGatePulleys(dir); err != nil {
		return err
	}
	if rt.structureUpgrades, err = loadStructureUpgrades(dir); err != nil {
		return err
	}
	rt.fortressForges = fortressForgeRows()
	for i := range rt.NpcRoster {
		for _, row := range fortresses {
			if rt.NpcRoster[i].Codename == row.OfficialNpc {
				rt.NpcRoster[i].TalkFlags |= simulation.NpcTalkFlagFortressOfficial
			}
		}
	}
	// The reference server gives a teleport's object service 8 as its
	// first link attaches, so a source with no link (the Bandit Den gate)
	// offers no destination row.
	linked := map[uint32]bool{}
	for key := range catalog.links {
		linked[key[0]] = true
	}
	for i := range rt.NpcRoster {
		if id, ok := catalog.sources[rt.NpcRoster[i].RefObjID]; ok {
			rt.NpcRoster[i].TalkFlags &^= simulation.NpcTalkFlagRecallPoint | simulation.NpcTalkFlagTeleport | simulation.NpcTalkFlagResurrectPoint
			if linked[id] {
				rt.NpcRoster[i].TalkFlags |= simulation.NpcTalkFlagTeleport
				rt.NpcRoster[i].Services = rt.NpcRoster[i].Services.With(simulation.NpcServiceTeleport)
			}
			rt.NpcRoster[i].RebirthPoint = simulation.Spawn{}
			destination := catalog.destinations[id]
			if destination.returnPoint {
				rt.NpcRoster[i].TalkFlags |= simulation.NpcTalkFlagResurrectPoint
				rt.NpcRoster[i].Services = rt.NpcRoster[i].Services.With(simulation.NpcServiceResurrectPoint)
			}
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
	if e == nil && kind == gateInstanceTravel {
		choice, e := r.U8()
		if e != nil || r.Done() != nil {
			return portalFailure(2)
		}
		return rt.handleQuestGate(division, c, gid, choice)
	}
	if e == nil && kind == gateTownReturn {
		if r.Done() != nil {
			return portalFailure(2)
		}
		return rt.handleGateTownReturn(division, c, gid)
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
	// Reference server: a murderer (PvP state 2) may not pass a building gate (the
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
	if destination.RegionID == 0 || (destination.RegionID&0x8000 != 0 && !source.building) {
		return portalFailure(2)
	}
	destinationWorld, refusal := rt.portalWorldAdmission(division, c, arrival)
	if refusal != 0 {
		return portalFailure(refusal)
	}
	// The gate fee's tax goes to the gate's fortress once the move holds
	// (the reference server's teleport use handler).
	var collected int64
	settled := func() { rt.collectFortressTax(division, npc, collected) }
	return rt.commitGateTravel(gateTravel{division: division, character: c, destination: destination, world: destinationWorld, reason: "portal-travel", settled: settled,
		arrived: func() []wire.Frame { return rt.questTeleportFrames(c, 2, gid) }}, func() (int64, OpResult, bool) {
		mask := uint32(0)
		if rt.QuestTravelBlocks != nil {
			mask = rt.QuestTravelBlocks(c)
		}
		fee, valid := commerce.AdjustPrice(uint64(link.fee), rt.commerceTax(division, npc, c), true)
		if !valid {
			return 0, portalFailure(2), false
		}
		collected = int64(fee) - link.fee
		link.fee = int64(fee)
		transport := rt.hasSummonedTransportCOS(c)
		traveller := portalTraveller{questMask: mask, intoDen: arrival.code == denGateCode, transportCOS: transport, transportFar: transport && rt.transportBeyond(division, c, gateTransportRange),
			transportAdmitted: rt.JobGoodsTeleport}
		if failure := portalAdmission(c, source, link, traveller); failure != 0 {
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
(a fortress gate's is the fortress world), the store reason, and what to
do once the move has committed and can no longer roll back.
================
*/
type gateTravel struct {
	division    string
	character   *enterworld.Character
	destination simulation.Spawn
	world       instance.ID
	reason      string
	settled     func()
	// arrived runs with settled, once the move can no longer roll
	// back; its frames join the traveller's answer (a 0x7495 use
	// raises its quest event there). Nil stays silent.
	arrived func() []wire.Frame
}

/*
================
commitGateTravel

Moves the character through a gate to destination: admit runs inside the
character transaction and answers the fee to charge, or the refusal to
send. A move into another world transfers the population session; a
failed transfer or re-entry rolls the move and the fee back. settled
and arrived run only once the move can no longer roll back. The caller
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
		rt.notePlacement(c, "gate-travel")
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
	rt.noteTeleportArrival(division, c)
	rt.bindResidentRegion(key, rt.Now().UnixMilli())
	if rt.WalkInZoneAt != nil {
		rt.walkInZones.Store(key, rt.walkInZoneAt(destination))
	} else {
		rt.walkInZones.Delete(key)
	}
	rt.Selected.Clear(division, c.Name)
	rt.closeStorageSession(division, c.Name)
	rt.NpcDialogs.Clear(division, c.Name)
	rt.ClearCombatIntent(division, c.Name)
	rt.clearSkillFinalizes(division, c.Name)
	rt.clearCompoundJob(compoundKey{division, c.Name})
	rt.Pending.Clear(grounditem.PendingKey(division, c.Name))
	corpses, corpseDespawns := rt.retireCompanionCorpses(division, c)
	if travel.settled != nil {
		travel.settled()
	}
	var arrived []wire.Frame
	if travel.arrived != nil {
		arrived = travel.arrived()
	}
	return OpResult{Frames: append(append(missionReentryFrames(packets), corpses...), arrived...), Broadcast: append(corpseDespawns, wire.Frame{Opcode: wire.OpObjectSourceCorrection, Payload: wire.ObjectSourceCorrection{Gid: enterworld.ObjectIDForCharacter(c), Position: wire.Position{RegionID: destination.RegionID, X: float32(destination.X), Y: float32(destination.Y), Z: float32(destination.Z), Heading: destination.Angle}}.Encode()})}
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

const (
	// denGateCode is the Bandit Den's teleport (teleportdata GATE_TD).
	denGateCode = "GATE_TD"
	// errCodeGateQuestBlock is 0x1C17: the quest operation bit 0x40000.
	errCodeGateQuestBlock byte = 0x17
	// errCodeDenJob is 0x1C18: a trader or a hunter may not enter the den.
	errCodeDenJob byte = 0x18
)

// gateTransportRange is how far a summoned transport may stand from its
// owner and still travel through a gate (the teleport use handler, case 2: the length of the
// relative position against 1000.0, else 0x1C13).
const gateTransportRange = 1000.0

/*
================
portalTraveller

What portalAdmission needs to know of the traveller beyond the character:
the quest travel mask, whether a transport is summoned, and whether it
stands beyond gateTransportRange.
================
*/
type portalTraveller struct {
	questMask uint32
	// intoDen is a link whose target is the Bandit Den (GATE_TD): the
	// reference server compares the link's target teleport, not its source.
	intoDen      bool
	transportCOS bool
	transportFar bool
	// transportAdmitted is the port-only SRO_JOB_GOODS_TELEPORT rule: a
	// town gate's Restrict kind 2 lets the transport through.
	transportAdmitted bool
}

// Reference server: the quest bit blocks buildings and the route into the
// Bandit Den; it does not turn ordinary ferries into town gates. The link
// check separately checks the authored transport-COS restriction on ferry
// links.
/*
================
portalAdmission

In the reference server's teleport use order (case 2): the quest bit at a
building, then a summoned transport beyond gateTransportRange (0x1C13: it
would be left behind), then the den rules when the link's TARGET is the
Bandit Den (the quest bit 0x1C17, then a trader or hunter 0x1C18: only a
thief or a jobless player enters), then the link's conditions, then the fee.
================
*/
func portalAdmission(c *enterworld.Character, source portalDestination, link portalLink, traveller portalTraveller) byte {
	if !enterworld.CharacterAlive(c) || c.DeletePending {
		return 2
	}
	if traveller.questMask&0x40000 != 0 && source.building {
		return errCodeGateQuestBlock
	}
	if traveller.transportCOS && traveller.transportFar {
		return 0x13
	}
	if traveller.intoDen {
		if traveller.questMask&0x40000 != 0 {
			return errCodeGateQuestBlock
		}
		if c.Job.Type == domain.JobTrader || c.Job.Type == domain.JobHunter {
			return errCodeDenJob
		}
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
		case portalConditionTransport:
			if traveller.transportCOS && !traveller.transportAdmitted {
				return 0x10
			}
		case portalConditionThiefSuitTransport:
			if traveller.transportCOS && enterworld.DressedJob(c) == uint8(domain.JobThief) {
				return 0x10
			}
		}
	}
	if link.fee > 0 && (c.Gold == nil || *c.Gold < link.fee) {
		return 7
	}
	return 0
}
