/*
===========================================================================

sookarena_test.go - arena integration through real relocation and HP owners

Synthetic encounters keep these tests independent of retail asset availability.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/sookarena"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
sookFixture
================
*/
func sookFixture(t *testing.T, characters ...*enterworld.Character) (*Runtime, *fakeClock) {
	t.Helper()
	rt, clock := newInstanceWorldRuntime(t, characters...)
	ref := monster.MonsterRef{RefObjID: 100, Codename: "MOB_CH_TEST", Level: 1, MaxHP: 100000, ExpToGive: 100, CombatPinned: true, RewardActionPinned: true, RunSpeed: 60, ModelPath: "char/test.bsr", DefaultSkillIDs: [10]uint32{1}}
	boss := ref
	boss.RefObjID = 101
	boss.MonsterType = 3
	boss.Level = 20
	boss.Codename = "MOB_CH_TEST_UNIQUE"
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{100: ref, 101: boss}, nil))
	rt.sookArena = &sookArenaRuntime{pool: sookarena.BuildPool([]monster.MonsterRef{ref}), resolve: func(_ *enterworld.Character, at simulation.Spawn) (simulation.Spawn, bool) { return at, true }}
	rt.NpcSpawn = enterworld.NpcSpawnConfig{Enabled: true}
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 333, RefObjID: 3861, Codename: sookarena.NPC, AuthoredSpawn: true, Spawn: instanceWorldTestStart}}
	for i, c := range characters {
		admitInstanceWorldCharacter(t, rt, c, uint64(i+1), instanceWorldTestStart)
		rt.Selected.Set(testDivision, c.Name, 333)
	}
	return rt, clock
}

/*
================
TestSookAdmissionIsolationAndVictory
================
*/
func TestSookAdmissionIsolationAndVictory(t *testing.T) {
	a, b := instanceWorldTestCharacter(11, "Survivor"), instanceWorldTestCharacter(12, "Other")
	rt, clock := sookFixture(t, a, b)
	rt.enterSookArena(testDivision, a)
	clock.now = clock.now.Add(time.Minute)
	rt.enterSookArena(testDivision, b)
	ra, rb := rt.sookRun(testDivision, a), rt.sookRun(testDivision, b)
	if ra == nil || rb == nil || ra.lease.ID == rb.lease.ID || a.SookArenaEntries != 1 || !a.SookArenaActive {
		t.Fatal("independent admitted runs missing")
	}
	clock.now = clock.now.Add(time.Duration(sookarena.OpeningDelayMs-sookarena.PandoraWindupMs) * time.Millisecond)
	rt.advanceSookArena(clock.Now().UnixMilli())
	clock.now = clock.now.Add(1200 * time.Millisecond)
	rt.advanceSookArena(clock.Now().UnixMilli())
	if len(rt.Monsters.PopulationInstances(testDivision, ra.lease, []uint16{ra.center.RegionID})) != 2 {
		t.Fatal("initial wave missing")
	}
	if len(rt.Monsters.PopulationInstances(testDivision, rb.lease, []uint16{rb.center.RegionID})) != 2 {
		t.Fatal("second player's independent wave missing")
	}
	aggressive := 0
	for _, actor := range rt.Monsters.PopulationInstances(testDivision, ra.lease, []uint16{ra.center.RegionID}) {
		mover, ok := rt.Monsters.Mover(testDivision, actor.Gid)
		if !ok {
			t.Fatal("spawned arena mover missing")
		}
		if mover.TargetGID() == enterworld.ObjectIDForCharacter(a) {
			aggressive++
		} else if monster.ResolveTactics(&actor).Aggressive {
			t.Fatal("unengaged actor can acquire a sight target")
		}
	}
	if aggressive != 1 {
		t.Fatalf("opening wave: %d aggressive, want one of three", aggressive)
	}
	before := goldOf(a)
	ra.program.Kills = 31
	ra.program.PausedAt = clock.Now().UnixMilli()
	ra.program.Completed = 5
	ra.program.DecisionUntil = clock.Now().UnixMilli() + sookarena.DecisionMs
	clock.now = time.UnixMilli(ra.program.DecisionUntil)
	rt.advanceSookArena(clock.Now().UnixMilli())
	if domain.CharacterWorldInstance(a) != domain.DefaultWorldInstance || a.SookArenaActive || goldOf(a) != before+60000 {
		t.Fatal("victory did not commit and return")
	}
	if _, live := rt.Monsters.PopulationLease(testDivision, ra.lease.ID); live {
		t.Fatal("victory leaked its layer")
	}
	if !b.SookArenaActive || rt.sookRun(testDivision, b) != rb || domain.CharacterWorldInstance(b) != uint32(rb.lease.ID) {
		t.Fatal("first player's finish ended the second player's run")
	}
	if _, live := rt.Monsters.PopulationLease(testDivision, rb.lease.ID); !live {
		t.Fatal("first player's finish released the second player's arena")
	}
	rt.advanceSookArena(clock.Now().UnixMilli())
	if goldOf(a) != before+60000 {
		t.Fatal("duplicate payout")
	}
}

