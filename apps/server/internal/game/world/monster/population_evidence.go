package monster

import (
	_ "embed"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// populationEvidenceTSV is a derived cross-version evidence table from v1.188
// population observations. Runtime identity is intentionally absent: rows
// join to v1.150 by codename, region, and anchor coordinates. The proprietary
// source database is not distributed with this repository.
//
//go:embed data/v1188_population_evidence.tsv
var populationEvidenceTSV string

// The archived ISRO-R nest table supplements missing exact v1.150 anchors.
// Its newer active-table balance is not merged into existing evidence.
//
//go:embed data/isror_population_supplement.tsv
var populationSupplementTSV string

type populationEvidence struct {
	TargetPolicy          uint8
	Radius                float64
	GenerateRadius        float64
	ChampionGenPercentage int
	RespawnDelayMinSec    int
	RespawnDelayMaxSec    int
	MaxCount              int
	Respawn               bool
	Aggressive            bool
	SightRange            float64
	NativeTacticsFlags    uint32
	// Champion projects four fields from the row named by dwChampionTacticsID;
	// it does not certify coverage of the remaining native tactics fields.
	// HasChampion is false when that id is zero.
	HasChampion bool
	Champion    ChampionTactics
	// InitialDir is Tab_RefNest.wInitialDir (runtime nest +1C).
	InitialDir uint16
}

type populationEvidenceKey struct {
	Codename string
	RegionID uint16
	X10      int64
	Y10      int64
	Z10      int64
}

func combinePopulationEvidence(base, supplement map[populationEvidenceKey]populationEvidence) map[populationEvidenceKey]populationEvidence {
	rows := make(map[populationEvidenceKey]populationEvidence, len(base)+len(supplement))
	for key, row := range base {
		rows[key] = row
	}
	for key, row := range supplement {
		if _, exists := rows[key]; exists {
			panic("supplement overwrites existing population evidence")
		}
		rows[key] = row
	}
	return rows
}

/*
==================
laterDisabledCodenames

Returns the monsters whose every evidence row is capped at zero.

The v1.188 evidence comes from one operator's shard backup, not from
retail. For all but these it agrees with the v1.150 client. For these it
disables every nest of a monster the v1.150 client still places (npcpos)
and still requires: MOB_DH_SOLDIEREARTHGHOST, the Donwhang Stone Cave
target of QNO_WC_SOLDIER_EA2_1 (1,600 kills) and a QNO_WC_PRIEST2_1 drop.
A grind quest of that size implies a natural population, so a zero cap on
every nest is a later disable, not the v1.150 rule. LoadTemplate gives
these nests the unmatched-anchor cap and keeps their other evidence.
==================
*/
func laterDisabledCodenames(rows map[populationEvidenceKey]populationEvidence) map[string]bool {
	live := make(map[string]bool)
	for key, row := range rows {
		if row.MaxCount > 0 {
			live[key.Codename] = true
		} else if !live[key.Codename] {
			live[key.Codename] = false
		}
	}
	disabled := make(map[string]bool)
	for codename, hasLiveNest := range live {
		if !hasLiveNest {
			disabled[codename] = true
		}
	}
	return disabled
}

// Native Nest coordinates are float32. Client text and recovered SQL rows
// can print the same float with different decimal precision. Normalize that
// representation before quantizing; otherwise half-tenth anchors lose both
// their population policy and shared hive identity.
func nativeAnchorTenth(value float64) int64 {
	return int64(math.Round(float64(float32(value)) * 10))
}

func evidenceKey(codename string, regionID uint16, x, y, z float64) populationEvidenceKey {
	return populationEvidenceKey{
		Codename: codename,
		RegionID: regionID,
		X10:      nativeAnchorTenth(x),
		Y10:      nativeAnchorTenth(y),
		Z10:      nativeAnchorTenth(z),
	}
}

func mustLoadPopulationEvidence(input string) map[populationEvidenceKey]populationEvidence {
	rows := make(map[populationEvidenceKey]populationEvidence)
	for lineNumber, line := range strings.Split(input, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		columns := strings.Split(line, "\t")
		if len(columns) != 22 {
			panic(fmt.Sprintf("monster evidence line %d: got %d columns, want 22", lineNumber+1, len(columns)))
		}

		region := mustEvidenceUint(columns[1], 16, lineNumber)
		x := mustEvidenceFloat(columns[2], lineNumber)
		y := mustEvidenceFloat(columns[3], lineNumber)
		z := mustEvidenceFloat(columns[4], lineNumber)
		row := populationEvidence{
			TargetPolicy:          uint8(mustEvidenceUint(columns[15], 8, lineNumber)),
			Radius:                mustEvidenceFloat(columns[5], lineNumber),
			GenerateRadius:        mustEvidenceFloat(columns[6], lineNumber),
			ChampionGenPercentage: mustEvidenceInt(columns[7], lineNumber),
			RespawnDelayMinSec:    mustEvidenceInt(columns[8], lineNumber),
			RespawnDelayMaxSec:    mustEvidenceInt(columns[9], lineNumber),
			MaxCount:              mustEvidenceInt(columns[10], lineNumber),
			Respawn:               mustEvidenceBool(columns[11], lineNumber),
			Aggressive:            mustEvidenceBool(columns[12], lineNumber),
			SightRange:            mustEvidenceFloat(columns[13], lineNumber),
			NativeTacticsFlags:    uint32(mustEvidenceUint(columns[14], 32, lineNumber)),
			HasChampion:           mustEvidenceBool(columns[16], lineNumber),
			Champion: ChampionTactics{
				Aggressive:         mustEvidenceBool(columns[17], lineNumber),
				SightRange:         mustEvidenceFloat(columns[18], lineNumber),
				NativeTacticsFlags: uint32(mustEvidenceUint(columns[19], 32, lineNumber)),
				TargetPolicy:       uint8(mustEvidenceUint(columns[20], 8, lineNumber)),
			},
			InitialDir: uint16(mustEvidenceUint(columns[21], 16, lineNumber)),
		}
		if row.Radius < 0 || row.GenerateRadius < 0 ||
			row.RespawnDelayMinSec < 0 ||
			row.RespawnDelayMaxSec < row.RespawnDelayMinSec ||
			row.ChampionGenPercentage < 0 || row.ChampionGenPercentage > 100 ||
			row.MaxCount < 0 || row.SightRange < 0 || row.Champion.SightRange < 0 ||
			!row.HasChampion && row.Champion != (ChampionTactics{}) {
			panic(fmt.Sprintf("monster evidence line %d: value outside contract", lineNumber+1))
		}

		key := evidenceKey(columns[0], uint16(region), x, y, z)
		if _, duplicate := rows[key]; duplicate {
			panic(fmt.Sprintf("monster evidence line %d: duplicate natural key", lineNumber+1))
		}
		rows[key] = row
	}
	return rows
}

func mustEvidenceUint(value string, bits int, lineNumber int) uint64 {
	parsed, err := strconv.ParseUint(value, 10, bits)
	if err != nil {
		panic(fmt.Sprintf("monster evidence line %d: invalid unsigned value %q", lineNumber+1, value))
	}
	return parsed
}

func mustEvidenceInt(value string, lineNumber int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		panic(fmt.Sprintf("monster evidence line %d: invalid integer %q", lineNumber+1, value))
	}
	return parsed
}

func mustEvidenceFloat(value string, lineNumber int) float64 {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		panic(fmt.Sprintf("monster evidence line %d: invalid float %q", lineNumber+1, value))
	}
	return parsed
}

func mustEvidenceBool(value string, lineNumber int) bool {
	switch value {
	case "0":
		return false
	case "1":
		return true
	default:
		panic(fmt.Sprintf("monster evidence line %d: invalid boolean %q", lineNumber+1, value))
	}
}
