/*
===========================================================================

monster_dormancy_test.go - simulation monster  dormancy test ownership

===========================================================================
*/

package simulation

import (
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

/*
================
TestUniqueRespawnClockContinuesWithoutPlayersWithDormancyEnabled
================
*/
func TestUniqueRespawnClockContinuesWithoutPlayersWithDormancyEnabled(t *testing.T) {
	ref := lifecycleRef(1)
	ref.MonsterType = 3
	w := newLifecycleWorld(t, ref, lifecycleNest(100))
	w.s.EnableRegionDormancy()
	first := w.expectLive(0, 1)[0]
	w.s.prepareDormancy(w.now.UnixMilli(), "division", nil)
	w.kill(0, first.Gid)
	w.expectLive(9999, 0)
	next := w.expectLive(10000, 1)[0]
	if next.Gid == first.Gid {
		t.Fatal("death resurrected stale identity")
	}
}

/*
================
TestDormancyPreservesPositionAndWakesBeforeVisibility
================
*/
func TestDormancyPreservesPositionAndWakesBeforeVisibility(t *testing.T) {
	s := NewMonsterState(monster.Template{})
	s.EnableRegionDormancy()
	if err := s.EnableDormantStorage(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.StartDivision("sleep")
	d := s.divs["sleep"]
	d.movers = newMoverStorage(nil)
	s.prepareDormancy(100000, "sleep", nil)
	actor := monster.Instance{Gid: 123, Ref: monster.MonsterRef{MaxHP: 100}, Nest: monster.NestRow{Respawn: true}, CurrentHP: 100}
	pose := monster.Pose{RegionID: 0x6060, X: 123, Y: 45, Z: 678, Heading: 321}
	mover := monster.PendingMover{Pose: pose, Activity: monster.ActivityCadence{Interval: 1500, LastCheck: 90000}, TransitionSerial: 91, Channel: 2}.Expand()
	d.instances.set(actor.Gid, actor)
	d.movers.set(actor.Gid, mover)
	d.behavior.set(actor.Gid, 0)
	timers := s.aiTimersLocked(d, actor, 100000)
	timers.CheckTimer(monster.TimerIDAcquisition, 99999)
	wantTimers := *timers
	s.scheduleBehavior(d, actor.Gid, 100000)
	s.scheduleBehavior(d, actor.Gid, 100000)
	if len(d.archiveQueue) != 1 {
		t.Fatal("repeated scheduling queued duplicate archive work")
	}
	if len(d.dormant) != 1 || d.behavior.Len() != 0 {
		t.Fatal("inactive actor still scheduled")
	}
	if d.aiTimers[actor.Gid] != nil || len(d.storedAITimers) != 1 {
		t.Fatal("sleep retained mutable timer bank")
	}
	s.prepareDormancy(110000, "sleep", nil)
	if len(d.instances.cold) != 1 || len(d.instances.hot) != 0 {
		t.Fatal("sleeping actor was not archived")
	}
	if got := d.movers.get(actor.Gid); got != mover {
		t.Fatal("sleep changed position, clock or movement identity")
	}
	session := SessionSnapshot{DivisionID: "sleep", Population: d.lease}
	session.World.Spawn.RegionID = 0x6061 // next sector, well outside visible block range
	s.prepareDormancy(200000, "sleep", []SessionSnapshot{session})
	if len(d.dormant) != 0 || d.behavior.Len() != 1 {
		t.Fatal("approaching player did not wake actor")
	}
	if len(d.instances.cold) != 0 || len(d.instances.hot) != 1 {
		t.Fatal("approach did not restore actor before visibility")
	}
	if d.instances.get(actor.Gid) != actor || d.movers.get(actor.Gid) != mover {
		t.Fatal("wake respawned or relocated actor")
	}
	if d.aiTimers[actor.Gid] == nil || *d.aiTimers[actor.Gid] != wantTimers || len(d.storedAITimers) != 0 {
		t.Fatal("wake reset or advanced AI timers")
	}
	s.scheduleBehavior(d, actor.Gid, 210000)
	if d.behavior.Len() != 1 {
		t.Fatal("grace period was ignored")
	}
	s.scheduleBehavior(d, actor.Gid, 240000)
	if d.behavior.Len() != 0 {
		t.Fatal("inactive actor did not return to sleep")
	}
}

/*
================
TestDormancyNeverSuspendsUniqueOrActiveEncounter
================
*/
func TestDormancyNeverSuspendsUniqueOrActiveEncounter(t *testing.T) {
	for _, kind := range []string{"unique", "wounded", "summoned", "controlled", "moving", "corpse"} {
		t.Run(kind, func(t *testing.T) {
			s := NewMonsterState(monster.Template{})
			s.EnableRegionDormancy()
			s.StartDivision("sleep")
			d := s.divs["sleep"]
			d.movers = newMoverStorage(nil)
			s.prepareDormancy(100000, "sleep", nil)
			actor := monster.Instance{Gid: 1, Ref: monster.MonsterRef{MaxHP: 100}, Nest: monster.NestRow{Respawn: true}, CurrentHP: 100}
			mover := monster.PendingMover{Pose: monster.Pose{RegionID: 0x6060}, Activity: monster.ActivityCadence{Interval: 1500}}.Expand()
			switch kind {
			case "unique":
				actor.Ref.MonsterType = 3
				actor.CurrentHP = actor.EffectiveMaxHP()
			case "wounded":
				actor.CurrentHP = 50
			case "summoned":
				actor.SummonerGID = 44
			case "controlled":
				if err := mover.BindController(monster.ControlOwned, 44); err != nil {
					t.Fatal(err)
				}
			case "moving":
				mover.From = mover.Pose
				mover.To = mover.Pose
				mover.To.X = 100
				mover.DepartMs = 99999
				mover.ArriveMs = 101000
			case "corpse":
				actor.CurrentHP = 0
			}
			d.instances.set(1, actor)
			d.movers.set(1, mover)
			s.scheduleBehavior(d, 1, 100000)
			if len(d.dormant) != 0 || d.behavior.Len() != 1 {
				t.Fatal("special or active actor was suspended")
			}
		})
	}
}
