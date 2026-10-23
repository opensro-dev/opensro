/*
===========================================================================

npc_headings_test.go - complete facing coverage and emitted spawn headings

The lookup rules run on small synthetic tables. The operator's table
(source-data, skipped without it) pins the reviewed directions, including
valid zero and signed SQL values; every published placement must resolve
and reach the shared spawn wire used by world entry and interest.

===========================================================================
*/
package simulation

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/testsupport/tier"

	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
sourceNPCHeadings

The operator's table, laid out as the projection's derived data holds it
(skips without it).
================
*/
func sourceNPCHeadings(t *testing.T) *NPCHeadings {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, filepath.FromSlash(NPCHeadingsFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, licensed.SourceDataFile(t, "server/"+NPCHeadingsFile), 0o644); err != nil {
		t.Fatal(err)
	}
	headings, err := LoadNPCHeadings(dir)
	if err != nil || headings == nil {
		t.Fatalf("load: %v, %v", headings, err)
	}
	return headings
}

/*
================
syntheticNPCHeadings

Rows written as the table holds them: [codename, region, x10, y10, z10,
heading], in the recovered, supplement and alexandria sections.
================
*/
func syntheticNPCHeadings(t *testing.T, recovered, supplement, alexandria string) *NPCHeadings {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, filepath.FromSlash(NPCHeadingsFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	text := `{"format":"sro-npc-headings-v1","recovered":[` + recovered + `],"supplement":[` + supplement +
		`],"alexandria":[` + alexandria + `]}`
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	headings, err := LoadNPCHeadings(dir)
	if err != nil {
		t.Fatal(err)
	}
	return headings
}

/*
================
TestNPCHeadingPrecedenceAndAbsence

The earlier section wins a placement two sections hold; a nil or absent
table knows none; a malformed table is refused.
================
*/
func TestNPCHeadingPrecedenceAndAbsence(t *testing.T) {
	headings := syntheticNPCHeadings(t, `["NPC_A",1,10,0,20,100]`, `["NPC_A",1,10,0,20,200],["NPC_B",1,10,0,20,300]`,
		`["NPC_C",2,0,0,0,0]`)
	for _, tc := range []struct {
		code   string
		region uint16
		want   uint16
	}{{"NPC_A", 1, 100}, {"NPC_B", 1, 300}, {"NPC_C", 2, 0}} {
		x, z := 1.0, 2.0
		if tc.code == "NPC_C" {
			x, z = 0, 0
		}
		if got, found := headings.placement(tc.code, tc.region, x, 0, z); !found || got != tc.want {
			t.Fatalf("%s heading=%d found=%v, want %d", tc.code, got, found, tc.want)
		}
	}
	var none *NPCHeadings
	if _, found := none.placement("NPC_A", 1, 1, 0, 2); found {
		t.Fatal("a missing table knows a placement")
	}
	if absent, err := LoadNPCHeadings(t.TempDir()); absent != nil || err != nil {
		t.Fatalf("absent table: %v, %v", absent, err)
	}
}

/*
================
TestAlexandriaNPCHeadingsUseAuthoredPlacements
================
*/
func TestAlexandriaNPCHeadingsUseAuthoredPlacements(t *testing.T) {
	headings := sourceNPCHeadings(t)
	for _, row := range []struct {
		code    string
		region  uint16
		x, y, z float64
		heading uint16
	}{
		{"NPC_SD_T_AREA_WAREHOUSE2", 23603, 462.17001, 1530.2, 240.59, 19842},
		{"NPC_SD_T_AREA_ARMOR", 23602, 639.19, 1364.63, 87.459999, 61530},
		{"NPC_SD_M_AREA_ARMOR", 23087, 1731.92, 863.31, 875.04999, 7827},
		{"NPC_SD_T_AREA_SMITH", 23346, 648.39001, 1310.96, 1730.5, 0},
		{"NPC_SD_M_AREA_WATERMAN_2", 24111, 1470.21, 545, 563.01001, 27670},
		{"NPC_SD_M_AREA_WATERMAN_2", 24112, 1689.52, 519.32001, 157.02, 33131},
	} {
		if got, found := headings.placement(row.code, row.region, row.x, row.y, row.z); !found || got != row.heading {
			t.Fatalf("%s heading=%d found=%v, want=%d", row.code, got, found, row.heading)
		}
	}
	if _, found := headings.placement("NPC_SD_T_AREA_ARMOR", 23346, 708.39001, 1310.96, 1730.5); found {
		t.Fatal("removed inferred Viviana position inherited an authored heading")
	}
}

/*
================
TestNPCHeadingUsesPlacementNotCrossVersionIdentity
================
*/
func TestNPCHeadingUsesPlacementNotCrossVersionIdentity(t *testing.T) {
	headings := syntheticNPCHeadings(t, `["NPC_CH_WAREHOUSE_M",25000,9808,-324,9891,16565],["NPC_CH_WAREHOUSE_W",25000,9803,-325,9892,16565]`, ``, ``)
	for _, code := range []string{"NPC_CH_WAREHOUSE_M", "NPC_CH_WAREHOUSE_W"} {
		x, y, z := 980.8, -32.43, 989.13
		if code == "NPC_CH_WAREHOUSE_W" {
			x, y, z = 980.35, -32.52, 989.19
		}
		if got, found := headings.placement(code, 25000, x, y, z); !found || got != 16565 {
			t.Fatalf("%s heading=%d found=%v", code, got, found)
		}
		if _, found := headings.placement(code, 25000, x+1, y, z); found {
			t.Fatal("moved NPC inherited heading")
		}
		if _, found := headings.placement(code, 25001, x, y, z); found {
			t.Fatal("different region inherited heading")
		}
	}
	if _, found := headings.placement("NPC_UNKNOWN", 25000, 980.8, -32.43, 989.13); found {
		t.Fatal("unknown codename inherited heading")
	}
}

/*
================
TestPublishedNPCHeadingCoverage
================
*/
func TestPublishedNPCHeadingCoverage(t *testing.T) {
	tier.Long(t, "every published NPC placement has a heading")
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	headings := sourceNPCHeadings(t)
	roster := LoadNpcWorldRoster(dir, headings)
	if len(roster) < 150 {
		t.Fatal("published NPC roster missing")
	}
	matched, sansan := 0, false
	const npcSpawnHeadingOffset = 22
	for _, npc := range roster {
		s := npc.Spawn
		heading, found := headings.placement(npc.Codename, s.RegionID, s.X, s.Y, s.Z)
		if !found {
			t.Fatalf("%s at region %d (%g,%g,%g) silently falls back to zero", npc.Codename, s.RegionID, s.X, s.Y, s.Z)
		}
		matched++
		if s.Angle != heading {
			t.Fatalf("%s lost heading", npc.Codename)
		}
		row := BuildNpcCreateRow(npc, Spawn{Angle: 12345})
		if got := binary.LittleEndian.Uint16(row[npcSpawnHeadingOffset:]); got != heading {
			t.Fatalf("%s wire heading=%d want=%d", npc.Codename, got, heading)
		}
		if npc.Codename == "NPC_CH_WAREHOUSE_M" {
			sansan = true
			if s.Angle != 16565 {
				t.Fatalf("Sansan heading=%d", s.Angle)
			}
		}
	}
	if !sansan || matched != len(roster) {
		t.Fatalf("heading coverage %d/%d Sansan=%v", matched, len(roster), sansan)
	}
	t.Logf("resolved headings for %d/%d published NPC placements", matched, len(roster))
}

/*
================
TestNPCClassStructuresUseRecoveredHeadings
================
*/
func TestNPCClassStructuresUseRecoveredHeadings(t *testing.T) {
	headings := sourceNPCHeadings(t)
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
		if got, ok := headings.placement(row.code, row.region, row.x, row.y, row.z); !ok || got != row.heading {
			t.Fatalf("%s: %d, found %v", row.code, got, ok)
		}
	}
}

