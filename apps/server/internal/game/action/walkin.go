/*
===========================================================================

walkin.go - walking into a gate's event zone teleports, as natively

Some gates are walked into rather than clicked: the Donwhang cave's mouth
and its way out. The client sends nothing; the server notices the player
entering the zone (movement.WaterValidator.WalkInZoneAt, the region
manager's event-zone callback in v1.188: CMap_Load 5304E0 registers
5301B0 -> 418110 -> the PC's vtable +0x4D4). The PC's handler,
4EF480 (labelled CGObjPC_HandleNpcTeleportCommand in the database), then
runs, in order:

 1. only a zone of type TELEPORT moves the PC;
 2. a PC still in teleport mode and standing (4DDC90, not 4EF380) is told
    its last teleport is still working (v1.150 0x3058 kind 3) and stays.
    Natively a PC a skill is displacing (PC+0xC04, set by
    SkillAction_ApplyPositionEffect 5862E0, cleared by 593540) skips this
    notice; the port commits a displacement in one step, never leaving a
    mover mid-push in a zone, so that case is left out;
 3. a riding PC (PC+0x1D18), or one with an active vehicle (4FD7F0, the
    summoned cargo transport the caravan code reads), is told a COS
    cannot enter (0x3058 kind 4) and stays;
 4. the zone's teleport must have exactly one link (a job server,
    +0x42404 == 1, exempts GATE_TEMPLE_THIEF_GATE; the temple is not in
    v1.150, so that case is trimmed);
 5. the link's conditions (CRefTeleportLink_CheckState 513A70: level,
    transport) refuse with the teleport answer's error byte;
 6. otherwise the PC teleports there (CGObjChar_SetTeleportModeAndSchedule
    Restore), free of charge and with no NPC in reach.

v1.188 sends 2 and 3's notice as 0x300C 0x0C03 / 0x0C04; v1.150's event
notice 0x3058 maps its kind to category 0x11, where 3 is
UIIT_MSG_JSERR_PREVIOUS_TELEPORT_WORKING and 4 UIIT_MSG_COS_CAN_NOT_ENTER_
DUNGEON (native-notice-data.ts 4355/4356).

Inference: natively the region manager reports a nav-cell entry along
the walked path; the port samples each player's live pose once per tick.
A zone is 30 x 30, so a straight walk or ride cannot step over one in a
tick, though a path that only clips a corner may.

===========================================================================
*/
package action

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// eventZoneTypeColumn and eventZoneTeleportColumn of eventzonedata:
	// the zone's kind and the teleport a TELEPORT zone names.
	eventZoneTypeColumn     = 3
	eventZoneTeleportColumn = 9
	eventZoneTypeTeleport   = "TELEPORT"
	// opEventNotice is v1.150's event notice (7508C0); a kind outside 5..10
	// shows category 0x11's notice of that code.
	opEventNotice                   uint16 = 0x3058
	walkInNoticeTeleportWorking     uint8  = 3
	walkInNoticeCOSCannotEnterGates uint8  = 4
)

/*
================
loadWalkIns

The walk-in gates: each TELEPORT event zone by codename, with the one link
of the teleport it names. A TELEPORT zone naming a missing teleport, or one
with other than one link, is a data error, as 4EF480 minidumps on it.
================
*/
func loadWalkIns(dir string, catalog *portalCatalog) (map[string]portalLink, error) {
	byCode := make(map[string]uint32, len(catalog.destinations))
	for id, destination := range catalog.destinations {
		byCode[destination.code] = id
	}
	walkIns := map[string]portalLink{}
	for _, row := range enterworld.ReadTextdataFile(filepath.Join(dir, "eventzonedata.txt")) {
		if len(row) <= eventZoneTeleportColumn || row[0] != "1" || strings.TrimSpace(row[eventZoneTypeColumn]) != eventZoneTypeTeleport {
			continue
		}
		zone, teleport := strings.TrimSpace(row[eventZoneCodenameColumn]), strings.TrimSpace(row[eventZoneTeleportColumn])
		source, ok := byCode[teleport]
		if !ok {
			return nil, fmt.Errorf("event zone %s names missing teleport %s", zone, teleport)
		}
		var links []portalLink
		for key, link := range catalog.links {
			if key[0] == source {
				links = append(links, link)
			}
		}
		if len(links) != 1 {
			return nil, fmt.Errorf("event zone %s teleport %s has %d links, want 1", zone, teleport, len(links))
		}
		// 4EF480 never charges: the walk-in carries no fee.
		link := links[0]
		link.fee = 0
		walkIns[zone] = link
	}
	return walkIns, nil
}

/*
================
walkInNotice
================
*/
func walkInNotice(kind uint8) OpResult {
	return OpResult{Frames: []wire.Frame{{Opcode: opEventNotice, Payload: []byte{kind}}}}
}

