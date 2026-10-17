package bot_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/bot"
)

// writePortalTable writes one tab-separated textdata file. The loader accepts
// UTF-8, so the fixture is not padded to UTF-16LE.
func writePortalTable(t *testing.T, dir, name string, rows [][]string) {
	t.Helper()
	var out strings.Builder
	for _, row := range rows {
		out.WriteString(strings.Join(row, "\t"))
		out.WriteString("\n")
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(out.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

// columnRow returns a row of size "0" cells with the named cells set.
func columnRow(size int, cells map[int]string) []string {
	row := make([]string, size)
	for i := range row {
		row[i] = "0"
	}
	for i, value := range cells {
		row[i] = value
	}
	return row
}

// portalFixture writes a two-region catalogue: one gate in region 100 with a
// link to a destination in region 200.
func portalFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writePortalTable(t, dir, "teleportdata.txt", [][]string{
		{"1", "1000", "GATE_A", "500", "0", "100", "500", "0", "500"},
		{"1", "2000", "GATE_B", "0", "0", "200", "900", "0", "900"},
	})
	writePortalTable(t, dir, "teleportbuilding.txt", [][]string{
		columnRow(58, map[int]string{0: "1", 1: "500", 41: "100", 43: "500", 44: "0", 45: "500"}),
	})
	writePortalTable(t, dir, "teleportlink.txt", [][]string{
		columnRow(21, map[int]string{0: "1", 1: "1000", 2: "2000", 3: "0", 4: "1", 5: "0"}),
	})
	return dir
}

// TestLoadPortalCatalogReadsTheGateGraph guards the shipped table shapes
// (issue #171): the gate identity is the base plus the teleportbuilding ref,
// and the link indexes the (source, target) pair.
func TestLoadPortalCatalogReadsTheGateGraph(t *testing.T) {
	catalog, err := bot.LoadPortalCatalog(portalFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	gate, ok := catalog.Sources[500]
	if !ok || gate != 1000 {
		t.Fatalf("source ref 500 = %d, %v, want 1000, true", gate, ok)
	}
	arrival, ok := catalog.Destinations[2000]
	if !ok || arrival.Region != 200 {
		t.Fatalf("destination 2000 = %+v, want region 200", arrival)
	}
	if _, ok := catalog.Links[[2]uint32{1000, 2000}]; !ok {
		t.Fatal("link (1000, 2000) is missing")
	}
	gates := catalog.GatesByRegion[100]
	if len(gates) != 1 {
		t.Fatalf("region 100 gates = %d, want 1", len(gates))
	}
	if gates[0].GID != bot.PortalGateIDBase+500 {
		t.Fatalf("gate gid = %d, want %d", gates[0].GID, bot.PortalGateIDBase+500)
	}
}

// TestPortalPlanFindsASingleHop guards the region search (issue #171): one
// gate crossing from the current region to the target region.
func TestPortalPlanFindsASingleHop(t *testing.T) {
	catalog, err := bot.LoadPortalCatalog(portalFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	hops, ok := catalog.Plan(100, 200)
	if !ok || len(hops) != 1 {
		t.Fatalf("plan = %+v, %v, want one hop", hops, ok)
	}
	hop := hops[0]
	if hop.GateRegion != 100 || hop.ArrivalRegion != 200 {
		t.Fatalf("hop regions = %d -> %d, want 100 -> 200", hop.GateRegion, hop.ArrivalRegion)
	}
	if hop.GateGID != bot.PortalGateIDBase+500 || hop.DestinationID != 2000 {
		t.Fatalf("hop gate/destination = %d/%d, want %d/2000", hop.GateGID, hop.DestinationID, bot.PortalGateIDBase+500)
	}
	if hop.ArrivalX != 900 || hop.ArrivalZ != 900 {
		t.Fatalf("arrival = %v,%v, want 900,900", hop.ArrivalX, hop.ArrivalZ)
	}
}

// TestPortalPlanReportsNoPath guards the disconnected graph (issue #171): a
// region with no outgoing gate has no plan.
func TestPortalPlanReportsNoPath(t *testing.T) {
	catalog, err := bot.LoadPortalCatalog(portalFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if hops, ok := catalog.Plan(200, 100); ok || len(hops) != 0 {
		t.Fatalf("reverse plan = %+v, %v, want no path", hops, ok)
	}
	if hops, ok := catalog.Plan(100, 999); ok || len(hops) != 0 {
		t.Fatalf("unknown region plan = %+v, %v, want no path", hops, ok)
	}
}

// TestPortalPlanIsEmptyInTheSameRegion guards the same-region case
// (issue #171).
func TestPortalPlanIsEmptyInTheSameRegion(t *testing.T) {
	catalog, err := bot.LoadPortalCatalog(portalFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if hops, ok := catalog.Plan(100, 100); !ok || len(hops) != 0 {
		t.Fatalf("same-region plan = %+v, %v, want empty, true", hops, ok)
	}
}

// chainFixture writes a three-region chain 100 -> 200 -> 300.
func chainFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writePortalTable(t, dir, "teleportdata.txt", [][]string{
		{"1", "1000", "A", "500", "0", "100", "0", "0", "0"},
		{"1", "2000", "B", "600", "0", "200", "0", "0", "0"},
		{"1", "3000", "C", "0", "0", "300", "0", "0", "0"},
	})
	writePortalTable(t, dir, "teleportbuilding.txt", [][]string{
		columnRow(58, map[int]string{0: "1", 1: "500", 41: "100"}),
		columnRow(58, map[int]string{0: "1", 1: "600", 41: "200"}),
	})
	writePortalTable(t, dir, "teleportlink.txt", [][]string{
		columnRow(21, map[int]string{0: "1", 1: "1000", 2: "2000", 4: "1", 5: "0"}),
		columnRow(21, map[int]string{0: "1", 1: "2000", 2: "3000", 4: "1", 5: "0"}),
	})
	return dir
}

// TestPortalPlanChainsHopsInTravelOrder guards the multi-hop search
// (issue #171): the plan lists the crossings from the current region.
func TestPortalPlanChainsHopsInTravelOrder(t *testing.T) {
	catalog, err := bot.LoadPortalCatalog(chainFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	hops, ok := catalog.Plan(100, 300)
	if !ok || len(hops) != 2 {
		t.Fatalf("plan = %+v, %v, want two hops", hops, ok)
	}
	if hops[0].GateRegion != 100 || hops[0].ArrivalRegion != 200 {
		t.Fatalf("hop 0 = %d -> %d, want 100 -> 200", hops[0].GateRegion, hops[0].ArrivalRegion)
	}
	if hops[1].GateRegion != 200 || hops[1].ArrivalRegion != 300 {
		t.Fatalf("hop 1 = %d -> %d, want 200 -> 300", hops[1].GateRegion, hops[1].ArrivalRegion)
	}
	if hops[0].GateGID != bot.PortalGateIDBase+500 || hops[1].GateGID != bot.PortalGateIDBase+600 {
		t.Fatalf("hop gates = %d, %d, want %d, %d",
			hops[0].GateGID, hops[1].GateGID, bot.PortalGateIDBase+500, bot.PortalGateIDBase+600)
	}
}

// TestLoadPortalCatalogRejectsAnAbsentTable guards the loader error
// (issue #171).
func TestLoadPortalCatalogRejectsAnAbsentTable(t *testing.T) {
	if _, err := bot.LoadPortalCatalog(t.TempDir()); err == nil {
		t.Fatal("an empty directory loaded a catalogue")
	}
}
