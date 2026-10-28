/*
===========================================================================

ainavdata_test.go - the native cave route tables load only when they fit
the v1.150 meshes (#1698)

The real files are read from the generated server bundle and skip
without game data; no route bytes are committed. Donwhang (0x8001) must
load and match its mesh exactly; 0x8009 must be refused on its cell
counts and keep the port's route. The parser's own checks run on a
synthetic file built here.

===========================================================================
*/
package movement

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
)

const (
	aiNavDonwhangRegion = 0x8001
	aiNavRefusedRegion  = 0x8009
)

/*
================
packedAINavFile

The bundle's packed route file for a region, decoded but not parsed.
================
*/
func packedAINavFile(t *testing.T, v *WaterValidator, region uint16) (int, string) {
	t.Helper()
	packed := &aiNavDataJSON{}
	if !v.readJSON(v.aiNavDataPath, packed) {
		t.Fatal("the bundle packs no dungeon route tables")
	}
	for _, file := range packed.Files {
		if file.RegionID == region {
			return file.ByteLength, file.RawBase64
		}
	}
	t.Fatalf("the bundle packs no route table for 0x%04X", region)
	return 0, ""
}

/*
================
TestAINavDataLoadsDonwhangAndRefuses8009
================
*/
func TestAINavDataLoadsDonwhangAndRefuses8009(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	donwhang := v.dungeonSpawnSurfaces[aiNavDonwhangRegion]
	if donwhang == nil || donwhang.routeTable == nil {
		t.Fatal("Donwhang's route table did not load")
	}
	table := donwhang.routeTable
	points, portals := 0, 0
	for i := range table.blocks {
		points += len(table.blocks[i].points)
		portals += len(table.blocks[i].portals)
	}
	// The counts read from the file when the layout was traced (#1698).
	if len(table.blocks) != 151 || portals != 300 {
		t.Fatalf("Donwhang: %d blocks, %d portals; want 151 and 300", len(table.blocks), portals)
	}
	t.Logf("Donwhang: %d blocks, %d points, %d portals", len(table.blocks), points, portals)

	refused := v.dungeonSpawnSurfaces[aiNavRefusedRegion]
	if refused == nil {
		t.Fatal("no 0x8009 dungeon surface")
	}
	if refused.routeTable != nil {
		t.Fatal("0x8009's route table loaded; its blocks do not match the v1.150 mesh")
	}
	length, raw := packedAINavFile(t, v, aiNavRefusedRegion)
	_, err := decodeAINavFile(aiNavRefusedRegion, length, raw, refused)
	if err == nil || !strings.Contains(err.Error(), "route cells") {
		t.Fatalf("0x8009 refused for %v, want a cell-count mismatch", err)
	}
	// #1979: this refusal is the recorded decision, logged at info level.
	if _, known := aiNavKnownRefusal(aiNavRefusedRegion, err); !known {
		t.Fatalf("0x8009's cell-count refusal is not the recorded mismatch: %v", err)
	}
}

/*
================
TestAINavKnownRefusalOnlyCoversTheRecordedMismatch

#1979: only 0x8009's cell-count refusal is the recorded decision. The
same region failing another way, or another region failing on cell
counts, still warns.
================
*/
func TestAINavKnownRefusalOnlyCoversTheRecordedMismatch(t *testing.T) {
	cells := fmt.Errorf("block 0: 57 route cells, the mesh has 187: %w", errAINavCellMismatch)
	for _, c := range []struct {
		name   string
		region uint16
		err    error
		known  bool
	}{
		{"the arena's cell counts", aiNavRefusedRegion, cells, true},
		{"the arena failing to parse", aiNavRefusedRegion, errors.New("route trailer 7, want 0"), false},
		{"another dungeon's cell counts", aiNavDonwhangRegion, cells, false},
	} {
		if _, known := aiNavKnownRefusal(c.region, c.err); known != c.known {
			t.Errorf("%s: known = %v, want %v", c.name, known, c.known)
		}
	}
}

