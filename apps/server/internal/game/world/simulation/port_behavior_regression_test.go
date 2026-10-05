/*
===========================================================================

port_behavior_regression_test.go - monster behaviour port regressions

===========================================================================
*/

package simulation

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"
	"time"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

/*
==================
TestPortAuditAttackUsesStrategyInterval

These are behavior tests against the actual population and mover owners,
not an alternative AI implementation. Geometry/attack side effects use the
same deterministic seams as the existing package tests.
==================
*/
func TestPortAuditAttackUsesStrategyInterval(t *testing.T) {
	ops, actor, target := earthGhostFixture(t)
	target.Pose = poseToSpawn(anchorPose(actor))
	target.Pose.X += 10
	calls := 0
	ops.BasicAttack = func(string, monster.Instance, uint32, uint32, int64) MonsterAttackResult {
		calls++
		return MonsterAttackResult{Accepted: true, TargetAlive: true}
	}
	ops.advanceInstance(monsterTestDivision, actor, []playerPose{target}, 10000)
	m, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	expected := monster.NextAttackInterval(0, uint32(m.AttackCooldownMs), 16384)
	if calls != 1 || expected <= uint32(m.AttackCooldownMs) {
		t.Fatal("fixture did not select a nonzero jitter")
	}
	if m.NextAttackMs != 10000+int64(expected) {
		t.Fatalf("deadline=%d want=%d (cooldown=%d randomized=%d)", m.NextAttackMs, 10000+int64(expected), m.AttackCooldownMs, expected)
	}
	ops.advanceInstance(monsterTestDivision, actor, []playerPose{target}, 10000+m.AttackCooldownMs)
	if calls != 1 {
		t.Fatal("raw cooldown fired before selected strategy interval")
	}
	ops.advanceInstance(monsterTestDivision, actor, []playerPose{target}, 10000+int64(expected))
	if calls != 2 {
		t.Fatal("selected strategy deadline did not release next attack")
	}
}

func TestPortAuditRefusedAttackIsNotFatalRecovery(t *testing.T) {
	for _, targetAlive := range []bool{false, true} {
		t.Run(fmt.Sprint(targetAlive), func(t *testing.T) {
			ops, actor := monsterLegFixture(t, aggressiveTactics())
			target := playerPose{Gid: PlayerObjectID(1), Pose: Spawn{RegionID: monsterTestRegion, X: 1005, Y: 20, Z: 1000}, BodyRadius: 4}
			ops.BasicAttack = func(string, monster.Instance, uint32, uint32, int64) MonsterAttackResult {
				return MonsterAttackResult{TargetAlive: targetAlive}
			}
			ops.advanceInstance(monsterTestDivision, actor, []playerPose{target}, 10000)
			m, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
			if m.Mode() == monster.MoverRecovering || m.LastEvent() == monster.MoverEventTargetDefeated || m.PreviousEvent() == monster.MoverEventTargetDefeated {
				t.Fatalf("refused callback fabricated fatal recovery: %s event=%s/%s", m.Mode(), m.PreviousEvent(), m.LastEvent())
			}
			if m.TargetGID() != 0 {
				t.Fatal("refused action retained a target")
			}
		})
	}
}

