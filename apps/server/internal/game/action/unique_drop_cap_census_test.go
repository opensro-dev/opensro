/*
===========================================================================

unique_drop_cap_census_test.go - census: the native rolls the drop cap cuts

Port-only, not native (decision record on #2538). The census the room
asked for before the fix: for every unique in the shipped catalog, its
native assigned rows (loot.AssignedDropChances - the native table) are
counted over seeded kills with the beta drop cap on and off, at the
deployed beta shape (drop rate 20, cap 16, mix on). The roll stream of a
kill pair is identical until the cap itself draws, so a native row the
cap-off kill pays and the same-seed cap-on kill does not is a cap-cut
roll, counted per unique.

The census reconciles against the native table twice. A row the table
guarantees (PerKill 1) must land in every cap-off kill - otherwise the
pipeline, not the cap, is what loses it. And it must land in every
cap-on kill too: that is the floor the fix restores. Partial rows are
counted and reported, never asserted: the cap still brakes everything
past a unique's first native round, as the decision keeps it.

Lanes (planMonsterKillLoot): the unique prepass and the assigned rounds
roll the native tables (traced native); their repetition counts, the
pass scaling and the cap are the port-only beta shape; this census
measures only the cap lane's cuts.

===========================================================================
*/
package action

import (
	"math/rand/v2"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

// censusKillsPerUnique is how many same-seed kill pairs the census runs
// per unique. A guaranteed row resolves exactly at any count; partial
// rows are only counted, so the count sets the census's resolution.
const censusKillsPerUnique = 20

// censusSeed seeds every census kill; a pair differs only in the seed's
// kill index and the cap, never in the stream the kill itself reads.
const censusSeed = 20261104

// censusDeployedDropCap is the deployed shard's SRO_BETA_DROP_CAP.
const censusDeployedDropCap = 16

/*
================
censusKillGround

One planned kill of the unique with the given cap, returning the set of
codenames on the ground and how many drops the kill left. The roll
stream is reseeded per kill, so the cap-on and cap-off plans of a pair
read the same draws until the cap.
================
*/
func censusKillGround(rt *Runtime, player *enterworld.Character, target monster.Instance, dropCap int, seed uint64) (map[string]bool, int) {
	source := rand.New(rand.NewPCG(censusSeed, seed))
	rt.DropRoll = func() (uint32, error) { return source.Uint32N(dropRollDomain), nil }
	rt.DropCap = dropCap
	drops := planUniqueKill(rt, player, target)
	ground := map[string]bool{}
	for _, drop := range drops {
		ground[drop.Codename] = true
	}
	return ground, len(drops)
}

/*
================
TestUniqueDropCapCensus

Every unique's native assigned rows, counted through the production
planner with the drop cap on and off: the cap may cut no guaranteed row.
================
*/
func TestUniqueDropCapCensus(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)
	items := shippedItems(t)
	refs := monster.LoadMonsterRefs(dir)
	t.Setenv(progression.EnvBetaDropMix, "on")
	mix, err := progression.BetaDropMixFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	censused, nativeRows, guaranteedRows, cutRolls := 0, 0, 0, 0
	capOffDrops, capOnDrops := 0, 0
	for _, ref := range refs {
		if !RollsUniqueDrops(ref.MonsterType) {
			continue
		}
		rows, err := loot.AssignedDropChances(ref.Codename)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			continue
		}
		censused++
		nativeRows += len(rows)
		rt, _, player, target := newCombatTestRuntimeAtLevel(t, 1, 1)
		target.Ref = ref
		target.Nest.HasRarityOverride = false
		// The hunter matches the unique's level, so the level-gap
		// admission roll admits every planned item (threshold 100) and
		// only the cap can remove a native row.
		player.Level = testInt64(int64(ref.Level))
		rt.deps.(*enterworld.Deps).Items = items
		rt.deps.(*enterworld.Deps).MagicOptions = enterworld.NewTextdataMagicOptions(dir)
		// The deployed beta shape for the lanes under census (growth
		// defaults: rate 20, rare 5, mix on); the cap is the only lane
		// switched between the two plans of a pair.
		rt.DropPassRate, rt.RareRate, rt.DropMix = 20, 5, mix
		// Per native row: cap-off kills paying it, cap-on kills paying
		// it, and kills where the cap alone cut it.
		counts := make([][3]int, len(rows))
		for kill := 0; kill < censusKillsPerUnique; kill++ {
			seed := uint64(ref.RefObjID)*1000 + uint64(kill)
			off, offCount := censusKillGround(rt, player, target, 0, seed)
			on, onCount := censusKillGround(rt, player, target, censusDeployedDropCap, seed)
			capOffDrops += offCount
			capOnDrops += onCount
			for i, row := range rows {
				if off[row.Codename] {
					counts[i][0]++
				}
				if on[row.Codename] {
					counts[i][1]++
				}
				if off[row.Codename] && !on[row.Codename] {
					counts[i][2]++
				}
			}
		}
		uniqueCuts := 0
		for i, row := range rows {
			uniqueCuts += counts[i][2]
			if row.PerKill < 1 {
				continue
			}
			guaranteedRows++
			if counts[i][0] != censusKillsPerUnique {
				t.Errorf("census %s: guaranteed row %s landed in %d of %d cap-off kills; the pipeline, not the cap, loses it",
					ref.Codename, row.Codename, counts[i][0], censusKillsPerUnique)
			}
			if counts[i][1] != censusKillsPerUnique {
				t.Errorf("census %s: the cap cut guaranteed row %s in %d of %d kills (native chance 1, cap-on %d)",
					ref.Codename, row.Codename, counts[i][2], censusKillsPerUnique, counts[i][1])
			}
		}
		cutRolls += uniqueCuts
		t.Logf("census %s (level %d): %d native rows, %d cap-cut rolls in %d kill pairs",
			ref.Codename, ref.Level, len(rows), uniqueCuts, censusKillsPerUnique)
	}
	if censused == 0 || guaranteedRows == 0 {
		t.Fatalf("census covered %d uniques and %d guaranteed rows; it measured nothing", censused, guaranteedRows)
	}
	// The cap still brakes everything past the first native round: the
	// same kills leave strictly fewer drops with it on, and rolls past
	// the floor are still cut (cutRolls counts them above).
	if capOnDrops >= capOffDrops {
		t.Errorf("the cap no longer brakes a unique kill: %d drops with it on, %d with it off", capOnDrops, capOffDrops)
	}
	t.Logf("census total: %d uniques, %d native rows, %d guaranteed rows, %d cap-cut rolls in %d kill pairs; drops %d cap-off, %d cap-on",
		censused, nativeRows, guaranteedRows, cutRolls, censused*censusKillsPerUnique, capOffDrops, capOnDrops)
}

