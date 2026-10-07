// The moveclip-oracle subcommand runs shared movement chords through the same
// ClipMovementPath implementation used by the server and writes comparable
// JSON results.
//
// This is an offline measurement tool: it does not start a server or mutate
// runtime state.
//
// Usage:
//
//	sro-evidence moveclip-oracle -chords <chords.json> -out <results.json> [-authority-root <server world-authority root>]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/gamedata"
)

// chordPoint is one endpoint in the contract frame: canonical region word +
// region-local floats.
type chordPoint struct {
	RegionWord uint16  `json:"regionWord"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Z          float64 `json:"z"`
}

type chord struct {
	ID               string          `json:"id"`
	From             chordPoint      `json:"from"`
	To               chordPoint      `json:"to"`
	SpeedUnitsPerSec float64         `json:"speedUnitsPerSec"`
	Strata           json.RawMessage `json:"strata,omitempty"`
}

type wallTile struct {
	TileX int `json:"tileX"`
	TileZ int `json:"tileZ"`
}

type resultMeta struct {
	TilesChecked    int  `json:"tilesChecked"`
	TilesUncovered  int  `json:"tilesUncovered"`
	Truncated       bool `json:"truncated"`
	NormalizedInput bool `json:"normalizedInput"`
}

type result struct {
	ID string `json:"id"`
	// Outcome: arrived|blocked|noCoverage|dungeonExempt|startBlocked -
	// the Go clip's native outcome set.
	Outcome string     `json:"outcome"`
	Rest    chordPoint `json:"rest"`
	// WallTile is the world-grid first-blocked tile:
	// sector*tilesPerAxis + floor(local/tileSize)); null unless blocked.
	WallTile *wallTile `json:"wallTile"`
	// BlockedCandidate is a leg-(1) field; the Go clip never emits it.
	BlockedCandidate *chordPoint `json:"blockedCandidate"`
	Meta             resultMeta  `json:"meta"`
}

func spawnOf(p chordPoint) simulation.Spawn {
	return simulation.Spawn{RegionID: p.RegionWord, X: p.X, Y: p.Y, Z: p.Z}
}

func pointOf(s simulation.Spawn) chordPoint {
	return chordPoint{RegionWord: s.RegionID, X: s.X, Y: s.Y, Z: s.Z}
}

func runMoveclipOracle(args []string) error {
	flags := flag.NewFlagSet("moveclip-oracle", flag.ContinueOnError)
	chordsPath := flags.String("chords", "", "path to chords.json (required)")
	outPath := flags.String("out", "", "path to write results json (required)")
	authorityRoot := flags.String("authority-root", "", "server world-authority root (default: verified $SRO_SERVER_GAME_DATA_ROOT projection)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *chordsPath == "" || *outPath == "" {
		flags.Usage()
		return fmt.Errorf("%w: -chords and -out are required", errCommandUsage)
	}

	raw, err := os.ReadFile(*chordsPath)
	if err != nil {
		return fmt.Errorf("reading chords: %w", err)
	}
	var chords []chord
	if err := json.Unmarshal(raw, &chords); err != nil {
		return fmt.Errorf("decoding chords: %w", err)
	}

	var validator *movement.WaterValidator
	if *authorityRoot != "" {
		validator = movement.NewAuthorityValidator(*authorityRoot)
	} else {
		dataPaths, err := gamedata.Resolve()
		if err != nil {
			return fmt.Errorf("server game data: %w", err)
		}
		validator = movement.NewAuthorityValidator(dataPaths.WorldAuthorityDir)
	}

	results := make([]result, 0, len(chords))
	for _, c := range chords {
		from := simulation.NormalizeSpawnFrame(spawnOf(c.From))
		to := simulation.NormalizeSpawnFrame(spawnOf(c.To))
		normalized := from != spawnOf(c.From) || to != spawnOf(c.To)

		report := validator.ClipMovementPath(from, to)
		r := result{
			ID:      c.ID,
			Outcome: string(report.Outcome),
			Rest:    pointOf(report.Rest),
			Meta: resultMeta{
				TilesChecked:    report.TilesChecked,
				TilesUncovered:  report.TilesUncovered,
				Truncated:       report.Truncated,
				NormalizedInput: normalized,
			},
		}
		if report.Outcome == movement.ClipBlocked {
			r.WallTile = &wallTile{TileX: report.BlockedTileX, TileZ: report.BlockedTileZ}
		}
		results = append(results, r)
	}

	encoded, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding results: %w", err)
	}
	if err := os.WriteFile(*outPath, encoded, 0o644); err != nil {
		return fmt.Errorf("writing results: %w", err)
	}
	fmt.Printf("moveclip-oracle: %d chords -> %s\n", len(results), *outPath)
	return nil
}