/*
================
TestSookColosseumGateDoesNotBlockAdmission
================
*/
func TestSookColosseumGateDoesNotBlockAdmission(t *testing.T) {
	for _, gate := range []bool{true, false} {
		c := instanceWorldTestCharacter(11, "Courtyard")
		rt, _ := sookFixture(t, c)
		npc := simulation.NpcDef{ObjectID: 444, Codename: "STORE_JA_CHARGE_GATE", Spawn: simulation.Spawn{RegionID: 25580, X: 619, Y: 0, Z: 915}}
		if gate {
			npc.Teleport = &simulation.TeleportGateBounds{}
		}
		rt.NpcRoster = append(rt.NpcRoster, npc)
		rt.enterSookArena(testDivision, c)
		if (rt.sookRun(testDivision, c) != nil) != gate {
			t.Fatalf("gate=%v: wrong admission result", gate)
		}
		if !gate && (c.SookArenaEntries != 0 || c.SookArenaActive) {
			t.Fatal("rejected occupied arena spent an entry")
		}
	}
}

/*
================
TestSookFreeMovementAndNormalWaves
================
*/
func TestSookFreeMovementAndNormalWaves(t *testing.T) {
	c := instanceWorldTestCharacter(11, "FreeMovement")
	rt, clock := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	run := rt.sookRun(testDivision, c)
	if run == nil || run.center.RegionID != 25580 {
		t.Fatal("Colosseum admission")
	}
	at := run.center
	at.X += 700
	if !rt.relocateCharacter(testDivision, c, "sook-test-roam", func() (travelPoint, bool) {
		return travelPoint{spawn: at, world: run.lease.ID}, true
	}) {
		t.Fatal("test movement")
	}
	for _, elapsed := range []int64{sookarena.OpeningDelayMs - sookarena.PandoraWindupMs, sookarena.OpeningDelayMs, 300000 + 10000, 300000 + 11200} {
		clock.now = time.UnixMilli(run.program.Started + elapsed)
		rt.advanceSookArena(clock.Now().UnixMilli())
		pose := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, clock.Now().UnixMilli())
		if simulation.WorldDistance2D(pose, at) > 1 {
			t.Fatal("ordinary movement was teleported to centre")
		}
	}
	actors := rt.Monsters.PopulationInstances(testDivision, run.lease, []uint16{run.center.RegionID})
	if len(actors) == 0 {
		t.Fatal("normal waves missing")
	}
	for _, actor := range actors {
		if actor.Rarity() != 0 && actor.Rarity() != 1 && actor.Rarity() != 6 {
			t.Fatalf("wave spawned rarity %d", actor.Rarity())
		}
	}
}

/*
================
TestSookFailedEntryAndDailyLimit
================
*/
func TestSookFailedEntryAndDailyLimit(t *testing.T) {
	c := instanceWorldTestCharacter(11, "Limited")
	rt, clock := sookFixture(t, c)
	push := rt.PushCharacterFrames
	rt.PushCharacterFrames = nil
	rt.enterSookArena(testDivision, c)
	if c.SookArenaEntries != 0 || c.SookArenaActive || c.World.InstanceReturn != nil {
		t.Fatal("failed relocation spent admission")
	}
	rt.PushCharacterFrames = push
	for range 3 {
		rt.Selected.Set(testDivision, c.Name, 333)
		rt.enterSookArena(testDivision, c)
		run := rt.sookRun(testDivision, c)
		if run == nil {
			t.Fatal("daily entry refused")
		}
		rt.endSookArena(run, c, false)
	}
	rt.enterSookArena(testDivision, c)
	if rt.sookRun(testDivision, c) != nil || c.SookArenaEntries != 3 {
		t.Fatal("fourth entry")
	}
	clock.now = clock.now.Add(24 * time.Hour)
	rt.Selected.Set(testDivision, c.Name, 333)
	rt.enterSookArena(testDivision, c)
	if rt.sookRun(testDivision, c) == nil || c.SookArenaEntries != 1 {
		t.Fatal("daily reset")
	}
}

/*
================
TestSookDeathDisconnectAndRestart
================
*/
func TestSookDeathDisconnectAndRestart(t *testing.T) {
	for _, cause := range []string{"death", "disconnect", "restart", "leave", "warp"} {
		t.Run(cause, func(t *testing.T) {
			c := instanceWorldTestCharacter(11, "SafeReturn")
			rt, clock := sookFixture(t, c)
			rt.enterSookArena(testDivision, c)
			run := rt.sookRun(testDivision, c)
			if run == nil {
				t.Fatal("entry")
			}
			before := goldOf(c)
			run.program.Kills = 11
			wantGold := before
			if cause == "death" {
				wantGold += 30000
			}
			switch cause {
			case "death":
				zero := int64(0)
				c.CurrentHP = &zero
				rt.advanceSookArena(clock.Now().UnixMilli())
				if rt.sookRun(testDivision, c) == nil {
					t.Fatal("death animation skipped")
				}
				for _, choice := range []byte{wire.RebirthAtSpecifiedPoint, wire.RebirthAtPresentPoint} {
					out := rt.HandleLocalRebirth(testDivision, c, []byte{choice})
					if out.DiagnosticRefusal != "sook-arena-auto-return" || len(out.Frames) != 0 || enterworld.CharacterAlive(c) || domain.CharacterWorldInstance(c) != uint32(run.lease.ID) {
						t.Fatal("native rebirth bypassed arena return", out)
					}
					commit, handled := rt.prepareLocalRebirth(testDivision, c, []byte{choice})
					if !handled || commit().DiagnosticRefusal != "sook-arena-auto-return" {
						t.Fatal("prepared rebirth bypassed arena return")
					}
				}
				clock.now = clock.now.Add(time.Duration(sookarena.DeathReturnDelayMs) * time.Millisecond)
				rt.advanceSookArena(clock.Now().UnixMilli())
			case "disconnect":
				rt.ForgetCharacter(testDivision, c.Name)
			case "restart":
				rt.sookArena = nil
				rt.recoverSookArena(testDivision, c)
			case "leave":
				rt.ReturnCharacterFromInstance(testDivision, c)
				rt.advanceSookArena(clock.Now().UnixMilli())
			case "warp":
				rt.relocateCharacter(testDivision, c, "sook-test-leave", func() (travelPoint, bool) {
					return travelPoint{spawn: instanceWorldTestEntry, world: instance.ID(domain.DefaultWorldInstance)}, true
				})
				rt.advanceSookArena(clock.Now().UnixMilli())
			}
			if c.SookArenaActive || c.World.InstanceReturn != nil || domain.CharacterWorldInstance(c) != domain.DefaultWorldInstance || !enterworld.CharacterAlive(c) || goldOf(c) != wantGold || c.SookArenaEntries != 1 {
				t.Fatalf("bad %s recovery: %+v", cause, c.World)
			}
			if rt.sookArena != nil {
				rt.endSookArena(run, c, false)
				if goldOf(c) != wantGold {
					t.Fatal("duplicate exit paid twice")
				}
			}
		})
	}
}

