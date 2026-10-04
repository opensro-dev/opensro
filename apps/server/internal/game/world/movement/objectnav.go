package movement

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"slices"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

// Object-nav sealed-deck detection for the enter-world stranded-spawn rescue.
//
// The Jangan walkway incident (region 0x61A7, spawn y~3.04 on the
// cj_pub03_floor deck over walkable terrain at y=0): a character persisted
// onto an above-terrain OBJECT surface whose payload lane is data-sealed
// (no open outline edge reachable) is trapped forever - the terrain
// walkability plane under the deck is walkable, so the tile-based rescue
// never fires. This file gives RelocateStrandedSpawn the object-nav view:
//
//  1. decode the region bundle's nav object placements (assetId + position
//     + yaw) and the BMS offset-7 object-nav payloads the placements
//     resolve to through the object-resource index - the same shipped
//     assets the client decodes (nativeObjectNavPayload.ts, sub_4265b0);
//  2. mirror the native nearest-Y owner arbitration (sub_403d20): the
//     spawn stands on the deck only when a containing object cell's plane
//     Y is closer to the persisted Y than the terrain height is;
//  3. run the sealed-lane BFS over the payload's own edge flags
//     (sub_428930 side-block bits: crossing an internal edge from srcCell
//     is blocked by 0x2, from dstCell by 0x1): the lane is SEALED when no
//     open outline edge (flags 0x0) is reachable. Sealed => trapped =>
//     stranded; a lane with a reachable open exit (bridge decks) is a
//     LEGAL elevated stand and must never rescue - not even through the
//     TILE verdict, because the tile under a bridge span is routinely
//     blocked seabed the character is not standing on.
//
// Everything here fails open (absent/undecodable assets => not trapped),
// matching the validator's degrade-to-accept posture.
//
// Beyond the rescue, this file is the OBJECT-NAV VIEW of the movement
// planes (clipreplicate-wave Ruling 27's owed work item): the deck-stand
// arbitration answers pathguard/clip blocked-tile overrides through
// worldPointOnObjectDeck, and objectChordFirstContact gives the clip its
// object-class first contact (segment vs blocking object edges; native
// evidence in that section's banner).

// objectNavPlacement is one nav object instance from the region bundle's
// navmesh region entry: object-local payload space maps into the region
// frame by yaw rotation + translation (makeObjectTransformMatrices).
type objectNavPlacement struct {
	terrainCells  *[][4]float32
	assetID       int
	x, y, z       float64
	yaw           float64
	linkEdgeCount int
	ordinal       int
	links         []objectNavLink
}

// objectNavEdges is one decoded edge group (columnar, like the wire):
// group 1 = outline (perimeter), group 2 = internal (cell<->cell).
// dstCell 0xffff = no neighbor. vertA/vertB carry the edge segment's
// vertex indices - the chord clip (objectMeshChordContact) intersects the
// move segment against them.
type objectNavLink struct{ target, targetEdge, edge int }

type objectNavEdges struct {
	vertA   []uint16
	vertB   []uint16
	srcCell []uint16
	dstCell []uint16
	flags   []byte
}

// objectNavMesh is the slice of a decoded BMS offset-7 object-nav payload
// the movement planes need: cell triangles over object-local vertices plus
// both edge groups. The grid tail, event names, region links and trigger
// bytes are parsed (the payload must exact-consume) but not retained.
// min/max bound the vertex cloud (object-local) so per-move queries can
// reject a whole mesh in a few compares.
type objectNavMesh struct {
	vertices         []float32 // xyz triplets, object-local native units
	vertexDirections []byte    // 43EC80 boundary-normal table indices
	cellA            []uint16
	cellB            []uint16
	cellC            []uint16
	outline          objectNavEdges
	internal         objectNavEdges

	minX, minY, minZ float64
	maxX, maxY, maxZ float64
}

func (m *objectNavMesh) cellCount() int {
	return len(m.cellA)
}

// objectResourceIndex maps a placement's assetId to the decodable mesh
// JSON public paths (bsr renderMeshSection paths resolved through the
// meshFiles source->public table, the client's exact preference order).
type objectResourceIndex struct {
	meshPublicPathsByObjectID map[int][]string
}

// ---- payload decode (port of decodeNativeObjectNavPayloadBytes) ----