/*
================
censusUniqueRuntime

A planner runtime for one shipped unique at the deployed beta shape
(rate 20, rare 5, mix on), with a hunter of the unique's own level so
admission never removes a planned item. The cap is set per kill.
================
*/
func censusUniqueRuntime(t *testing.T, codename string) (*Runtime, *enterworld.Character, monster.Instance) {
	t.Helper()
	dir := gamedatatest.TextdataDir(t)
	var ref monster.MonsterRef
	found := false
	for _, candidate := range monster.LoadMonsterRefs(dir) {
		if candidate.Codename == codename {
			ref, found = candidate, true
			break
		}
	}
	if !found {
		t.Fatalf("unique reference %s is absent", codename)
	}
	if !RollsUniqueDrops(ref.MonsterType) {
		t.Fatalf("%s has non-unique monster type %d", codename, ref.MonsterType)
	}
	t.Setenv(progression.EnvBetaDropMix, "on")
	mix, err := progression.BetaDropMixFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	rt, _, player, target := newCombatTestRuntimeAtLevel(t, 1, 1)
	target.Ref = ref
	target.Nest.HasRarityOverride = false
	player.Level = testInt64(int64(ref.Level))
	rt.deps.(*enterworld.Deps).Items = shippedItems(t)
	rt.deps.(*enterworld.Deps).MagicOptions = enterworld.NewTextdataMagicOptions(dir)
	rt.DropPassRate, rt.RareRate, rt.DropMix = 20, 5, mix
	return rt, player, target
}