/*
================
TestSookRecoveryDiscardsArenaPoseCache
================
*/
func TestSookRecoveryDiscardsArenaPoseCache(t *testing.T) {
	c := instanceWorldTestCharacter(11, "RecoveredPose")
	rt, clock := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	if rt.sookRun(testDivision, c) == nil {
		t.Fatal("entry")
	}
	rt.recoverSookArena(testDivision, c)
	stored := simulation.SeedWorldState(c).PersistedSpawn()
	live := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, clock.Now().UnixMilli())
	if stored.RegionID != instanceWorldTestStart.RegionID || live != stored {
		t.Fatalf("recovery kept stale arena pose: stored=%+v live=%+v", stored, live)
	}
}

/*
================
TestSookAggroRatioAndPassiveRetaliation
================
*/
func TestSookAggroRatioAndPassiveRetaliation(t *testing.T) {
	c := instanceWorldTestCharacter(11, "AggroRatio")
	rt, clock := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	run := rt.sookRun(testDivision, c)
	for range 10 {
		if !rt.spawnSookMonster(run, run.refs[0], 0, clock.Now().UnixMilli()) {
			t.Fatal("spawn failed")
		}
		run.sequence++
	}
	aggressive := 0
	for _, actor := range rt.Monsters.PopulationInstances(testDivision, run.lease, []uint16{run.center.RegionID}) {
		mover, _ := rt.Monsters.Mover(testDivision, actor.Gid)
		if mover.TargetGID() == enterworld.ObjectIDForCharacter(c) {
			aggressive++
			continue
		}
		rt.commitAggression(testDivision, actor.Gid, simulation.HostilityEvent{Attacker: enterworld.ObjectIDForCharacter(c), Aggression: 1}, clock.Now().UnixMilli())
		mover, _ = rt.Monsters.Mover(testDivision, actor.Gid)
		if mover.TargetGID() != enterworld.ObjectIDForCharacter(c) {
			t.Fatal("passive monster cannot retaliate")
		}
	}
	if aggressive != 3 {
		t.Fatalf("initially aggressive=%d, want three of ten", aggressive)
	}
}

/*
================
TestSookOldCleanupCannotRetireANewRun
================
*/
func TestSookOldCleanupCannotRetireANewRun(t *testing.T) {
	c := instanceWorldTestCharacter(11, "Reenter")
	rt, _ := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	old := rt.sookRun(testDivision, c)
	rt.ReturnCharacterFromInstance(testDivision, c)
	rt.Selected.Set(testDivision, c.Name, 333)
	rt.enterSookArena(testDivision, c)
	next := rt.sookRun(testDivision, c)
	if next == nil || next == old {
		t.Fatal("new run")
	}
	rt.endSookArena(old, c, false)
	if rt.sookRun(testDivision, c) != next || !c.SookArenaActive || rt.sookPopulation(testDivision, next.lease) != next {
		t.Fatal("old cleanup damaged new run")
	}
}

/*
================
TestSookPowerupCannotEscapeItsLease
================
*/
func TestSookPowerupCannotEscapeItsLease(t *testing.T) {
	a, b := instanceWorldTestCharacter(11, "Picker"), instanceWorldTestCharacter(12, "Foreign")
	rt, clock := sookFixture(t, a, b)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	rt.enterSookArena(testDivision, a)
	rt.enterSookArena(testDivision, b)
	run := rt.sookRun(testDivision, a)
	item := rt.addCharacterGround(testDivision, a, grounditem.Item{ArenaPowerup: 1, RefObjID: 9, Codename: sookDamageScroll, DroppedAt: clock.Now()})
	if out := rt.pickupSookPowerup(testDivision, simulation.WorldKey(testDivision, b.Name), b, item); len(out.Broadcast) != 0 {
		t.Fatal("foreign pickup")
	}
	if out := rt.pickupSookPowerup(testDivision, simulation.WorldKey(testDivision, a.Name), a, item); len(out.Broadcast) == 0 {
		t.Fatal("powerup not consumed")
	}
	if _, exists := rt.Ground.Get(testDivision, item.Gid); exists {
		t.Fatal("powerup retained")
	}
	formula := combat.Result{Damage: 100, PhysicalDamage: 100}
	if got := rt.sookDamageBoost(testDivision, a.Name, formula, clock.Now().UnixMilli()); got.Damage != 200 {
		t.Fatal("damage boost")
	}
	if got := rt.sookDamageBoost(testDivision, b.Name, formula, clock.Now().UnixMilli()); got.Damage != 100 {
		t.Fatal("foreign damage boost")
	}
	if got := rt.sookDamageBoost(testDivision, a.Name, formula, clock.Now().UnixMilli()+30000); got.Damage != 100 {
		t.Fatal("expired damage boost")
	}
	old := run.lease
	rt.endSookArena(run, a, false)
	lease, status := rt.OpenInstanceWorld(InstanceOpenRequest{Division: testDivision, Definition: 30})
	if status != instance.Success || lease.ID != old.ID || lease.Generation == old.Generation || rt.sookPopulation(testDivision, old) != nil {
		t.Fatal("lease reuse")
	}
}