const (
	objectNavVertexWireStride = 13 // f32 x,y,z + u8 boundaryDirection
	objectNavCellWireStride   = 8  // u16 v0,v1,v2 + u16 eventZoneWord
	objectNavEdgeWireStride   = 9  // u16 vA,vB + u16 srcCell,dstCell + u8 flag

	// Vertex and cell references are u16 on the native wire. Counts beyond
	// this cannot be addressed by any valid record and are rejected before
	// allocation.
	objectNavMaxVertices = 1 << 16
	objectNavMaxCells    = 1 << 16

	objectNavFlagEdgeExtraByte = 1 // flags&1 -> +1 byte per edge
	objectNavFlagCellExtraByte = 2 // flags&2 -> +1 byte per cell
	objectNavFlagEventNames    = 4 // flags&4 -> event-name section
	objectNavKnownFlags        = objectNavFlagEdgeExtraByte | objectNavFlagCellExtraByte | objectNavFlagEventNames
)

// objectNavCursor is the running wire offset over the preserved slice.
type objectNavCursor struct {
	raw []byte
	off int
}

func (c *objectNavCursor) ensure(n int, label string) error {
	if n < 0 || c.off < 0 || c.off > len(c.raw) || n > len(c.raw)-c.off {
		return fmt.Errorf("object nav payload truncated in %s at %d/%d", label, c.off, len(c.raw))
	}
	return nil
}

func (c *objectNavCursor) countBytes(count uint32, stride int, limit uint32, label string) (int, error) {
	if stride <= 0 || count > limit {
		return 0, fmt.Errorf("object nav %s count %d exceeds limit %d", label, count, limit)
	}
	remaining := len(c.raw) - c.off
	if uint64(count) > uint64(remaining/stride) {
		return 0, fmt.Errorf("object nav payload truncated in %s at %d/%d", label, c.off, len(c.raw))
	}
	return int(count) * stride, nil
}

func (c *objectNavCursor) u32(label string) (uint32, error) {
	if err := c.ensure(4, label); err != nil {
		return 0, err
	}
	v := binary.LittleEndian.Uint32(c.raw[c.off:])
	c.off += 4
	return v, nil
}

// decodeObjectNavEdgeGroup reads one edge group; range checks and the
// stride mirror the client decoder (a mismatch is a hard error and the
// caller drops the mesh - never a silent partial).
func decodeObjectNavEdgeGroup(c *objectNavCursor, label string, stride, vertexCount, cellCount int) (objectNavEdges, error) {
	count, err := c.u32(label + " edge count")
	if err != nil {
		return objectNavEdges{}, err
	}
	// A triangle cell contributes at most three edges to either group.
	// Binding the count to the already-validated cell population prevents
	// allocation amplification before the per-edge range checks run.
	recordBytes, err := c.countBytes(count, stride, uint32(cellCount)*3, label+" edge records")
	if err != nil {
		return objectNavEdges{}, err
	}
	countInt := int(count)
	edges := objectNavEdges{
		vertA:   make([]uint16, countInt),
		vertB:   make([]uint16, countInt),
		srcCell: make([]uint16, countInt),
		dstCell: make([]uint16, countInt),
		flags:   make([]byte, countInt),
	}
	for i := 0; i < countInt; i++ {
		base := c.off + i*stride
		vA := binary.LittleEndian.Uint16(c.raw[base:])
		vB := binary.LittleEndian.Uint16(c.raw[base+2:])
		src := binary.LittleEndian.Uint16(c.raw[base+4:])
		dst := binary.LittleEndian.Uint16(c.raw[base+6:])
		if int(vA) >= vertexCount || int(vB) >= vertexCount {
			return objectNavEdges{}, fmt.Errorf("object nav %s edge %d vertex out of range", label, i)
		}
		if int(src) >= cellCount || (dst != 0xffff && int(dst) >= cellCount) {
			return objectNavEdges{}, fmt.Errorf("object nav %s edge %d cell out of range", label, i)
		}
		edges.vertA[i] = vA
		edges.vertB[i] = vB
		edges.srcCell[i] = src
		edges.dstCell[i] = dst
		edges.flags[i] = c.raw[base+8]
	}
	c.off += recordBytes
	return edges, nil
}

