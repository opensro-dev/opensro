/*
===========================================================================

fortress_battle_test.go - combat, party and rank lifecycle integration

===========================================================================
*/
package action

import (
	"encoding/binary"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/social/union"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"testing"
)

/*
================
fortressBattlePair
================
*/
func fortressBattlePair(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character, *enterworld.Character, uint32) {
	t.Helper()
	rt, clock, killer, victim := newPvpPair(t)
	if err := rt.ConfigurePortals(gamedatatest.TextdataDir(t)); err != nil {
		t.Fatal(err)
	}
	packed := uint32(instance.Pack(2, 1))
	killer.World.PackedInstance = &packed
	victim.World.PackedInstance = &packed
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	id, ok := rt.activeBattleFortress(testDivision, victim)
	if !ok {
		t.Fatal("fixture not in fortress")
	}
	return rt, clock, killer, victim, id
}

/*
================
TestFortressFatalAttackCreditsOnce
================
*/
func TestFortressFatalAttackCreditsOnce(t *testing.T) {
	rt, _, killer, victim, id := fortressBattlePair(t)
	victim.CurrentHP = testInt64(1)
	victim.PK = &domain.PKRecord{Penalty: 6000, DailyCount: 8}
	var granted int64
	rt.UpdateExperience = func(c *enterworld.Character, exp, _ int64, _ uint32) ([]wire.Frame, bool) {
		if c == killer {
			granted += exp
		}
		return nil, true
	}
	if got := rt.deathKind(testDivision, victim, rt.prepareDeathKiller(testDivision, victim, deathKiller{player: killer})); got != pk.DeathSpecialWorld {
		t.Fatalf("death kind %d", got)
	}
	var scores = map[string][]wire.Frame{}
	rt.PushCharacterFrames = func(_, name string, frames []wire.Frame) {
		for _, f := range frames {
			if f.Opcode == opFortressWarState && len(f.Payload) > 0 && f.Payload[0] == 0x11 {
				scores[name] = append(scores[name], f)
			}
		}
	}
	payload := wire.BasicAttackEngage{TargetGid: enterworld.ObjectIDForCharacter(victim)}.Encode()
	rt.HandleTargetInteract(testDivision, killer, payload)
	if enterworld.CharacterAlive(victim) {
		t.Fatal("victim survived")
	}
	rt.HandleTargetInteract(testDivision, killer, payload)
	if killer.PK != nil && killer.PK.TotalCount != 0 {
		t.Fatalf("fortress kill counted as murder: %+v", killer.PK)
	}
	if victim.PK.Penalty != 6000 || victim.PK.DailyCount != 8 {
		t.Fatalf("fortress death relieved PK %+v", victim.PK)
	}
	if granted != 26 {
		t.Fatalf("siege PvP EXP %d want 26 without murderer doubling", granted)
	}
	for _, c := range []*enterworld.Character{killer, victim} {
		row, ok := rt.Fortresses.BattleRecord(testDivision, id, c.ID)
		if !ok || (c == killer && (row.Kills != 1 || row.Deaths != 0)) || (c == victim && (row.Kills != 0 || row.Deaths != 1)) {
			t.Fatalf("%s: %+v", c.Name, row)
		}
		if len(scores[c.Name]) != 1 {
			t.Fatalf("%s got %d scores", c.Name, len(scores[c.Name]))
		}
		frames := rt.FortressBattleFrames(testDivision, c)
		if len(frames) != 1 || binary.LittleEndian.Uint32(frames[0].Payload[1:]) != id || binary.LittleEndian.Uint32(frames[0].Payload[5:]) != row.Kills {
			t.Fatalf("entry score %+v", frames)
		}
	}
}