/*
================
TestSookEntryCountdownAndBombNotice
================
*/
func TestSookEntryCountdownAndBombNotice(t *testing.T) {
	c := instanceWorldTestCharacter(11, "Notices")
	rt, clock := sookFixture(t, c)
	frames := rt.enterSookArena(testDivision, c)
	want := sookBanner("Survival: five stages, EXP/SP x5, chest rewards, no floor loot. Entry 1/3. First wave in 20s.")
	if len(frames) != 2 || frames[0].Opcode != want.Opcode || string(frames[0].Payload) != string(want.Payload) || frames[1].Opcode != opArenaChest {
		t.Fatal("entry countdown notification missing")
	}
	item := rt.addCharacterGround(testDivision, c, grounditem.Item{ArenaPowerup: sookBomb, RefObjID: 9272, Codename: sookEgg, DroppedAt: clock.Now()})
	out := rt.pickupSookPowerup(testDivision, simulation.WorldKey(testDivision, c.Name), c, item)
	want = sookBanner("KABUMMMMMM")
	if len(out.Frames) == 0 || out.Frames[len(out.Frames)-1].Opcode != want.Opcode || string(out.Frames[len(out.Frames)-1].Payload) != string(want.Payload) {
		t.Fatal("blast pickup notification missing")
	}
	if out.Frames[len(out.Frames)-2].Opcode != crowVisualOpcode || out.Frames[len(out.Frames)-2].Payload[4] != 9 {
		t.Fatal("blast position animation missing")
	}
}

/*
================
TestSookDamageSurgeRollsEveryMultiplier
================
*/
func TestSookDamageSurgeRollsEveryMultiplier(t *testing.T) {
	c := instanceWorldTestCharacter(11, "Surge")
	rt, clock := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	for roll := uint32(0); roll < 9; roll++ {
		rt.CombatRoll = func() (uint32, error) { return roll, nil }
		item := rt.addCharacterGround(testDivision, c, grounditem.Item{ArenaPowerup: sookDamage, RefObjID: 9272, Codename: sookEgg, DroppedAt: clock.Now()})
		rt.pickupSookPowerup(testDivision, simulation.WorldKey(testDivision, c.Name), c, item)
		got := rt.sookDamageBoost(testDivision, c.Name, combat.Result{Damage: 100, PhysicalDamage: 60, MagicalDamage: 40}, clock.Now().UnixMilli())
		if got.Damage != 100*(roll+2) || got.PhysicalDamage != 60*(roll+2) || got.MagicalDamage != 40*(roll+2) {
			t.Fatalf("roll %d: %+v", roll, got)
		}
	}
}

/*
================
TestSookFirePitRadiusTicksExpiryAndIsolation
================
*/
func TestSookFirePitRadiusTicksExpiryAndIsolation(t *testing.T) {
	a, b := instanceWorldTestCharacter(11, "Burner"), instanceWorldTestCharacter(12, "Neighbour")
	rt, clock := sookFixture(t, a, b)
	rt.enterSookArena(testDivision, a)
	rt.enterSookArena(testDivision, b)
	ra, rb := rt.sookRun(testDivision, a), rt.sookRun(testDivision, b)
	spawn := func(run *sookArenaRun, ref uint32, distance float64) uint32 {
		at := run.center
		at.X += distance
		ids := rt.Monsters.SpawnGMMonstersTracked(simulation.GMMonsterSpawn{Division: testDivision, Population: run.lease, RefObjID: ref, Count: 1, Position: at, NowMs: clock.Now().UnixMilli(), Arena: true})
		if len(ids) != 1 {
			t.Fatal("spawn")
		}
		return ids[0]
	}
	normal, edge, outside, unique, foreign := spawn(ra, 100, 0), spawn(ra, 100, 290), spawn(ra, 100, 400), spawn(ra, 101, 0), spawn(rb, 100, 0)
	u, _ := rt.Monsters.Get(testDivision, unique)
	item := rt.addCharacterGround(testDivision, a, grounditem.Item{ArenaPowerup: sookFirePit, RefObjID: 9272, Codename: sookEgg, DroppedAt: clock.Now(), Position: grounditem.Point{RegionID: ra.center.RegionID, X: float32(ra.center.X), Z: float32(ra.center.Z)}})
	out := rt.pickupSookPowerup(testDivision, simulation.WorldKey(testDivision, a.Name), a, item)
	if len(out.Frames) == 0 || ra.firePitUntil != clock.Now().UnixMilli()+60000 {
		t.Fatal("pit pickup")
	}
	start := clock.Now().UnixMilli()
	for _, elapsed := range []int64{999, 1000, 1500, 2000, 60000, 61000} {
		rt.tickSookFirePit(ra, a, start+elapsed)
		want := uint32(100000)
		if elapsed >= 1000 {
			want -= 3000
		}
		if elapsed >= 2000 {
			want -= 3000
		}
		if elapsed >= 60000 {
			want -= 3000
		}
		for _, gid := range []uint32{normal, edge} {
			actor, _ := rt.Monsters.Get(testDivision, gid)
			if actor.CurrentHP != want {
				t.Fatalf("tick %d: hp %d want %d", elapsed, actor.CurrentHP, want)
			}
		}
	}
	for _, gid := range []uint32{outside, foreign} {
		actor, _ := rt.Monsters.Get(testDivision, gid)
		if actor.CurrentHP != 100000 {
			t.Fatal("fire escaped radius or instance")
		}
	}
	after, _ := rt.Monsters.Get(testDivision, unique)
	if after.CurrentHP != u.CurrentHP {
		t.Fatal("fire damaged unique")
	}
}

