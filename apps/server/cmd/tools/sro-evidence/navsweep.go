/*
===========================================================================

navsweep.go - sro-evidence navsweep: find where the server can trap a player

A player report ("I almost got stuck at X, Y") names a place, not a chord.
navsweep grids the area around a region-local point, keeps every point the
server would accept as a standing surface, and from each one tries short
moves in eight directions through the same ClipMovementPath the server
enforces. It reports:

	traps      - standing points from which no direction moves at all;
	pockets    - standing points with a single way out;
	one-way    - A reaches B, but B (itself standing) cannot move back to A;
	uncovered  - moves that cross tiles the navmesh does not cover.

Those are the places a click-to-move player gets stuck or rubber-bands.
Offline: it reads the verified server game data and changes nothing.

Usage:

	sro-evidence navsweep -region 0x694F -x 1040 -z 160 [-radius 300] [-step 10] [-move 20] [-out result.json]

===========================================================================
*/
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"

	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/gamedata"
)

// navSweepDirections are the eight compass moves, unit vectors in the
// region-local x/z plane.
var navSweepDirections = [8][2]float64{
	{1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}, {0, -1}, {1, -1},
}

type navSweepPoint struct {
	RegionWord uint16  `json:"regionWord"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Z          float64 `json:"z"`
	// WorldX/WorldY are the in-game map coordinates players report.
	WorldX float64 `json:"worldX"`
	WorldY float64 `json:"worldY"`
}

type navSweepFinding struct {
	Kind      string        `json:"kind"`
	At        navSweepPoint `json:"at"`
	Exits     int           `json:"exits"`
	Blocked   []string      `json:"blocked,omitempty"`
	Direction string        `json:"direction,omitempty"`
}

// navSweepMove is one probe move and the server's verdict, the input a
// client clipper replays to find where the two disagree.
type navSweepMove struct {
	From    navSweepPoint `json:"from"`
	Goal    navSweepPoint `json:"goal"`
	Outcome string        `json:"outcome"`
	Rest    navSweepPoint `json:"rest"`
}

type navSweepResult struct {
	Center    navSweepPoint     `json:"center"`
	Radius    float64           `json:"radius"`
	Step      float64           `json:"step"`
	Move      float64           `json:"move"`
	Sampled   int               `json:"sampled"`
	Standing  int               `json:"standing"`
	Traps     int               `json:"traps"`
	Pockets   int               `json:"pockets"`
	OneWay    int               `json:"oneWay"`
	Uncovered int               `json:"uncovered"`
	Findings  []navSweepFinding `json:"findings"`
}

/*
================
navSweepWorldPoint

Region-local position to the map coordinates the game displays: 192 world
units per region, region (135, 92) at the origin, local units in tenths.
================
*/
func navSweepWorldPoint(spawn simulation.Spawn) navSweepPoint {
	const regionWorldUnits = 192
	const originX, originY = 135, 92
	return navSweepPoint{
		RegionWord: spawn.RegionID,
		X:          spawn.X,
		Y:          spawn.Y,
		Z:          spawn.Z,
		WorldX:     float64(simulation.SectorX(spawn.RegionID)-originX)*regionWorldUnits + spawn.X/10,
		WorldY:     float64(simulation.SectorY(spawn.RegionID)-originY)*regionWorldUnits + spawn.Z/10,
	}
}

/*
================
navSweepStanding

The surface the server accepts at a region-local x/z, normalized into the
region that owns it, or false when nobody could stand there.
================
*/
func navSweepStanding(validator *movement.WaterValidator, region uint16, x, z float64) (simulation.Spawn, bool) {
	spawn := simulation.NormalizeSpawnFrame(simulation.Spawn{RegionID: region, X: x, Z: z})
	terrain, ok := validator.TerrainHeightAt(spawn.RegionID, spawn.X, spawn.Z)
	if !ok {
		return spawn, false
	}
	height, ok := validator.WalkableSpawnHeightAt(spawn.RegionID, spawn.X, terrain, spawn.Z)
	if !ok {
		return spawn, false
	}
	spawn.Y = height
	return spawn, true
}

/*
================
navSweepBlock

A refused move as "outcome/class", with the first blocked terrain tile when
the obstacle is terrain (an object contact is an edge, not a tile).
================
*/
func navSweepBlock(report movement.ClipReport) string {
	text := string(report.Outcome)
	if report.Class != "" {
		text += "/" + string(report.Class)
	}
	if report.Outcome == movement.ClipBlocked && report.BlockedTileX != 0 {
		text += fmt.Sprintf("@tile(%d,%d)", report.BlockedTileX, report.BlockedTileZ)
	}
	return text
}

/*
================
runNavSweep
================
*/
func runNavSweep(args []string) error {
	flags := flag.NewFlagSet("navsweep", flag.ContinueOnError)
	regionText := flags.String("region", "", "region word, e.g. 0x694F (required)")
	centerX := flags.Float64("x", 960, "region-local x of the center")
	centerZ := flags.Float64("z", 960, "region-local z of the center")
	radius := flags.Float64("radius", 300, "half-width of the swept square, local units")
	step := flags.Float64("step", 10, "grid spacing, local units")
	move := flags.Float64("move", 20, "length of each probe move, local units")
	outPath := flags.String("out", "", "optional path for the full JSON result")
	movesPath := flags.String("moves", "", "optional path for every probe move and the server's verdict")
	authorityRoot := flags.String("authority-root", "", "server world-authority root (default: the verified game data)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	region64, err := strconv.ParseUint(*regionText, 0, 16)
	if *regionText == "" || err != nil || *step <= 0 || *move <= 0 || *radius <= 0 {
		flags.Usage()
		return fmt.Errorf("%w: -region (hex or decimal) and positive -radius/-step/-move are required", errCommandUsage)
	}
	root := *authorityRoot
	if root == "" {
		paths, err := gamedata.Resolve()
		if err != nil {
			return fmt.Errorf("server game data: %w", err)
		}
		root = paths.WorldAuthorityDir
	}
	validator := movement.NewAuthorityValidator(root)
	region := uint16(region64)

	center, _ := navSweepStanding(validator, region, *centerX, *centerZ)
	result := navSweepResult{Center: navSweepWorldPoint(center), Radius: *radius, Step: *step, Move: *move}
	type grid struct{ i, j int }
	standing := map[grid]simulation.Spawn{}
	span := int(math.Floor(*radius / *step))
	for i := -span; i <= span; i++ {
		for j := -span; j <= span; j++ {
			result.Sampled++
			if spawn, ok := navSweepStanding(validator, region, *centerX+float64(i)**step, *centerZ+float64(j)**step); ok {
				standing[grid{i, j}] = spawn
			}
		}
	}
	result.Standing = len(standing)

	names := [8]string{"E", "NE", "N", "NW", "W", "SW", "S", "SE"}
	keys := make([]grid, 0, len(standing))
	for key := range standing {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(a, b int) bool {
		if keys[a].j != keys[b].j {
			return keys[a].j < keys[b].j
		}
		return keys[a].i < keys[b].i
	})
	var moves []navSweepMove
	for _, key := range keys {
		from := standing[key]
		exits := 0
		var blocked []string
		for d, dir := range navSweepDirections {
			length := *move / math.Hypot(dir[0], dir[1])
			goal, goalStands := navSweepStanding(validator, region,
				*centerX+float64(key.i)**step+dir[0]*length, *centerZ+float64(key.j)**step+dir[1]*length)
			report := validator.ClipWalkFrom(from, simulation.NavOwner{}, goal)
			moves = append(moves, navSweepMove{
				From: navSweepWorldPoint(from), Goal: navSweepWorldPoint(goal),
				Outcome: string(report.Outcome), Rest: navSweepWorldPoint(report.Rest),
			})
			if report.TilesUncovered > 0 || report.Outcome == movement.ClipNoCoverage {
				result.Uncovered++
			}
			if report.Outcome == movement.ClipArrived {
				exits++
				// A reachable standing goal that cannot come back is a one-way edge.
				// The way back starts where the player actually is: the forward
				// move's rest and the surface owner it retained, never a
				// re-guessed owner (players keep their nav cell).
				if goalStands {
					if back := validator.ClipWalkFrom(report.Rest, report.RestOwner, from); back.Outcome != movement.ClipArrived {
						result.OneWay++
						result.Findings = append(result.Findings, navSweepFinding{
							Kind: "one-way", At: navSweepWorldPoint(from), Exits: -1, Direction: names[d],
							Blocked: []string{"back:" + navSweepBlock(back)},
						})
					}
				}
				continue
			}
			blocked = append(blocked, names[d]+":"+navSweepBlock(report))
		}
		switch exits {
		case 0:
			result.Traps++
			result.Findings = append(result.Findings, navSweepFinding{Kind: "trap", At: navSweepWorldPoint(from), Blocked: blocked})
		case 1:
			result.Pockets++
			result.Findings = append(result.Findings, navSweepFinding{Kind: "pocket", At: navSweepWorldPoint(from), Exits: 1, Blocked: blocked})
		}
	}

	fmt.Printf("navsweep: region 0x%04X around (%.0f, %.0f) = map (%.1f, %.1f): %d sampled, %d standing, %d traps, %d pockets, %d one-way edges, %d uncovered moves\n",
		region, *centerX, *centerZ, result.Center.WorldX, result.Center.WorldY,
		result.Sampled, result.Standing, result.Traps, result.Pockets, result.OneWay, result.Uncovered)
	for _, finding := range result.Findings {
		if finding.Kind == "one-way" {
			continue
		}
		fmt.Printf("  %-6s map (%.1f, %.1f) region 0x%04X local (%.0f, %.1f, %.0f) blocked %v\n", finding.Kind,
			finding.At.WorldX, finding.At.WorldY, finding.At.RegionWord, finding.At.X, finding.At.Y, finding.At.Z, finding.Blocked)
	}
	if *movesPath != "" {
		encoded, err := json.Marshal(moves)
		if err != nil {
			return fmt.Errorf("encoding moves: %w", err)
		}
		if err := os.WriteFile(*movesPath, encoded, 0o644); err != nil {
			return err
		}
	}
	if *outPath == "" {
		return nil
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding result: %w", err)
	}
	return os.WriteFile(*outPath, encoded, 0o644)
}
