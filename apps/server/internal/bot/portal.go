/*
===========================================================================

portal.go - the shipped teleport catalogue for cross-region bot travel

The server's NPC gate network lives in the game-data textdata files.
teleportbuilding.txt names the static gate buildings and their positions.
teleportdata.txt names the destinations and links each gate to a source
destination. teleportlink.txt names the (source, target) edges. The bot loads
the three tables, builds a region graph and plans a gate route between two
regions (issue #171).

The reader is local to this package on purpose. It shares the file grammar of
the shipped UTF-16LE textdata (BOM or a null-heavy body) without importing a
game subsystem.

===========================================================================
*/
package bot

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"

	"opensro.online/server/internal/data/texttable"
)

// PortalGateIDBase is the object-id base of a static teleport gate NPC. The
// server assigns a gate the identity base plus its teleportbuilding ref, so the
// bot can name the gate without an object-list NPC row (issue #171).
const PortalGateIDBase uint32 = 250000

// PortalDestination is one teleportdata.txt row: an arrival point.
type PortalDestination struct {
	// ID is the destination identity the portal request names as its target.
	ID uint32
	// Ref is the teleportbuilding ref this destination belongs to, or zero.
	Ref uint32
	// Region is the arrival region.
	Region uint16
	X      float64
	Y      float64
	Z      float64
}

// PortalGate is one static gate from teleportbuilding.txt.
type PortalGate struct {
	// Ref is the teleportbuilding ref.
	Ref uint32
	// SourceID is the teleportdata destination id whose ref is this gate.
	SourceID uint32
	// Region is the region the gate stands in.
	Region uint16
	X      float64
	Y      float64
	Z      float64
	// GID is the gate NPC object id the select and portal requests name.
	GID uint32
}

// PortalLink is one teleportlink.txt edge.
type PortalLink struct {
	Source uint32
	Target uint32
	Fee    int64
}

// PortalHop is one gate crossing in a region route.
type PortalHop struct {
	GateRef       uint32
	GateGID       uint32
	GateRegion    uint16
	GateX         float64
	GateY         float64
	GateZ         float64
	DestinationID uint32
	ArrivalRegion uint16
	ArrivalX      float64
	ArrivalY      float64
	ArrivalZ      float64
}

// PortalEvent reports one portal request for the telemetry (issue #171). The
// brain hands it to the service once through TakePortalEvent.
type PortalEvent struct {
	GateGID       uint32 `json:"gate"`
	DestinationID uint32 `json:"destination"`
	FromRegion    uint16 `json:"fromRegion"`
	ToRegion      uint16 `json:"toRegion"`
}

// PortalCatalog is the shipped teleport catalogue.
type PortalCatalog struct {
	// Destinations maps a destination id to its arrival point.
	Destinations map[uint32]PortalDestination
	// Sources maps a teleportbuilding ref to its teleportdata source id.
	Sources map[uint32]uint32
	// Links indexes a link by its (source, target) destination pair.
	Links map[[2]uint32]PortalLink
	// GatesByRegion lists the gates of a region, so the route walks to one.
	GatesByRegion map[uint16][]PortalGate
	// linksBySource indexes the outgoing links of a source destination.
	linksBySource map[uint32][]PortalLink
}

// portalStep is one breadth-first parent edge: the region it came from and the
// hop that crosses into it (issue #171).
type portalStep struct {
	region uint16
	hop    PortalHop
}