/*
================
TestSookTimedPowersProtectOnlyTheirOwnerAndExpire
================
*/
func TestSookTimedPowersProtectOnlyTheirOwnerAndExpire(t *testing.T) {
	for _, kind := range []uint8{sookUnstoppable, sookInvincible, sookMana} {
		a, b := instanceWorldTestCharacter(11, "PowerOwner"), instanceWorldTestCharacter(12, "OtherOwner")
		rt, clock := sookFixture(t, a, b)
		rt.enterSookArena(testDivision, a)
		rt.enterSookArena(testDivision, b)
		now := clock.Now().UnixMilli()
		if kind == sookUnstoppable {
			owner := rt.newPlayerAbnormalOwner(testDivision, a, now)
			owner.applyHit(abnormal.HitContext{}, []abnormal.Record{{Status: abnormal.Stun, Grade: 1, DurationMs: 5000}})
			rt.playerDisplacements.Store(simulation.WorldKey(testDivision, a.Name), playerDisplacement{down: true, untilMs: now + 5000})
		}
		item := rt.addCharacterGround(testDivision, a, grounditem.Item{ArenaPowerup: kind, RefObjID: 9272, Codename: sookEgg, DroppedAt: clock.Now()})
		out := rt.pickupSookPowerup(testDivision, simulation.WorldKey(testDivision, a.Name), a, item)
		if len(out.Broadcast) == 0 || !rt.sookPowerActive(testDivision, a, kind, now) || rt.sookPowerActive(testDivision, b, kind, now) {
			t.Fatalf("power kind=%d not applied privately", kind)
		}
		switch kind {
		case sookUnstoppable:
			if rt.PlayerMotionLocked(testDivision, a.Name) || rt.PlayerMovementBlocked(testDivision, a.Name) {
				t.Fatal("Unstoppable did not clear existing control")
			}
			owner := rt.newPlayerAbnormalOwner(testDivision, a, now)
			owner.applyHit(abnormal.HitContext{}, []abnormal.Record{{Status: abnormal.Stun, Grade: 1, DurationMs: 5000}})
			if block := rt.playerAbnormal(testDivision, a.Name); block != nil && block.Has(abnormal.Stun) {
				t.Fatal("Unstoppable accepted stun")
			}
		case sookInvincible:
			hp := int64(10)
			a.CurrentHP = &hp
			hit := rt.strikePlayerInDoor(playerStrike{division: testDivision, victim: a, owner: rt.newPlayerAbnormalOwner(testDivision, a, now), now: now, formulas: []combat.Result{{Damage: 10000, Slain: true, ResultFlags: 1}}})
			if hit.fatal || *a.CurrentHP != 1 {
				t.Fatal("Unkillable failed on direct/slaying damage")
			}
			owner := rt.newPlayerAbnormalOwner(testDivision, a, now)
			owner.Hit(999, false, 10000, 0, abnormal.Poison)
			if owner.fatal || *a.CurrentHP != 1 {
				t.Fatal("Unkillable failed on periodic damage")
			}
		case sookMana:
			mp := int64(1)
			a.CurrentMP = &mp
			skill := enterworld.SkillRow{Group: 555, Consumption: enterworld.SkillConsumption{Pinned: true, MP: 10000}}
			if cost, refusal := rt.offensiveResourceCost(testDivision, a, skill); cost != 0 || refusal != 0 {
				t.Fatalf("free mana admission %d/%x", cost, refusal)
			}
			if cost, err := rt.preparedExecutionMPCost(testDivision, a, skill); cost != 0 || err != nil {
				t.Fatal("free mana preparation")
			}
			rt.commitOffensiveResources(testDivision, a, skillCharge{mp: 10000})
			if *a.CurrentMP != 1 {
				t.Fatal("free mana charged a prepared spell")
			}
		}
		if rt.sookPowerActive(testDivision, a, kind, now+60000) {
			t.Fatal("power did not expire after one minute")
		}
		rt.endSookArena(rt.sookRun(testDivision, a), a, false)
		if rt.sookPowerActive(testDivision, a, kind, now) {
			t.Fatal("power survived leaving")
		}
	}
}

