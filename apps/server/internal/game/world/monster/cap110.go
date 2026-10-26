/*
===========================================================================
cap110.go - optional Alexandria field nests from the verified projection.

Port-only, not native. SRO_CAP110_MONSTERS is off by default. The compiler
joins database identities by codename; runtime resolves the installed refs.
The nest validation is shared with the Pharaoh Tomb population
(pharaoh_tomb.go): both read the same row schema and differ only in which
monster references they admit. SRO_CAP110_LEVEL100 (also off by default)
separately admits the level-100 slice of the field population (#1741).
===========================================================================
*/
package monster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"log"
	"path/filepath"
	"strings"

	"opensro.online/server/internal/game/world/alexandria"
)

const EnvCap110Monsters = "SRO_CAP110_MONSTERS"
const EnvCap110Level100 = "SRO_CAP110_LEVEL100"
const cap110PopulationFile = "cap110-monsters.txt"
const cap110PopulationVersion = 1

// Port-only, not native: the optional expansion's monster level window.
const (
	cap110FirstLevel = 100
	cap110LastLevel  = 110
)

/*
================
Cap110Enabled
================
*/
func Cap110Enabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvCap110Monsters))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
Cap110Level100Enabled

Port-only, not native (#1741, the owner-decided P3-A): admits the
level-100 field monsters to hunting. Off, their nests are held back
at population load; levels 101-110 follow SRO_CAP110_MONSTERS alone.
Ships off; it switches on with the cap-110 catalogue (#1607).
================
*/
func Cap110Level100Enabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvCap110Level100))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
cap110Nest
================
*/
type cap110Nest struct {
	Codename                                         string
	NestID                                           uint32
	RegionID                                         uint16
	X, Y, Z, Radius, GenerateRadius                  float64
	MaxCount, RespawnDelayMinSec, RespawnDelayMaxSec int
	Respawn                                          bool
	InitialDir                                       uint16
	ChampionGenPercentage                            int
	Controls                                         TacticsControls
	Champion                                         *TacticsControls
}

/*
================
AppendCap110

Off never reads the optional file. On fails before startup when its data is
absent or malformed; a partial field population must not reach gameplay.
The nests stand in Alexandria's regions, which the server hides unless
SRO_ALEXANDRIA is on (world/alexandria), so on also requires that switch.
================
*/
func AppendCap110(template Template, textdataDir string, enabled bool) (Template, error) {
	if !enabled {
		return template, nil
	}
	if city, err := alexandria.EnabledFromEnv(); err != nil || !city {
		return Template{}, fmt.Errorf("%s needs %s on: its nests stand in the Alexandria regions", EnvCap110Monsters, alexandria.EnvAlexandria)
	}
	data, err := os.ReadFile(filepath.Join(textdataDir, cap110PopulationFile))
	if err != nil {
		return Template{}, fmt.Errorf("cap110 population: %w", err)
	}
	return appendCap110Data(template, data)
}

/*
================
appendCap110Data
================
*/
func appendCap110Data(template Template, data []byte) (Template, error) {
	return appendNests(template, data, "cap110 population", admitCap110Field, skipCap110Level100)
}

/*
================
skipCap110Level100

Port-only, not native (#1741): a level-100 field nest is held back
while SRO_CAP110_LEVEL100 is off, so the admission can switch on with
the cap-110 catalogue (#1607) instead of riding the population switch.
The row is still validated like every other; only its joining waits.
================
*/
func skipCap110Level100(ref MonsterRef, _ string) bool {
	return ref.Level == cap110FirstLevel && !Cap110Level100Enabled()
}

/*
================
admitCap110Field

Port-only, not native: Apis retains source grade 8; other later special
monsters remain outside the optional outdoor population.
================
*/
func admitCap110Field(ref MonsterRef, codename string) bool {
	const apisSourceGrade = 8
	apis := codename == "MOB_SD_APIS" && ref.MonsterType == apisSourceGrade
	return strings.HasPrefix(codename, "MOB_SD_") && ref.Level >= cap110FirstLevel && ref.Level <= cap110LastLevel &&
		(ref.MonsterType == 0 || apis)
}