/*
================
advanceWalkIns

The tick's walk-in pass: each admitted player whose live position has
entered a walk-in zone since the last pass runs 4EF480. Entering is the
edge: standing on in a zone (or arriving into one) does nothing more,
as the region manager reports a cell entry, not a stay.
================
*/
func (rt *Runtime) advanceWalkIns(nowMs int64) {
	if rt.WalkInZoneAt == nil || rt.portals == nil || len(rt.portals.walkIns) == 0 {
		return
	}
	var keys []string
	admitted := map[string]bool{}
	rt.characterAdmissions.Range(func(key, _ any) bool {
		keys = append(keys, key.(string))
		admitted[key.(string)] = true
		return true
	})
	rt.walkInZones.Range(func(key, _ any) bool {
		if !admitted[key.(string)] {
			rt.walkInZones.Delete(key)
		}
		return true
	})
	sort.Strings(keys)
	for _, key := range keys {
		value, exists := rt.characterAdmissions.Load(key)
		if !exists {
			continue
		}
		owner := value.(populationAdmission)
		unlock := rt.lockDivision(owner.division)
		out := rt.stepWalkIn(key, owner, nowMs)
		unlock()
		if len(out.Frames) > 0 && rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(owner.division, owner.name, out.Frames)
		}
		if len(out.Broadcast) > 0 && rt.PushDivisionPeerFrames != nil {
			rt.PushDivisionPeerFrames(owner.division, owner.name, out.Broadcast)
		}
	}
}

/*
================
stepWalkIn

One player's walk-in check. The caller holds the division lock.
================
*/
func (rt *Runtime) stepWalkIn(key string, owner populationAdmission, nowMs int64) OpResult {
	lease, live := rt.CharacterPopulationLease(owner.division, owner.name, owner.session)
	if !live || lease != owner.lease {
		return OpResult{}
	}
	c := rt.findCharacter(owner.division, owner.name)
	if c == nil {
		return OpResult{}
	}
	var pose simulation.Spawn
	var inWorld, moving bool
	rt.deps.Read(owner.division, func() {
		inWorld = instance.ID(domain.CharacterWorldInstance(c)) == lease.ID
		if !inWorld {
			return
		}
		world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(c) })
		pose, moving = world.LiveSpawnAt(nowMs), world.MovingAt(nowMs)
	})
	if !inWorld {
		return OpResult{}
	}
	zone := rt.WalkInZoneAt(pose)
	if previous, _ := rt.walkInZones.Load(key); previous == zone {
		return OpResult{}
	}
	rt.walkInZones.Store(key, zone)
	link, ok := rt.portals.walkIns[zone]
	if !ok {
		return OpResult{}
	}
	out, arrived := rt.enterWalkIn(owner.division, c, link, moving)
	if arrived {
		// The arrival point may itself lie in a zone; it was not entered.
		rt.walkInZones.Store(key, rt.WalkInZoneAt(rt.portals.destinations[link.target].spawn))
	}
	return out
}

/*
================
enterWalkIn

4EF480 after the TELEPORT test, for a player that just entered the zone
of link's source. Reports whether the player was moved. The caller holds
the division lock.
================
*/
func (rt *Runtime) enterWalkIn(division string, c *enterworld.Character, link portalLink, moving bool) (OpResult, bool) {
	var alive, teleporting, riding bool
	rt.deps.Read(division, func() {
		alive = !c.DeletePending && enterworld.CharacterAlive(c)
		teleporting, riding = c.NativeTeleportMode != 0, mountedOnCOS(c) || rt.hasSummonedTransportCOS(c)
	})
	if !alive {
		return OpResult{}, false
	}
	if teleporting && !moving {
		return walkInNotice(walkInNoticeTeleportWorking), false
	}
	if riding {
		return walkInNotice(walkInNoticeCOSCannotEnterGates), false
	}
	source, arrival := rt.portals.destinations[link.source], rt.portals.destinations[link.target]
	if arrival.spawn.RegionID == 0 {
		return portalFailure(2), false
	}
	destinationWorld, refusal := rt.portalWorldAdmission(division, c, arrival)
	if refusal != 0 {
		return portalFailure(refusal), false
	}
	moved := false
	out := rt.commitGateTravel(gateTravel{division: division, character: c, destination: arrival.spawn, world: destinationWorld, reason: "walk-in-travel",
		settled: func() { moved = true }}, func() (int64, OpResult, bool) {
		traveller := portalTraveller{transportCOS: rt.hasSummonedTransportCOS(c)}
		if failure := portalAdmission(c, source, link, traveller); failure != 0 {
			return 0, portalFailure(failure), false
		}
		return 0, OpResult{}, true
	})
	return out, moved
}