func TestPortAuditHelpUsesRetainedHomeAndAttachment(t *testing.T) {
	for _, tc := range []struct {
		name                                     string
		spawnOffset, actorOffset, targetDistance float64
		detached                                 bool
		want                                     bool
	}{
		{"spawn-cannot-extend-home", 500, 500, 100, false, false},
		{"spawn-cannot-shrink-home", -500, 0, 100, false, true},
		{"detached-ignores-old-home", 0, 600, 100, true, true},
		{"attached-admits-equality", 0, 0, 500, false, true},
		{"detached-rejects-equality", 0, 0, 500, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ops, actor, target := helpFixture(t)
			s := ops.Monsters
			actor.Spawn.X = actor.Nest.X + tc.spawnOffset
			actor.NestDetached = tc.detached
			s.mu.Lock()
			state := s.populationForObject(monsterTestDivision, actor.Gid)
			state.instances.set(actor.Gid, actor)
			m := state.movers.get(actor.Gid)
			m.Pose.X = actor.Nest.X + tc.actorOffset
			state.movers.set(actor.Gid, m)
			s.mu.Unlock()
			target.Pose.X = m.Pose.X + tc.targetDistance
			if err := s.DeliverMonsterHelp(monsterTestDivision, actor.Gid, helpPayload(1, target.Gid, 0, 0, target.Gid)); err != nil {
				t.Fatal(err)
			}
			actor, _ = s.Get(monsterTestDivision, actor.Gid)
			frames, handled := ops.handleHelp(monsterTestDivision, actor, m, []playerPose{target}, 10000)
			after, _ := s.Mover(monsterTestDivision, actor.Gid)
			if !handled || (after.TargetGID() == target.Gid) != tc.want {
				t.Fatalf("help=%v want=%v; frames=%d", after.TargetGID() == target.Gid, tc.want, len(frames))
			}
			row, _ := s.Get(monsterTestDivision, actor.Gid)
			if _, pending := row.Help.Pending(); pending {
				t.Fatal("help not consumed")
			}
		})
	}
}

func TestPortAuditFatalDamageFreezesCorpse(t *testing.T) {
	for _, burn := range []bool{false, true} {
		t.Run(fmt.Sprint(burn), func(t *testing.T) {
			s := damageTestState()
			now := int64(10000)
			s.clock = func() time.Time { return time.UnixMilli(now) }
			actor := firstDamageTestMonster(t, s, "audit")
			m, _ := s.Mover("audit", actor.Gid)
			mustMoverTransition(&m, monster.MoverEventSpawnHoldElapsed, 0)
			mustMoverTransition(&m, monster.MoverEventStartWander, 0)
			m.From = m.Pose
			m.To = m.Pose
			m.To.X += 100
			m.To.Y += 20
			m.DepartMs = 10000
			m.ArriveMs = 12000
			if !s.CommitMover("audit", actor.Gid, m) {
				t.Fatal("seed movement")
			}
			now = 10500
			want := m.LivePoseAt(now, nil)
			if burn {
				if result, ok := burnTick(t, s, "audit", actor.Gid, 1, 5533, now); !ok || !result.Fatal {
					t.Fatal("burn not fatal")
				}
			} else {
				if result, ok := s.ApplyDamage("audit", actor.Gid, actor.CurrentHP); !ok || !result.Fatal {
					t.Fatal("damage not fatal")
				}
			}
			after, _ := s.Mover("audit", actor.Gid)
			for _, at := range []int64{10500, 11000, 20000} {
				if got := after.LivePoseAt(at, nil); got != want {
					t.Fatalf("corpse at %d moved: %+v want %+v", at, got, want)
				}
			}
			if after.ArriveMs > after.DepartMs {
				t.Fatal("corpse retains a movement segment")
			}
			if s.CommitMover("audit", actor.Gid, m) {
				t.Fatal("old live plan revived movement")
			}
		})
	}
}

func TestPortAuditFatalBurnClearsMotionHold(t *testing.T) {
	s := damageTestState()
	now := int64(10000)
	s.clock = func() time.Time { return time.UnixMilli(now) }
	actor := firstDamageTestMonster(t, s, "audit")
	pose := monster.Pose{RegionID: actor.Spawn.RegionID, X: actor.Spawn.X, Y: actor.Spawn.Y, Z: actor.Spawn.Z}
	hits := s.ApplyDamageSequence("audit", actor.Gid, actor.CurrentHP, []MonsterDamagePlan{{GID: actor.Gid, Damage: 1, Knockdown: &MonsterKnockdownPlan{Pose: pose, UntilMs: 20000}}})
	if len(hits) != 1 {
		t.Fatal("seed knockdown")
	}
	hit, ok := burnTick(t, s, "audit", actor.Gid, 1, 5533, now+1)
	if !ok || !hit.Fatal {
		t.Fatal("seed fatal burn")
	}
	if hit.Instance.Motion.StateAt(now+1) != 0 {
		t.Fatal("burn corpse retained living knockdown state")
	}
}