/*
================
TestNPCSupplementPreservesReviewedPlacements
================
*/
func TestNPCSupplementPreservesReviewedPlacements(t *testing.T) {
	headings := sourceNPCHeadings(t)
	cases := []struct {
		code    string
		region  uint16
		x, y, z float64
		heading uint16
	}{
		{"NPC_CH_SOLDIER_EA2", 25001, 1387.17, -0.07, 1765.02, 32767},
		{"NPC_CH_SOLDIER_EM1", 25000, 1009.54, 0.0, 1902.64, 49333},
		{"NPC_CH_SOLDIER_WE1", 25255, 334.98999, -0.28, 34.18, 0},
		{"NPC_TD_THIEF_D", 24758, 627.47998, 19.360001, 401.48999, 8191},
		{"NPC_CH_SOLDIER_SO1", 25000, 930.07001, -0.41, 30.66, 16019},
		{"NPC_TK_SPECIAL", 26753, 131.41, 113.1, 1404.95, 13653},
		{"NPC_CH_SPECIAL2", 23712, 411.03, 1383.3199, 1515.89, 52792},
		{"NPC_WC_SPECIAL2", 23445, 1132.42, 192.99001, 833.71997, 32767},
		{"NPC_CA_ACCESSORY", 27243, 1635.8101, 180.0, 1460.36, 10922},
		{"NPC_CA_SPECIAL", 27244, 837.14001, 180.0, 1823.25, 16201},
		{"NPC_RM_SPECIAL", 23411, 377.14999, 2628.78, 104.86, 54430},
		{"NPC_EU_ADVICE2", 26957, 1650.2, 83.870003, 1332.48, 16565},
		{"NPC_EU_ADVICE", 27471, 1349.25, 82.699997, 412.72, 24575},
		{"NPC_EU_ADVICE3", 26959, 568.85999, 83.629997, 1116.58, 21845},
		{"NPC_EU_SPECIAL", 26959, 351.01001, 80.400002, 228.24001, 16383},
		{"NPC_CH_EVENT_KISAENG1", 26959, 832.84998, 83.739998, 1113.0, 24757},
		{"NPC_CH_EVENT_KISAENG1", 26265, 911.34003, -106.76, 1569.58, 16201},
	}
	for _, row := range cases {
		got, found := headings.placement(row.code, row.region, row.x, row.y, row.z)
		if !found || got != row.heading {
			t.Fatalf("%s region %d heading=%d found=%v want=%d", row.code, row.region, got, found, row.heading)
		}
		if _, found := headings.placement(row.code, row.region, row.x+1, row.y, row.z); found {
			t.Fatalf("%s silently inherited a relocated heading", row.code)
		}
	}
}

/*
================
TestNPCHeadingKeepsEarlierExactEvidence
================
*/
func TestNPCHeadingKeepsEarlierExactEvidence(t *testing.T) {
	headings := sourceNPCHeadings(t)
	cases := []struct {
		code    string
		region  uint16
		x, y, z float64
		heading uint16
	}{
		{"NPC_KT_HORSE", 23431, 1540.88, 249.81, 1873.05, 56432},
		{"NPC_CA_ARMOR", 27499, 1298.11, 180.0, 358.28, 52063},
	}
	for _, row := range cases {
		got, found := headings.placement(row.code, row.region, row.x, row.y, row.z)
		if !found || got != row.heading {
			t.Fatalf("%s lost earlier evidence: heading=%d found=%v", row.code, got, found)
		}
	}
}
