/*
===========================================================================

ainavdata.go - the native cave route tables (AINavData_<region>.DAT)

Parses each dungeon's route file and validates it against that dungeon's
own v1.150 nav meshes before the cave route may use it (#1698). A file
that fails any check is refused, and its dungeon keeps the port's own
portal route (cave_route.go). 0x8009 is refused this way: its route
blocks do not match the v1.150 mesh's cell counts.

The file has two sections, both read by the native loader (layout read
in the disassembly; the server binary is described in words only):

	route section     u8 version (1), u32 offset of the point section,
	                  u16 region, u32 block count n; per block u32 index,
	                  u32 cell count m, u32 point count c, m*m u32 route
	                  words [dest*m + cur], u32 portal count, then per
	                  portal {u32 key, u16 cell, u16 landing block,
	                  u16 landing point}; then n*n u16 fastest cells
	                  [dest*n + cur], and a u32 0 trailer.
	point section     at the offset: u16 region, u32 n; per block u32 c
	                  and c f32 xyz points in region coordinates; a u32 0
	                  trailer, ending the file.

A route word's two halves index the current block's points: the high
half first, the low half when the line to the high one clips. The points
are crossing-edge midpoints at the block's floor height: the X and Z of
each edge's midpoint, the Y of the block's placement (the vertices' own
heights are not used), for the mesh's internal edges in order, then its
portal outline edges in outline order. Checked on all 151 Donwhang blocks.

0x8009's refusal is a recorded decision, not a fault (#1979). It is the
Capture the Flag arena (dungeon/wchina/event.dof). Its file in the v1.150
Data.pk2 gives its two rooms 57 cells, while every room mesh the v1.150
client ships for them has 185 or 187 (room102event and room102 alike);
its three small blocks match. The table was built for meshes the client
never shipped. The client never reads these files (no reference to them
in SRO_Client.exe), and the server's loader trusts its own DATA copy
(version byte, offset and trailer only, no check against the meshes), so
native has no counterpart to this pairing. Indexing the 57-cell route
with a 187-cell mesh's cell would read past the table. The arena keeps
the port's portal route, and the boot log records the decision at info
level; any other refusal stays a warning.

===========================================================================
*/
package movement

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	log "github.com/sirupsen/logrus"
)

const (
	aiNavDataVersion = 1
	aiNavDataFormat  = "sro-server-dungeon-ainav"
	// aiNavPointTolerance is how far a file point may sit from the
	// midpoint the port computes for its edge: the native file stores
	// float32 and the port rotates in float64 (Donwhang's worst is 1.4e-4).
	aiNavPointTolerance = 0.01
)

// errAINavCellMismatch marks a block whose route cell count is not its
// mesh's: the file was built for another mesh.
var errAINavCellMismatch = errors.New("route cells do not match the mesh")

// aiNavKnownMismatches are the route files the v1.150 Data.pk2 ships for
// meshes it does not ship (#1979): their cell-count refusal is expected.
var aiNavKnownMismatches = map[uint16]string{
	0x8009: "the Capture the Flag arena's file gives its two rooms 57 cells; the v1.150 room meshes have 185-187",
}

// caveRouteTable is one dungeon's parsed route file.
type caveRouteTable struct {
	region  uint16
	blocks  []caveRouteBlock
	fastest []uint16 // n*n, [dest*n + cur]: the current block's portal key
}

// caveRouteBlock is one DOF block's within-block route and its points.
type caveRouteBlock struct {
	cells   int
	route   []uint32 // cells*cells, [dest*cells + cur]
	portals []caveRoutePortal
	points  [][3]float32
}

// caveRoutePortal is one doorway out of a block, keyed as the file keys it.
type caveRoutePortal struct {
	key          uint32
	cell         uint16
	landingBlock uint16
	landingPoint uint16
}

type aiNavDataJSON struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
	Files   []struct {
		RegionID   uint16 `json:"regionId"`
		SourcePath string `json:"sourcePath"`
		ByteLength int    `json:"byteLength"`
		RawBase64  string `json:"rawBase64"`
	} `json:"files"`
}

/*
================
aiNavCursor

A little-endian reader that refuses to run past the end.
================
*/
type aiNavCursor struct {
	raw []byte
	at  int
	err error
}

