/*
===========================================================================

fortress_upgrades.go - the siege structure upgrade table the bootstrap
publishes

The 0xB1E1 upgrade answer (client 754A40 case 0xB) names the standing
structure's resolved upgrade variant for a level-0 defensive position;
the browser judges that reply from this table, resolved here from the
same siegestructupgradedata.txt rows the action lane answers the
upgrade from (fortress_upgrade.go).

===========================================================================
*/
package enterworld

import (
	"path/filepath"
	"sort"
	"strings"
)

// structureUpgradeFile is the client's siege structure upgrade table.
const structureUpgradeFile = "siegestructupgradedata.txt"

// structureUpgradeTargetColumn is the first target's column; the columns
// before it are Service, the structure's codename and its level-0 base.
const structureUpgradeTargetColumn = 3

/*
================
SiegeStructUpgradeRow

One structure reference's upgrade targets in table order, as reference
ids. A target codename with no reference stays 0, the native loader's
top rank (buildFortressMapAsset.mjs resolves the same rows that way).
================
*/
type SiegeStructUpgradeRow struct {
	Ref     uint32   `json:"ref"`
	Targets []uint32 `json:"targets"`
}

/*
================
siegeStructureUpgradeRows

The enabled rows of siegestructupgradedata.txt resolved to reference
ids: each structure's targets until the first "xxx". A missing table
leaves the bootstrap without rows; the action lane already refuses to
boot on the same file, so production never reaches entry without it.
================
*/
func siegeStructureUpgradeRows(textdataDir string, characters CharacterRefSource) []SiegeStructUpgradeRow {
	if textdataDir == "" || characters == nil {
		return nil
	}
	byCodename := map[string][]uint32{}
	for _, row := range ReadTextdataFile(filepath.Join(textdataDir, structureUpgradeFile)) {
		if len(row) <= structureUpgradeTargetColumn || row[0] != "1" {
			continue
		}
		codename := strings.TrimSpace(row[1])
		if codename == "" {
			continue
		}
		var targets []uint32
		for _, cell := range row[structureUpgradeTargetColumn:] {
			code := strings.TrimSpace(cell)
			if code == "" || code == "xxx" {
				break
			}
			target := uint32(0)
			if ref, found := characters.CharacterRefByCodename(code); found && ref != nil {
				target = ref.RefObjID
			}
			targets = append(targets, target)
		}
		byCodename[codename] = targets
	}
	rows := make([]SiegeStructUpgradeRow, 0, len(byCodename))
	for codename, targets := range byCodename {
		ref, found := characters.CharacterRefByCodename(codename)
		if !found || ref == nil || len(targets) == 0 {
			continue
		}
		rows = append(rows, SiegeStructUpgradeRow{Ref: ref.RefObjID, Targets: targets})
	}
	if len(rows) == 0 {
		return nil
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Ref < rows[j].Ref })
	return rows
}
