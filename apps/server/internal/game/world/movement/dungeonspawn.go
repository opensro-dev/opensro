/*
===========================================================================

dungeonspawn.go - the dungeon resident surfaces and their floor heights

Loads each dungeon's DOF blocks and packaged nav meshes into a surface
(newDungeonSpawnSurface fixes each block's rotation, reach and cell boxes
once) and answers heightAt for spawns, steps and clips.

===========================================================================
*/
package movement

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	worldgeom "opensro.online/server/internal/game/world"
	"strings"

	log "github.com/sirupsen/logrus"
)

const dungeonResourcesPublicPath = "assets/world/dungeon/dungeon-resources.json"

// dungeonSpawnSurface is the server-side counterpart of one
// CRTNavMeshDungeon resident host. DOF blocks own transforms; their BSR
// paths resolve to the exact packaged BMS object-nav cells.
type dungeonSpawnSurface struct {
	messageGrid worldgeom.DungeonMessageGrid
	blocks      []dungeonSpawnBlock
	objects     []resolvedObjectNav
	// ordinalIndex maps a block ordinal to its index in objects, built once
	// for the passage resolve every chord runs.
	ordinalIndex map[int]int
	// blockNextHop is the cave route's per-floor portal table (cave_route.go):
	// for each (from block, destination block) the doorway to leave by and
	// the cell it lands on, built once here because the floors are static.
	blockNextHop []caveNextHop
}

type dungeonObstacle struct{ x, y, z, radiusSquared float64 }

type dungeonSpawnBlock struct {
	obstacles []dungeonObstacle
	ordinal   int
	connected []int
	x, y, z   float64
	yaw       float64
	meshes    []*objectNavMesh
	// cosYaw, sinYaw and reach are fixed at construction
	// (newDungeonSpawnSurface): the rotation every height query uses, and
	// the box outside which no mesh of the block can hold a point.
	cosYaw, sinYaw float64
	reach          objectReach
}

/*
================
newDungeonSpawnSurface

A resident host's surface from its blocks: each block's rotation and reach
fixed once, and the object-nav set with its links resolved. Every surface
is built here, so heightAt and the chord scans never see a block without
them.
================
*/
func newDungeonSpawnSurface(grid worldgeom.DungeonMessageGrid, blocks []dungeonSpawnBlock) *dungeonSpawnSurface {
	surface := &dungeonSpawnSurface{messageGrid: grid, blocks: blocks}
	surface.objects = resolveDungeonLinks(blocks)
	surface.ordinalIndex = make(map[int]int, len(surface.objects))
	for i := range surface.objects {
		surface.ordinalIndex[surface.objects[i].placement.ordinal] = i
	}
	surface.blockNextHop = surface.buildBlockNextHop()
	for i := range blocks {
		for _, mesh := range blocks[i].meshes {
			mesh.buildCellBoxes()
		}
		blocks[i].cosYaw, blocks[i].sinYaw = surface.objects[i].cosYaw, surface.objects[i].sinYaw
		blocks[i].reach = surface.objects[i].reach
	}
	return surface
}

type dungeonResourceManifestJSON struct {
	Entries []struct {
		SectorID       uint16 `json:"sectorId"`
		NormalizedName string `json:"normalizedName"`
	} `json:"entries"`
	Resources []struct {
		NormalizedName string `json:"normalizedName"`
		ByteLength     int    `json:"byteLength"`
		RawBase64      string `json:"rawBase64"`
	} `json:"resources"`
	NavResources struct {
		Bsr []struct {
			SourcePath        string `json:"sourcePath"`
			RenderMeshSection struct {
				Paths []string `json:"paths"`
			} `json:"renderMeshSection"`
			MeshPaths []string `json:"meshPaths"`
		} `json:"bsr"`
		Meshes []struct {
			SourcePath string `json:"sourcePath"`
			objectNavMeshWireJSON
		} `json:"meshes"`
	} `json:"navResources"`
}

type dungeonDofBlock struct {
	obstacles  []dungeonObstacle
	connected  []int
	path       string
	x, y, z    float64
	yawRadians float64
}

// dungeonSpawnHeightAt selects the walkable dungeon resident plane nearest
// the authored nest Y. The one-shot manifest load is immutable for the
// validator lifetime, matching every other movement asset cache.
func (v *WaterValidator) dungeonSpawnHeightAt(
	regionID uint16,
	x, authoredY, z float64,
) (float64, bool) {
	v.dungeonSpawnOnce.Do(func() {
		v.dungeonSpawnSurfaces = v.loadDungeonSpawnSurfaces()
	})
	surface := v.dungeonSpawnSurfaces[regionID]
	if surface == nil {
		return 0, false
	}
	return surface.heightAt(x, authoredY, z)
}