/*
================
TestAINavDataRefusesAMovedPointOrCell

The validator's mesh checks bite: a point moved off its edge's midpoint
or its block's height, a dropped point, a route half past the point
list, a portal landing past its block and a fastest cell that is no
portal each refuse the file.
================
*/
func TestAINavDataRefusesAMovedPointOrCell(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	surface := v.dungeonSpawnSurfaces[aiNavDonwhangRegion]
	if surface == nil || surface.routeTable == nil {
		t.Fatal("Donwhang's route table did not load")
	}
	length, raw := packedAINavFile(t, v, aiNavDonwhangRegion)
	fresh := func() *caveRouteTable {
		table, err := decodeAINavFile(aiNavDonwhangRegion, length, raw, surface)
		if err != nil {
			t.Fatal(err)
		}
		return table
	}
	mutations := map[string]func(*caveRouteTable){
		"a moved internal point": func(table *caveRouteTable) { table.blocks[0].points[0][0] += 0.5 },
		"a route half past the points": func(table *caveRouteTable) {
			table.blocks[0].route[0] = uint32(len(table.blocks[0].points)) << 16
		},
		"a landing past its block": func(table *caveRouteTable) {
			portal := &table.blocks[0].portals[0]
			portal.landingPoint = uint16(len(table.blocks[portal.landingBlock].points))
		},
		"a dropped point": func(table *caveRouteTable) {
			table.blocks[1].points = table.blocks[1].points[:len(table.blocks[1].points)-1]
		},
		"a point off its block's height": func(table *caveRouteTable) { table.blocks[2].points[0][1] += 0.5 },
		"a fastest cell that is no portal": func(table *caveRouteTable) {
			// fastest[dest 1, cur 0]: block 0's portals are cells 1 and 5.
			table.fastest[len(table.blocks)] = 0
		},
	}
	for name, mutate := range mutations {
		table := fresh()
		mutate(table)
		if err := validateAINavData(table, surface); err == nil {
			t.Errorf("%s: validated", name)
		}
	}
}

//============================================================================
// synthetic files

/*
================
syntheticAINavData

A self-consistent one-block file: two cells, three points, one portal.
================
*/
func syntheticAINavData() []byte {
	var route []byte
	le := binary.LittleEndian
	route = append(route, aiNavDataVersion)
	route = le.AppendUint32(route, 0) // patched below
	route = le.AppendUint16(route, 0x8123)
	route = le.AppendUint32(route, 1)
	route = le.AppendUint32(route, 0) // index
	route = le.AppendUint32(route, 2) // cells
	route = le.AppendUint32(route, 3) // points
	for _, word := range []uint32{0x00000000, 0x00010002, 0x00020001, 0x00000000} {
		route = le.AppendUint32(route, word)
	}
	route = le.AppendUint32(route, 1)
	route = le.AppendUint32(route, 1)
	route = le.AppendUint16(route, 1)
	route = le.AppendUint16(route, 0)
	route = le.AppendUint16(route, 2)
	route = le.AppendUint16(route, 0xFFFF) // fastest[0]
	route = le.AppendUint32(route, 0)
	le.PutUint32(route[1:], uint32(len(route)))

	route = le.AppendUint16(route, 0x8123)
	route = le.AppendUint32(route, 1)
	route = le.AppendUint32(route, 3)
	for k := 0; k < 9; k++ {
		route = le.AppendUint32(route, math.Float32bits(float32(k)))
	}
	return le.AppendUint32(route, 0)
}

/*
================
TestAINavDataParsesTheTracedLayout
================
*/
func TestAINavDataParsesTheTracedLayout(t *testing.T) {
	table, err := parseAINavData(syntheticAINavData())
	if err != nil {
		t.Fatal(err)
	}
	block := table.blocks[0]
	if table.region != 0x8123 || block.cells != 2 || block.route[1] != 0x00010002 ||
		block.portals[0] != (caveRoutePortal{key: 1, cell: 1, landingBlock: 0, landingPoint: 2}) ||
		block.points[2] != [3]float32{6, 7, 8} || table.fastest[0] != 0xFFFF {
		t.Fatalf("parsed %+v", table)
	}
}

/*
================
TestAINavDataRefusesABrokenFile

Every damaged copy of the synthetic file is refused, none panics.
================
*/
func TestAINavDataRefusesABrokenFile(t *testing.T) {
	good := syntheticAINavData()
	damage := map[string]func([]byte) []byte{
		"version 2":            func(b []byte) []byte { b[0] = 2; return b },
		"a wrong offset":       func(b []byte) []byte { b[1]++; return b },
		"a wrong block index":  func(b []byte) []byte { b[11] = 1; return b },
		"a nonzero trailer":    func(b []byte) []byte { b[len(b)-1] = 1; return b },
		"a trailing byte":      func(b []byte) []byte { return append(b, 0) },
		"a huge cell count":    func(b []byte) []byte { binary.LittleEndian.PutUint32(b[15:], 1<<20); return b },
		"another point region": func(b []byte) []byte { b[binary.LittleEndian.Uint32(b[1:])] ^= 1; return b },
	}
	for name, broken := range damage {
		if _, err := parseAINavData(broken(append([]byte(nil), good...))); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	for cut := 0; cut < len(good); cut++ {
		if _, err := parseAINavData(good[:cut]); err == nil {
			t.Fatalf("cut at %d of %d: parsed", cut, len(good))
		}
	}
}