func (c *aiNavCursor) take(n int, what string) []byte {
	if c.err != nil {
		return nil
	}
	if n < 0 || n > len(c.raw)-c.at {
		c.err = fmt.Errorf("%s: %d bytes at %d run past the end (%d)", what, n, c.at, len(c.raw))
		return nil
	}
	b := c.raw[c.at : c.at+n]
	c.at += n
	return b
}

func (c *aiNavCursor) u8(what string) uint8 {
	if b := c.take(1, what); b != nil {
		return b[0]
	}
	return 0
}

func (c *aiNavCursor) u16(what string) uint16 {
	if b := c.take(2, what); b != nil {
		return binary.LittleEndian.Uint16(b)
	}
	return 0
}

func (c *aiNavCursor) u32(what string) uint32 {
	if b := c.take(4, what); b != nil {
		return binary.LittleEndian.Uint32(b)
	}
	return 0
}

/*
================
parseAINavData

Parses both sections and checks the file is self-consistent: version 1,
both sections name the same region and block count, block i is index i,
each block's point count equals its route header's, both trailers are
zero and the point section ends the file. Mesh agreement is checked
separately (validateAINavData).
================
*/
func parseAINavData(raw []byte) (*caveRouteTable, error) {
	c := &aiNavCursor{raw: raw}
	if version := c.u8("version"); c.err == nil && version != aiNavDataVersion {
		return nil, fmt.Errorf("version %d, want %d", version, aiNavDataVersion)
	}
	pointsAt := c.u32("point section offset")
	table := &caveRouteTable{region: c.u16("region")}
	n := int(c.u32("block count"))
	if c.err != nil {
		return nil, c.err
	}
	// Every block takes at least its 16 header bytes: bound n before
	// allocating by it.
	if n > len(raw)/16 {
		return nil, fmt.Errorf("block count %d cannot fit %d bytes", n, len(raw))
	}
	table.blocks = make([]caveRouteBlock, n)
	counts := make([]int, n)
	for i := range table.blocks {
		block := &table.blocks[i]
		if index := c.u32("block index"); c.err == nil && int(index) != i {
			return nil, fmt.Errorf("block %d: index %d", i, index)
		}
		m := int(c.u32("cell count"))
		counts[i] = int(c.u32("point count"))
		if c.err != nil {
			return nil, c.err
		}
		if m > len(raw)/4 || m*m > (len(raw)-c.at)/4 {
			return nil, fmt.Errorf("block %d: %d cells cannot fit the file", i, m)
		}
		block.cells = m
		words := c.take(m*m*4, "route words")
		block.route = make([]uint32, m*m)
		for k := range block.route {
			block.route[k] = binary.LittleEndian.Uint32(words[k*4:])
		}
		portals := int(c.u32("portal count"))
		if c.err != nil {
			return nil, c.err
		}
		if portals > (len(raw)-c.at)/10 {
			return nil, fmt.Errorf("block %d: %d portals cannot fit the file", i, portals)
		}
		block.portals = make([]caveRoutePortal, portals)
		for k := range block.portals {
			block.portals[k] = caveRoutePortal{
				key: c.u32("portal key"), cell: c.u16("portal cell"),
				landingBlock: c.u16("landing block"), landingPoint: c.u16("landing point"),
			}
		}
	}
	if c.err == nil && n*n > (len(raw)-c.at)/2 {
		return nil, fmt.Errorf("fastest table of %d blocks cannot fit the file", n)
	}
	fastest := c.take(n*n*2, "fastest table")
	table.fastest = make([]uint16, n*n)
	for k := range table.fastest {
		table.fastest[k] = binary.LittleEndian.Uint16(fastest[k*2:])
	}
	if trailer := c.u32("route trailer"); c.err == nil && trailer != 0 {
		return nil, fmt.Errorf("route trailer %d, want 0", trailer)
	}
	if c.err != nil {
		return nil, c.err
	}
	if int(pointsAt) != c.at {
		return nil, fmt.Errorf("point section offset %d, route section ends at %d", pointsAt, c.at)
	}

	if region := c.u16("point region"); c.err == nil && region != table.region {
		return nil, fmt.Errorf("point section region 0x%04X, route section 0x%04X", region, table.region)
	}
	if pn := int(c.u32("point block count")); c.err == nil && pn != n {
		return nil, fmt.Errorf("point section has %d blocks, route section %d", pn, n)
	}
	for i := range table.blocks {
		count := int(c.u32("point count"))
		if c.err != nil {
			return nil, c.err
		}
		if count != counts[i] {
			return nil, fmt.Errorf("block %d: %d points, route header says %d", i, count, counts[i])
		}
		if count > (len(raw)-c.at)/12 {
			return nil, fmt.Errorf("block %d: %d points cannot fit the file", i, count)
		}
		points := c.take(count*12, "points")
		block := &table.blocks[i]
		block.points = make([][3]float32, count)
		for k := range block.points {
			for axis := 0; axis < 3; axis++ {
				block.points[k][axis] = math.Float32frombits(binary.LittleEndian.Uint32(points[k*12+axis*4:]))
			}
		}
	}
	if trailer := c.u32("point trailer"); c.err == nil && trailer != 0 {
		return nil, fmt.Errorf("point trailer %d, want 0", trailer)
	}
	if c.err != nil {
		return nil, c.err
	}
	if c.at != len(raw) {
		return nil, fmt.Errorf("%d bytes after the point section", len(raw)-c.at)
	}
	return table, nil
}

