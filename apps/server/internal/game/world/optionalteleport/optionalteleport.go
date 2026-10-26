/*
===========================================================================

optionalteleport.go - the imported reverse return map table

Port-only, not native. v1.150 ships no optional-teleport table; its
reverse return scroll offers only the last recall and last death points.
A later client adds choice 7 followed by a u32 id, which it looks up in
its own table of map destinations. The operator imports the VSRO client's refoptionalteleport.txt
(scripts/import_vsro_optional_teleport.py) and the server bundle projects
it as textdata/vsro-optional-teleport.json; without the import there is no
file and the action runtime keeps its inferred table (reversemap.go).

This package owns which rows the port serves: the outdoor map points of the
loaded world. A row is served when it is in service, in the field world
(WorldID 1), drawn on the map (MapPoint 1) and not part of a dungeon group
(RegionIDGroup -1), and its region is an outdoor region the loaded textdata
names (textzonename) and the Alexandria overlay does not hide. That drops the
dungeon floors and their entrance flags (the port's map has no dungeon
destinations), and drops Alexandria's rows unless its regions are loaded and
SRO_ALEXANDRIA is on.

INFERENCE: LevelMin / LevelMax are the table's level band (0 means no
limit); the port refuses a use outside it. The shipped rows use 0/0,
50/999, 70/999 and 95/999.

===========================================================================
*/
package optionalteleport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/alexandria"
	"opensro.online/server/internal/game/world/simulation"
)

// File is the projected import, inside the textdata directory.
const File = "vsro-optional-teleport.json"

const (
	importFormat = "opensro-vsro-optional-teleport-v1"
	// maxImportBytes bounds the file (the VSRO table is 38 rows, ~8 KB).
	maxImportBytes = 1 << 20
	// maxPoints bounds the served table (the public reference bound).
	maxPoints = 4096
	// maxNameBytes bounds a point name (the public reference bound).
	maxNameBytes = 128
	// regionSize is a region's local extent (x and z in [0, 1920)).
	regionSize = 1920

	serviceOn       = 1
	fieldWorldID    = 1
	mapPointShown   = 1
	noRegionGroup   = -1
	zoneNameColumns = 9
	zoneNameRegion  = 1
	zoneNameEnglish = 8
)

/*
================
Point

One served destination, by the table's own id.
================
*/
type Point struct {
	ID       uint32
	Name     string
	RegionID uint16
	X, Y, Z  float64
	LevelMin int64
	LevelMax int64
}

/*
================
Point.Admits

The level band: zero on either side is no limit there.
================
*/
func (p Point) Admits(level int64) bool {
	if p.LevelMin > 0 && level < p.LevelMin {
		return false
	}
	return p.LevelMax <= 0 || level <= p.LevelMax
}

type importRow struct {
	Service     int64   `json:"service"`
	ID          int64   `json:"id"`
	Zone        string  `json:"zone"`
	Name        string  `json:"name"`
	RegionID    int64   `json:"regionId"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Z           float64 `json:"z"`
	WorldID     int64   `json:"worldId"`
	RegionGroup int64   `json:"regionGroup"`
	MapPoint    int64   `json:"mapPoint"`
	LevelMin    int64   `json:"levelMin"`
	LevelMax    int64   `json:"levelMax"`
}

type importFile struct {
	Format string          `json:"format"`
	Source json.RawMessage `json:"source"`
	Rows   []importRow     `json:"rows"`
}

/*
================
Load

The served points of the projected import, sorted by id. A missing file
is a game root without the import (nil, nil); a present file must parse
exactly.
================
*/
func Load(textdataDir string) ([]Point, error) {
	path := filepath.Join(textdataDir, File)
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("optional teleport: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > maxImportBytes {
		return nil, fmt.Errorf("optional teleport %s: not a regular file of at most %d bytes", path, maxImportBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("optional teleport: %w", err)
	}
	var raw importFile
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("optional teleport %s: %w", path, err)
	}
	if raw.Format != importFormat || len(raw.Rows) == 0 {
		return nil, fmt.Errorf("optional teleport %s: format %q with %d rows", path, raw.Format, len(raw.Rows))
	}
	hidden, err := alexandria.Hidden(textdataDir)
	if err != nil {
		return nil, err
	}
	return selectPoints(raw.Rows, zoneNames(textdataDir), hidden)
}

/*
================
zoneNames

Region id -> English zone name, from the loaded textzonename.
================
*/
func zoneNames(textdataDir string) map[uint16]string {
	names := map[uint16]string{}
	for _, row := range enterworld.ReadTextdataFile(filepath.Join(textdataDir, "textzonename.txt")) {
		if len(row) < zoneNameColumns || row[0] != "1" {
			continue
		}
		region, err := strconv.ParseUint(row[zoneNameRegion], 10, 16)
		if err != nil {
			continue
		}
		if _, seen := names[uint16(region)]; !seen {
			names[uint16(region)] = row[zoneNameEnglish]
		}
	}
	return names
}

/*
================
selectPoints

The rows the port serves (see the file header), sorted by id. A repeated
id refuses the table.
================
*/
func selectPoints(rows []importRow, zones map[uint16]string, hidden *alexandria.Overlay) ([]Point, error) {
	var points []Point
	seen := map[int64]bool{}
	for _, row := range rows {
		if row.ID < 1 || row.ID > math.MaxUint32 || seen[row.ID] {
			return nil, fmt.Errorf("optional teleport: row id %d is not a new positive id", row.ID)
		}
		seen[row.ID] = true
		if row.Service != serviceOn || row.WorldID != fieldWorldID || row.MapPoint != mapPointShown ||
			row.RegionGroup != noRegionGroup || row.RegionID < 1 || row.RegionID > math.MaxUint16 {
			continue
		}
		region := uint16(row.RegionID)
		zone, known := zones[region]
		if !known || simulation.PlaneKindOf(region) == simulation.PlaneCave || hidden.HidesRegion(region) || !inRegion(row) {
			continue
		}
		name := row.Name
		if name == "" {
			name = zone
		}
		if name == "" || len(name) > maxNameBytes {
			continue
		}
		points = append(points, Point{ID: uint32(row.ID), Name: name, RegionID: region,
			X: row.X, Y: row.Y, Z: row.Z, LevelMin: row.LevelMin, LevelMax: row.LevelMax})
	}
	if len(points) > maxPoints {
		return nil, fmt.Errorf("optional teleport: %d points, at most %d", len(points), maxPoints)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].ID < points[j].ID })
	return points, nil
}

/*
================
inRegion

Finite coordinates inside the region's local extent.
================
*/
func inRegion(row importRow) bool {
	for _, v := range [3]float64{row.X, row.Y, row.Z} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return row.X >= 0 && row.X < regionSize && row.Z >= 0 && row.Z < regionSize
}