// decodeObjectNavPayload decodes a preserved BMS offset-7 object-nav slice
// (sub_4265b0 CRTNavMeshObj_ReadFromArchive grammar; byte-exact port of
// the client's decodeNativeObjectNavPayloadBytes). navFlags is the BMS
// headerOffsets[11] word gating the optional record bytes. The payload
// must consume exactly to EOF or the decode fails.
func decodeObjectNavPayload(raw []byte, navFlags int) (*objectNavMesh, error) {
	c := &objectNavCursor{raw: raw}
	if navFlags < 0 || navFlags&^objectNavKnownFlags != 0 {
		return nil, fmt.Errorf("object nav flags 0x%x contain unsupported bits", navFlags)
	}

	// 1. Vertices: { f32 x,y,z; u8 boundaryDirection }.
	vertexCount, err := c.u32("vertex count")
	if err != nil {
		return nil, err
	}
	vertexBytes, err := c.countBytes(vertexCount, objectNavVertexWireStride, objectNavMaxVertices, "vertex records")
	if err != nil {
		return nil, err
	}
	vertexCountInt := int(vertexCount)
	vertices := make([]float32, vertexCountInt*3)
	vertexDirections := make([]byte, vertexCountInt)
	for i := 0; i < vertexCountInt; i++ {
		base := c.off + i*objectNavVertexWireStride
		vertexDirections[i] = c.raw[base+12]
		for axis := 0; axis < 3; axis++ {
			value := math.Float32frombits(binary.LittleEndian.Uint32(c.raw[base+axis*4:]))
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return nil, fmt.Errorf("object nav vertex %d axis %d is not finite", i, axis)
			}
			vertices[i*3+axis] = value
		}
	}
	c.off += vertexBytes

	// 2. Cells: { u16 v0,v1,v2; u16 eventZoneWord } (+u8 iff navFlags&2).
	cellStride := objectNavCellWireStride
	if navFlags&objectNavFlagCellExtraByte != 0 {
		cellStride++
	}
	cellCount, err := c.u32("cell count")
	if err != nil {
		return nil, err
	}
	cellBytes, err := c.countBytes(cellCount, cellStride, objectNavMaxCells, "cell records")
	if err != nil {
		return nil, err
	}
	cellCountInt := int(cellCount)
	mesh := &objectNavMesh{
		vertices:         vertices,
		vertexDirections: vertexDirections,
		cellA:            make([]uint16, cellCountInt),
		cellB:            make([]uint16, cellCountInt),
		cellC:            make([]uint16, cellCountInt),
	}
	for i := 0; i < cellCountInt; i++ {
		base := c.off + i*cellStride
		v0 := binary.LittleEndian.Uint16(c.raw[base:])
		v1 := binary.LittleEndian.Uint16(c.raw[base+2:])
		v2 := binary.LittleEndian.Uint16(c.raw[base+4:])
		if uint32(v0) >= vertexCount || uint32(v1) >= vertexCount || uint32(v2) >= vertexCount {
			return nil, fmt.Errorf("object nav cell %d vertex out of range", i)
		}
		mesh.cellA[i] = v0
		mesh.cellB[i] = v1
		mesh.cellC[i] = v2
	}
	c.off += cellBytes

	// 3. Two edge groups: OUTLINE then INTERNAL.
	edgeStride := objectNavEdgeWireStride
	if navFlags&objectNavFlagEdgeExtraByte != 0 {
		edgeStride++
	}
	if mesh.outline, err = decodeObjectNavEdgeGroup(c, "outline", edgeStride, vertexCountInt, cellCountInt); err != nil {
		return nil, err
	}
	if mesh.internal, err = decodeObjectNavEdgeGroup(c, "internal", edgeStride, vertexCountInt, cellCountInt); err != nil {
		return nil, err
	}

	// 4. Optional event-name strings (navFlags&4).
	if navFlags&objectNavFlagEventNames != 0 {
		nameCount, err := c.u32("event name count")
		if err != nil {
			return nil, err
		}
		if uint64(nameCount) > uint64((len(c.raw)-c.off)/4) {
			return nil, fmt.Errorf("object nav event name count %d exceeds remaining payload", nameCount)
		}
		for i := uint32(0); i < nameCount; i++ {
			length, err := c.u32("event name length")
			if err != nil {
				return nil, err
			}
			if uint64(length) > uint64(len(c.raw)-c.off) {
				return nil, fmt.Errorf("object nav payload truncated in event name text at %d/%d", c.off, len(c.raw))
			}
			if err := c.ensure(int(length), "event name text"); err != nil {
				return nil, err
			}
			c.off += int(length)
		}
	}

	// 5. Outline-edge grid tail (sub_426160): 20-byte header then per tile
	//    { u32 refCount, refCount x u16 }. Parsed only to prove the exact
	//    consume; the rescue never queries the grid.
	if err := c.ensure(20, "grid header"); err != nil {
		return nil, err
	}
	countX := binary.LittleEndian.Uint32(c.raw[c.off+8:])
	countZ := binary.LittleEndian.Uint32(c.raw[c.off+12:])
	tileCount := binary.LittleEndian.Uint32(c.raw[c.off+16:])
	c.off += 20
	if countX > math.MaxInt32 || countZ > math.MaxInt32 ||
		uint64(tileCount) != uint64(countX)*uint64(countZ) {
		return nil, fmt.Errorf("object nav grid tileCount %d != %d*%d", tileCount, countX, countZ)
	}
	if uint64(tileCount) > uint64((len(c.raw)-c.off)/4) {
		return nil, fmt.Errorf("object nav grid tile count %d exceeds remaining payload", tileCount)
	}
	for t := uint32(0); t < tileCount; t++ {
		refCount, err := c.u32("grid tile ref count")
		if err != nil {
			return nil, err
		}
		refBytes, err := c.countBytes(refCount, 2, math.MaxUint32, "grid tile refs")
		if err != nil {
			return nil, err
		}
		c.off += refBytes
	}
	if c.off != len(c.raw) {
		return nil, fmt.Errorf("object nav payload not exactly consumed (%d/%d)", c.off, len(c.raw))
	}

	// Vertex-cloud bounds for the per-move mesh rejects. A zero-vertex
	// payload only reaches callers as cellCount 0 (dropped by the loader),
	// so degenerate bounds never answer a query.
	mesh.minX, mesh.minY, mesh.minZ = math.Inf(1), math.Inf(1), math.Inf(1)
	mesh.maxX, mesh.maxY, mesh.maxZ = math.Inf(-1), math.Inf(-1), math.Inf(-1)
	for i := 0; i < vertexCountInt; i++ {
		x := float64(vertices[i*3])
		y := float64(vertices[i*3+1])
		z := float64(vertices[i*3+2])
		mesh.minX = math.Min(mesh.minX, x)
		mesh.maxX = math.Max(mesh.maxX, x)
		mesh.minY = math.Min(mesh.minY, y)
		mesh.maxY = math.Max(mesh.maxY, y)
		mesh.minZ = math.Min(mesh.minZ, z)
		mesh.maxZ = math.Max(mesh.maxZ, z)
	}
	return mesh, nil
}

