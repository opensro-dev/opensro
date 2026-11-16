/*
===========================================================================

muhan.go - optional Muhan wave table (port-only, not native).

SRO_MUHAN is off by default. The import (scripts/muhan_import.py)
writes no nests for these worlds - the source authors none - only
the wave table (muhan-waves.txt): twenty waves per arena naming a
point, a delay, a count, a level offset, a radius and a monster
type, never a kind. The server's wave director
(simulation/muhan_chain.go) resolves each wave to the MOB_MUHAN_*
kind at its world's band level plus the offset when it installs,
against this table and the template the character shard joined.
Off never reads the wave file; the native game has no Muhan, so off
is the native state.

===========================================================================
*/
package monster

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"opensro.online/server/internal/game/world/muhan"
)

const EnvMuhan = "SRO_MUHAN"
const muhanWavesFile = "muhan-waves.txt"
const muhanNPCsFile = "muhan-npcs.txt"
const muhanTransformsFile = "muhan-transforms.txt"
const muhanNPCPrefix = "NPC_MUHAN_"
const muhanChangeNPCPrefix = "NPC_MUHAN_CHANGE_"
const muhanChangeFormPrefix = "MOB_MUHAN_CHANGE_"
const muhanSkillPrefix = "SKILL_MUHAN_"

/*
================
MuhanEnabled
================
*/
func MuhanEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvMuhan))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
MuhanWave

One authored wave row, as the import writes it (Version 2). The
kind is resolved at install: the kind at the world band (the
muhan leaf's World.Band) plus LevelOffset. MonsterType rides the
spawn as its rarity override.
================
*/
type MuhanWave struct {
	WorldID     int     `json:"WorldID"`
	Seq         int     `json:"Seq"`
	RegionID    int     `json:"RegionID"`
	X           float64 `json:"X"`
	Y           float64 `json:"Y"`
	Z           float64 `json:"Z"`
	DelayMs     int64   `json:"DelayMs"`
	Count       int     `json:"Count"`
	LevelOffset int     `json:"LevelOffset"`
	Radius      int     `json:"Radius"`
	MonsterType int     `json:"MonsterType"`
}

// muhanWavesDocument is the Version-2 envelope of muhan-waves.txt.
type muhanWavesDocument struct {
	Version int         `json:"Version"`
	Rows    []MuhanWave `json:"Rows"`
}

/*
================
AppendMuhanNPCs

Off never reads the optional file. On fails before startup when the
arena NPC data is absent or malformed; the managers and change NPCs
are how anyone reaches an arena, so a partial set must not reach
gameplay. Registering the wave worlds with the world catalogue is
the boot composition's job (enterworld), beside this call.
================
*/
func AppendMuhanNPCs(template Template, textdataDir string, enabled bool) (Template, error) {
	if !enabled {
		return template, nil
	}
	data, err := os.ReadFile(filepath.Join(textdataDir, muhanNPCsFile))
	if err != nil {
		return Template{}, fmt.Errorf("muhan npc population: %w", err)
	}
	return appendMuhanNPCData(template, data)
}

/*
================
appendMuhanNPCData
================
*/
func appendMuhanNPCData(template Template, data []byte) (Template, error) {
	return appendNests(template, data, "muhan npc population", admitMuhanNPC, nil, muhanNPCWorldCode)
}

/*
================
admitMuhanNPC

Port-only, not native: the arena NPCs alone (NPC_MUHAN_*). A wave
kind or transform form in the NPC file is refused here, as imports.
================
*/
func admitMuhanNPC(ref MonsterRef, codename string) bool {
	return strings.HasPrefix(codename, muhanNPCPrefix)
}

/*
================
muhanNPCWorldCode

The wave world a Muhan NPC nest joins, from the source world its
row carries. A row naming any other world is refused: the NPC
population must not leak onto another world.
================
*/
func muhanNPCWorldCode(row cap110Nest) (string, error) {
	code, ok := muhan.CodeName(row.WorldID)
	if !ok {
		return "", fmt.Errorf("muhan npc population has nest %d in unknown world %d", row.NestID, row.WorldID)
	}
	return code, nil
}

/*
================
LoadMuhanWaves

Off never reads the optional file. On fails before startup when the
wave data is absent or malformed; a partial arena set must not
reach gameplay. Registering the wave worlds with the world
catalogue, and resolving the rows against the template, are the
boot composition's and the director's jobs.
================
*/
func LoadMuhanWaves(textdataDir string, enabled bool) ([]MuhanWave, error) {
	if !enabled {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(textdataDir, muhanWavesFile))
	if err != nil {
		return nil, fmt.Errorf("muhan waves: %w", err)
	}
	return ParseMuhanWaves(data)
}

/*
================
ParseMuhanWaves

The wave table's own contract: a Version-2 envelope with rows.
World, sequence and kind resolution are the director's install
checks (simulation.InstallMuhanChain), which can name the template
rows involved.
================
*/
func ParseMuhanWaves(data []byte) ([]MuhanWave, error) {
	var document muhanWavesDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("muhan waves: %w", err)
	}
	if document.Version != 2 {
		return nil, fmt.Errorf("muhan waves: version %d, want 2", document.Version)
	}
	if len(document.Rows) == 0 {
		return nil, fmt.Errorf("muhan waves: no rows")
	}
	return document.Rows, nil
}

