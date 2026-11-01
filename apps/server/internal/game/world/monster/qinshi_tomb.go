/*
===========================================================================

qinshi_tomb.go - Qin-Shi Emperor Tomb Floor 1-6 monster refs and spawns

The v1.150 client ships the Qin-Shi Emperor Tomb world geometry, BSR/BMS
character assets and strings, while its textdata and server spawn tables omit
the 79 monster species and 1,042 Floor 1-6 nests introduced with the Legend IV
server tables. When SRO_QINSHI_TOMB is enabled (port-only, not native; off by
default), WithQinShiTomb appends the embedded characterdata rows (remapped into
the v1.150 ID gaps 14791..14851 and 22638..22655) and Floor 1-6 nest/hive/tactics
rows onto the monster Template.

On Floors 5-6 (regions 0x8003 and 0x8002), the later server's nest rows
23716..23721, 23723 and 23724 zeroed their max-count column when the
floors' trigger setup took over spawning 1 live instance ("Count" = "1")
of each guardian/boss via MobShower.
In the persistent INS_DEFAULT world, those 8 unique nests restore maxCount=1
and the Floor 1-3 Tomb unique respawn window (1800..3600s from nests 23055..23061,
23077).

===========================================================================
*/

package monster

import (
	_ "embed"
	"fmt"
	"os"
	"slices"
	"strings"
)

// EnvQinShiTomb gates Qin-Shi Emperor Tomb Floor 1-4 monsters, skills and
// walk-in portals (port-only, not native; off by default).
const EnvQinShiTomb = "SRO_QINSHI_TOMB"

//go:embed data/qinshi_tomb_characterdata.tsv
var qinshiTombCharacterDataTSV string

//go:embed data/qinshi_tomb_nests.tsv
var qinshiTombNestsTSV string

//go:embed data/qinshi_tomb_hives.tsv
var qinshiTombHivesTSV string

//go:embed data/qinshi_tomb_tactics.json
var qinshiTombTacticsJSON []byte

/*
================
QinShiTombFromEnv

Reports whether SRO_QINSHI_TOMB enables the Qin-Shi Emperor Tomb Floor 1-4
data tables (port-only, not native; off by default).
================
*/
func QinShiTombFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvQinShiTomb))) {
	case "1", "on", "true", "yes":
		return true
	default:
		return false
	}
}

/*
================
SplitTabbedText

Splits tab-separated textdata into rows, skipping blank, // and # comment
lines.
================
*/
func SplitTabbedText(text string) [][]string {
	rows := splitTabbedText(text)
	filtered := rows[:0]
	for _, row := range rows {
		if len(row) > 0 && strings.HasPrefix(strings.TrimSpace(row[0]), "#") {
			continue
		}
		filtered = append(filtered, row)
	}
	return filtered
}

/*
================
loadQinShiTombRefs

Parses the 79 embedded Qin-Shi Emperor Tomb characterdata rows (78 monster
refs passing monsterTidGate plus 1 COS mount row MOB_TQ_HORSE01 registered
for ride resolution), joining localized display names and ride metadata from
textdataDir when available.
================
*/
func loadQinShiTombRefs(textdataDir string) map[uint32]MonsterRef {
	return parseMonsterRefTables(textdataDir, [][][]string{SplitTabbedText(qinshiTombCharacterDataTSV)})
}