// ---- asset JSON shapes (trimmed to the rescue's needs) ----

// navObjectRecordJSON is one navmesh region object placement row. The
// records ride inside the same bundle the surface loader already reads;
// they are parsed defensively from raw JSON so a malformed object list
// degrades only the object-nav view, never the whole surface.
type navObjectRecordJSON struct {
	AssetID       json.Number `json:"assetId"`
	X             json.Number `json:"x"`
	Y             json.Number `json:"y"`
	Z             json.Number `json:"z"`
	Yaw           json.Number `json:"yaw"`
	LinkEdgeCount json.Number `json:"linkEdgeCount"`
	LinkEdges     string      `json:"linkEdges"`
}

// decodeObjectPlacements parses a navmesh region entry's raw objects
// array; nil/malformed degrades to no placements.
func decodeObjectPlacements(raw json.RawMessage) []objectNavPlacement {
	if len(raw) == 0 {
		return nil
	}
	var rows []navObjectRecordJSON
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil
	}
	placements := make([]objectNavPlacement, 0, len(rows))
	for ordinal, row := range rows {
		assetID, okID := jsonInt(row.AssetID)
		x, errX := row.X.Float64()
		y, errY := row.Y.Float64()
		z, errZ := row.Z.Float64()
		if !okID || assetID <= 0 || errX != nil || errY != nil || errZ != nil {
			continue
		}
		yaw, err := row.Yaw.Float64()
		if err != nil {
			yaw = 0
		}
		linkEdgeCount, ok := jsonInt(row.LinkEdgeCount)
		if !ok {
			linkEdgeCount = 0
		}
		var links []objectNavLink
		if linkEdgeCount > 0 {
			raw, err := base64.StdEncoding.DecodeString(row.LinkEdges)
			if err == nil && len(raw) == linkEdgeCount*6 {
				for at := 0; at < len(raw); at += 6 {
					links = append(links, objectNavLink{int(binary.LittleEndian.Uint16(raw[at:])), int(binary.LittleEndian.Uint16(raw[at+2:])), int(binary.LittleEndian.Uint16(raw[at+4:]))})
				}
			}
		}
		placements = append(placements, objectNavPlacement{ordinal: ordinal, links: links,
			assetID:       assetID,
			x:             x,
			y:             y,
			z:             z,
			yaw:           yaw,
			linkEdgeCount: linkEdgeCount,
		})
	}
	return placements
}