func (v *WaterValidator) loadDungeonSpawnSurfaces() map[uint16]*dungeonSpawnSurface {
	manifest := &dungeonResourceManifestJSON{}
	if !v.readJSON(v.dungeonResourcesPath, manifest) {
		log.Warn("movement: dungeon resident-nav manifest unavailable; generated dungeon populations remain inactive")
		return nil
	}

	meshesByPath := make(map[string][]*objectNavMesh, len(manifest.NavResources.Meshes))
	for _, row := range manifest.NavResources.Meshes {
		meshes := decodeObjectNavMeshes(row.objectNavMeshWireJSON)
		if len(meshes) > 0 {
			meshesByPath[normalizeDungeonResourcePath(row.SourcePath)] = meshes
		}
	}
	meshPathsByBsr := make(map[string][]string, len(manifest.NavResources.Bsr))
	for _, row := range manifest.NavResources.Bsr {
		paths := row.RenderMeshSection.Paths
		if len(paths) == 0 {
			paths = row.MeshPaths
		}
		meshPathsByBsr[normalizeDungeonResourcePath(row.SourcePath)] = paths
	}

	dofBytesByName := make(map[string][]byte, len(manifest.Resources))
	for _, resource := range manifest.Resources {
		raw, err := base64.StdEncoding.DecodeString(resource.RawBase64)
		if err != nil || len(raw) != resource.ByteLength {
			log.Warnf("movement: dungeon DOF %s failed its manifest length/base64 contract", resource.NormalizedName)
			continue
		}
		dofBytesByName[normalizeDungeonResourcePath(resource.NormalizedName)] = raw
	}

	surfaces := make(map[uint16]*dungeonSpawnSurface, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		raw := dofBytesByName[normalizeDungeonResourcePath(entry.NormalizedName)]
		blocks, err := parseDungeonDofBlocks(raw)
		if err != nil {
			log.Warnf("movement: dungeon DOF 0x%04X is invalid: %v", entry.SectorID, err)
			continue
		}
		grid, err := worldgeom.ParseDungeonMessageGrid(raw)
		if err != nil {
			log.Warnf("movement: dungeon DOF 0x%04X message grid is invalid: %v", entry.SectorID, err)
			continue
		}
		var resident []dungeonSpawnBlock
		for ordinal, block := range blocks {
			var meshes []*objectNavMesh
			for _, meshPath := range meshPathsByBsr[normalizeDungeonResourcePath(block.path)] {
				meshes = append(meshes, meshesByPath[normalizeDungeonResourcePath(meshPath)]...)
			}
			if len(meshes) == 0 {
				log.Warnf("movement: dungeon DOF resident %s has no packaged nav mesh", block.path)
				continue
			}
			resident = append(resident, dungeonSpawnBlock{
				obstacles: block.obstacles, ordinal: ordinal, connected: block.connected, x: block.x, y: block.y, z: block.z,
				yaw: block.yawRadians, meshes: meshes,
			})
		}
		if len(resident) > 0 {
			surfaces[entry.SectorID] = newDungeonSpawnSurface(grid, resident)
		}
	}
	return surfaces
}

func (surface *dungeonSpawnSurface) heightAt(x, authoredY, z float64) (float64, bool) {
	bestY := 0.0
	bestDelta := math.Inf(1)
	for i := range surface.blocks {
		block := &surface.blocks[i]
		// Outside the block's reach no mesh's local bounds can hold the
		// point, so the per-mesh tests below would all reject it.
		if block.reach.missedBy(x, z, x, z) {
			continue
		}
		dx, dz := x-block.x, z-block.z
		cosYaw, sinYaw := block.cosYaw, block.sinYaw
		localX := cosYaw*dx + sinYaw*dz
		localZ := -sinYaw*dx + cosYaw*dz
		localHintY := authoredY - block.y

		for _, mesh := range block.meshes {
			if localX < mesh.minX || localX > mesh.maxX ||
				localZ < mesh.minZ || localZ > mesh.maxZ ||
				localHintY-mesh.maxY > bestDelta ||
				mesh.minY-localHintY > bestDelta {
				continue
			}
			for cell := 0; cell < mesh.cellCount(); cell++ {
				if mesh.cellMissedBy(cell, localX, localX, localZ, localZ) {
					continue
				}
				localY, inside := objectCellPlaneYAt(mesh, cell, localX, localZ)
				if !inside {
					continue
				}
				worldY := localY + block.y
				delta := math.Abs(worldY - authoredY)
				if delta < bestDelta {
					bestY, bestDelta = worldY, delta
				}
			}
		}
	}
	return bestY, !math.IsInf(bestDelta, 1)
}