func TestPortAuditDeadMonsterScopeSpawnIsDead(t *testing.T) {
	ops, actor := monsterLegFixture(t, passiveTactics())
	ops.Monsters.ApplyDamage(monsterTestDivision, actor.Gid, actor.CurrentHP)
	actor, _ = ops.Monsters.Get(monsterTestDivision, actor.Gid)
	row := BuildMonsterCreateRow(monsterWireDefFromInstance(actor, 10000), actor.Gid, poseToSpawn(anchorPose(actor)))
	if row[29] != wire.LifeStateDead {
		t.Fatalf("late-viewer spawn LIFE=%d want DEAD=%d", row[29], wire.LifeStateDead)
	}
	session := playerSessionAt(1, 1000, 1000)
	session.PublishedObjects = []uint32{}
	push := &fakePusher{}
	ops.RunMonsterLeg(10000, []SessionSnapshot{session}, push)
	found := false
	for _, frame := range sessionFrames(push, session.SessionID) {
		if frame.Opcode == wire.OpSingleObjectSpawn && binary.LittleEndian.Uint32(frame.Payload[4:8]) == actor.Gid {
			found = true
			if frame.Payload[29] != wire.LifeStateDead {
				t.Fatal("scope path lost dead LIFE")
			}
		}
		if frame.Opcode == OpMovementAck {
			t.Fatal("scope replay moved retained corpse")
		}
	}
	if !found {
		t.Fatal("corpse scope fixture produced no create")
	}
}

func TestPortAuditDungeonMonsterSpawnKeepsSignedLocals(t *testing.T) {
	for _, p := range []Spawn{{RegionID: 0x8001, X: -100.25, Y: 12, Z: -300.5}, {RegionID: 0x8001, X: 70000.5, Y: -15, Z: -70000.25}} {
		row := BuildMonsterCreateRow(MonsterDef{RefObjID: 1933}, 400001, p)
		x := math.Float32frombits(binary.LittleEndian.Uint32(row[10:14]))
		z := math.Float32frombits(binary.LittleEndian.Uint32(row[18:22]))
		if x != float32(p.X) || z != float32(p.Z) {
			t.Fatalf("dungeon locals truncated: %g/%g want %g/%g", x, z, p.X, p.Z)
		}
		single := BuildMonsterSpawnSingle(MonsterDef{RefObjID: 1933}, 400001, p)
		if string(single[:len(row)]) != string(row) {
			t.Fatal("single changed signed coordinate encoding")
		}
	}
}

func TestPortAuditAreaAndChainCannotCrossCoordinatePlane(t *testing.T) {
	s := damageTestState()
	actor := firstDamageTestMonster(t, s, "audit")
	m, _ := s.Mover("audit", actor.Gid)
	// A matching low 15-bit region word currently aliases to distance zero.
	center := poseToSpawn(m.Pose)
	center.RegionID ^= 0x8000
	if got := s.CombatCandidatesInSphere("audit", center, 10, s.CurrentTimeMillis()); len(got) != 0 {
		t.Fatalf("outdoor actor selected from indoor AoE: %+v", got)
	}
	if got := s.CombatCandidatesForChain("audit", center, 10, s.CurrentTimeMillis()); len(got) != 0 {
		t.Fatal("outdoor actor selected from indoor chain")
	}
	center.RegionID ^= 0x8000
	if len(s.CombatCandidatesInSphere("audit", center, 10, s.CurrentTimeMillis())) != 1 {
		t.Fatal("same-plane control lost its actor")
	}
}