/*
================
appendNests

Validates one optional population file and appends its nests onto a copy of
the template. label names the population in errors; admit says which installed
monster references the population may spawn. Every row must pass: a partial
population must not reach gameplay. A non-nil skip holds back admitted rows
its own flag has not admitted yet; skipped rows are validated like the rest
and counted in the log, never silently dropped.
================
*/
func appendNests(template Template, data []byte, label string, admit func(MonsterRef, string) bool, skip func(MonsterRef, string) bool) (Template, error) {
	var source struct {
		Version int
		Rows    []cap110Nest
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&source); err != nil {
		return Template{}, fmt.Errorf("%s: %w", label, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Template{}, fmt.Errorf("%s has trailing data", label)
	}
	if source.Version != cap110PopulationVersion || len(source.Rows) == 0 {
		return Template{}, fmt.Errorf("%s has invalid version or no nests", label)
	}
	refs := make(map[string]MonsterRef, len(template.Refs))
	for _, ref := range template.Refs {
		refs[ref.Codename] = ref
	}
	nests := append([]NestRow(nil), template.Nests...)
	seen := make(map[uint32]bool, len(source.Rows))
	skipped := 0
	for _, row := range source.Rows {
		ref, ok := refs[row.Codename]
		if !ok || !admit(ref, row.Codename) {
			return Template{}, fmt.Errorf("%s has invalid field reference %q", label, row.Codename)
		}
		if row.NestID == 0 || seen[row.NestID] || row.RegionID&0x8000 != 0 || row.MaxCount < 0 || row.MaxCount > math.MaxUint16 ||
			row.RespawnDelayMinSec < 0 || row.RespawnDelayMaxSec < row.RespawnDelayMinSec ||
			row.ChampionGenPercentage < 0 || row.ChampionGenPercentage > 100 || row.Controls.ChangeTarget > 2 {
			return Template{}, fmt.Errorf("%s has invalid nest %d", label, row.NestID)
		}
		for _, value := range []float64{row.X, row.Y, row.Z, row.Radius, row.GenerateRadius} {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return Template{}, fmt.Errorf("%s has nonfinite nest %d", label, row.NestID)
			}
		}
		if row.Radius < 0 || row.GenerateRadius < 0 || row.Controls.SightRange < 0 ||
			(row.Champion != nil) != (row.Controls.ChampionID != 0) {
			return Template{}, fmt.Errorf("%s has inconsistent tactics %d", label, row.NestID)
		}
		seen[row.NestID] = true
		if skip != nil && skip(ref, row.Codename) {
			skipped++
			continue
		}
		nest := NestRow{
			WorldCode: "INS_DEFAULT", PolicyPinned: true,
			SpawnPoint: SpawnPoint{RefObjID: ref.RefObjID, RegionID: row.RegionID, X: row.X, Y: row.Y, Z: row.Z},
			Radius:     row.Radius, GenerateRadius: row.GenerateRadius, MaxCount: row.MaxCount,
			RespawnDelayMinSec: row.RespawnDelayMinSec, RespawnDelayMaxSec: row.RespawnDelayMaxSec,
			Respawn: row.Respawn, InitialDir: row.InitialDir, ChampionGenPercentage: row.ChampionGenPercentage,
			Controls: row.Controls, HasControls: true, Aggressive: row.Controls.AggressType == 0,
			SightRange: float64(row.Controls.SightRange), TargetPolicy: row.Controls.ChangeTarget,
			NativeTacticsFlags: row.Controls.Flags,
		}
		if row.Champion != nil {
			c := *row.Champion
			nest.HasChampionTactics = true
			nest.ChampionTactics = ChampionTactics{Controls: c, HasControls: true, Aggressive: c.AggressType == 0,
				SightRange: float64(c.SightRange), TargetPolicy: c.ChangeTarget, NativeTacticsFlags: c.Flags}
		}
		nests = append(nests, nest)
	}
	if skipped > 0 {
		log.Printf("%s: %d authored nest(s) held back by their admission flag (port-only)", label, skipped)
	}
	result := TemplateFromParts(template.Refs, nests)
	result.SummonRefs = append([]uint32(nil), template.SummonRefs...)
	result.EvidenceMatches = template.EvidenceMatches
	return result, nil
}