/*
================
TestSookBuffBoardTimersExpireAndClear
================
*/
func TestSookBuffBoardTimersExpireAndClear(t *testing.T) {
	run := &sookArenaRun{ownerID: 11, program: sookarena.New(1000)}
	run.program.DamageUntil = 31000
	run.program.UnstoppableUntil, run.program.InvincibleUntil, run.program.ManaUntil = 61000, 61000, 61000
	if frames := sookPowerEnds(run, 30999, false); len(frames) != 0 {
		t.Fatal("HUD timer ended early")
	}
	frames := sookPowerEnds(run, 31000, false)
	if len(frames) != 1 || frames[0].Opcode != wire.OpParamJobEnd || binary.LittleEndian.Uint32(frames[0].Payload[4:]) != 0x7f530001 {
		t.Fatal("Damage Surge end row")
	}
	frames = sookPowerEnds(run, 40000, true)
	if len(frames) != 3 {
		t.Fatal("exit did not clear all power icons")
	}
	if len(sookPowerEnds(run, 70000, true)) != 0 {
		t.Fatal("power icons ended twice")
	}
	for _, kind := range []uint8{sookDamage, sookUnstoppable, sookInvincible, sookMana} {
		if sookPowerDuration(kind) == 0 {
			t.Fatal("missing HUD duration")
		}
	}
	if sookPowerDuration(sookBomb) != 0 {
		t.Fatal("instant bomb received a timed icon")
	}
}

/*
================
TestSookFlagOffKeepsNativePaths
================
*/
func TestSookFlagOffKeepsNativePaths(t *testing.T) {
	c := instanceWorldTestCharacter(11, "Native")
	rt, _ := newInstanceWorldRuntime(t, c)
	rt.ConfigureSookArena("off")
	if rt.sookArena != nil || len(rt.sookTalkOptions(sookarena.NPC)) != 0 || rt.sookRun(testDivision, c) != nil {
		t.Fatal("flag-off changed native routing")
	}
	formula := combat.Result{Damage: 123}
	if rt.sookDamageBoost(testDivision, c.Name, formula, 0) != formula {
		t.Fatal("native damage changed")
	}
}

/*
================
TestSookBombSettlesKillsAndLeavesUniquesAndForeignWorldAlone
================
*/
func TestSookBombSettlesKillsAndLeavesUniquesAndForeignWorldAlone(t *testing.T) {
	a, b := instanceWorldTestCharacter(11, "BombOwner"), instanceWorldTestCharacter(12, "Neighbour")
	rt, clock := sookFixture(t, a, b)
	rt.enterSookArena(testDivision, a)
	rt.enterSookArena(testDivision, b)
	ra, rb := rt.sookRun(testDivision, a), rt.sookRun(testDivision, b)
	spawn := func(run *sookArenaRun, ref uint32) uint32 {
		ids := rt.Monsters.SpawnGMMonstersTracked(simulation.GMMonsterSpawn{Division: testDivision, Population: run.lease, RefObjID: ref, Count: 1, Position: run.center, NowMs: clock.Now().UnixMilli(), Arena: true})
		if len(ids) != 1 {
			t.Fatal("spawn")
		}
		return ids[0]
	}
	normal, boss, foreign := spawn(ra, 100), spawn(ra, 101), spawn(rb, 100)
	before, _ := rt.Monsters.Get(testDivision, boss)
	rt.detonateSookBomb(ra, a, ra.center, clock.Now().UnixMilli())
	n, _ := rt.Monsters.Get(testDivision, normal)
	u, _ := rt.Monsters.Get(testDivision, boss)
	f, _ := rt.Monsters.Get(testDivision, foreign)
	if n.CurrentHP != 0 || u.CurrentHP != before.CurrentHP-before.EffectiveMaxHP()/4 || f.CurrentHP != 100000 || ra.program.Kills != 1 || rb.program.Kills != 0 {
		t.Fatalf("shockwave normal=%d boss=%d foreign=%d kills=%d/%d", n.CurrentHP, u.CurrentHP, f.CurrentHP, ra.program.Kills, rb.program.Kills)
	}
	if rt.Monsters.Defeat(testDivision, normal, clock.Now()) == false {
		t.Fatal("ordinary death lifecycle missing")
	}
}