func TestPortAuditCorpseSettlesSurfaceOnEveryDamageDoor(t *testing.T) {
	for _, door := range []string{"direct", "batch", "sequence", "burn"} {
		t.Run(door, func(t *testing.T) {
			s := damageTestState()
			now := int64(10000)
			s.clock = func() time.Time { return time.UnixMilli(now) }
			actor := firstDamageTestMonster(t, s, "surface")
			m, _ := s.Mover("surface", actor.Gid)
			mustMoverTransition(&m, monster.MoverEventSpawnHoldElapsed, 0)
			mustMoverTransition(&m, monster.MoverEventStartWander, 0)
			m.From, m.To = m.Pose, m.Pose
			m.To.X += 100
			m.To.Y += 20
			m.To.Heading = 0x4321
			m.DepartMs, m.ArriveMs = 10000, 12000
			m.BeginNavigation(monster.NewNavigationRoute(m.To, []monster.Pose{m.To}))
			m.AdoptNavigation(monster.NewNavigationPath(m.From, m.To, m.To, 0, func(t float64, _ monster.Pose) (float64, bool) {
				return 20 + 20*t + 320*t*(1-t), true
			}))
			if !s.CommitMover("surface", actor.Gid, m) {
				t.Fatal("seed surface mover")
			}
			now = 10500
			want := m.LivePoseAt(now, nil)
			if want.Y != 85 {
				t.Fatal("surface fixture is a chord")
			}
			plan := MonsterDamagePlan{GID: actor.Gid, ExpectedHP: actor.CurrentHP, Damage: actor.CurrentHP}
			switch door {
			case "direct":
				if hit, ok := s.ApplyDamage("surface", actor.Gid, actor.CurrentHP); !ok || !hit.Fatal {
					t.Fatal("direct death")
				}
			case "batch":
				if hits, ok := s.ApplyDamageBatch("surface", []MonsterDamagePlan{plan}); !ok || len(hits) != 1 || !hits[0].Fatal {
					t.Fatal("batch death")
				}
			case "sequence":
				if hits := s.ApplyDamageSequence("surface", actor.Gid, actor.CurrentHP, []MonsterDamagePlan{plan}); len(hits) != 1 || !hits[0].Fatal {
					t.Fatal("sequence death")
				}
			case "burn":
				if hit, ok := burnTick(t, s, "surface", actor.Gid, 1, 5533, now); !ok || !hit.Fatal {
					t.Fatal("burn death")
				}
			}
			after, _ := s.Mover("surface", actor.Gid)
			if _, active := after.NavigationGoal(); active {
				t.Fatal("corpse retained navigation intent")
			}
			for _, at := range []int64{now, now + 300, now + 5000} {
				if got := after.LivePoseAt(at, func(uint16, float64, float64) (float64, bool) { t.Fatal("corpse resampled terrain"); return 0, false }); got != want {
					t.Fatalf("surface pose changed: %+v want %+v", got, want)
				}
			}
			now += 500
			if repeat, ok := s.ApplyDamage("surface", actor.Gid, 10); !ok || repeat.Fatal || repeat.Applied != 0 {
				t.Fatal("postmortem hit acquired another death")
			}
			again, _ := s.Mover("surface", actor.Gid)
			if again != after {
				t.Fatal("postmortem hit moved corpse")
			}
		})
	}
}

func TestPortAuditSurvivingDamageKeepsNavigation(t *testing.T) {
	for _, burn := range []bool{false, true} {
		s := damageTestState()
		now := int64(10000)
		s.clock = func() time.Time { return time.UnixMilli(now) }
		actor := firstDamageTestMonster(t, s, "survive")
		m, _ := s.Mover("survive", actor.Gid)
		mustMoverTransition(&m, monster.MoverEventSpawnHoldElapsed, 0)
		mustMoverTransition(&m, monster.MoverEventStartWander, 0)
		m.From, m.To = m.Pose, m.Pose
		m.To.X += 100
		m.DepartMs, m.ArriveMs = 10000, 12000
		s.CommitMover("survive", actor.Gid, m)
		if burn {
			// Level-1 table damage (8) leaves the actor alive.
			if r, ok := burnTick(t, s, "survive", actor.Gid, 1, 8, now+1); !ok || r.Fatal {
				t.Fatal("nonfatal burn fixture")
			}
		} else if r, ok := s.ApplyDamage("survive", actor.Gid, 1); !ok || r.Fatal {
			t.Fatal("nonfatal hit fixture")
		}
		after, _ := s.Mover("survive", actor.Gid)
		if after != m || !after.InFlight(10500) {
			t.Fatal("nonfatal damage cancelled movement")
		}
	}
}