func normalizeDungeonResourcePath(path string) string {
	return strings.ToLower(strings.TrimLeft(
		strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"),
		"/",
	))
}

func parseDungeonDofBlocks(raw []byte) ([]dungeonDofBlock, error) {
	cursor := dungeonDofCursor{raw: raw}
	signature, err := cursor.bytes(12, "signature")
	if err != nil {
		return nil, err
	}
	if string(signature) != "JMXVDOF 0101" {
		return nil, fmt.Errorf("bad signature %q", signature)
	}
	offsets := [8]uint32{}
	for i := range offsets {
		if offsets[i], err = cursor.u32("header offset"); err != nil {
			return nil, err
		}
	}
	if _, err = cursor.u32("general type"); err != nil {
		return nil, err
	}
	if _, err = cursor.str("general name"); err != nil {
		return nil, err
	}
	if err = cursor.skip(4+4+2, "general tail"); err != nil {
		return nil, err
	}
	if err = cursor.seek(int(offsets[7]), "bounding box"); err != nil {
		return nil, err
	}
	if err = cursor.skip(12*4, "bounding boxes"); err != nil {
		return nil, err
	}
	if err = cursor.seek(int(offsets[0]), "block section"); err != nil {
		return nil, err
	}
	blockCount, err := cursor.count("block count", 4096)
	if err != nil {
		return nil, err
	}
	blocks := make([]dungeonDofBlock, 0, blockCount)
	for blockIndex := 0; blockIndex < blockCount; blockIndex++ {
		block, err := cursor.block(blockIndex)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	if cursor.off != int(offsets[2]) {
		return nil, fmt.Errorf("block section consumed %d, expected grid offset %d", cursor.off, offsets[2])
	}
	for _, block := range blocks {
		for _, neighbor := range block.connected {
			if neighbor < 0 || neighbor >= len(blocks) {
				return nil, fmt.Errorf("invalid dungeon neighbor %d", neighbor)
			}
		}
	}
	return blocks, nil
}

type dungeonDofCursor struct {
	raw []byte
	off int
}

func (c *dungeonDofCursor) ensure(n int, label string) error {
	if n < 0 || c.off < 0 || c.off > len(c.raw) || n > len(c.raw)-c.off {
		return fmt.Errorf("truncated %s at %d/%d", label, c.off, len(c.raw))
	}
	return nil
}

func (c *dungeonDofCursor) seek(offset int, label string) error {
	if offset < c.off || offset > len(c.raw) {
		return fmt.Errorf("invalid %s offset %d", label, offset)
	}
	c.off = offset
	return nil
}

func (c *dungeonDofCursor) skip(n int, label string) error {
	if err := c.ensure(n, label); err != nil {
		return err
	}
	c.off += n
	return nil
}

func (c *dungeonDofCursor) bytes(n int, label string) ([]byte, error) {
	if err := c.ensure(n, label); err != nil {
		return nil, err
	}
	value := c.raw[c.off : c.off+n]
	c.off += n
	return value, nil
}

func (c *dungeonDofCursor) u8(label string) (byte, error) {
	raw, err := c.bytes(1, label)
	if err != nil {
		return 0, err
	}
	return raw[0], nil
}

func (c *dungeonDofCursor) u32(label string) (uint32, error) {
	raw, err := c.bytes(4, label)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(raw), nil
}

func (c *dungeonDofCursor) f32(label string) (float64, error) {
	bits, err := c.u32(label)
	if err != nil {
		return 0, err
	}
	value := float64(math.Float32frombits(bits))
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("%s is not finite", label)
	}
	return value, nil
}

func (c *dungeonDofCursor) count(label string, maximum uint32) (int, error) {
	value, err := c.u32(label)
	if err != nil {
		return 0, err
	}
	if value > maximum {
		return 0, fmt.Errorf("%s %d exceeds %d", label, value, maximum)
	}
	return int(value), nil
}

