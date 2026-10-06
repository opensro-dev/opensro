/*
===========================================================================

population_settled_world_test.go - the real world's boot fill settles

Runs the shipped monster template (every nest and hive of the 1.150
server data) through population passes on a simulated 100 ms clock and
requires PopulationSettled within the GameWorld's admission bound. The
single-nest test could not show that a whole-world rule never settles;
this one would have.

===========================================================================
*/

package simulation

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

const (
	// worldFillBoundMs mirrors the GameWorld's bootFillLimit (cmd/services/
	// sro-gameworld), which must stay well under the job's healthy_deadline.
	worldFillBoundMs = 60000
	// worldAfterSettleMs is how long the passes after admission are watched.
	worldAfterSettleMs = 60000
	// worldMaxSpawnsAfterSettle bounds one pass after admission. The 2026-10-07
	// boot spent 342 ms on about 6000 placements (~57 us each), so 1500 is
	// about 85 ms: under SlowHookThreshold (100 ms) on that machine.
	worldMaxSpawnsAfterSettle = 1500
)

/*
================
TestRealWorldBootFillSettlesInsideTheBound
================
*/
func TestRealWorldBootFillSettlesInsideTheBound(t *testing.T) {
	root := os.Getenv("SRO_SERVER_GAME_DATA_ROOT")
	if root == "" {
		t.Skip("set SRO_SERVER_GAME_DATA_ROOT to the server game data for the whole-world fill")
	}
	template := monster.LoadTemplate(filepath.Join(root, "textdata"))
	if len(template.Nests) < 1000 {
		t.Fatalf("loaded %d nests; not the shipped world", len(template.Nests))
	}
	s := NewMonsterState(template)
	now := time.Unix(100, 0)
	s.SetTimeSource(func() time.Time { return now })
	s.StartDivision("world")
	settledAt := int64(-1)
	for elapsed := int64(0); elapsed <= worldFillBoundMs; elapsed += int64(DefaultTickInterval / time.Millisecond) {
		s.AdvancePopulation(s.CurrentTimeMillis())
		if s.PopulationSettled("world") {
			settledAt = elapsed
			break
		}
		now = now.Add(DefaultTickInterval)
	}
	resident := s.Observatory("world", nil).Resident
	if settledAt < 0 {
		t.Fatalf("the shipped world did not settle within %d ms (%d resident)", worldFillBoundMs, resident)
	}
	// After admission the passes must be light: the heavy placement is done.
	state := s.divs["world"]
	var heaviest uint64
	for elapsed := int64(0); elapsed < worldAfterSettleMs; elapsed += int64(DefaultTickInterval / time.Millisecond) {
		now = now.Add(DefaultTickInterval)
		before := state.spawns
		s.AdvancePopulation(s.CurrentTimeMillis())
		heaviest = max(heaviest, state.spawns-before)
	}
	t.Logf("%d nests: settled after %d ms with %d resident; heaviest pass after admission placed %d",
		len(template.Nests), settledAt, resident, heaviest)
	if heaviest > worldMaxSpawnsAfterSettle {
		t.Fatalf("a pass after admission placed %d monsters (bound %d)", heaviest, worldMaxSpawnsAfterSettle)
	}
	if resident < len(template.Nests)/2 {
		t.Fatalf("settled with only %d resident for %d nests: the fill was cut short", resident, len(template.Nests))
	}
}