/*
================
TestUniqueFirstAssignedRoundSurvivesMinimumCap

Decision record on #2538, at the cap's tightest: a unique's guaranteed
rows (native chance 1) land in every kill at cap 1 and at the deployed
cap 16, because the first assigned round - the native round - sits
outside the cap. Before the fix they landed only when a beta duplicate
copy won the cap's uniform draw.
================
*/
func TestUniqueFirstAssignedRoundSurvivesMinimumCap(t *testing.T) {
	rt, player, target := censusUniqueRuntime(t, "MOB_CH_TIGERWOMAN")
	rows, err := loot.AssignedDropChances("MOB_CH_TIGERWOMAN")
	if err != nil {
		t.Fatal(err)
	}
	var guaranteed []string
	for _, row := range rows {
		if row.PerKill >= 1 {
			guaranteed = append(guaranteed, row.Codename)
		}
	}
	if len(guaranteed) == 0 {
		t.Fatal("Tiger Girl has no guaranteed native row; the test cannot show the rule")
	}
	source := rand.New(rand.NewPCG(2538, 1))
	rt.DropRoll = func() (uint32, error) { return source.Uint32N(dropRollDomain), nil }
	for _, dropCap := range []int{1, censusDeployedDropCap} {
		rt.DropCap = dropCap
		for kill := 0; kill < censusKillsPerUnique; kill++ {
			ground := map[string]bool{}
			for _, drop := range planUniqueKill(rt, player, target) {
				ground[drop.Codename] = true
			}
			for _, codename := range guaranteed {
				if !ground[codename] {
					t.Fatalf("cap %d, kill %d: guaranteed row %s is missing", dropCap, kill, codename)
				}
			}
		}
	}
}

/*
================
censusPlanCodenames

One planned kill's drop codenames in order, from a fresh roll stream.
================
*/
func censusPlanCodenames(rt *Runtime, player *enterworld.Character, target monster.Instance, dropCap int, seed uint64) []string {
	source := rand.New(rand.NewPCG(censusSeed, seed))
	rt.DropRoll = func() (uint32, error) { return source.Uint32N(dropRollDomain), nil }
	rt.DropCap = dropCap
	var codenames []string
	for _, drop := range planUniqueKill(rt, player, target) {
		codenames = append(codenames, drop.Codename)
	}
	return codenames
}

/*
================
censusRequireSamePlan

The cap-off plan and the unreachable-cap plan must agree drop for drop.
================
*/
func censusRequireSamePlan(t *testing.T, kind string, off, huge []string) {
	t.Helper()
	if len(off) == 0 || len(off) != len(huge) {
		t.Fatalf("%s plan changed with the cap off: %d drops at cap 0, %d at an unreachable cap", kind, len(off), len(huge))
	}
	for i := range off {
		if off[i] != huge[i] {
			t.Fatalf("%s plan drop %d changed with the cap off: %s at cap 0, %s at an unreachable cap", kind, i, off[i], huge[i])
		}
	}
}

/*
================
TestDropCapOffLeavesThePlanUntouched

Cap 0 and a cap no kill can reach leave the same plan, drop for drop,
for an ordinary monster and for a unique: the cap lane reads nothing
and draws nothing when it does not cut, so SRO_BETA_DROP_CAP off stays
the native plan byte for byte with the fix in place.
================
*/
func TestDropCapOffLeavesThePlanUntouched(t *testing.T) {
	rt, player, target := censusUniqueRuntime(t, "MOB_CH_TIGERWOMAN")
	censusRequireSamePlan(t, "unique",
		censusPlanCodenames(rt, player, target, 0, 7),
		censusPlanCodenames(rt, player, target, 1_000_000, 7))
	ordinary, _, ordinaryPlayer, ordinaryTarget := newCombatTestRuntimeAtLevel(t, 1, 1)
	ordinary.deps.(*enterworld.Deps).Items = shippedItems(t)
	ordinary.DropPassRate = 20
	censusRequireSamePlan(t, "ordinary",
		censusPlanCodenames(ordinary, ordinaryPlayer, ordinaryTarget, 0, 7),
		censusPlanCodenames(ordinary, ordinaryPlayer, ordinaryTarget, 1_000_000, 7))
}
