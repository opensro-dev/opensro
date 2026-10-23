/*
===========================================================================

qinshi_tomb.go - Qin-Shi Emperor Tomb Floor 1-6 monster refs and spawns

The v1.150 client ships the Qin-Shi Emperor Tomb world geometry, BSR/BMS
character assets and strings, while its textdata and server spawn tables omit
the 79 monster species and 1,042 Floor 1-6 nests introduced with the Legend IV
server tables. When SRO_QINSHI_TOMB is enabled (port-only, not native; off by
default), WithQinShiTomb appends the characterdata rows (QinShiTombTables) (remapped into
the v1.150 ID gaps 14791..14851 and 22638..22655) and Floor 1-6 nest/hive/tactics
rows onto the monster Template.

On Floors 5-6 (regions 0x8003 and 0x8002), the v1.188 Tab_RefNest_Back rows
23716..23721, 23723 and 23724 zeroed dwMaxTotalCount when CGameWorld_Chins_Floor5::InitTriggers
(0x005ef090) and CGameWorld_Chins_Floor6::InitTriggers (0x005ef9b0) took over
spawning 1 live instance ("Count" = "1") of each guardian/boss via MobShower.
In the persistent INS_DEFAULT world, those 8 unique nests restore maxCount=1
and the Floor 1-3 Tomb unique respawn window (1800..3600s from nests 23055..23061,
23077).

===========================================================================
*/

package monster

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"opensro.online/server/internal/gamedata"
)

// EnvQinShiTomb gates Qin-Shi Emperor Tomb Floor 1-4 monsters, skills and
// walk-in portals (port-only, not native; off by default).
const EnvQinShiTomb = "SRO_QINSHI_TOMB"

// QinShiTombDir is the tomb's folder in the projection's derived data
// (gamedata DerivedDir), from the operator's own source-data. The repository
// never carries these tables; without them the tomb stays sealed whatever
// SRO_QINSHI_TOMB says.
const QinShiTombDir = "qinshi-tomb"

/*
================
QinShiTombTables

The tomb's tables as the operator's source-data holds them: the monster
characterdata, Floor 1-6 nests, hives and tactics, the monster skills, and
the walk-in teleport data, links and event zones.
================
*/
type QinShiTombTables struct {
	CharacterData, Nests, Hives string
	Tactics                     []byte
	SkillData                   string
	TeleportData, TeleportLink  string
	EventZone                   string
}

// qinshiTomb is set once at startup by LoadQinShiTomb (tests:
// InstallQinShiTomb); nil keeps the tomb sealed.
var qinshiTomb *QinShiTombTables

/*
================
LoadQinShiTomb

All eight tables from derivedDir, or false when the folder or any table is
absent. Startup only, before any loader that asks QinShiTombEnabled.
================
*/
func LoadQinShiTomb(derivedDir string) (bool, error) {
	names := [8]string{"characterdata.tsv", "nests.tsv", "hives.tsv", "tactics.json",
		"skilldata.tsv", "teleportdata.tsv", "teleportlink.tsv", "eventzone.tsv"}
	var data [8][]byte
	for i, name := range names {
		bytes, err := gamedata.ReadDerived(derivedDir, QinShiTombDir+"/"+name)
		if err != nil {
			return false, err
		}
		if bytes == nil {
			return false, nil
		}
		data[i] = bytes
	}
	InstallQinShiTomb(&QinShiTombTables{
		CharacterData: string(data[0]), Nests: string(data[1]), Hives: string(data[2]), Tactics: data[3],
		SkillData: string(data[4]), TeleportData: string(data[5]), TeleportLink: string(data[6]), EventZone: string(data[7]),
	})
	return true, nil
}

/*
================
InstallQinShiTomb

Publishes the tables (nil seals the tomb). Startup and tests only.
================
*/
func InstallQinShiTomb(tables *QinShiTombTables) {
	qinshiTomb = tables
}

/*
================
QinShiTomb

The loaded tables, or nil.
================
*/
func QinShiTomb() *QinShiTombTables {
	return qinshiTomb
}

/*
================
QinShiTombEnabled

The tomb is open: SRO_QINSHI_TOMB is on and its tables are loaded.
================
*/
func QinShiTombEnabled() bool {
	return qinshiTomb != nil && QinShiTombFromEnv()
}

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

Parses the 79 Qin-Shi Emperor Tomb characterdata rows (78 monster
refs passing monsterTidGate plus 1 COS mount row MOB_TQ_HORSE01 registered
for ride resolution), joining localized display names and ride metadata from
textdataDir when available.
================
*/
func loadQinShiTombRefs(textdataDir string) map[uint32]MonsterRef {
	return parseMonsterRefTables(textdataDir, [][][]string{SplitTabbedText(qinshiTomb.CharacterData)})
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
	if qinshiTomb == nil {
		return Template{}, fmt.Errorf("qinshi tomb: tables not loaded (LoadQinShiTomb)")
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

	evidenceRows := mustLoadPopulationEvidence(qinshiTomb.Nests)
	hiveCaps := loadHiveCaps(qinshiTomb.Hives)
	tactics := loadTacticsControls(qinshiTomb.Tactics)

	nests := make([]NestRow, 0, len(t.Nests)+len(evidenceRows))
	nests = append(nests, t.Nests...)
	evidenceMatches := t.EvidenceMatches

	for lineNumber, line := range strings.Split(qinshiTomb.Nests, "\n") {
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
