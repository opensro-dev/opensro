package movement

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
)

// Terrain cells own outside-object candidate admission, native 404510/428300.
func admitTerrainObjectCells(region navmeshRegion, rows []objectNavPlacement) error {
	c := region.Cells
	if c.ObjectIndexOffsets == "" {
		return nil
	}
	count, ok := jsonInt(c.Count)
	if !ok || count < 0 || count > 65536 {
		return fmt.Errorf("invalid terrain cell count")
	}
	column := func(s string, n int) ([]byte, error) {
		b, e := base64.StdEncoding.DecodeString(s)
		if e != nil || len(b) != n {
			return nil, fmt.Errorf("invalid terrain cell column")
		}
		return b, nil
	}
	offsets, e := column(c.ObjectIndexOffsets, (count+1)*4)
	if e != nil {
		return e
	}
	length := int(binary.LittleEndian.Uint32(offsets[count*4:]))
	if length > 1048576 || binary.LittleEndian.Uint32(offsets) != 0 {
		return fmt.Errorf("invalid terrain cell offsets")
	}
	ids, e := column(c.ObjectIndices, length*2)
	if e != nil {
		return e
	}
	var cols [4][]byte
	for i, s := range []string{c.MinX, c.MinZ, c.MaxX, c.MaxZ} {
		cols[i], e = column(s, count*4)
		if e != nil {
			return e
		}
	}
	cells := make([][][4]float32, len(rows))
	for i := range rows {
		rows[i].terrainCells = &cells[i]
	}
	for i := 0; i < count; i++ {
		a, b := int(binary.LittleEndian.Uint32(offsets[i*4:])), int(binary.LittleEndian.Uint32(offsets[(i+1)*4:]))
		if a > b || b > length {
			return fmt.Errorf("invalid terrain cell offsets")
		}
		var r [4]float32
		for j := range r {
			r[j] = math.Float32frombits(binary.LittleEndian.Uint32(cols[j][i*4:]))
			if math.IsNaN(float64(r[j])) || math.IsInf(float64(r[j]), 0) {
				return fmt.Errorf("invalid terrain cell bounds")
			}
		}
		if r[0] > r[2] || r[1] > r[3] {
			return fmt.Errorf("invalid terrain cell bounds")
		}
		for j := a; j < b; j++ {
			id := int(binary.LittleEndian.Uint16(ids[j*2:]))
			if id >= len(rows) {
				return fmt.Errorf("invalid terrain object reference")
			}
			cells[id] = append(cells[id], r)
		}
	}
	return nil
}

/*
==================
terrainVisitKey

The chord fraction at which native CRTNavMeshTerrain_Move (404510) first
stands in a terrain cell this placement is registered in, and so steps it
(CRTNavMeshTerrain_StepPlacedObject 403FB0 -> CRTNavMeshObj_EnterFromOutside
428300) against the rest of the chord. x0..z1 are the chord in the anchor
region's frame, the frame the cell rectangles use. +Inf when the chord never
enters a registered cell; 0 for a placement without registration data (every
object is then visited from the start, the pre-registration behaviour).
==================
*/
func terrainVisitKey(cells *[][4]float32, x0, z0, x1, z1 float64) float64 {
	if cells == nil {
		return 0
	}
	best := math.Inf(1)
	for _, r := range *cells {
		if t, ok := chordRectEntry(x0, z0, x1, z1, float64(r[0]), float64(r[1]), float64(r[2]), float64(r[3])); ok && t < best {
			best = t
		}
	}
	return best
}

/*
==================
chordRectEntry

Slab clip of the chord x0,z0 -> x1,z1 (t in [0,1]) against a closed
rectangle: the first t inside it.
==================
*/
func chordRectEntry(x0, z0, x1, z1, minX, minZ, maxX, maxZ float64) (float64, bool) {
	lo, hi := 0.0, 1.0
	for _, axis := range [2][4]float64{{x0, x1, minX, maxX}, {z0, z1, minZ, maxZ}} {
		p, q, a, b := axis[0], axis[1]-axis[0], axis[2], axis[3]
		if math.Abs(q) < 1e-12 {
			if p < a || p > b {
				return 0, false
			}
			continue
		}
		t0, t1 := (a-p)/q, (b-p)/q
		if t0 > t1 {
			t0, t1 = t1, t0
		}
		lo, hi = math.Max(lo, t0), math.Min(hi, t1)
		if lo > hi {
			return 0, false
		}
	}
	return lo, true
}
