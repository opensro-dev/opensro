/*
===========================================================================

fortress_structures.go - the structures a fortress world starts with

A fortress structure stands on an event zone: eventzonedata.txt names each
zone (STRUCTURE_POS_JA_GATE_01), its kind, its default structure codename
and its world; navmesh\objectstring.ifo places it (the server bundle's
world-authority/structure-zones.json). The retail shard keeps installed
structures in _SiegeFortressStruct and installs new ones through
_SiegeFortressStructInsert; nothing in SR_GameServer creates a fortress's
first structures, which a new shard's database holds.

INFERENCE: a fresh shard's fortress holds, on every placed zone of kinds 1
(fort stone, guard towers) and 3 (gates), that zone's default structure.
Kind 2 zones (barricades) stay empty until a barricade is placed. A default
whose record has no hit points (the _00 defensive sites) is an unbuilt site,
not a live object. Each structure is one ordinary population nest of its
fortress world: one instance, no respawn, never roaming.

===========================================================================
*/
package enterworld

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"opensro.online/server/internal/game/world/monster"
)

const (
	// eventzonedata kinds that start built (1) and gates (3).
	eventZoneBuilt = "1"
	eventZoneGate  = "3"
)

/*
================
structureZone

One placed event zone of world-authority/structure-zones.json.
================
*/
type structureZone struct {
	Name     string  `json:"name"`
	RegionID uint16  `json:"regionId"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Z        float64 `json:"z"`
}

/*
================
appendFortressStructures

Adds each fortress world's starting structures to the population template.
An empty zones path leaves the template alone (fixtures); a named file that
is missing is a data-contract error.
================
*/
func appendFortressStructures(template monster.Template, textdataDir, zonesPath string) (monster.Template, error) {
	if zonesPath == "" {
		return template, nil
	}
	data, err := os.ReadFile(zonesPath)
	if err != nil {
		return monster.Template{}, fmt.Errorf("fortress structures: %w (rebuild the server game data)", err)
	}
	var file struct {
		Format string          `json:"format"`
		Zones  []structureZone `json:"zones"`
	}
	if err := json.Unmarshal(data, &file); err != nil || file.Format != "sro-server-structure-zones" {
		return monster.Template{}, fmt.Errorf("fortress structures: %s is not a structure-zone projection", zonesPath)
	}
	placed := make(map[string]structureZone, len(file.Zones))
	for _, zone := range file.Zones {
		placed[zone.Name] = zone
	}
	refs := make(map[string]monster.MonsterRef)
	for _, ref := range template.Refs {
		if ref.Structure {
			refs[ref.Codename] = ref
		}
	}
	var nests []monster.NestRow
	for _, row := range ReadTextdataFile(filepath.Join(textdataDir, "eventzonedata.txt")) {
		if len(row) < 11 || row[0] != "1" || (row[4] != eventZoneBuilt && row[4] != eventZoneGate) {
			continue
		}
		world := strings.TrimSpace(row[10])
		if !strings.HasPrefix(world, "INS_FORT_") {
			continue
		}
		zone, ok := placed[strings.TrimSpace(row[2])]
		if !ok {
			continue
		}
		ref, ok := refs[strings.TrimSpace(row[9])]
		if !ok || ref.MaxHP == 0 {
			continue
		}
		id, err := strconv.ParseUint(row[1], 10, 32)
		if err != nil || id == 0 {
			return monster.Template{}, fmt.Errorf("fortress structures: event zone %q has no id", row[2])
		}
		nests = append(nests, monster.NestRow{
			WorldCode:     world,
			SpawnPoint:    monster.SpawnPoint{RefObjID: ref.RefObjID, RegionID: zone.RegionID, X: zone.X, Y: zone.Y, Z: zone.Z},
			PolicyPinned:  true,
			MaxCount:      1,
			EventStructID: uint32(id),
		})
	}
	return template.WithAdditionalNests(nests), nil
}
