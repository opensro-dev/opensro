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
	"strings"
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
	if len(ids) != 20 {
		t.Fatalf("ground-target coverage=%d, want 12 Phantom + 4 Shadow + 4 Wizard Teleport", len(ids))
	}
	for _, id := range ids {
		skill, ok := source.SkillByID(id)
		if !ok || !skill.PositionEffect.Pinned || source.ExecutionPlan(id).Kind() != enterworld.SkillExecutionPosition {
			t.Fatalf("rank %d: %+v", id, skill)
		}
		// Ghost Walk shares group 59; the Wizard's Teleport (tel2) owns 43.
		wantGroup := uint8(59)
		if strings.HasPrefix(skill.Codename, "SKILL_EU_WIZARD_") {
			wantGroup = 43
		}
		if skill.CoolTimeGroup != wantGroup || skill.CoolTimeMs != 5000 {
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
				// tel2 owns an open bracket until its mode-2 close, so it arms
				// the action latch every open skill cast arms (0xB2CD).
				wizard := strings.HasPrefix(skill.Codename, "SKILL_EU_WIZARD_")
				wantOps := []uint16{wire.OpSkillCastResult, wire.OpSkillEffectControl, simulation.OpVitalsUpdate}
				if wizard {
					wantOps = append(wantOps, wire.OpActionState)
				}
				if len(r.Frames) != len(wantOps) {
					t.Fatalf("acceptance failed: %+v payload=%v called=%v", r, r.Frames, called)
				}
				assertOpcodes(t, r.Frames, wantOps...)
				if !called || *c.CurrentMP != 0 || c.OffensiveSkillCooldowns[skill.Group] != clock.NowMs()+5000 {
					t.Fatalf("cost/cooldown: %+v", r)
				}
				p := r.Frames[0].Payload
				to := rt.liveSpawn(key, c, clock.NowMs())
				distance := float64(skill.PositionEffect.Range)
				if mode == "clipped" {
					distance = 50
				}
				// tel2 (the Wizard's Teleport) publishes bit 2, an
				// instant landing; tele (Ghost Walk) bit 8, a guided travel.
				wantFlag := byte(8)
				if wizard {
					wantFlag = 2
				}
				if len(p) != 27 || p[18] != wantFlag || to.X != before.X+distance || int16(binary.LittleEndian.Uint16(p[21:])) != int16(to.X) || c.World.MoveSegment != nil {
					t.Fatalf("wire/authority mismatch %x %+v", p, to)
				}
				if len(r.Broadcast) != 2 || r.Frames[1].Payload[0] != 1 || len(r.ActorPrivate) != len(wantOps)-2 {
					t.Fatal("incorrect publication or premature finalize")
				}
				// tel2 lands at once and has no guided arrival, so its bracket
				// needs the ordinary mode-2 close; tele's arrival closes it.
				token := binary.LittleEndian.Uint32(p[10:])
				var closes int
				for _, out := range rt.drainSkillFinalizes(clock.NowMs() + 600000) {
					for _, f := range out.Frames {
						if f.Opcode == wire.OpSkillEffectControl && f.Payload[0] == 2 && binary.LittleEndian.Uint32(f.Payload[2:]) == token {
							closes++
						}
					}
				}
				if want := map[byte]int{2: 1, 8: 0}[wantFlag]; closes != want {
					t.Fatalf("mode-2 closes for flag %d = %d, want %d", wantFlag, closes, want)
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

/*
================
TestTeleportSeatsTheNearestLayer

A deck stands over the ground. The walk rests with a terrain owner (the
hint a walk under the deck would carry), and the clicked point is on the
deck. tel2 seats the point itself (nearest height), so it lands on the
deck; Ghost Walk keeps the walk's surface as before (#3927).
================
*/
func TestTeleportSeatsTheNearestLayer(t *testing.T) {
	const deckY, groundY = -30.0, -111.0
	deck := simulation.NavOwner{Kind: simulation.NavOwnerObject, Object: simulation.NavObjectCell{Surface: 26247, Cell: 32}}
	terrain := simulation.NavOwner{Kind: simulation.NavOwnerTerrain}
	for _, teleport := range []bool{true, false} {
		rt, _, _, _ := newCombatTestRuntime(t, 100)
		rt.ConstrainMovement = nil
		rt.ConstrainWalk = func(_ string, _ simulation.Spawn, _ simulation.NavOwner, to simulation.Spawn) (simulation.Spawn, simulation.NavWalk, *simulation.MoveError) {
			return to, simulation.NavWalk{Rest: terrain}, nil
		}
		var hints []simulation.NavOwner
		rt.ResolveNavOwner = func(p simulation.Spawn, hint simulation.NavOwner) (simulation.NavOwner, float64, bool) {
			hints = append(hints, hint)
			// The outdoor resolver keeps a terrain hint under a deck and
			// otherwise takes the nearer layer.
			if hint.Kind == simulation.NavOwnerTerrain || math.Abs(p.Y-groundY) < math.Abs(p.Y-deckY) {
				return terrain, groundY, true
			}
			return deck, deckY, true
		}
		from := simulation.Spawn{RegionID: 26247, X: 400, Y: -35, Z: 300}
		to := simulation.Spawn{RegionID: 26247, X: 341, Y: deckY, Z: 398}
		plan, ok := skillTravelPlan{}, false
		if teleport {
			plan, ok = rt.planSkillTeleport("c", from, deck, to)
		} else {
			plan, ok = rt.planSkillTravel("c", from, deck, to)
		}
		if !ok || len(hints) != 1 {
			t.Fatalf("teleport=%v: plan refused or resolved %d times", teleport, len(hints))
		}
		wantY, wantOwner := groundY, terrain
		if teleport {
			wantY, wantOwner = deckY, deck
			if hints[0] != (simulation.NavOwner{}) {
				t.Fatalf("teleport seated with hint %+v", hints[0])
			}
		}
		if plan.to.Y != wantY || plan.owner != wantOwner {
			t.Fatalf("teleport=%v landed y=%v owner=%+v, want y=%v owner=%+v", teleport, plan.to.Y, plan.owner, wantY, wantOwner)
		}
	}
}