/*
================
validateAINavData

Checks a parsed file against the dungeon's own v1.150 nav meshes, block
by DOF ordinal: one block per resident block, each with a single mesh;
the cell count equals the mesh's; the point count equals its internal
edges plus its portal outline edges, and each point is its edge's
midpoint placed by the block's transform, at the block's height; every route half, portal
cell and landing is in range, and every fastest cell between two blocks
is a portal of the current one. Anything else means the file
was built for another mesh, and the native route over it would walk the
wrong cells.
================
*/
func validateAINavData(table *caveRouteTable, surface *dungeonSpawnSurface) error {
	n := len(table.blocks)
	if n != len(surface.objects) {
		return fmt.Errorf("%d route blocks, the dungeon has %d resident blocks", n, len(surface.objects))
	}
	for i := range table.blocks {
		block := &table.blocks[i]
		index, ok := surface.ordinalIndex[i]
		if !ok {
			return fmt.Errorf("route block %d has no resident block at that ordinal", i)
		}
		object := &surface.objects[index]
		if len(object.meshes) != 1 {
			return fmt.Errorf("block %d: %d nav meshes, want 1", i, len(object.meshes))
		}
		mesh := object.meshes[0]
		if block.cells != mesh.cellCount() {
			return fmt.Errorf("block %d: %d route cells, the mesh has %d: %w", i, block.cells, mesh.cellCount(), errAINavCellMismatch)
		}
		edges := aiNavPointEdges(object)
		if len(block.points) != len(edges) {
			return fmt.Errorf("block %d: %d points, the mesh has %d internal and portal edges", i, len(block.points), len(edges))
		}
		for k, edge := range edges {
			a := portalPoint(object.placement, mesh, int(edge[0]))
			b := portalPoint(object.placement, mesh, int(edge[1]))
			want := [3]float64{(a[0] + b[0]) / 2, object.placement.y, (a[2] + b[2]) / 2}
			got := block.points[k]
			for axis := 0; axis < 3; axis++ {
				if math.Abs(float64(got[axis])-want[axis]) > aiNavPointTolerance {
					return fmt.Errorf("block %d: point %d is %v, its edge's midpoint is %v", i, k, got, want)
				}
			}
		}
		for k, word := range block.route {
			if int(word>>16) >= len(block.points) || int(word&0xFFFF) >= len(block.points) {
				return fmt.Errorf("block %d: route word %d (0x%08X) indexes past %d points", i, k, word, len(block.points))
			}
		}
		for _, portal := range block.portals {
			if int(portal.key) >= block.cells || int(portal.cell) >= block.cells {
				return fmt.Errorf("block %d: portal %+v names a cell past %d", i, portal, block.cells)
			}
			if int(portal.landingBlock) >= n || int(portal.landingPoint) >= len(table.blocks[portal.landingBlock].points) {
				return fmt.Errorf("block %d: portal %+v lands past the table", i, portal)
			}
		}
	}
	// The route reads the fastest table only between different blocks;
	// the diagonal is never read (Donwhang's holds 0, and 376 at [0]).
	for k, cell := range table.fastest {
		cur, dest := k%n, k/n
		if _, ok := table.blocks[cur].portal(uint32(cell)); cur != dest && !ok {
			return fmt.Errorf("fastest[%d] = %d is no portal of block %d", k, cell, cur)
		}
	}
	return nil
}

/*
================
caveRouteBlock.portal

The block's portal under a fastest-table key.
================
*/
func (block *caveRouteBlock) portal(key uint32) (caveRoutePortal, bool) {
	for _, portal := range block.portals {
		if portal.key == key {
			return portal, true
		}
	}
	return caveRoutePortal{}, false
}

