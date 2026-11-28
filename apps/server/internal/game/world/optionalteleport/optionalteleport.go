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

This package owns which rows the port serves: the map points of the
loaded world and the dungeon floors they open. A field row is served when
it is in service, in the field world (WorldID 1), drawn on the map
(MapPoint 1), and its region is an outdoor region the loaded textdata
names (textzonename) and the Alexandria overlay does not hide; that drops
Alexandria's rows unless its regions are loaded and SRO_ALEXANDRIA is on.

A dungeon group (RegionIDGroup other than -1) is one map row, the flag,
and the floor rows of the same group (MapPoint 0). The v1.188 client
draws the flag like any other point; clicking it opens a small menu of
the group's floors, labelled with each floor row's ZoneName128 text
(in the v1.188 client, its button-up handler 0x6971c8..0x6973bd lists
the group's rows whose MapPoint is not 1 through client 0x94d270, and
client 0x631d60 adds one 155 x 22 map_return button per row; research
findings/reverse-return-dungeon-groups-v1188.md). The flag itself is
never a destination: the shipped row 31 names itself "flag position
only". A floor is served when its region is a dungeon floor the shard
has open (the Ch'in Tomb floors only while SRO_QINSHI_TOMB is on); a
group is served only with its flag and at least one floor.

INFERENCE: LevelMin / LevelMax are the table's level band (0 means no
limit); the port refuses a use outside it. The shipped rows use 0/0,
50/999, 70/999 and 95/999.

INFERENCE: the table was written for the VSRO 110 cap. On a shard whose
cap is lower (Alexandria open at the native 90), a minimum above the cap
could never be met while the city's gates still take the player there,
so CapLevelBands lowers such a minimum to the cap.

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
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/alexandria"
	"opensro.online/server/internal/game/world/monster"
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
	// The Ch'in Tomb's dungeon ids, floors 6 down to 1 (geometry.go
	// DungeonRegionID: 0x8002..0x8007).
	tombFirstDungeon = 2
	tombLastDungeon  = 7
)

/*
================
Point

One served row, by the table's own id. Group is the row's dungeon group
(0 for a plain field point). A Floor row is a dungeon destination its
group's flag lists; a grouped row that is not a Floor is that flag, drawn
on the map but never a destination.
================
*/
type Point struct {
	ID       uint32
	Name     string
	RegionID uint16
	X, Y, Z  float64
	LevelMin int64
	LevelMax int64
	Group    uint32
	Floor    bool
}

/*
================
Point.Destination

Whether a use may name this row: a field point or a floor, never a flag.
================
*/
func (p Point) Destination() bool {
	return p.Group == 0 || p.Floor
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

/*
================
CapLevelBands

Lowers every minimum level above levelCap to levelCap (see the file
header), so the highest level the shard reaches always admits the row.
A non-positive cap leaves the bands as imported.
================
*/
func CapLevelBands(points []Point, levelCap int64) {
	if levelCap <= 0 {
		return
	}
	for i := range points {
		if points[i].LevelMin > levelCap {
			points[i].LevelMin = levelCap
		}
	}
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
	return selectPoints(raw.Rows, zoneNames(textdataDir), hidden, monster.QinShiTombFromEnv())
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
id refuses the table. tombOpen is SRO_QINSHI_TOMB: the Ch'in Tomb floors
are served only while the tomb is open.
================
*/
func selectPoints(rows []importRow, zones map[uint16]string, hidden *alexandria.Overlay, tombOpen bool) ([]Point, error) {
	var points []Point
	seen := map[int64]bool{}
	for _, row := range rows {
		if row.ID < 1 || row.ID > math.MaxUint32 || seen[row.ID] {
			return nil, fmt.Errorf("optional teleport: row id %d is not a new positive id", row.ID)
		}
		seen[row.ID] = true
		point, served := servedRow(row, zones, hidden, tombOpen)
		if served {
			points = append(points, point)
		}
	}
	points = completeGroups(points)
	if len(points) > maxPoints {
		return nil, fmt.Errorf("optional teleport: %d points, at most %d", len(points), maxPoints)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].ID < points[j].ID })
	return points, nil
}

/*
================
servedRow

One row as a served point: a field map point or a group's flag on a named
outdoor region, or a group's floor on an open dungeon floor.
================
*/
func servedRow(row importRow, zones map[uint16]string, hidden *alexandria.Overlay, tombOpen bool) (Point, bool) {
	if row.Service != serviceOn || row.WorldID != fieldWorldID {
		return Point{}, false
	}
	var group uint32
	if row.RegionGroup != noRegionGroup {
		if row.RegionGroup < 1 || row.RegionGroup > math.MaxUint32 {
			return Point{}, false
		}
		group = uint32(row.RegionGroup)
	}
	// The table's dungeon regions are signed (-32762 is 0x8006).
	if row.RegionID < math.MinInt16 || row.RegionID > math.MaxUint16 || row.RegionID == 0 {
		return Point{}, false
	}
	region := uint16(row.RegionID)
	if row.MapPoint != mapPointShown {
		if group == 0 || !floorOpen(region, tombOpen) || !finite(row) || row.Name == "" || len(row.Name) > maxNameBytes {
			return Point{}, false
		}
		return Point{ID: uint32(row.ID), Name: row.Name, RegionID: region, X: row.X, Y: row.Y, Z: row.Z,
			LevelMin: row.LevelMin, LevelMax: row.LevelMax, Group: group, Floor: true}, true
	}
	zone, known := zones[region]
	if !known || simulation.PlaneKindOf(region) == simulation.PlaneCave || hidden.HidesRegion(region) || !inRegion(row) {
		return Point{}, false
	}
	name := row.Name
	if name == "" {
		name = zone
	}
	if name == "" || len(name) > maxNameBytes {
		return Point{}, false
	}
	return Point{ID: uint32(row.ID), Name: name, RegionID: region, X: row.X, Y: row.Y, Z: row.Z,
		LevelMin: row.LevelMin, LevelMax: row.LevelMax, Group: group}, true
}

/*
================
floorOpen

A dungeon floor region the shard can land on: the Ch'in Tomb floors while
the tomb is open, any other dungeon (Donwhang Stone Cave) always.
================
*/
func floorOpen(region uint16, tombOpen bool) bool {
	if simulation.PlaneKindOf(region) != simulation.PlaneCave {
		return false
	}
	if region >= worldgeom.DungeonRegionID(tombFirstDungeon) && region <= worldgeom.DungeonRegionID(tombLastDungeon) {
		return tombOpen
	}
	return true
}

/*
================
completeGroups

Drops a group's rows unless it has exactly one flag and at least one
floor: a flag without floors would open an empty menu, and a floor
without its flag could never be chosen.
================
*/
func completeGroups(points []Point) []Point {
	flags, floors := map[uint32]int{}, map[uint32]int{}
	for _, point := range points {
		switch {
		case point.Group == 0:
		case point.Floor:
			floors[point.Group]++
		default:
			flags[point.Group]++
		}
	}
	kept := points[:0]
	for _, point := range points {
		if point.Group == 0 || flags[point.Group] == 1 && floors[point.Group] > 0 {
			kept = append(kept, point)
		}
	}
	return kept
}

/*
================
finite

Finite coordinates (a floor's locals have no fixed extent).
================
*/
func finite(row importRow) bool {
	for _, v := range [3]float64{row.X, row.Y, row.Z} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

/*
================
inRegion

Finite coordinates inside the region's local extent.
================
*/
func inRegion(row importRow) bool {
	if !finite(row) {
		return false
	}
	return row.X >= 0 && row.X < regionSize && row.Z >= 0 && row.Z < regionSize
}