func TestPortAuditScopeNeverReplaysDeadMovement(t *testing.T) {
	for _, seeded := range []bool{false, true} {
		t.Run(fmt.Sprint(seeded), func(t *testing.T) {
			ops, actor := monsterLegFixture(t, passiveTactics())
			ops.Monsters.mu.Lock()
			state := ops.Monsters.populationForObject(monsterTestDivision, actor.Gid)
			actor.CurrentHP = 0
			actor.Motion = monster.MotionHold{State: 8, UntilMs: 20000}
			state.instances.set(actor.Gid, actor)
			// Even an older/stale producer with a live path cannot replay it
			// for a known corpse. Normal death also freezes that path at source.
			m := state.movers.get(actor.Gid)
			m.From, m.To = m.Pose, m.Pose
			m.To.X += 20
			m.DepartMs, m.ArriveMs = 9000, 11000
			state.movers.set(actor.Gid, m)
			ops.Monsters.mu.Unlock()
			session := playerSessionAt(1, 1000, 1000)
			session.PublishedObjects = []uint32{}
			if seeded {
				session.PublishedObjects = []uint32{actor.Gid}
			}
			push := &fakePusher{}
			ops.RunMonsterLeg(10000, []SessionSnapshot{session}, push)
			for _, f := range sessionFrames(push, session.SessionID) {
				if f.Opcode == OpMovementAck {
					t.Fatal("retained corpse received movement replay")
				}
				if f.Opcode == wire.OpSingleObjectSpawn && (f.Payload[29] != wire.LifeStateDead || f.Payload[30] != 0) {
					t.Fatal("corpse scope reintroduced living state")
				}
			}
		})
	}
}

func TestPortAuditHelpTraceBypassesAndSummonedBoundary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		boundary uint8
		limit    int32
		summoned bool
		distance float64
		want     bool
	}{
		{"disabled", 0, 500, false, 700, true},
		{"mode-two", 2, 500, false, 700, true},
		{"zero-limit", 1, 0, false, 700, true},
		{"summoned-inside", 1, 500, true, 100, true},
		{"summoned-equality", 1, 500, true, 500, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ops, actor, target := helpFixture(t)
			actor.Nest.Controls.TraceBoundary = tc.boundary
			actor.Nest.Controls.TraceData = tc.limit
			if tc.summoned {
				actor.SummonerGID = 999
			}
			s := ops.Monsters
			s.mu.Lock()
			state := s.populationForObject(monsterTestDivision, actor.Gid)
			state.instances.set(actor.Gid, actor)
			m := state.movers.get(actor.Gid)
			m.Pose.X += 600
			state.movers.set(actor.Gid, m)
			s.mu.Unlock()
			target.Pose.X = m.Pose.X + tc.distance
			if err := s.DeliverMonsterHelp(monsterTestDivision, actor.Gid, helpPayload(1, target.Gid, 0, 0, target.Gid)); err != nil {
				t.Fatal(err)
			}
			actor, _ = s.Get(monsterTestDivision, actor.Gid)
			ops.handleHelp(monsterTestDivision, actor, m, []playerPose{target}, 10000)
			after, _ := s.Mover(monsterTestDivision, actor.Gid)
			if (after.TargetGID() == target.Gid) != tc.want {
				t.Fatalf("accepted=%v want=%v", after.TargetGID() == target.Gid, tc.want)
			}
		})
	}
}
