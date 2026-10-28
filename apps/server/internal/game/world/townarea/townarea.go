/*
===========================================================================

townarea.go - the town rectangles of regioninfo.txt (port-only, not native)

The thief transport rule (#721) is a region rule: natively a thief's
transport may not cross from a battlefield region into a protected one.
Some towns' walls run into battlefield regions, and the client's own town
list, regioninfo.txt, names those parts as RECT areas of a #TOWN section.
#1985 (owner, 2026-10-28): with SRO_THIEF_CART_TOWN_AREAS on (the default)
the thief rule also treats those parts as town, so a cart stops at the
wall of such a town rather than at its protected core. Off is native.

The file format, from v1.150 CRegionManager_LoadRegionInfo (412D80):
a "#TOWN" or "#FIELD" header line starts a section, and each row is
"x z ALL" (the whole region) or "x z RECT x0 z0 x1 z1". The four RECT
numbers are corners, not a size: the client's point test (414030) checks
x0 <= x <= x1 and z0 <= z <= z1, inclusive, and the parser (4133EA) keeps
the tokens as written. Several rows are lines (z0 == z1), which hold no
ground; they are skipped.

Only towns are kept, and only their parts outside protected regions (the
region rule already covers those). A #TOWN section none of whose rows is
a protected region is a fortress, a fighting ground, and is skipped.

===========================================================================
*/
package townarea

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

// EnvThiefCartTownAreas names the switch: "on" (the default) or "off".
const EnvThiefCartTownAreas = "SRO_THIEF_CART_TOWN_AREAS"

// RegionInfoFile is the client's town and field list in the textdata
// directory.
const RegionInfoFile = "regioninfo.txt"

// entryBackoff is how far before an area's edge, in world units, a chord
// is sampled to see where it came from.
const entryBackoff = 0.01

/*
================
ThiefCartTownAreasFromEnv
================
*/
func ThiefCartTownAreasFromEnv() (bool, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvThiefCartTownAreas))) {
	case "", "1", "on", "true":
		return true, nil
	case "0", "off", "false":
		return false, nil
	}
	return false, fmt.Errorf("%s=%q: want on or off", EnvThiefCartTownAreas, os.Getenv(EnvThiefCartTownAreas))
}

/*
================
area

One town part in the continuous outdoor grid, corners inclusive.
================
*/
type area struct {
	x0, z0, x1, z1 float64
}

/*
================
area.contains
================
*/
func (a area) contains(p worldgeom.GridXZ) bool {
	return p.X >= a.x0 && p.X <= a.x1 && p.Z >= a.z0 && p.Z <= a.z1
}

/*
================
area.entersAt

Where the chord p0 + t*(p1-p0), t in [0,1], first enters the area, by
Liang-Barsky clipping. ok is false when the chord misses it or starts
inside it.
================
*/
func (a area) entersAt(p0, p1 worldgeom.GridXZ) (t float64, ok bool) {
	if a.contains(p0) {
		return 0, false
	}
	low, high := 0.0, 1.0
	dx, dz := p1.X-p0.X, p1.Z-p0.Z
	clip := func(p, q float64) bool {
		if p == 0 {
			return q >= 0
		}
		r := q / p
		if p < 0 {
			if r > high {
				return false
			}
			low = math.Max(low, r)
		} else {
			if r < low {
				return false
			}
			high = math.Min(high, r)
		}
		return true
	}
	if !clip(-dx, p0.X-a.x0) || !clip(dx, a.x1-p0.X) || !clip(-dz, p0.Z-a.z0) || !clip(dz, a.z1-p0.Z) {
		return 0, false
	}
	return low, true
}

/*
================
Areas

The town parts that lie in battlefield regions. The zero value holds none.
================
*/
type Areas struct {
	areas []area
}

