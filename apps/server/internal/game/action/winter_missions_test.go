/*
===========================================================================

winter_missions_test.go - the Server Missions' reports, rewards and
their keeping

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
memoryWinterStore
================
*/
type memoryWinterStore struct {
	kept map[string]domain.WinterMissions
}

func (s *memoryWinterStore) WinterMissions(division string) domain.WinterMissions {
	return s.kept[division]
}

func (s *memoryWinterStore) SetWinterMissions(division string, missions domain.WinterMissions) {
	if s.kept == nil {
		s.kept = map[string]domain.WinterMissions{}
	}
	s.kept[division] = missions
}

/*
================
winterMissionRuntime

The Christmas fixture on, its division prepared from the store as the
first tick would, with nobody counted online (the goals are their +100).
================
*/
func winterMissionRuntime(t *testing.T, c *enterworld.Character, store *memoryWinterStore) *Runtime {
	t.Helper()
	rt := christmasLootRuntime(t, c, true)
	rt.WinterMissionStore = store
	rt.prepareWinterDivision(testDivision, rt.Now())
	return rt
}

/*
================
TestWinterHuntCompletesAfterItsDelayAndCrownsTheHunter

The hundredth Snow Slave completes the hunt and, through it, the knights'
summon; nothing pays before the ten-second delay. The hunter who landed
the last kill wins +100% EXP and skill EXP for three hours on the buff
board's flake row. Other kills never count.
================
*/
func TestWinterHuntCompletesAfterItsDelayAndCrownsTheHunter(t *testing.T) {
	c := testCharacter()
	rt := winterMissionRuntime(t, c, &memoryWinterStore{})
	rt.RewardActorPresent = func(string, string) bool { return true }
	nowMs := rt.Now().UnixMilli()
	slave := monster.Instance{Ref: monster.MonsterRef{Codename: progression.ChristmasSnowSlaveCodename}}
	rt.reportWinterKill(testDivision, c, monster.Instance{Ref: monster.MonsterRef{Codename: "MOB_CH_MANGNYANG"}}, nowMs)
	for range 100 {
		rt.reportWinterKill(testDivision, c, slave, nowMs)
	}
	if due := rt.dueWinterRewards(rt.Now()); len(due) != 0 {
		t.Fatalf("paid before the delay: %+v", due)
	}
	due := rt.dueWinterRewards(rt.Now().Add(progression.WinterMissionRewardDelay))
	if len(due) != 2 || due[0].completion.ID != progression.WinterMissionSnowSlave || due[0].completion.Winner != c.Name ||
		due[1].completion.ID != progression.WinterMissionKnights {
		t.Fatalf("due rewards %+v", due)
	}
	rt.grantWinterWinner(testDivision, c.Name, nowMs)
	wantEnd := nowMs + progression.WinterWinnerBuffDuration.Milliseconds()
	if len(c.ParamJobs) != 2 || c.ParamJobs[0].ItemRefObjID != testSnowflakeRef || c.ParamJobs[0].EndUnixMs != wantEnd {
		t.Fatalf("winner's jobs %+v", c.ParamJobs)
	}
	if exp, skill := paramJobRewardBonus(c, 100, 50, nowMs); exp != 200 || skill != 100 {
		t.Fatalf("winner's bonus %d/%d, want 200/100", exp, skill)
	}
}

/*
================
TestWinterSnowFlakeEventRaisesEveryKill

So-ok's gifts count for the Snow Flake Event; its completion gives every
kill of the division +20% EXP and skill EXP for an hour, on the kill's own
award, and the end time is kept for a restart.
================
*/
func TestWinterSnowFlakeEventRaisesEveryKill(t *testing.T) {
	c := testCharacter()
	store := &memoryWinterStore{}
	rt := winterMissionRuntime(t, c, store)
	rt.recordWinterMission(testDivision, progression.WinterTriggerExchange, 100, c.Name, rt.Now())
	at := rt.Now().Add(progression.WinterMissionRewardDelay)
	due := rt.dueWinterRewards(at)
	if len(due) != 1 || due[0].completion.ID != progression.WinterMissionSnowFlake {
		t.Fatalf("due rewards %+v", due)
	}
	if exp, skill := rt.winterExpUpBonus(testDivision, 100, 50, 130, 50, at.UnixMilli()); exp != 130 || skill != 50 {
		t.Fatal("a bonus before the reward paid")
	}
	rt.payWinterReward(due[0], at)
	if exp, skill := rt.winterExpUpBonus(testDivision, 100, 50, 130, 50, at.UnixMilli()); exp != 150 || skill != 60 {
		t.Fatalf("bonus %d/%d, want 150/60", exp, skill)
	}
	after := at.Add(progression.WinterExpUpDuration)
	if exp, _ := rt.winterExpUpBonus(testDivision, 100, 50, 100, 50, after.UnixMilli()); exp != 100 {
		t.Fatal("the bonus outlived its hour")
	}
	rt.saveWinterMissions()
	kept := store.kept[testDivision]
	if kept.ExpUpUntilMs != after.UnixMilli() || len(kept.Rows) != 4 || kept.Rows[0].Completions != 1 {
		t.Fatalf("kept %+v", kept)
	}
}

/*
================
TestWinterMissionsResumeFromTheStore

A restart reads the kept rows back: the half-filled hunt goes on from
where it stood, not from zero.
================
*/
func TestWinterMissionsResumeFromTheStore(t *testing.T) {
	store := &memoryWinterStore{kept: map[string]domain.WinterMissions{testDivision: {
		Rows: []domain.WinterMissionRow{{ID: progression.WinterMissionSnowSlave, Completions: 3, Goal: 460, Count: 459}},
	}}}
	c := testCharacter()
	rt := winterMissionRuntime(t, c, store)
	rt.reportWinterKill(testDivision, c, monster.Instance{Ref: monster.MonsterRef{Codename: progression.ChristmasSnowSlaveCodename}}, rt.Now().UnixMilli())
	rows := rt.WinterMissionProgress(testDivision)
	if rows[1].Completions != 4 || rows[1].Count != 0 {
		t.Fatalf("resumed hunt %+v", rows[1])
	}
}

/*
================
TestWinterMissionsStayQuietOutsideTheEvent

With the event off nothing counts and no table replaces a spawn.
================
*/
func TestWinterMissionsStayQuietOutsideTheEvent(t *testing.T) {
	c := testCharacter()
	rt := winterMissionRuntime(t, c, &memoryWinterStore{})
	t.Setenv(progression.EnvChristmasEvent, "off")
	rt.Christmas = progression.ChristmasEventFromEnv()
	rt.recordWinterMission(testDivision, progression.WinterTriggerExchange, 1000, c.Name, rt.Now())
	if due := rt.dueWinterRewards(rt.Now().Add(time.Hour)); len(due) != 0 {
		t.Fatalf("event off: %+v", due)
	}
	table := rt.WinterSpawnReplacements(monster.MonsterRef{RefObjID: 1}, true)
	if rows := table(rt.Now().UnixMilli()); len(rows) != 0 {
		t.Fatalf("event off: replace table %+v", rows)
	}
}
