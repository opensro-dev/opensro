/*
===========================================================================

portal_catalog.go - the portal catalogue: the teleport tables, loaded once

What this file owns: reading teleportbuilding, teleportdata and
teleportlink (with the event-zone and walk-in tables) into one
immutable catalogue, validating every row against the registered
worlds as it loads, and the shared cache that lets every runtime
over the same files and switches reuse one parse. What decides a
click and commits the travel stays in portal.go; the Alexandria
overlay and the selected-NPC destination shapes stay in their own
files.

===========================================================================
*/
package action

import (
	"fmt"
	"math"
	"opensro.online/server/internal/data/texttable"
	"path/filepath"
	"strconv"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/alexandria"
	"opensro.online/server/internal/game/world/arabia"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/pharaohtemple"
	"opensro.online/server/internal/game/world/simulation"
)

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
	if monster.MuhanEnabled() {
		key += "+muhan"
	}
	if on, _ := alexandria.EnabledFromEnv(); on {
		key += "+alexandria"
	}
	if pharaohtemple.Enabled() {
		key += "+pharaoh_temple"
	}
	if monster.SkytempleEnabled() {
		key += "+skytemple"
	}
	if monster.BaghdadDungeonEnabled() {
		key += "+baghdad_dungeon"
	}
	if JanganTombTeleportEnabled() {
		key += "+jangan_tomb_teleport"
	}
	if CrossCityTeleportEnabled() {
		key += "+cross_city_teleport"
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
	hiddenCodes := map[string]bool{}
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
		if len(r) > 2 && (hidden.HidesBuilding(r[2]) || pharaohtemple.HidesBuilding(r[2]) ||
			monster.SkytempleHidesBuilding(r[2])) {
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
		if len(r) > 2 && (r[2] == pharaohtemple.TempleTeleporter || r[2] == pharaohtemple.SethTeleporter) {
			gate, e := questGateBuildingRow(r)
			if e != nil {
				return nil, e
			}
			if c.teleporters == nil {
				c.teleporters = map[string]*questGateBuilding{}
			}
			c.teleporters[r[2]] = gate
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
	if monster.MuhanEnabled() {
		// Port-only, not native (#3314): append the arena manager hub and
		// the eight Muhan gate rows (IDs 227..235) from the v1.696 client.
		rows = append(rows, monster.SplitTabbedText(muhanTeleportDataTSV)...)
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
		// Port-only, not native: the Sky Temple fields stay closed, with every
		// link to them, while SRO_SKYTEMPLE is off.
		if hidden.HidesTeleport(r[2]) || monster.SkytempleHidesGate(r[2]) {
			hiddenIDs[id] = true
			hiddenCodes[r[2]] = true
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
		// Port-only, not native: the Pharaoh Temple's gates exist only while
		// SRO_PHARAOH_TEMPLE is on; off, they and every link to them are
		// skipped, as the Alexandria overlay's are.
		if pharaohtemple.Hides(r[2], uint16(region)) {
			hiddenIDs[id] = true
			hiddenCodes[r[2]] = true
			continue
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
		// Port-only, not native: the Baghdad dungeon's gates exist only
		// while SRO_BAGHDAD_DUNGEON is on (#4134); off, they and every
		// link to them are skipped, as the Pharaoh Temple's are. The
		// hideout's field entry names the field's world, as the source
		// authored it, so it hides by its code beside them.
		if !monster.BaghdadDungeonEnabled() && (arabia.IsDungeonWorld(int(world)) || arabia.HidesGateCode(r[2])) {
			hiddenIDs[id] = true
			hiddenCodes[r[2]] = true
			continue
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
	if monster.MuhanEnabled() {
		// Port-only, not native (#3314): append the 16 Muhan links (hub to
		// gates, gates back to the town gate), fee 0 and condition-free.
		rows = append(rows, monster.SplitTabbedText(muhanTeleportLinkTSV)...)
	}
	if JanganTombTeleportEnabled() {
		// Port-only, not native: the Jangan gate's tomb floor links.
		rows = append(rows, monster.SplitTabbedText(janganTombTeleportLinkTSV)...)
	}
	if CrossCityTeleportEnabled() {
		// Port-only, not native: the direct town-to-town gate links.
		rows = append(rows, monster.SplitTabbedText(crossCityTeleportLinkTSV)...)
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
			case portalConditionHeldItem, portalConditionSpentItem:
				if !portalItemConditionValid(a, b) {
					return nil, fmt.Errorf("teleportlink row %d unsupported condition %d", i+1, k)
				}
			case portalConditionNoJob:
				if !portalNoJobConditionValid(a, b) {
					return nil, fmt.Errorf("teleportlink row %d unsupported condition %d", i+1, k)
				}
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
	walkIns, err := loadWalkIns(dir, c, hiddenCodes)
	if err != nil {
		return nil, err
	}
	c.walkIns = walkIns
	return c, nil
}
