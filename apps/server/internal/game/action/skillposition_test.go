/*
===========================================================================

skillposition_test.go - Ghost Walk destination and recovery admission

Exercise the authority boundary with shipped movement ranks so position
validation and reuse timing remain part of the same accepted action.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"math"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"os"
	"path/filepath"
	"testing"
)

/*
================
TestGhostWalkShippedRanksAndAuthority
================
*/
func TestGhostWalkShippedRanksAndAuthority(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)
	if _, err := os.Stat(filepath.Join(dir, "skilldata.txt")); os.IsNotExist(err) {
		t.Skip("production v1.150 textdata unavailable")
	}
	source := enterworld.NewTextdataSkills(dir)
	if err := source.Load(); err != nil {
		t.Fatal(err)
	}
	ids := []uint32{}
	for _, projection := range source.SpawnSkillRows() {
		if projection.UI != nil && projection.UI.GroundTarget {
			ids = append(ids, projection.ID)
		}
	}
	if len(ids) != 16 {
		t.Fatalf("ground-target coverage=%d, want 12 Phantom + 4 Shadow", len(ids))
	}
	for _, id := range ids {
		skill, ok := source.SkillByID(id)
		if !ok || !skill.PositionEffect.Pinned || source.ExecutionPlan(id).Kind() != enterworld.SkillExecutionPosition {
			t.Fatalf("rank %d: %+v", id, skill)
		}
		if skill.CoolTimeGroup != 59 || skill.CoolTimeMs != 5000 {
			t.Fatalf("authored cooldown: %+v", skill)
		}
		if skill.Group == 768 && skill.PositionEffect.Range != 215+uint32(skill.Level-1)*5 {
			t.Fatalf("Shadow rank range: %+v", skill.PositionEffect)
		}
		for _, mode := range []string{"accept", "owned-navigation", "blocked", "no-navigation", "no-mana", "unlearned", "entity-target", "cross-plane", "cooldown", "moving", "clipped", "mounted", "commit-refused", "cooldown-group"} {
			t.Run(skill.Codename+"/"+mode, func(t *testing.T) {
				rt, clock, c, _ := newCombatTestRuntime(t, 100)
				rt.deps.SkillData().(staticSkillSource)[id] = skill
				c.Skills = append(c.Skills, id)
				mp := int64(skill.Consumption.MP)
				c.CurrentMP = &mp
				c.Intellect = testInt64(1000)
				key := simulation.WorldKey(testDivision, c.Name)
				if mode == "moving" {
					rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
						from := w.Spawn
						w.Spawn.X += 100
						w.MoveSegment = &simulation.MoveSegment{From: from, StartedAtMs: clock.NowMs() - 500, ArrivesAtMs: clock.NowMs() + 500}
					})
				}
				before := rt.liveSpawn(key, c, clock.NowMs())
				called := false
				rt.ConstrainMovement = func(_ string, from, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) {
					called = true
					if from != before {
						t.Fatalf("stale source %+v != %+v", from, before)
					}
					if math.Abs(to.X-from.X-float64(skill.PositionEffect.Range)) > 0.001 {
						t.Fatalf("unclamped target %+v", to)
					}
					if mode == "blocked" {
						return from, &simulation.MoveError{}
					}
					if mode == "clipped" {
						to.X = from.X + 50
					}
					return to, nil
				}
				request := wire.SkillAction{ActionId: id, HasGroundTarget: true, Region: before.RegionID, GroundX: uint16(before.X + 500), GroundY: uint16(before.Y), GroundZ: uint16(before.Z)}
				switch mode {
				case "owned-navigation":
					legacy := rt.ConstrainMovement
					rt.ConstrainMovement = nil
					rt.ConstrainWalk = func(name string, from simulation.Spawn, owner simulation.NavOwner, to simulation.Spawn) (simulation.Spawn, simulation.NavWalk, *simulation.MoveError) {
						goal, err := legacy(name, from, to)
						return goal, simulation.NavWalk{Rest: owner}, err
					}
				case "mounted":
					c.ActiveCOS = &enterworld.CharacterCOS{Mounted: true}
				case "commit-refused":
					rt.deps.(*enterworld.Deps).UpdateCharacter = func(*enterworld.Character, string, func() bool) bool { return false }
				case "no-navigation":
					rt.ConstrainMovement = nil
				case "no-mana":
					mp--
				case "unlearned":
					c.Skills = []uint32{2}
				case "entity-target":
					request.HasGroundTarget = false
					request.HasTarget = true
					request.TargetGid = 123
				case "cross-plane":
					request.Region = 0x8001
				case "cooldown-group":
					c.SharedSkillCooldowns = map[uint8]int64{skill.CoolTimeGroup: clock.NowMs() + 5000}
				case "cooldown":
					registerOffensiveCooldown(c, skill, clock.NowMs())
				}
				beforeMP := mp
				beforeCooldown := c.OffensiveSkillCooldowns[skill.Group]
				r := rt.HandleTargetInteract(testDivision, c, request.Encode())
				if mode != "accept" && mode != "owned-navigation" && mode != "moving" && mode != "clipped" {
					if *c.CurrentMP != beforeMP || rt.liveSpawn(key, c, clock.NowMs()) != before || len(r.Broadcast) != 0 || c.OffensiveSkillCooldowns[skill.Group] != beforeCooldown {
						t.Fatalf("refusal changed authority: %+v", r)
					}
					return
				}
				if len(r.Frames) != 3 {
					t.Fatalf("acceptance failed: %+v payload=%v called=%v", r, r.Frames, called)
				}
				assertOpcodes(t, r.Frames, wire.OpSkillCastResult, wire.OpSkillEffectControl, simulation.OpVitalsUpdate)
				if !called || *c.CurrentMP != 0 || c.OffensiveSkillCooldowns[skill.Group] != clock.NowMs()+5000 {
					t.Fatalf("cost/cooldown: %+v", r)
				}
				p := r.Frames[0].Payload
				to := rt.liveSpawn(key, c, clock.NowMs())
				distance := float64(skill.PositionEffect.Range)
				if mode == "clipped" {
					distance = 50
				}
				if len(p) != 27 || p[18] != 8 || to.X != before.X+distance || int16(binary.LittleEndian.Uint16(p[21:])) != int16(to.X) || c.World.MoveSegment != nil {
					t.Fatalf("wire/authority mismatch %x %+v", p, to)
				}
				if len(r.Broadcast) != 2 || r.Frames[1].Payload[0] != 1 || len(r.ActorPrivate) != 1 {
					t.Fatal("incorrect publication or premature finalize")
				}
			})
		}
	}
}

/*
================
TestPositionSkillRangeUses3DAndRegionFrames
================
*/
func TestPositionSkillRangeUses3DAndRegionFrames(t *testing.T) {
	from := simulation.Spawn{RegionID: 0x61a8, X: 1900, Y: 10, Z: 100}
	to := simulation.Spawn{RegionID: 0x61a9, X: 280, Y: 410, Z: 100}
	p, ok := positionSkillGoal(from, to, 100)
	if !ok || p.RegionID != 0x61a9 || math.Abs(p.X-40) > 1e-6 || p.Y != 90 {
		t.Fatalf("3D clamp: %+v %v", p, ok)
	}
	if _, ok := positionSkillGoal(simulation.Spawn{RegionID: 0x8001}, simulation.Spawn{RegionID: 0x8002}, 100); ok {
		t.Fatal("cross-dungeon accepted")
	}
}
