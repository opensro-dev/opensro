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
)

const EnvMuhan = "SRO_MUHAN"
const muhanWavesFile = "muhan-waves.txt"

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