func (c *dungeonDofCursor) str(label string) (string, error) {
	length, err := c.count(label+" length", uint32(len(c.raw)))
	if err != nil {
		return "", err
	}
	raw, err := c.bytes(length, label)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (c *dungeonDofCursor) skipWords(label string) error {
	count, err := c.count(label+" count", 1<<20)
	if err != nil {
		return err
	}
	return c.skip(count*4, label)
}

func (c *dungeonDofCursor) block(index int) (dungeonDofBlock, error) {
	prefix := fmt.Sprintf("block %d", index)
	path, err := c.str(prefix + " path")
	if err != nil {
		return dungeonDofBlock{}, err
	}
	if _, err = c.str(prefix + " name"); err != nil {
		return dungeonDofBlock{}, err
	}
	if err = c.skip(4, prefix+" unknown 0"); err != nil {
		return dungeonDofBlock{}, err
	}
	x, err := c.f32(prefix + " x")
	if err != nil {
		return dungeonDofBlock{}, err
	}
	y, err := c.f32(prefix + " y")
	if err != nil {
		return dungeonDofBlock{}, err
	}
	z, err := c.f32(prefix + " z")
	if err != nil {
		return dungeonDofBlock{}, err
	}
	yaw, err := c.f32(prefix + " yaw")
	if err != nil {
		return dungeonDofBlock{}, err
	}
	if err = c.skip(4+6*4+4, prefix+" placement tail"); err != nil {
		return dungeonDofBlock{}, err
	}
	if err = c.skip(4+3*4, prefix+" fog"); err != nil {
		return dungeonDofBlock{}, err
	}
	heightFog, err := c.u8(prefix + " height-fog flag")
	if err != nil {
		return dungeonDofBlock{}, err
	}
	if heightFog != 0 {
		if err = c.skip(4*4, prefix+" height fog"); err != nil {
			return dungeonDofBlock{}, err
		}
	}
	optionalVectors, err := c.u8(prefix + " optional-vector flag")
	if err != nil {
		return dungeonDofBlock{}, err
	}
	if optionalVectors != 0 {
		if err = c.skip(7*4, prefix+" optional vectors"); err != nil {
			return dungeonDofBlock{}, err
		}
	}
	if _, err = c.str(prefix + " unknown string"); err != nil {
		return dungeonDofBlock{}, err
	}
	if err = c.skip(2*4, prefix+" room/floor"); err != nil {
		return dungeonDofBlock{}, err
	}
	nc, err := c.count(prefix+" connected blocks", 4096)
	if err != nil {
		return dungeonDofBlock{}, err
	}
	connected := make([]int, nc)
	for i := range connected {
		n, e := c.u32(prefix + " connected block")
		if e != nil {
			return dungeonDofBlock{}, e
		}
		connected[i] = int(n)
	}
	if err = c.skipWords(prefix + " visible blocks"); err != nil {
		return dungeonDofBlock{}, err
	}

	objectCount, err := c.count(prefix+" object count", 1<<20)
	if err != nil {
		return dungeonDofBlock{}, err
	}
	if _, err = c.u32(prefix + " collision object count"); err != nil {
		return dungeonDofBlock{}, err
	}
	var obstacles []dungeonObstacle
	for objectIndex := 0; objectIndex < objectCount; objectIndex++ {
		objectPrefix := fmt.Sprintf("%s object %d", prefix, objectIndex)
		if _, err = c.str(objectPrefix + " name"); err != nil {
			return dungeonDofBlock{}, err
		}
		if _, err = c.str(objectPrefix + " path"); err != nil {
			return dungeonDofBlock{}, err
		}
		px, e := c.f32(objectPrefix + " x")
		if e != nil {
			return dungeonDofBlock{}, e
		}
		py, e := c.f32(objectPrefix + " y")
		if e != nil {
			return dungeonDofBlock{}, e
		}
		pz, e := c.f32(objectPrefix + " z")
		if e != nil {
			return dungeonDofBlock{}, e
		}
		if e = c.skip(24, objectPrefix+" rotation/scale"); e != nil {
			return dungeonDofBlock{}, e
		}
		flags, flagsErr := c.u32(objectPrefix + " flags")
		if flagsErr != nil {
			return dungeonDofBlock{}, flagsErr
		}
		if err = c.skip(4, objectPrefix+" tail"); err != nil {
			return dungeonDofBlock{}, err
		}
		radius, e := c.f32(objectPrefix + " radius squared")
		if e != nil {
			return dungeonDofBlock{}, e
		}
		if flags&2 != 0 {
			if radius < 0 {
				return dungeonDofBlock{}, fmt.Errorf("negative collision radius")
			}
			obstacles = append(obstacles, dungeonObstacle{px, py, pz, radius})
		}
		if flags&4 != 0 {
			if _, err = c.u32(objectPrefix + " water color"); err != nil {
				return dungeonDofBlock{}, err
			}
		}
	}
	lightCount, err := c.count(prefix+" light count", 1<<20)
	if err != nil {
		return dungeonDofBlock{}, err
	}
	for lightIndex := 0; lightIndex < lightCount; lightIndex++ {
		if _, err = c.str(fmt.Sprintf("%s light %d name", prefix, lightIndex)); err != nil {
			return dungeonDofBlock{}, err
		}
		if err = c.skip(15*4, fmt.Sprintf("%s light %d fields", prefix, lightIndex)); err != nil {
			return dungeonDofBlock{}, err
		}
	}
	return dungeonDofBlock{obstacles: obstacles, connected: connected, path: path, x: x, y: y, z: z, yawRadians: yaw}, nil
}