/*
================
TestSookSettlementPaysFivefoldAndUniqueNeverReachesLeaderboard
================
*/
func TestSookSettlementPaysFivefoldAndUniqueNeverReachesLeaderboard(t *testing.T) {
	var nativeExp, nativeSP int64
	for _, arena := range []bool{false, true} {
		rt, clock, c, target := newCombatTestRuntimeMonsterTuned(t, 100, 1, 0, func(ref *monster.MonsterRef) { ref.MonsterType = 3; ref.RunSpeed = 60 })
		if err := rt.AdmitCharacterSession(testDivision, c.Name, 1); err != nil {
			t.Fatal(err)
		}
		lease, _ := rt.EntryPopulationLease(testDivision, c.Name)
		rt.Monsters.DrainUniqueNotices(testDivision)
		if arena {
			run := &sookArenaRun{division: testDivision, name: c.Name, lease: lease, program: sookarena.New(clock.Now().UnixMilli()), bossPowerups: map[uint32]bool{}}
			rt.sookArena = &sookArenaRuntime{}
			rt.sookArena.runs.Store(simulation.WorldKey(testDivision, c.Name), run)
			rt.sookArena.byWorld.Store(instanceWorldKey{testDivision, lease.ID}, run)
			ids := rt.Monsters.SpawnGMMonstersTracked(simulation.GMMonsterSpawn{Division: testDivision, Population: lease, RefObjID: target.Ref.RefObjID, Count: 1, Position: simulation.Spawn{RegionID: target.Spawn.RegionID, X: target.Spawn.X, Y: target.Spawn.Y, Z: target.Spawn.Z}, NowMs: clock.Now().UnixMilli(), Arena: true})
			target, _ = rt.Monsters.Get(testDivision, ids[0])
		}
		var exp, sp int64
		records := 0
		rt.UpdateExperience = func(_ *enterworld.Character, x, y int64, _ uint32) ([]wire.Frame, bool) {
			exp += x
			sp += y
			return nil, true
		}
		rt.RecordUniqueKill = func(string, domain.UniqueKill) { records++ }
		roster := rt.monsterRewardRoster(testDivision, c, clock.Now().UnixMilli())
		if !rt.deps.UpdateManyDeferred(roster.characters, "sook-test-settlement", func() bool {
			impacts := rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, target.CurrentHP, []simulation.MonsterDamagePlan{{GID: target.Gid, ExpectedHP: target.CurrentHP, Damage: target.CurrentHP, CreditGID: enterworld.ObjectIDForCharacter(c)}})
			if len(impacts) != 1 {
				t.Fatal("fatal impact")
			}
			rt.settleMonsterInsideDoor(testDivision, c, roster, impacts[0], monster.Pose{}, clock.Now().UnixMilli())
			return true
		}) {
			t.Fatal("reward commit")
		}
		if !arena {
			nativeExp, nativeSP = exp, sp
			if nativeExp == 0 || records != 1 {
				t.Fatal("native control not rewarded")
			}
		} else {
			if exp != nativeExp*5 || sp != nativeSP*5 || records != 0 {
				t.Fatalf("rewards %d/%d native %d/%d leaderboard %d", exp, sp, nativeExp, nativeSP, records)
			}
			rt.Monsters.Defeat(testDivision, target.Gid, clock.Now())
			if len(rt.Monsters.DrainUniqueNotices(testDivision)) != 0 {
				t.Fatal("private boss death leaked globally")
			}
		}
	}
}

/*
================
TestSookPropExpiryAndNoticeBounds
================
*/
func TestSookPropExpiryAndNoticeBounds(t *testing.T) {
	c := instanceWorldTestCharacter(11, "Expiry")
	rt, clock := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	run := rt.sookRun(testDivision, c)
	for _, kind := range []uint8{1, 4} {
		item := rt.addCharacterGround(testDivision, c, grounditem.Item{ArenaPowerup: kind, DroppedAt: clock.Now()})
		lifetime := int64(40000)
		if kind == 4 {
			lifetime = 1200
		}
		if run.props == nil {
			run.props = map[uint32]int64{}
		}
		run.props[item.Gid] = clock.Now().UnixMilli() + lifetime
		rt.expireSookProps(run, clock.Now().UnixMilli()+lifetime-1)
		if _, ok := rt.Ground.Get(testDivision, item.Gid); !ok {
			t.Fatal("early expiry")
		}
		rt.expireSookProps(run, clock.Now().UnixMilli()+lifetime)
		if _, ok := rt.Ground.Get(testDivision, item.Gid); ok {
			t.Fatal("prop did not expire")
		}
	}
	frame := sookBanner(strings.Repeat("a", 95) + "\U0001f642tail")
	if frame.Opcode != 0x3667 || frame.Payload[0] != 7 || binary.LittleEndian.Uint16(frame.Payload[1:]) != 95 {
		t.Fatalf("notice UTF-16 limit %x", frame.Payload[:3])
	}
}

/*
================
TestSookPetExperienceAndNativeLootPlanner
================
*/
func TestSookPetExperienceAndNativeLootPlanner(t *testing.T) {
	var nativePet uint32
	var nativeGold uint32
	for _, arena := range []bool{false, true} {
		rt, c, pet := petGrowthFixture(t)
		target := monster.Instance{Gid: 9001, Arena: arena, Ref: monster.MonsterRef{RefObjID: 1, Level: 1, MaxHP: 50, ExpToGive: 1, RewardActionPinned: true}}
		frames, _ := rt.awardAttackPetExperience(c, target, 50, 1, 0)
		if len(frames) != 1 {
			t.Fatal("missing pet award")
		}
		delta := binary.LittleEndian.Uint32(frames[0].Payload[5:])
		if !arena {
			nativePet = delta
		} else if delta != nativePet*5 || pet.Experience != uint64(delta) {
			t.Fatalf("pet award %d native %d", delta, nativePet)
		}
		rt, _, c, target = newCombatTestRuntime(t, 1)
		target.Arena = arena
		installSmallGoldRef(rt)
		rt.deps.ItemReferences().(staticItemSource)["ITEM_CH_SWORD_01_A"] = &enterworld.ItemRef{RefObjID: 90000, Codename: "ITEM_CH_SWORD_01_A", TypeIDs: [4]int64{3, 1, 6, 2}}
		rt.DropRoll = constantDropRoll(0)
		drops := rt.planMonsterKillLoot(c, target, monster.Pose{RegionID: target.Spawn.RegionID, X: target.Spawn.X, Y: target.Spawn.Y, Z: target.Spawn.Z}, rt.Now().UnixMilli())
		swords, goldCount := 0, 0
		for _, drop := range drops {
			if drop.IsGold() {
				goldCount++
				if !arena {
					nativeGold = drop.GoldAmount
				} else if drop.GoldAmount != nativeGold {
					t.Fatal("gold multiplier")
				}
			} else if drop.Codename == "ITEM_CH_SWORD_01_A" {
				swords++
			}
		}
		want := 1
		if swords != want || goldCount != 1 {
			t.Fatalf("swords=%d gold=%d arena=%t", swords, goldCount, arena)
		}
	}
}