// LoadPortalCatalog reads teleportdata.txt, teleportbuilding.txt and
// teleportlink.txt from dir and builds the region graph (issue #171).
func LoadPortalCatalog(dir string) (*PortalCatalog, error) {
	catalog := &PortalCatalog{
		Destinations:  map[uint32]PortalDestination{},
		Sources:       map[uint32]uint32{},
		Links:         map[[2]uint32]PortalLink{},
		GatesByRegion: map[uint16][]PortalGate{},
		linksBySource: map[uint32][]PortalLink{},
	}
	destinations := readPortalRows(filepath.Join(dir, "teleportdata.txt"))
	if len(destinations) == 0 {
		return nil, fmt.Errorf("portal: teleportdata.txt is absent or empty")
	}
	for row, cells := range destinations {
		if len(cells) < 9 || cells[0] != "1" {
			continue
		}
		id, ok := portalUint32(cells[1])
		if !ok || id == 0 {
			return nil, fmt.Errorf("portal: teleportdata row %d identity", row+1)
		}
		region, ok := portalRegion(cells[5])
		if !ok {
			return nil, fmt.Errorf("portal: teleportdata row %d region", row+1)
		}
		x, okX := portalFloat(cells[6])
		y, okY := portalFloat(cells[7])
		z, okZ := portalFloat(cells[8])
		if !okX || !okY || !okZ {
			return nil, fmt.Errorf("portal: teleportdata row %d coordinate", row+1)
		}
		ref := uint32(0)
		if value, ok := portalUint32(cells[3]); ok {
			ref = value
		}
		if _, exists := catalog.Destinations[id]; exists {
			return nil, fmt.Errorf("portal: duplicate teleport destination %d", id)
		}
		catalog.Destinations[id] = PortalDestination{ID: id, Ref: ref, Region: region, X: x, Y: y, Z: z}
		if ref != 0 {
			if _, exists := catalog.Sources[ref]; !exists {
				catalog.Sources[ref] = id
			}
		}
	}
	buildings := readPortalRows(filepath.Join(dir, "teleportbuilding.txt"))
	if len(buildings) == 0 {
		return nil, fmt.Errorf("portal: teleportbuilding.txt is absent or empty")
	}
	for row, cells := range buildings {
		if len(cells) <= 45 || cells[0] != "1" {
			continue
		}
		ref, ok := portalUint32(cells[1])
		region, okRegion := portalRegion(cells[41])
		if !ok || !okRegion || ref == 0 || region == 0 {
			continue
		}
		gateX, okX := portalFloat(cells[43])
		gateY, okY := portalFloat(cells[44])
		gateZ, okZ := portalFloat(cells[45])
		if !okX || !okY || !okZ {
			return nil, fmt.Errorf("portal: teleportbuilding row %d coordinate", row+1)
		}
		sourceID, ok := catalog.Sources[ref]
		if !ok {
			// The building is not a teleport source. Skip it.
			continue
		}
		gate := PortalGate{Ref: ref, SourceID: sourceID, Region: region, X: gateX, Y: gateY, Z: gateZ, GID: PortalGateIDBase + ref}
		catalog.GatesByRegion[region] = append(catalog.GatesByRegion[region], gate)
	}
	links := readPortalRows(filepath.Join(dir, "teleportlink.txt"))
	if len(links) == 0 {
		return nil, fmt.Errorf("portal: teleportlink.txt is absent or empty")
	}
	for row, cells := range links {
		if len(cells) < 21 || cells[0] != "1" {
			continue
		}
		source, okSource := portalUint32(cells[1])
		target, okTarget := portalUint32(cells[2])
		if !okSource || !okTarget {
			return nil, fmt.Errorf("portal: teleportlink row %d identity", row+1)
		}
		fee, _ := portalUint32(cells[3])
		if _, exists := catalog.Destinations[source]; !exists {
			continue
		}
		if _, exists := catalog.Destinations[target]; !exists {
			continue
		}
		// Columns 4 and 5 are the scheduling and combination selectors. The
		// server accepts the default rows (1 and 0). Skip the others, so the
		// plan never proposes a conditional gate (issue #171).
		if cells[4] != "1" || cells[5] != "0" {
			continue
		}
		link := PortalLink{Source: source, Target: target, Fee: int64(fee)}
		key := [2]uint32{source, target}
		if _, exists := catalog.Links[key]; exists {
			continue
		}
		catalog.Links[key] = link
		catalog.linksBySource[source] = append(catalog.linksBySource[source], link)
	}
	return catalog, nil
}

