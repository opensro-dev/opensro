/*
===========================================================================

guildwar_test.go - declared hostility and the fatal combat handoff

The actual attack path must classify a war death before changing progression,
publish one score, pay PvP EXP and avoid murder accounting.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/social/guildwar"
)

/*
================
warRelationStore

Only snapshot loading is exercised. Mutations belong to the wire/store tests.
================
*/
type warRelationStore struct{ domain.GuildWarStore }

/*
================
GuildWars
================
*/
func (warRelationStore) GuildWars(string) ([]domain.GuildWarRecord, error) {
	return []domain.GuildWarRecord{{ID: 77, Guilds: [2]int64{7, 8}}}, nil
}

/*
================
TestGuildWarFatalAttackCreditsOnceWithoutMurder
================
*/
func TestGuildWarFatalAttackCreditsOnceWithoutMurder(t *testing.T) {
	rt, _, a, v := newPvpPair(t)
	var err error
	rt.GuildWars, err = guildwar.New(testDivision, warRelationStore{})
	if err != nil {
		t.Fatal(err)
	}
	a.GuildID, v.GuildID = testInt64(7), testInt64(8)
	a.Aggressions = nil
	skills := rt.deps.SkillData().(staticSkillSource)
	basic := skills[2]
	basic.ReplacementPinned, basic.Replacement.MatchesExecutionSelector = true, true
	skills[2] = basic
	v.CurrentHP = testInt64(1)
	var credits []domain.GuildWarCombat
	rt.GuildWarKill = func(_ string, combat domain.GuildWarCombat, _ int64) { credits = append(credits, combat) }
	var granted int64
	rt.UpdateExperience = func(c *enterworld.Character, exp, _ int64, _ uint32) ([]wire.Frame, bool) {
		if c == a {
			granted += exp
		}
		return nil, true
	}
	if !rt.hostilePlayerRelation(testDivision, a, v) {
		t.Fatal("war was not hostile below the ordinary PK level floor")
	}
	out := rt.HandleTargetInteract(testDivision, a, wire.BasicAttackEngage{TargetGid: enterworld.ObjectIDForCharacter(v)}.Encode())
	if enterworld.CharacterAlive(v) || len(credits) != 1 || credits[0] != (domain.GuildWarCombat{WarID: 77, KillerID: a.ID, VictimID: v.ID, Score: 100}) {
		t.Fatalf("fatal credit %+v, alive=%v result=%+v", credits, enterworld.CharacterAlive(v), out)
	}
	if a.PK != nil && (a.PK.Penalty != 0 || a.PK.TotalCount != 0) {
		t.Fatalf("war kill became murder %+v", a.PK)
	}
	if a.PVPState() != 0 {
		t.Fatalf("war attack entered aggression state %d", a.PVPState())
	}
	if granted != 26 {
		t.Fatalf("guild-war PvP EXP %d", granted)
	}
	rt.HandleTargetInteract(testDivision, a, wire.BasicAttackEngage{TargetGid: enterworld.ObjectIDForCharacter(v)}.Encode())
	if len(credits) != 1 {
		t.Fatal("corpse attack earned another score")
	}
}

/*
================
TestGuildWarCompanionLevelAndPartyProtection
================
*/
func TestGuildWarCompanionLevelAndPartyProtection(t *testing.T) {
	rt, _, a, v := newPvpPair(t)
	rt.GuildWars, _ = guildwar.New(testDivision, warRelationStore{})
	a.GuildID, v.GuildID = testInt64(7), testInt64(8)
	if kind := rt.deathKind(testDivision, v, deathKiller{player: a}); kind != pk.DeathGuildWar {
		t.Fatalf("kind %d", kind)
	}
	credit := rt.prepareGuildWarCombat(testDivision, v, deathKiller{player: a, strikerLevel: 7})
	if credit.Score != 1 {
		t.Fatalf("companion level ignored: %+v", credit)
	}
	members := []uint32{enterworld.ObjectIDForCharacter(a), enterworld.ObjectIDForCharacter(v)}
	rt.RewardParties = func(string) []RewardParty { return []RewardParty{{Members: members}} }
	result := rt.HandleTargetInteract(testDivision, a, wire.BasicAttackEngage{TargetGid: members[1]}.Encode())
	answer, ok := findFrame(result.Frames, wire.OpSkillCastResult)
	if !ok || len(answer.Payload) != 2 || answer.Payload[1] != 0x22 {
		t.Fatalf("war bypassed party protection: %+v", result)
	}
}

/*
================
TestGuildWarCompanionStatusSourceLifecycle

A pet's GID survives in the status record. Resolve its current owner before
entering the victim transaction and score with the pet's own level.
================
*/
func TestGuildWarCompanionStatusSourceLifecycle(t *testing.T) {
	rt, _, a, v := newPvpPair(t)
	rt.GuildWars, _ = guildwar.New(testDivision, warRelationStore{})
	a.GuildID, v.GuildID = testInt64(7), testInt64(8)
	equipCombatTestPet(t, rt, a, cosBandAttackPet)
	pet := a.ActiveCOS
	pet.Level = 7
	source := rt.captureAbnormalSource(testDivision, pet.GID)
	if !source.exists || source.dead || source.killer.player == nil || source.killer.player.ID != a.ID || source.killer.strikerLevel != 7 {
		t.Fatalf("source %+v", source)
	}
	v.CurrentHP = testInt64(1)
	owner := rt.newPlayerAbnormalOwner(testDivision, v, 1000)
	owner.sources = map[uint32]abnormalSourceState{pet.GID: source}
	owner.Hit(pet.GID, true, 1, abnormalDamageOverTimeReason, abnormal.Burn)
	var credits []domain.GuildWarCombat
	rt.GuildWarKill = func(_ string, credit domain.GuildWarCombat, _ int64) { credits = append(credits, credit) }
	rt.playerAbnormalPublication(testDivision, v, owner)
	if len(credits) != 1 || credits[0].Score != 1 || credits[0].KillerID != a.ID {
		t.Fatalf("pet status credits %+v", credits)
	}
	pet.CurrentHP = 0
	if source := rt.captureAbnormalSource(testDivision, pet.GID); !source.exists || !source.dead {
		t.Fatalf("dead pet source %+v", source)
	}
	pet.Summoned = false
	if source := rt.captureAbnormalSource(testDivision, pet.GID); source.exists || source.killer.player != nil {
		t.Fatalf("released pet retained credit %+v", source)
	}
}