/*
================
TestFortressPartyCreditRequiresLivingSameWorldAndRange
================
*/
func TestFortressPartyCreditRequiresLivingSameWorldAndRange(t *testing.T) {
	rt, clock, killer, victim, id := fortressBattlePair(t)
	deps := rt.deps.(*enterworld.Deps)
	party := RewardParty{Members: []uint32{enterworld.ObjectIDForCharacter(killer)}}
	origin := rt.liveSpawn(simulation.WorldKey(testDivision, killer.Name), killer, clock.NowMs())
	for i, tc := range []struct {
		name                   string
		distance               float64
		dead, otherWorld, want bool
	}{
		{"boundary", 1000, false, false, true}, {"outside", 1000.01, false, false, false}, {"dead", 0, true, false, false}, {"other", 0, false, true, false},
	} {
		c := *killer
		c.ID = int64(20 + i)
		c.Name = tc.name
		c.CurrentHP = testInt64(100)
		world := *killer.World
		c.World = &world
		if tc.dead {
			c.CurrentHP = testInt64(0)
		}
		if tc.otherWorld {
			packed := uint32(instance.Pack(2, 2))
			c.World.PackedInstance = &packed
		}
		fixtureCharacters(deps.Characters)[testDivision] = append(fixtureCharacters(deps.Characters)[testDivision], &c)
		party.Members = append(party.Members, enterworld.ObjectIDForCharacter(&c))
		rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(&c) }, func(w *simulation.WorldState) { w.Spawn = origin; w.Spawn.X += tc.distance; w.SpawnSet = true })
	}
	rt.RewardParties = func(string) []RewardParty { return []RewardParty{party} }
	killer.CurrentHP = testInt64(0) // Only the other recipients must be alive.
	rt.recordFortressDeath(testDivision, victim, killer, clock.NowMs())
	for i, want := range []bool{true, false, false, false} {
		_, ok := rt.Fortresses.BattleRecord(testDivision, id, int64(20+i))
		if ok != want {
			t.Fatalf("member %d credit=%v", i, ok)
		}
	}
	row, _ := rt.Fortresses.BattleRecord(testDivision, id, killer.ID)
	if row.Kills != 1 {
		t.Fatal("dead killer lost credit")
	}
}

/*
================
TestFortressRankSkillsReplaceAndWarEndRetires
================
*/
func TestFortressRankSkillsReplaceAndWarEndRetires(t *testing.T) {
	rt, clock, c, _, _ := fortressBattlePair(t)
	rt.deps.(*enterworld.Deps).Skills = enterworld.NewTextdataSkills(gamedatatest.TextdataDir(t))
	if err := rt.AdmitCharacterSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	id, ok := rt.activeBattleFortress(testDivision, c)
	if !ok {
		t.Fatal("not in fortress")
	}
	guild := int64(77)
	c.GuildID = &guild
	rt.Fortresses.Occupy(testDivision, id, guild)
	var notices int
	rt.PushCharacterFrames = func(_, name string, frames []wire.Frame) {
		for _, f := range frames {
			if f.Opcode == opFortressWarState && len(f.Payload) > 0 && f.Payload[0] == 14 {
				notices++
			}
		}
	}
	for kills := uint32(1); kills <= 150; kills++ {
		rt.creditFortressBattle(testDivision, id, c, true, clock.NowMs())
		record, _ := rt.Fortresses.BattleRecord(testDivision, id, c.ID)
		var expected uint8
		for rank := uint8(1); rank <= 6; rank++ {
			threshold, _, _ := fortress.BattleRank(rank)
			if kills >= threshold {
				expected = rank
			}
		}
		if record.Rank != expected {
			t.Fatalf("kill %d rank %d want %d", kills, record.Rank, expected)
		}
		effects := rt.effects.Snapshot(testDivision, c.Name)
		rankEffects := 0
		for _, e := range effects {
			if e.SkillID >= 20510 && e.SkillID <= 20515 {
				rankEffects++
				_, skill, _ := fortress.BattleRank(expected)
				if e.SkillID != skill {
					t.Fatalf("old rank skill %d", e.SkillID)
				}
			}
		}
		if expected > 0 && rankEffects != 1 {
			t.Fatalf("rank %d effects %d", expected, rankEffects)
		}
	}
	if notices != 6 {
		t.Fatalf("rank notices %d", notices)
	}
	rt.runFortressPhase(fortressPhase{division: testDivision, mode: fortressPhaseEnd, dueMs: clock.NowMs()})
	for _, e := range rt.effects.Snapshot(testDivision, c.Name) {
		if e.SkillID >= 20510 && e.SkillID <= 20515 {
			t.Fatalf("rank skill survived war: %+v", e)
		}
	}
	if _, ok := rt.Fortresses.BattleRecord(testDivision, id, c.ID); ok {
		t.Fatal("live score survived war")
	}
	if domain.CharacterWorldInstance(c) != uint32(instance.Pack(2, 1)) {
		t.Fatal("holder expelled")
	}
}