type objectResourceIndexJSON struct {
	Bsr []struct {
		ObjectID          json.Number `json:"objectId"`
		RenderMeshSection struct {
			Paths []string `json:"paths"`
		} `json:"renderMeshSection"`
		MeshPaths []string `json:"meshPaths"`
	} `json:"bsr"`
	MeshFiles []struct {
		SourcePath string `json:"sourcePath"`
		PublicPath string `json:"publicPath"`
		Path       string `json:"path"`
	} `json:"meshFiles"`
}

type objectNavMeshWireJSON struct {
	ByteLength     json.Number `json:"byteLength"`
	HeaderOffsets  []int       `json:"headerOffsets"`
	NativePayloads []struct {
		Kind       string      `json:"kind"`
		ByteOffset json.Number `json:"byteOffset"`
		ByteLength json.Number `json:"byteLength"`
		RawBase64  string      `json:"rawBase64"`
	} `json:"nativePayloads"`
}

type objectMeshFileJSON struct {
	Mesh objectNavMeshWireJSON `json:"mesh"`
}

// ---- loaders (same cache/lock discipline as the surface loaders) ----

// loadObjectResourceIndex loads (or answers from cache) the object
// resource index the region bundle names. A failed load caches nil (the
// standard negative). The read runs with v.mu released.
func (v *WaterValidator) loadObjectResourceIndex(publicPath string) *objectResourceIndex {
	if publicPath == "" {
		return nil
	}
	v.mu.Lock()
	index, cached := v.objectIndexes[publicPath]
	v.mu.Unlock()
	if cached {
		return index
	}

	loaded := v.buildObjectResourceIndex(publicPath)

	v.mu.Lock()
	defer v.mu.Unlock()
	if index, cached := v.objectIndexes[publicPath]; cached {
		return index
	}
	v.objectIndexes[publicPath] = loaded
	return loaded
}

// buildObjectResourceIndex is the uncached index read: resolve each bsr
// row's mesh SOURCE paths (renderMeshSection preferred, the client order)
// to PUBLIC mesh JSON paths through the meshFiles table.
func (v *WaterValidator) buildObjectResourceIndex(publicPath string) *objectResourceIndex {
	parsed := &objectResourceIndexJSON{}
	if !v.readJSON(publicPath, parsed) {
		return nil
	}
	publicBySource := make(map[string]string, len(parsed.MeshFiles))
	for _, mesh := range parsed.MeshFiles {
		meshPath := mesh.Path
		if meshPath == "" {
			meshPath = mesh.PublicPath
		}
		if mesh.SourcePath != "" && meshPath != "" {
			publicBySource[mesh.SourcePath] = meshPath
		}
	}
	index := &objectResourceIndex{meshPublicPathsByObjectID: make(map[int][]string, len(parsed.Bsr))}
	for _, bsr := range parsed.Bsr {
		objectID, ok := jsonInt(bsr.ObjectID)
		if !ok || objectID <= 0 {
			continue
		}
		sourcePaths := bsr.RenderMeshSection.Paths
		if len(sourcePaths) == 0 {
			sourcePaths = bsr.MeshPaths
		}
		var meshPublicPaths []string
		for _, sourcePath := range sourcePaths {
			if resolved := publicBySource[sourcePath]; resolved != "" {
				meshPublicPaths = append(meshPublicPaths, resolved)
			}
		}
		if len(meshPublicPaths) > 0 {
			index.meshPublicPathsByObjectID[objectID] = meshPublicPaths
		}
	}
	return index
}