// Plan returns the gate hops that travel from fromRegion to toRegion, or
// reports false when no gate path exists. A same-region plan is empty and true
// (issue #171). The search is a breadth-first search over the region graph, so
// the plan uses the fewest gate crossings.
func (c *PortalCatalog) Plan(fromRegion, toRegion uint16) ([]PortalHop, bool) {
	if fromRegion == toRegion {
		return nil, true
	}
	visited := map[uint16]bool{fromRegion: true}
	previous := map[uint16]portalStep{}
	queue := []uint16{fromRegion}
	for len(queue) > 0 {
		region := queue[0]
		queue = queue[1:]
		for _, gate := range c.GatesByRegion[region] {
			for _, link := range c.linksBySource[gate.SourceID] {
				arrival, ok := c.Destinations[link.Target]
				if !ok || arrival.Region == 0 || arrival.Region == region {
					continue
				}
				if visited[arrival.Region] {
					continue
				}
				visited[arrival.Region] = true
				previous[arrival.Region] = portalStep{region: region, hop: PortalHop{
					GateRef: gate.Ref, GateGID: gate.GID, GateRegion: gate.Region,
					GateX: gate.X, GateY: gate.Y, GateZ: gate.Z,
					DestinationID: link.Target, ArrivalRegion: arrival.Region,
					ArrivalX: arrival.X, ArrivalY: arrival.Y, ArrivalZ: arrival.Z,
				}}
				if arrival.Region == toRegion {
					return assembleHops(previous, toRegion), true
				}
				queue = append(queue, arrival.Region)
			}
		}
	}
	return nil, false
}

// assembleHops walks the breadth-first parent chain from toRegion back to the
// start region and returns the hops in travel order (issue #171).
func assembleHops(previous map[uint16]portalStep, toRegion uint16) []PortalHop {
	var reversed []PortalHop
	for region := toRegion; ; {
		step, ok := previous[region]
		if !ok {
			break
		}
		reversed = append(reversed, step.hop)
		region = step.region
	}
	hops := make([]PortalHop, 0, len(reversed))
	for i := len(reversed) - 1; i >= 0; i-- {
		hops = append(hops, reversed[i])
	}
	return hops
}

// readPortalRows reads one shipped textdata file into tab-separated cells. The
// file is UTF-16LE (BOM or a null-heavy body) or UTF-8. Blank lines and
// // comments are skipped (issue #171).
func readPortalRows(path string) [][]string {
	buffer, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := decodePortalText(buffer)
	cells := make(texttable.Cells)
	var rows [][]string
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r\t ")
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		rows = append(rows, cells.Split(line))
	}
	return rows
}

// decodePortalText decodes the UTF-16LE encodings the Silkroad textdata ships
// and falls back to UTF-8 (issue #171).
func decodePortalText(buffer []byte) string {
	if len(buffer) >= 2 && buffer[0] == 0xff && buffer[1] == 0xfe {
		return decodePortalUTF16LE(buffer[2:])
	}
	sampled := len(buffer)
	if sampled > 512 {
		sampled = 512
	}
	nulls := 0
	for i := 0; i < sampled; i++ {
		if buffer[i] == 0x00 {
			nulls++
		}
	}
	if sampled > 0 && float64(nulls)/float64(sampled) > 0.2 {
		return decodePortalUTF16LE(buffer)
	}
	return string(buffer)
}

// decodePortalUTF16LE decodes a UTF-16LE byte body (issue #171).
func decodePortalUTF16LE(buffer []byte) string {
	units := make([]uint16, 0, len(buffer)/2)
	for i := 0; i+1 < len(buffer); i += 2 {
		units = append(units, binary.LittleEndian.Uint16(buffer[i:i+2]))
	}
	return string(utf16.Decode(units))
}

// portalUint32 parses one unsigned cell (issue #171).
func portalUint32(value string) (uint32, bool) {
	n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
	return uint32(n), err == nil
}

// portalRegion parses one region cell. A negative value keeps its low 16 bits,
// as the shipped tables store the region as a signed word (issue #171).
func portalRegion(value string) (uint16, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 32)
	if err != nil || n < -0x8000 || n > 0xffff {
		return 0, false
	}
	return uint16(n), true
}

// portalFloat parses one coordinate cell (issue #171).
func portalFloat(value string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}