/*
================
TestFortressDamageOverTimeCreditsOnce
================
*/
func TestFortressDamageOverTimeCreditsOnce(t *testing.T) {
	rt, clock, killer, victim, id := fortressBattlePair(t)
	victim.CurrentHP = testInt64(1)
	var granted int64
	rt.UpdateExperience = func(c *enterworld.Character, exp, _ int64, _ uint32) ([]wire.Frame, bool) {
		if c == killer {
			granted += exp
		}
		return nil, true
	}
	rt.deps.(*enterworld.Deps).Skills = enterworld.NewTextdataSkills(gamedatatest.TextdataDir(t))
	for i := 0; i < 14; i++ {
		rt.creditFortressBattle(testDivision, id, killer, true, clock.NowMs())
	}
	now := clock.NowMs()
	record := abnormal.Record{Status: abnormal.Burn, Level: 4, DurationMs: 30000, Rate24: 10, Scale20: 1, SourceGID: enterworld.ObjectIDForCharacter(killer)}
	if rt.applyPlayerAbnormalInDoor(testDivision, victim, false, []abnormal.Record{record}, now) == nil {
		t.Fatal("no burn owner")
	}
	rt.advancePlayerAbnormals(now)
	rt.advancePlayerAbnormals(now + 10000)
	if enterworld.CharacterAlive(victim) {
		t.Fatal("burn victim survived")
	}
	kill, ok := rt.Fortresses.BattleRecord(testDivision, id, killer.ID)
	death, _ := rt.Fortresses.BattleRecord(testDivision, id, victim.ID)
	if granted != 26 {
		t.Fatalf("DOT siege EXP %d want 26", granted)
	}
	if !ok || kill.Kills != 15 || kill.Rank != 1 || death.Deaths != 1 || len(killer.TimedSkillJobs) != 1 {
		t.Fatalf("burn scores %+v %+v", kill, death)
	}
}

/*
================
TestFortressEndKeepsOnlyRegisteredAlliesOfTheCurrentHolder
================
*/
func TestFortressEndKeepsOnlyRegisteredAlliesOfTheCurrentHolder(t *testing.T) {
	rt, _, _, _, id := fortressBattlePair(t)
	rt.Unions = union.New()
	for _, guild := range []int64{8, 9} {
		if _, err := rt.Unions.Join(testDivision, 7, guild); err != nil {
			t.Fatal(err)
		}
	}
	rt.Fortresses.Occupy(testDivision, id, 6)
	rt.Fortresses.Capture(testDivision, id, 7, 0)
	rt.Fortresses.SetApplication(testDivision, id, 8, fortress.RequestAttack, true)
	rt.Fortresses.SetApplication(testDivision, id, 10, fortress.RequestAttack, true)
	kept := rt.fortressEndGuilds(testDivision)[id]
	if len(kept) != 2 || !kept[7] || !kept[8] || kept[6] || kept[9] || kept[10] {
		t.Fatalf("war-end side %+v", kept)
	}
}