// loadObjectNavMeshes loads (or answers from cache) the decoded object-nav
// meshes of one mesh JSON public path. Most meshes carry no nav payload;
// the empty slice is the cached negative. The read runs with v.mu
// released.
func (v *WaterValidator) loadObjectNavMeshes(publicPath string) []*objectNavMesh {
	v.mu.Lock()
	meshes, cached := v.objectNavMeshes[publicPath]
	v.mu.Unlock()
	if cached {
		return meshes
	}

	loaded := v.buildObjectNavMeshes(publicPath)

	v.mu.Lock()
	defer v.mu.Unlock()
	if meshes, cached := v.objectNavMeshes[publicPath]; cached {
		return meshes
	}
	v.objectNavMeshes[publicPath] = loaded
	return loaded
}

// buildObjectNavMeshes is the uncached mesh JSON read + payload decode,
// mirroring the client gate (decodeNativeObjectNavPayload): kind must be
// the preserved offset-7 tail, headerOffsets[7] must name the payload's
// byte offset, headerOffsets[11] is the flags word, and the section ends
// at the nearest higher header offset (offset 11 excluded, it is not an
// offset) or the mesh end. Undecodable payloads are dropped (fail open).
func (v *WaterValidator) buildObjectNavMeshes(publicPath string) []*objectNavMesh {
	parsed := &objectMeshFileJSON{}
	if !v.readJSON(publicPath, parsed) {
		return []*objectNavMesh{}
	}
	return decodeObjectNavMeshes(parsed.Mesh)
}

