package simulation

import (
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"
)

func TestNPCHeadingUsesPlacementNotCrossVersionIdentity(t *testing.T) {
	for _, code := range []string{"NPC_CH_WAREHOUSE_M", "NPC_CH_WAREHOUSE_W"} {
		x, y, z := 980.8, -32.43, 989.13
		if code == "NPC_CH_WAREHOUSE_W" {
			x, y, z = 980.35, -32.52, 989.19
		}
		if got, found := npcPlacementHeading(code, 25000, x, y, z); !found || got != 16565 {
			t.Fatalf("%s heading=%d found=%v", code, got, found)
		}
		if _, found := npcPlacementHeading(code, 25000, x+1, y, z); found {
			t.Fatal("moved NPC inherited heading")
		}
		if _, found := npcPlacementHeading(code, 25001, x, y, z); found {
			t.Fatal("different region inherited heading")
		}
	}
	if _, found := npcPlacementHeading("NPC_UNKNOWN", 25000, 980.8, -32.43, 989.13); found {
		t.Fatal("unknown codename inherited heading")
	}
}

func TestPublishedNPCHeadingCoverage(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	roster := LoadNpcWorldRoster(dir)
	if len(roster) < 150 {
		t.Fatal("published NPC roster missing")
	}
	matched, sansan := 0, false
	for _, npc := range roster {
		s := npc.Spawn
		if heading, found := npcPlacementHeading(npc.Codename, s.RegionID, s.X, s.Y, s.Z); found {
			matched++
			if s.Angle != heading {
				t.Fatalf("%s lost heading", npc.Codename)
			}
		}
		if npc.Codename == "NPC_CH_WAREHOUSE_M" {
			sansan = true
			if s.Angle != 16565 {
				t.Fatalf("Sansan heading=%d", s.Angle)
			}
		}
	}
	if !sansan || matched < 100 {
		t.Fatalf("heading coverage %d/%d Sansan=%v", matched, len(roster), sansan)
	}
	t.Logf("recovered headings for %d/%d published NPC placements", matched, len(roster))
}

func TestNPCClassStructuresUseRecoveredHeadings(t *testing.T) {
	for _, row := range []struct {
		code    string
		region  uint16
		x, y, z float64
		heading uint16
	}{
		{"STRUCTURE_GATE_PULLEY_JA_03", 17736, 1520.12, 39.990002, 1514.85, 48605},
		{"STRUCTURE_GATE_PULLEY_JA_02", 17990, 353.53, 40, 597.38, 48423},
		{"STRUCTURE_GATE_PULLEY_JA_01", 17735, 370.66, 0, 420.91, 48423},
	} {
		if got, ok := npcPlacementHeading(row.code, row.region, row.x, row.y, row.z); !ok || got != row.heading {
			t.Fatalf("%s: %d, found %v", row.code, got, ok)
		}
	}
}