/*
================
aiNavPointEdges

The edges a block's points stand on, in file order: every internal edge,
then every outline edge linked to another block (resolveDungeonLinks), in
outline order. A doorway-flagged edge that meets no neighbour carries no
point.
================
*/
func aiNavPointEdges(object *resolvedObjectNav) [][2]uint16 {
	mesh := object.meshes[0]
	linked := make(map[int]bool, len(object.placement.links))
	for _, link := range object.placement.links {
		linked[link.edge] = true
	}
	edges := make([][2]uint16, 0, len(mesh.internal.vertA)+len(mesh.outline.vertA))
	for e := range mesh.internal.vertA {
		edges = append(edges, [2]uint16{mesh.internal.vertA[e], mesh.internal.vertB[e]})
	}
	for e := range mesh.outline.vertA {
		if linked[e] {
			edges = append(edges, [2]uint16{mesh.outline.vertA[e], mesh.outline.vertB[e]})
		}
	}
	return edges
}

/*
================
portalPoint

A mesh vertex in region coordinates, placed as portalEndpoint
(dungeon_links.go) places outline endpoints, without the float32 rounding.
================
*/
func portalPoint(p objectNavPlacement, m *objectNavMesh, vertex int) [3]float64 {
	x, y, z := float64(m.vertices[vertex*3]), float64(m.vertices[vertex*3+1]), float64(m.vertices[vertex*3+2])
	c, s := math.Cos(p.yaw), math.Sin(p.yaw)
	return [3]float64{c*x - s*z + p.x, y + p.y, s*x + c*z + p.z}
}

/*
================
loadAINavData

Decodes, parses and validates every packed route file, keeping the
tables that pass; each refusal is logged and that dungeon keeps the
port's own route. Returns nil when the bundle packs none.
================
*/
func (v *WaterValidator) loadAINavData(surfaces map[uint16]*dungeonSpawnSurface) map[uint16]*caveRouteTable {
	if v.aiNavDataPath == "" {
		return nil
	}
	packed := &aiNavDataJSON{}
	if !v.readJSON(v.aiNavDataPath, packed) {
		log.Warn("movement: dungeon route tables unavailable; every cave keeps the port's portal route")
		return nil
	}
	if packed.Format != aiNavDataFormat || packed.Version != aiNavDataVersion {
		log.Warnf("movement: dungeon route tables are %s v%d; every cave keeps the port's portal route", packed.Format, packed.Version)
		return nil
	}
	tables := make(map[uint16]*caveRouteTable, len(packed.Files))
	for _, file := range packed.Files {
		table, err := decodeAINavFile(file.RegionID, file.ByteLength, file.RawBase64, surfaces[file.RegionID])
		if err != nil {
			if reason, known := aiNavKnownRefusal(file.RegionID, err); known {
				log.Infof("movement: %s not used (%s, #1979); 0x%04X keeps the port's portal route", file.SourcePath, reason, file.RegionID)
				continue
			}
			log.Warnf("movement: %s refused (%v); 0x%04X keeps the port's portal route", file.SourcePath, err, file.RegionID)
			continue
		}
		tables[file.RegionID] = table
	}
	return tables
}

/*
================
aiNavKnownRefusal

Whether a refusal is the recorded mismatch for its region: only a cell
count disagreement counts, so a file that fails another way (a new data
build, a parse error) still warns.
================
*/
func aiNavKnownRefusal(region uint16, err error) (string, bool) {
	reason, known := aiNavKnownMismatches[region]
	return reason, known && errors.Is(err, errAINavCellMismatch)
}

/*
================
decodeAINavFile
================
*/
func decodeAINavFile(region uint16, byteLength int, rawBase64 string, surface *dungeonSpawnSurface) (*caveRouteTable, error) {
	raw, err := base64.StdEncoding.DecodeString(rawBase64)
	if err != nil || len(raw) != byteLength {
		return nil, fmt.Errorf("failed its length/base64 contract")
	}
	table, err := parseAINavData(raw)
	if err != nil {
		return nil, err
	}
	if table.region != region {
		return nil, fmt.Errorf("file names region 0x%04X", table.region)
	}
	if surface == nil {
		return nil, fmt.Errorf("no dungeon surface to validate against")
	}
	if err := validateAINavData(table, surface); err != nil {
		return nil, err
	}
	return table, nil
}