func decodeObjectNavMeshes(wire objectNavMeshWireJSON) []*objectNavMesh {
	headers := wire.HeaderOffsets
	navOffset := 0
	if len(headers) > 7 {
		navOffset = headers[7]
	}
	navFlags := 0
	if len(headers) > 11 {
		navFlags = headers[11]
	}
	meshByteLength, _ := jsonInt(wire.ByteLength)
	meshes := []*objectNavMesh{}
	if navOffset == 0 {
		return meshes
	}
	for _, payload := range wire.NativePayloads {
		if payload.Kind != "bms-offset7-post-payload-tail" {
			continue
		}
		byteOffset, okOffset := jsonInt(payload.ByteOffset)
		payloadByteLength, okLength := jsonInt(payload.ByteLength)
		if !okOffset || !okLength || byteOffset != navOffset {
			continue
		}
		sectionEnd := meshByteLength
		for i, header := range headers {
			if i == 11 {
				continue
			}
			if header > byteOffset && header < sectionEnd {
				sectionEnd = header
			}
		}
		sectionByteLength := sectionEnd - byteOffset
		if sectionByteLength > payloadByteLength {
			sectionByteLength = payloadByteLength
		}
		if sectionByteLength <= 0 {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(payload.RawBase64)
		if err != nil || len(raw) < sectionByteLength {
			continue
		}
		mesh, err := decodeObjectNavPayload(raw[:sectionByteLength], navFlags)
		if err != nil || mesh.cellCount() == 0 {
			continue
		}
		meshes = append(meshes, mesh)
	}
	return meshes
}

// ---- resolved placement sets (per surface + sector offset) ----

// resolvedObjectNav is one placement whose asset resolved to at least one
// decoded object-nav mesh - the unit the per-move queries iterate.
type resolvedObjectNav struct {
	placement objectNavPlacement
	meshes    []*objectNavMesh
}

// objectNavSetKey addresses one region entry's resolved placement set.
// Surfaces are load-once and cached for the validator's lifetime, so the
// pointer is a stable identity.
type objectNavSetKey struct {
	surface *groundSurface
	offset  int64
}

// objectNavSetForOffset loads (or answers from cache) the resolved
// object-nav placements of one region entry: placements whose assetId
// resolves through the resource index to at least one decodable nav mesh.
// This collapses the per-query index/mesh cache traffic (one lock acquire
// per warm query instead of one per placement) - the movement planes call
// it per accepted move. The build runs with v.mu released.
func (v *WaterValidator) objectNavSetForOffset(surface *groundSurface, dxRegion, dzRegion int) []resolvedObjectNav {
	key := objectNavSetKey{surface: surface, offset: offsetKey(dxRegion, dzRegion)}
	v.mu.Lock()
	set, cached := v.objectNavSets[key]
	v.mu.Unlock()
	if cached {
		return set
	}

	built := v.buildObjectNavSet(surface, key.offset)

	v.mu.Lock()
	defer v.mu.Unlock()
	if set, cached := v.objectNavSets[key]; cached {
		return set
	}
	v.objectNavSets[key] = built
	return built
}

// buildObjectNavSet is the uncached resolution (empty slice = the cached
// negative; absent index or undecodable meshes degrade to fewer entries,
// never an error - the package's fail-open standard).
func (v *WaterValidator) buildObjectNavSet(surface *groundSurface, offset int64) []resolvedObjectNav {
	placements := surface.objectPlacementsByOffset[offset]
	if len(placements) == 0 {
		return nil
	}
	index := v.loadObjectResourceIndex(surface.objectResourceIndexPublicPath)
	if index == nil {
		return nil
	}
	set := make([]resolvedObjectNav, 0, len(placements))
	for _, placement := range placements {
		var meshes []*objectNavMesh
		for _, meshPath := range index.meshPublicPathsByObjectID[placement.assetID] {
			meshes = append(meshes, v.loadObjectNavMeshes(meshPath)...)
		}
		if len(meshes) > 0 {
			set = append(set, resolvedObjectNav{placement: placement, meshes: meshes})
		}
	}
	return set
}

// ---- the sealed-deck verdict ----

// objectDeckStand is the winning nearest-Y candidate: the object cell the
// spawn stands on, when one beats the terrain plane.
type objectDeckStand struct {
	set              []resolvedObjectNav
	objectIndex      int
	mesh             *objectNavMesh
	cellIndex        int
	planeY           float64
	placement        objectNavPlacement
	anchorX, anchorZ float64 // world-grid origin of the placement's region
	// surface/setDX/setDZ/meshIndex address the stand as a value
	// simulation.NavObjectCell (navowner.go): the cached set is
	// objectNavSetForOffset(surface, setDX, setDZ).
	surface      *groundSurface
	setDX, setDZ int
	meshIndex    int
}

// objectCellPlaneYAt reports whether the object-LOCAL point (lx, lz) lies
// inside cell's XZ triangle and, when it does, the cell plane's
// barycentric Y there. The small negative tolerance absorbs edge-exact
// spawns (the native containment is inclusive at cell borders).
func objectCellPlaneYAt(mesh *objectNavMesh, cell int, lx, lz float64) (float64, bool) {
	const eps = -1e-4
	a := mesh.cellA[cell]
	b := mesh.cellB[cell]
	cc := mesh.cellC[cell]
	ax, ay, az := float64(mesh.vertices[a*3]), float64(mesh.vertices[a*3+1]), float64(mesh.vertices[a*3+2])
	bx, by, bz := float64(mesh.vertices[b*3]), float64(mesh.vertices[b*3+1]), float64(mesh.vertices[b*3+2])
	cx, cy, cz := float64(mesh.vertices[cc*3]), float64(mesh.vertices[cc*3+1]), float64(mesh.vertices[cc*3+2])
	den := (bz-cz)*(ax-cx) + (cx-bx)*(az-cz)
	if math.Abs(den) < 1e-9 {
		return 0, false
	}
	l0 := ((bz-cz)*(lx-cx) + (cx-bx)*(lz-cz)) / den
	l1 := ((cz-az)*(lx-cx) + (ax-cx)*(lz-cz)) / den
	l2 := 1 - l0 - l1
	if l0 < eps || l1 < eps || l2 < eps {
		return 0, false
	}
	return l0*ay + l1*by + l2*cy, true
}

// objectLaneSealed runs the sealed-lane BFS from startCell: crossing an
// internal edge from its srcCell is blocked by flag 0x2, from its dstCell
// by 0x1 (sub_428930's side-block bits). The lane is sealed when no OPEN
// outline edge (flags 0x0) belongs to a reachable cell - the only edge
// kind whose reflect path lets the native walk exit the object
// (sub_403fb0's exitObjectToTerrain tail).
func objectLaneSealed(mesh *objectNavMesh, startCell int) bool {
	return objectLinkedLaneSealed([]resolvedObjectNav{{meshes: []*objectNavMesh{mesh}}}, 0, mesh, startCell)
}

// spawnObjectDeckStand resolves the object cell a seed-frame spawn point
// stands on, mirroring the native nearest-Y arbitration. It searches the
// point sector and adjacent placement anchors so an overhanging deck is
// owned by its mesh rather than the terrain below.
func (v *WaterValidator) spawnObjectDeckStand(surface *groundSurface, baseX, baseZ, spawnY, terrainY float64) *objectDeckStand {
	terrainDelta := math.Abs(terrainY - spawnY)
	var best *objectDeckStand
	bestDelta := terrainDelta
	point := worldgeom.NormalizeOutdoor(worldgeom.RegionXZ{RegionID: surface.seedRegionID, X: baseX, Z: baseZ})
	pointSectorX := worldgeom.SectorX(point.RegionID)
	pointSectorZ := worldgeom.SectorY(point.RegionID)
	gridPoint := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: surface.seedRegionID, X: baseX, Z: baseZ})
	// A handful of sets (nine anchors on at most two surfaces): a scan of a
	// stack array, not a map that allocated its buckets every monster step.
	var checkedBuf [18]objectNavSetKey
	checked := checkedBuf[:0]

	probe := func(candidate *groundSurface, anchorX, anchorZ int) {
		if candidate == nil {
			return
		}
		dx := anchorX - simulation.SectorX(candidate.seedRegionID)
		dz := anchorZ - simulation.SectorY(candidate.seedRegionID)
		key := objectNavSetKey{surface: candidate, offset: offsetKey(dx, dz)}
		if slices.Contains(checked, key) {
			return
		}
		checked = append(checked, key)
		set := v.objectNavSetForOffset(candidate, dx, dz)
		anchorRegion := worldgeom.RegionIDForSectors(anchorX, anchorZ)
		anchorLocal := worldgeom.LocalFromGrid(anchorRegion, gridPoint)
		localX, localZ := anchorLocal.X, anchorLocal.Z

		for i := range set {
			placement := set[i].placement
			// Inverse of the forward yaw+translate transform: object-local
			// point = R(-yaw) * (region point - origin); Y is untouched.
			cosYaw := math.Cos(placement.yaw)
			sinYaw := math.Sin(placement.yaw)
			objectLocalX := cosYaw*(localX-placement.x) + sinYaw*(localZ-placement.z)
			objectLocalZ := -sinYaw*(localX-placement.x) + cosYaw*(localZ-placement.z)
			objectLocalY := spawnY - placement.y
			for meshIndex, mesh := range set[i].meshes {
				if objectLocalX < mesh.minX || objectLocalX > mesh.maxX ||
					objectLocalZ < mesh.minZ || objectLocalZ > mesh.maxZ {
					continue
				}
				if objectLocalY-mesh.maxY > bestDelta || mesh.minY-objectLocalY > bestDelta {
					continue
				}
				for cell := 0; cell < mesh.cellCount(); cell++ {
					cellPlaneY, inside := objectCellPlaneYAt(mesh, cell, objectLocalX, objectLocalZ)
					if !inside {
						continue
					}
					planeY := cellPlaneY + placement.y
					delta := math.Abs(planeY - spawnY)
					if delta < bestDelta {
						bestDelta = delta
						best = &objectDeckStand{set: set, objectIndex: i, mesh: mesh, cellIndex: cell, planeY: planeY, placement: placement, anchorX: float64(anchorX) * simulation.NativeRegionSize, anchorZ: float64(anchorZ) * simulation.NativeRegionSize,
							surface: candidate, setDX: dx, setDZ: dz, meshIndex: meshIndex}
					}
				}
			}
		}
	}

	for dz := -objectAnchorSearchRadiusSectors; dz <= objectAnchorSearchRadiusSectors; dz++ {
		for dx := -objectAnchorSearchRadiusSectors; dx <= objectAnchorSearchRadiusSectors; dx++ {
			anchorX, anchorZ := pointSectorX+dx, pointSectorZ+dz
			if anchorX < 0 || anchorZ < 0 || anchorX > 0xff || anchorZ > 0xff {
				continue
			}
			probe(surface, anchorX, anchorZ)
			probe(v.surfaceForRegion(simulation.RegionIDForSectors(anchorX, anchorZ)), anchorX, anchorZ)
		}
	}
	return best
}
