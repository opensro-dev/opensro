/*
===========================================================================

skillassist.go - which skills send the caster's attack pets in

The v1.150 client orders its attack pets onto its own cast's target
(CPSMission_OnSkillCastBroadcast0xB245 776830) only for a skill whose
motion record has byte +0xBD set. CSkillEffectManager_ParseSkillLightRecord
(client 91DAE0) fills that byte from skilleffect.txt's skillaniset column 24 at
client 91E6E1 (nonzero reads as set). In the shipped data it is set on the basic
attacks and the attack skills, and clear on buffs, shields and modes.

The browser client learns the flag through the skill catalogue
(SpawnSkillRow.AssistsCompanions). The record lookup goes through the
skill's Basic_Original once, ahead of any record of its own (917240), and
otherwise through its animation set's base name or its own codename, as the
client's effect records join them (scripts/build/char/parseSkillEffect.mjs).

===========================================================================
*/

package enterworld

import (
	"path/filepath"
	"strings"
)

const (
	// skilldataColAnimBase is the skill's skillaniset base name.
	skilldataColAnimBase = 5
	// skilldataColBasicOriginal is the skill its effect record is taken from.
	skilldataColBasicOriginal = 6
	// skillAniSetColName and skillAniSetColCompanionAssist are skillaniset
	// columns after a skillaniset2 row's leading service column.
	skillAniSetColName            = 1
	skillAniSetColCompanionAssist = 24
)

/*
================
skillAniSetAssists

Each skillaniset row's base name and its column-24 flag. A skillaniset2 row
leads with a service column and is skipped when that column is 0, as the
client's parser skips it.
================
*/
func skillAniSetAssists(textdataDir string) map[string]bool {
	out := map[string]bool{}
	section := ""
	for _, fields := range readTextdataFile(filepath.Join(textdataDir, "skilleffect.txt")) {
		if len(fields) == 0 {
			continue
		}
		if strings.HasPrefix(strings.ToLower(fields[0]), "#section") {
			words := strings.Fields(strings.ToLower(strings.Join(fields, " ")))
			section = ""
			if len(words) > 1 && (words[1] == "skillaniset" || words[1] == "skillaniset2") {
				section = words[1]
			}
			continue
		}
		if section == "" {
			continue
		}
		columns := fields
		if section == "skillaniset2" {
			if n, ok := textdataInt(fields[0]); !ok || n == 0 {
				continue
			}
			columns = fields[1:]
		}
		if len(columns) <= skillAniSetColName || columns[skillAniSetColName] == "" {
			continue
		}
		assist := false
		if len(columns) > skillAniSetColCompanionAssist {
			n, ok := textdataInt(columns[skillAniSetColCompanionAssist])
			assist = ok && n != 0
		}
		out[columns[skillAniSetColName]] = assist
	}
	return out
}

/*
================
skillRecordKey

The skillaniset row a skill's own record comes from: its animation set's
base name, else its codename (a monster or pet row authored by level name).
Empty when neither names a row.
================
*/
func skillRecordKey(animBase, codename string, sets map[string]bool) string {
	if _, ok := sets[animBase]; ok && animBase != "" {
		return animBase
	}
	if _, ok := sets[codename]; ok {
		return codename
	}
	return ""
}

/*
================
applySkillAssists

Sets each row's AssistsCompanions from its record: the Basic_Original's
when that skill has a record (917240 follows it once, ahead of the row's
own), else the row's own.
================
*/
func (t *TextdataSkills) applySkillAssists(keys map[uint32]skillRecordSource, sets map[string]bool) {
	for id, row := range t.rows.values() {
		source := keys[id]
		key := source.key
		if original, ok := keys[source.original]; ok && source.original != 0 && source.original != id && original.key != "" {
			key = original.key
		}
		if key == "" || !sets[key] {
			continue
		}
		row.AssistsCompanions = true
		t.rows.set(id, row)
	}
}

/*
================
skillRecordSource

One skill row's own record key and its Basic_Original, gathered while the
rows parse.
================
*/
type skillRecordSource struct {
	key      string
	original uint32
}