/*
================
TestSookBadEggAggroStaysInItsPrivateArena
================
*/
func TestSookBadEggAggroStaysInItsPrivateArena(t *testing.T) {
	a, b := instanceWorldTestCharacter(11, "AlarmOwner"), instanceWorldTestCharacter(12, "AlarmNeighbour")
	rt, clock := sookFixture(t, a, b)
	rt.enterSookArena(testDivision, a)
	rt.enterSookArena(testDivision, b)
	ra, rb := rt.sookRun(testDivision, a), rt.sookRun(testDivision, b)
	spawn := func(run *sookArenaRun) uint32 {
		ids := rt.Monsters.SpawnGMMonstersTracked(simulation.GMMonsterSpawn{Division: testDivision, Population: run.lease, RefObjID: 100, Count: 1, Position: run.center, NowMs: clock.Now().UnixMilli(), Arena: true, ArenaPassive: true})
		if len(ids) != 1 {
			t.Fatal("spawn")
		}
		return ids[0]
	}
	first, second, foreign := spawn(ra), spawn(ra), spawn(rb)
	item := rt.addCharacterGround(testDivision, a, grounditem.Item{ArenaPowerup: sookAlarm, RefObjID: 9272, Codename: sookEgg, DroppedAt: clock.Now()})
	out := rt.pickupSookPowerup(testDivision, simulation.WorldKey(testDivision, a.Name), a, item)
	if len(out.Broadcast) == 0 {
		t.Fatal("bad egg refused")
	}
	for _, gid := range []uint32{first, second, foreign} {
		mover, ok := rt.Monsters.Mover(testDivision, gid)
		if !ok || (mover.TargetGID() == enterworld.ObjectIDForCharacter(a)) != (gid != foreign) {
			t.Fatalf("wrong target for %d", gid)
		}
	}
	if _, exists := rt.Ground.Get(testDivision, item.Gid); exists {
		t.Fatal("egg not consumed")
	}
}

/*
================
TestSookAmbushEggSpawnsTenAndRespectsPrivatePopulationCap
================
*/
func TestSookAmbushEggSpawnsTenAndRespectsPrivatePopulationCap(t *testing.T) {
	for _, existing := range []int{0, 195, 200} {
		a, b := instanceWorldTestCharacter(11, "AmbushOwner"), instanceWorldTestCharacter(12, "AmbushNeighbour")
		rt, clock := sookFixture(t, a, b)
		rt.enterSookArena(testDivision, a)
		rt.enterSookArena(testDivision, b)
		ra, rb := rt.sookRun(testDivision, a), rt.sookRun(testDivision, b)
		for i := 0; i < existing; i++ {
			if !rt.spawnSookMonster(ra, ra.refs[0], 0, clock.Now().UnixMilli()) {
				t.Fatal("fixture spawn")
			}
			ra.sequence++
		}
		item := rt.addCharacterGround(testDivision, a, grounditem.Item{ArenaPowerup: sookAmbush, RefObjID: 9272, Codename: sookEgg, DroppedAt: clock.Now()})
		out := rt.pickupSookPowerup(testDivision, simulation.WorldKey(testDivision, a.Name), a, item)
		if len(out.Broadcast) == 0 {
			t.Fatal("ambush refused")
		}
		actors := rt.Monsters.PopulationInstances(testDivision, ra.lease, []uint16{ra.center.RegionID})
		if len(actors) != min(sookarena.MaxAlive, existing+10) {
			t.Fatalf("existing%d: got%d monsters", existing, len(actors))
		}
		if len(rt.Monsters.PopulationInstances(testDivision, rb.lease, []uint16{rb.center.RegionID})) != 0 {
			t.Fatal("ambush crossed private arena")
		}
		if _, exists := rt.Ground.Get(testDivision, item.Gid); exists {
			t.Fatal("ambush egg not consumed")
		}
	}
}

/*
================
TestSookResurrectionScrollsPreserveInventory
Bag and premium scroll effects share the arena refusal, without a revive.
================
*/
func TestSookResurrectionScrollsPreserveInventory(t *testing.T) {
	c, items, body := resurrectionScrollFixture(200)
	rt, clock := sookFixture(t, c)
	for code, ref := range items {
		rt.deps.ItemReferences().(staticItemSource)[code] = ref
	}
	rt.enterSookArena(testDivision, c)
	run := rt.sookRun(testDivision, c)
	if run == nil {
		t.Fatal("entry")
	}
	zero := int64(0)
	c.CurrentHP = &zero
	before := c.MissionInventory[len(c.MissionInventory)-1].StackCount
	out := rt.HandleItemUse(testDivision, c, body)
	if len(out.Frames) < 1 || out.Frames[0].Opcode != wire.OpItemUseResponse || out.Frames[0].Payload[0] != wire.ResultError || enterworld.CharacterAlive(c) || c.MissionInventory[len(c.MissionInventory)-1].StackCount != before {
		t.Fatal("arena scroll revived or consumed", out)
	}
	var result OpResult
	ref := items["ITEM_MALL_RESURRECTION_60P_SCROLL"]
	if _, ok := rt.resurrectWithScroll(testDivision, c, ref, clock.Now().UnixMilli(), &result); ok || enterworld.CharacterAlive(c) {
		t.Fatal("premium resurrection bypassed arena death")
	}
	if domain.CharacterWorldInstance(c) != uint32(run.lease.ID) {
		t.Fatal("scroll relocated corpse")
	}
}