/*
================
WithQinShiTomb

Returns a copy of the Template with the 78 Qin-Shi Emperor Tomb monster
references and 1,033 Floor 1-4 nests appended (port-only, not native).
================
*/
func (t Template) WithQinShiTomb(textdataDir string) (Template, error) {
	if len(t.Refs) == 0 && len(t.Nests) == 0 {
		return t, nil
	}
	tombRefs := loadQinShiTombRefs(textdataDir)
	if len(tombRefs) == 0 {
		return Template{}, fmt.Errorf("load qinshi tomb characterdata: parsed 0 refs")
	}

	refs := make(map[uint32]MonsterRef, len(t.Refs)+len(tombRefs))
	byCode := make(map[string]MonsterRef, len(t.Refs)+len(tombRefs))
	for id, ref := range t.Refs {
		refs[id] = ref
		byCode[ref.Codename] = ref
	}
	for id, ref := range tombRefs {
		if existing, exists := refs[id]; exists {
			return Template{}, fmt.Errorf("qinshi tomb monster ID %d (%s) collides with %s", id, ref.Codename, existing.Codename)
		}
		if existing, exists := byCode[ref.Codename]; exists {
			return Template{}, fmt.Errorf("qinshi tomb monster codename %s (%d) collides with %d", ref.Codename, id, existing.RefObjID)
		}
		refs[id] = ref
		byCode[ref.Codename] = ref
	}

	evidenceRows := mustLoadPopulationEvidence(qinshiTombNestsTSV)
	hiveCaps := loadHiveCaps(qinshiTombHivesTSV)
	tactics := loadTacticsControls(qinshiTombTacticsJSON)

	nests := make([]NestRow, 0, len(t.Nests)+len(evidenceRows))
	nests = append(nests, t.Nests...)
	evidenceMatches := t.EvidenceMatches

	for lineNumber, line := range strings.Split(qinshiTombNestsTSV, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		codename := cols[0]
		ref, ok := byCode[codename]
		if !ok {
			return Template{}, fmt.Errorf("qinshi tomb nest line %d codename %s not found in characterdata", lineNumber+1, codename)
		}
		regionID := uint16(mustEvidenceUint(cols[1], 16, lineNumber))
		x := mustEvidenceFloat(cols[2], lineNumber)
		y := mustEvidenceFloat(cols[3], lineNumber)
		z := mustEvidenceFloat(cols[4], lineNumber)
		key := evidenceKey(codename, regionID, x, y, z)
		evidence, ok := evidenceRows[key]
		if !ok {
			return Template{}, fmt.Errorf("qinshi tomb nest line %d missing evidence row", lineNumber+1)
		}
		controls, ok := tactics[key]
		if !ok {
			return Template{}, fmt.Errorf("qinshi tomb nest line %d (%s) missing tactics controls", lineNumber+1, codename)
		}
		nest := NestRow{
			SpawnPoint: SpawnPoint{
				RefObjID: ref.RefObjID,
				RegionID: regionID,
				X:        x,
				Y:        y,
				Z:        z,
			},
			WorldCode:             "INS_DEFAULT",
			RetailEvidence:        true,
			PolicyPinned:          true,
			Radius:                evidence.Radius,
			GenerateRadius:        evidence.GenerateRadius,
			ChampionGenPercentage: evidence.ChampionGenPercentage,
			MaxCount:              evidence.MaxCount,
			RespawnDelayMinSec:    evidence.RespawnDelayMinSec,
			RespawnDelayMaxSec:    evidence.RespawnDelayMaxSec,
			Respawn:               evidence.Respawn,
			Aggressive:            evidence.Aggressive,
			TargetPolicy:          evidence.TargetPolicy,
			SightRange:            evidence.SightRange,
			NativeTacticsFlags:    evidence.NativeTacticsFlags,
			HasChampionTactics:    evidence.HasChampion,
			ChampionTactics:       evidence.Champion,
			InitialDir:            evidence.InitialDir,
			Controls:              controls.Normal,
			HasControls:           true,
		}
		if nest.HasChampionTactics && controls.HasChampion {
			nest.ChampionTactics.Controls = controls.Champion
			nest.ChampionTactics.HasControls = true
		}
		if capRow, ok := hiveCaps[key]; ok {
			nest.HiveKey = capRow.Key
			nest.HiveMaxCount = capRow.Limit
			nest.HiveOrder = capRow.Order
			nest.HiveDensity = capRow.Density
			nest.WorldCode = capRow.WorldCode
		}
		nests = append(nests, nest)
		evidenceMatches++
	}

	out := TemplateFromParts(refs, nests)
	out.SummonRefs = slices.Clone(t.SummonRefs)
	out.EvidenceMatches = evidenceMatches
	return out, nil
}