/*
================
MuhanTransform

One change pairing, as the import writes it (Version 2, from the
later server's _RefMuhanMonsterTransMask): the change NPC whose Talk
casts SkillCodename, a BUFF01 whose msch word 7 becomes the monster
form MonsterCodename. Port-only, not native.
================
*/
type MuhanTransform struct {
	NpcCodename     string `json:"NpcCodename"`
	SkillCodename   string `json:"SkillCodename"`
	MonsterCodename string `json:"MonsterCodename"`
}

// muhanTransformsDocument is the Version-2 envelope of muhan-transforms.txt.
type muhanTransformsDocument struct {
	Version int              `json:"Version"`
	Rows    []MuhanTransform `json:"Rows"`
}

/*
================
LoadMuhanTransforms

Off never reads the optional file. On fails before startup when the
pairing is absent or malformed: a change NPC with no pairing would
answer a Talk with nothing, so a partial set must not reach gameplay.
Resolving the skill and the form against the loaded tables is the
action runtime's install check.
================
*/
func LoadMuhanTransforms(textdataDir string, enabled bool) ([]MuhanTransform, error) {
	if !enabled {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(textdataDir, muhanTransformsFile))
	if err != nil {
		return nil, fmt.Errorf("muhan transforms: %w", err)
	}
	return ParseMuhanTransforms(data)
}

/*
================
ParseMuhanTransforms

The pairing's own contract: a Version-2 envelope with rows, each a
change NPC (once), a Muhan skill and a change form. The import refuses
the same defects; the server refuses them again rather than trust a
hand-edited file.
================
*/
func ParseMuhanTransforms(data []byte) ([]MuhanTransform, error) {
	var document muhanTransformsDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("muhan transforms: %w", err)
	}
	if document.Version != 2 {
		return nil, fmt.Errorf("muhan transforms: version %d, want 2", document.Version)
	}
	if len(document.Rows) == 0 {
		return nil, fmt.Errorf("muhan transforms: no rows")
	}
	seen := make(map[string]bool, len(document.Rows))
	for _, row := range document.Rows {
		if !strings.HasPrefix(row.NpcCodename, muhanChangeNPCPrefix) || !strings.HasPrefix(row.SkillCodename, muhanSkillPrefix) ||
			!strings.HasPrefix(row.MonsterCodename, muhanChangeFormPrefix) {
			return nil, fmt.Errorf("muhan transforms: row %+v names a non-muhan npc, skill or form", row)
		}
		if seen[row.NpcCodename] {
			return nil, fmt.Errorf("muhan transforms: npc %s is paired twice", row.NpcCodename)
		}
		seen[row.NpcCodename] = true
	}
	return document.Rows, nil
}
