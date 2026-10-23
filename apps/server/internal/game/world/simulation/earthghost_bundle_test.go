package simulation

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

// Optional deployed-data audit. Ordinary regressions use the checked-in subset;
// this test verifies the production loader joins ALL supplied Earth Ghost nests.
// Set SRO_EARTH_GHOST_TEST_BUNDLE_ROOT to the extracted server/ directory.
func TestEarthGhostSuppliedBundlePopulation(t *testing.T) {
	root := os.Getenv("SRO_EARTH_GHOST_TEST_BUNDLE_ROOT")
	if root == "" {
		t.Skip("set SRO_EARTH_GHOST_TEST_BUNDLE_ROOT for the supplied-data audit")
	}
	bytes, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	const manifest = "5682775b689f28a468448c3a989c51fecc73932514fd26f3d02cb32779eaa603"
	if got := fmt.Sprintf("%x", sha256.Sum256(bytes)); got != manifest {
		t.Fatalf("this regression pins supplied manifest %s, got %s", manifest, got)
	}
	// The references alone: no population evidence changes a reference row.
	catalog, err := monster.LoadTemplate(filepath.Join(root, "textdata"), nil)
	if err != nil {
		t.Fatal(err)
	}
	ref, ok := catalog.Refs[1963]
	if !ok || ref.Codename != "MOB_WC_EARTHGHOST" || ref.RunSpeed != 75 || ref.BodyRadius != 3 || ref.DefaultSkillIDs[0] != 210 {
		t.Fatalf("reference mismatch %+v", ref)
	}
	nests, checks := 0, 0
	radii := map[float64]int{}
	for _, nest := range catalog.Nests {
		if nest.RefObjID != ref.RefObjID {
			continue
		}
		nests++
		radii[nest.Radius]++
		variants := []monster.NestRow{nest}
		if nest.HasChampionTactics {
			variants = append(variants, nest.PromoteToChampionTactics(1), nest.PromoteToChampionTactics(4))
		}
		for grade, variant := range variants {
			t.Run(fmt.Sprintf("nest_%02d_variant_%d", nests, grade), func(t *testing.T) {
				if !variant.HasControls || variant.Controls.TraceBoundary != 1 || variant.Controls.TraceData != 500 || variant.Controls.HomingType != 1 || !ordinarySquadApproach(monster.Instance{Nest: variant}) {
					t.Fatalf("unverified ordinary policy %+v", variant.Controls)
				}
				ops, a, target := earthGhostFixture(t)
				s := ops.Monsters
				a.Ref = ref
				a.Nest = variant
				a.Spawn = variant.SpawnPoint
				m := monster.NewSpawnMover(a, 10000)
				mustMoverTransition(&m, monster.MoverEventSpawnHoldElapsed, 0)
				m.Pose.X += 1.5*variant.Radius + float64(variant.Controls.TraceData) + 50
				m.Pose = normalizeMonsterPose(m.Pose)
				s.mu.Lock()
				state := s.populationForObject(monsterTestDivision, a.Gid)
				state.instances.set(a.Gid, a)
				state.movers.set(a.Gid, m)
				s.mu.Unlock()
				if !s.ArmRetaliation(monsterTestDivision, a.Gid, target.Gid) {
					t.Fatal("retaliation not admitted")
				}
				target.Pose = poseToSpawn(m.Pose)
				target.Pose.X += 10
				target.Pose = NormalizeSpawnFrame(target.Pose)
				ops.advanceInstance(monsterTestDivision, a, []playerPose{target}, 10000)
				after, _ := s.Mover(monsterTestDivision, a.Gid)
				if after.TargetGID() != 0 || after.Mode() != monster.MoverReturning {
					t.Fatal("nest did not disengage and return", after.Mode(), after.TargetGID())
				}
				checks++
			})
		}
	}
	if nests != 65 || checks != 195 {
		t.Fatalf("population coverage changed: nests=%d variants=%d", nests, checks)
	}
	t.Logf("production-loaded reference=%d nests=%d normal/champion/giant checks=%d radii=%v manifest=%s", ref.RefObjID, nests, checks, radii, manifest)
}
