/*
===========================================================================

portal.go - authoritative travel and scene re-entry

===========================================================================
*/
package action

import (
	_ "embed"
	"math"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/commerce"
	"os"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/pharaohtemple"
	"opensro.online/server/internal/game/world/simulation"
)

//go:embed data/qinshi_tomb_teleportdata.tsv
var qinshiTombTeleportDataTSV string

//go:embed data/qinshi_tomb_teleportlink.tsv
var qinshiTombTeleportLinkTSV string

// Port-only, not native (#3314): the Muhan arena gates' teleport rows
// and links, from the pinned v1.696 client (scripts/muhan_import.py),
// appended only while SRO_MUHAN is on.
//
//go:embed data/muhan_teleportdata.tsv
var muhanTeleportDataTSV string

//go:embed data/muhan_teleportlink.tsv
var muhanTeleportLinkTSV string

// Port-only, not native: the Jangan town gate's links to the Qin-Shi
// Emperor Tomb floors 1-4, appended only while SRO_JANGAN_TOMB_TELEPORT
// and SRO_QINSHI_TOMB are both on.
//
//go:embed data/jangan_tomb_teleportlink.tsv
var janganTombTeleportLinkTSV string

// EnvJanganTombTeleport turns the Jangan gate's tomb floor rows on.
const EnvJanganTombTeleport = "SRO_JANGAN_TOMB_TELEPORT"

/*
================
JanganTombTeleportEnabled

Port-only, not native: the Jangan town gate also offers the Qin-Shi
Emperor Tomb floors 1-4. The rows land on the tomb, so they follow
SRO_QINSHI_TOMB too: with the tomb closed the gate keeps its native
menu. Mirrored by scripts/build/shared/janganTombTeleport.mjs.
================
*/
func JanganTombTeleportEnabled() bool {
	if !monster.QinShiTombFromEnv() {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvJanganTombTeleport))) {
	case "1", "on", "true", "yes":
		return true
	}
	return false
}

// Port-only, not native: direct links between the five Silk Road town
// gates, appended only while SRO_CROSS_CITY_TELEPORT is on.
//
//go:embed data/cross_city_teleportlink.tsv
var crossCityTeleportLinkTSV string

// EnvCrossCityTeleport turns the direct town-to-town gate links on.
const EnvCrossCityTeleport = "SRO_CROSS_CITY_TELEPORT"

/*
================
CrossCityTeleportEnabled

Port-only, not native: every Silk Road town gate (Jangan, Donwhang,
Hotan, Samarkand, Constantinople) also leads straight to the other four,
for the gold the native chain of gates would cost. Mirrored by
scripts/build/shared/crossCityTeleport.mjs.
================
*/
func CrossCityTeleportEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvCrossCityTeleport))) {
	case "1", "on", "true", "yes":
		return true
	}
	return false
}

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
	// teleporters are the temple sanctums' gate buildings by codename
	// (temple_sanctum.go); absent while the temple is off or unstaged.
	teleporters map[string]*questGateBuilding
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
// id. Client 75B930 consumes [2,error-byte] refusals. The NPC source must still be
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
	// Outside its window the Sarin gate's schedule job has cleared its
	// link (tombgate.go): no link, the same refusal.
	if !ok || rt.tombGateShut(source.code) {
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
		if portalFareFree(c) {
			collected, link.fee = 0, 0
		}
		transport := rt.hasSummonedTransportCOS(c)
		traveller := portalTraveller{questMask: mask, intoDen: arrival.code == denGateCode, transportCOS: transport, transportFar: transport && rt.transportBeyond(division, c, gateTransportRange),
			transportAdmitted: rt.JobGoodsTeleport}
		if failure := portalAdmission(c, source, link, traveller); failure != 0 {
			return 0, portalFailure(failure), false
		}
		if c.NativeTeleportMode != 0 || rt.hasOpenSkillCast(division, c.Name) {
			return 0, portalFailure(0x14), false
		}
		if failure := portalSpendItems(c, link); failure != 0 {
			return 0, portalFailure(failure), false
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
	previousInventory := c.MissionInventory
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
				c.MissionInventory = previousInventory
				return true
			})
			return portalFailure(portalTransferError(status))
		}
		membership = previousMembership
	}
	rt.endTransformForLoading(division, c)
	rt.endPartyAurasForLoading(division, c)
	rt.endBerserkForLoading(division, c)
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
			c.MissionInventory = previousInventory
			return true
		})
		return portalFailure(2)
	}
	rt.noteTeleportArrival(division, c)
	rt.bindResidentRegion(key, rt.Now().UnixMilli())
	if rt.WalkInZoneAt != nil {
		rt.walkInZones.Store(key, walkInSample{zone: rt.walkInZoneAt(destination), pose: destination})
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
	return OpResult{Frames: append(append(append(missionReentryFrames(packets), corpses...), rt.petArrivalFrames(division, c)...), arrived...), Broadcast: append(corpseDespawns, wire.Frame{Opcode: wire.OpObjectSourceCorrection, Payload: wire.ObjectSourceCorrection{Gid: enterworld.ObjectIDForCharacter(c), Position: wire.Position{RegionID: destination.RegionID, X: float32(destination.X), Y: float32(destination.Y), Z: float32(destination.Z), Heading: destination.Angle}}.Encode()})}
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
	// errCodeTempleJob is the Pharaoh Temple's wrong-job refusal. The client's
	// teleport notices carry no text clearer for "wrong job" than the den's,
	// so it reuses 0x1C18 (errCodeDenJob).
	errCodeTempleJob = errCodeDenJob
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

// portalFareFreeMaxLevel is the highest level reached a character may
// have and still ride gates and ferries free.
const portalFareFreeMaxLevel = 20

/*
================
highestLevelReached

The character record's level watermark: the highest level the character
has ever reached, which the native level gates read. A record without
the watermark reads its current level.
================
*/
func highestLevelReached(c *enterworld.Character) int64 {
	if c.MaxLevel != nil {
		return *c.MaxLevel
	}
	if c.Level != nil {
		return *c.Level
	}
	return 0
}

/*
================
portalFareFree

Natively a character whose highest level reached is 20 or less pays no
gate or ferry fare (the same test stands before the gold check in the
gate body and in the NPC teleport handler).
================
*/
func portalFareFree(c *enterworld.Character) bool {
	return highestLevelReached(c) <= portalFareFreeMaxLevel
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
	// Port-only, not native-verified: the Pharaoh Temple's desert gates admit
	// only the job suit that fits them (pharaohtemple.GateJobGate). The check
	// runs before the link's conditions, as the den's job rule does.
	if gated, admitted := pharaohtemple.GateJobGate(source.code, enterworld.DressedJob(c)); gated && !admitted {
		return errCodeTempleJob
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
		case portalConditionHeldItem, portalConditionSpentItem, portalConditionNoJob:
			if failure := portalItemCondition(c, condition); failure != 0 {
				return failure
			}
		}
	}
	if link.fee > 0 && (c.Gold == nil || *c.Gold < link.fee) {
		return 7
	}
	return 0
}