/*
================
Load

Reads regioninfo.txt from the textdata directory.
================
*/
func Load(textdataDir string) (*Areas, error) {
	f, err := os.Open(filepath.Join(textdataDir, RegionInfoFile))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

/*
================
section

One #TOWN section being read: its parts, and whether any row is a
protected region (a real town rather than a fortress).
================
*/
type section struct {
	areas     []area
	protected bool
}

/*
================
Parse

The file is CP949; only the header keywords and the numbers matter, all
ASCII, so the bytes are read as they are. A malformed row is an error: a
town list that does not read is a broken data build.
================
*/
func Parse(r io.Reader) (*Areas, error) {
	var out Areas
	var town *section
	flush := func() {
		if town != nil && town.protected {
			out.areas = append(out.areas, town.areas...)
		}
		town = nil
	}
	scanner := bufio.NewScanner(r)
	line := 0
	for scanner.Scan() {
		line++
		fields := strings.Split(string(bytes.TrimRight(scanner.Bytes(), "\r")), "\t")
		switch {
		case fields[0] == "#TOWN":
			flush()
			town = &section{}
			continue
		case strings.HasPrefix(fields[0], "#"):
			flush()
			continue
		case strings.TrimSpace(fields[0]) == "":
			continue
		}
		if town == nil {
			continue
		}
		if err := town.addRow(fields); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", RegionInfoFile, line, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	flush()
	return &out, nil
}

/*
================
section.addRow
================
*/
func (s *section) addRow(fields []string) error {
	if len(fields) < 3 {
		return fmt.Errorf("short row %q", strings.Join(fields, "\t"))
	}
	numbers, err := atois(fields[:2])
	if err != nil {
		return err
	}
	x, z := numbers[0], numbers[1]
	if x < 0 || x > 0xff || z < 0 || z > 0x7f {
		return fmt.Errorf("region %d,%d is not an outdoor sector", x, z)
	}
	region := worldgeom.RegionIDForSectors(x, z)
	allowed, known := worldgeom.RegionPlayerCombat(region)
	if known && !allowed {
		s.protected = true
		return nil
	}
	ox, oz := float64(x)*worldgeom.OutdoorRegionSize, float64(z)*worldgeom.OutdoorRegionSize
	switch fields[2] {
	case "ALL":
		s.areas = append(s.areas, area{ox, oz, ox + worldgeom.OutdoorRegionSize, oz + worldgeom.OutdoorRegionSize})
	case "RECT":
		if len(fields) < 7 {
			return fmt.Errorf("short RECT row %q", strings.Join(fields, "\t"))
		}
		corners, err := atois(fields[3:7])
		if err != nil {
			return err
		}
		// A line (or a reversed rectangle) holds no ground.
		if corners[0] >= corners[2] || corners[1] >= corners[3] {
			return nil
		}
		s.areas = append(s.areas, area{
			ox + float64(corners[0]), oz + float64(corners[1]),
			ox + float64(corners[2]), oz + float64(corners[3]),
		})
	default:
		return fmt.Errorf("area kind %q", fields[2])
	}
	return nil
}

/*
================
atois
================
*/
func atois(fields []string) ([]int, error) {
	out := make([]int, len(fields))
	for i, field := range fields {
		n, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil {
			return nil, err
		}
		out[i] = n
	}
	return out, nil
}

/*
================
Areas.Len
================
*/
func (a *Areas) Len() int {
	if a == nil {
		return 0
	}
	return len(a.areas)
}

/*
================
Areas.Contains

Whether the point lies in a town part. Dungeon points never do.
================
*/
func (a *Areas) Contains(p worldgeom.RegionXZ) bool {
	if a == nil || simulation.PlaneKindOf(p.RegionID) != simulation.PlaneOutdoor {
		return false
	}
	return a.containsGrid(worldgeom.ExpandGrid(p))
}

/*
================
Areas.containsGrid
================
*/
func (a *Areas) containsGrid(p worldgeom.GridXZ) bool {
	for _, part := range a.areas {
		if part.contains(p) {
			return true
		}
	}
	return false
}

/*
================
Areas.EnteredFromBattlefield

Whether the chord from -> to walks into a town part from open battlefield:
the point just before the edge is in no town part and in a battlefield
region, as the native rule needs the region a transport leaves to be a
battlefield. A chord from a protected town core into the town's outer
part is town to town and passes, as does one that starts inside a part.
================
*/
func (a *Areas) EnteredFromBattlefield(from, to worldgeom.RegionXZ) bool {
	if a.Len() == 0 || simulation.PlaneKindOf(from.RegionID) != simulation.PlaneOutdoor ||
		simulation.PlaneKindOf(to.RegionID) != simulation.PlaneOutdoor {
		return false
	}
	p0, p1 := worldgeom.ExpandGrid(from), worldgeom.ExpandGrid(to)
	length := math.Hypot(p1.X-p0.X, p1.Z-p0.Z)
	if length == 0 {
		return false
	}
	var entries []float64
	for _, part := range a.areas {
		if t, ok := part.entersAt(p0, p1); ok {
			entries = append(entries, t)
		}
	}
	sort.Float64s(entries)
	for _, t := range entries {
		before := math.Max(t-entryBackoff/length, 0)
		p := worldgeom.GridXZ{X: p0.X + (p1.X-p0.X)*before, Z: p0.Z + (p1.Z-p0.Z)*before}
		if a.containsGrid(p) {
			continue
		}
		region := worldgeom.RegionIDForSectors(
			int(math.Floor(p.X/worldgeom.OutdoorRegionSize)),
			int(math.Floor(p.Z/worldgeom.OutdoorRegionSize)),
		)
		if allowed, known := worldgeom.RegionPlayerCombat(region); known && allowed {
			return true
		}
	}
	return false
}
