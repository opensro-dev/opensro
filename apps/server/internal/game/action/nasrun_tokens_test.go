/*
===========================================================================

nasrun_tokens_test.go - tests for nasrun_tokens.go

Through the real kill settlement (HandleTargetInteract), as the damage
ranking's test: a partner's earlier damage and the actor's fatal blow on
Tiger Girl (an easy unique) each earn one token, written on the character
with a notice; off, nothing. One account earns once, on its top
contributor; a contributor past the level band earns nothing; a capped
day pays nothing more, and an older day's cap restarts.

===========================================================================
*/
package action

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/nasrun"
	"opensro.online/server/internal/game/social/chat"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
tokenKill

One fatal blow by the actor on a Tiger Girl the partner has already hit
for 99 of its 100 HP. shape edits both characters before the kill, with
the kill's token day; on is the SRO_NASRUN switch.
================
*/
func tokenKill(t *testing.T, on bool, shape func(today int64, actor, partner *enterworld.Character)) (*enterworld.Character, *enterworld.Character, OpResult) {
	t.Helper()
	rt, _, actor, target := newCombatTestRuntime(t, 100)
	rt.NasrunTokens = on
	ref := target.Ref
	ref.Codename, ref.MonsterType = "MOB_CH_TIGERWOMAN", uniqueRarityGrade
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{ref.RefObjID: ref}, []monster.NestRow{{SpawnPoint: target.Spawn, RetailEvidence: true, MaxCount: 1}}))
	rt.Monsters.SetTimeSource(rt.Now)
	rt.Monsters.StartDivision(testDivision)
	rt.Monsters.AdvancePopulation(rt.Monsters.CurrentTimeMillis())
	target = rt.Monsters.InstancesInRegions(testDivision, []uint16{target.Spawn.RegionID})[0]
	deps := rt.deps.(*enterworld.Deps)
	source := fixtureCharacters(deps.Characters)
	partner := *actor
	partner.ID, partner.Name, partner.MissionInventory = 4, "partner", nil
	actor.AccountID, partner.AccountID = "acct-actor", "acct-partner"
	if shape != nil {
		shape(nasrun.TokenDay(rt.Now().UTC()), actor, &partner)
	}
	source[testDivision] = append(source[testDivision], &partner)
	hits := rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, 100, []simulation.MonsterDamagePlan{
		{GID: target.Gid, Damage: 99, CreditGID: enterworld.ObjectIDForCharacter(&partner)},
	})
	if len(hits) != 1 || hits[0].Fatal {
		t.Fatal("nonfatal contribution setup failed")
	}
	rt.DropRoll = constantDropRoll(0)
	result := rt.HandleTargetInteract(testDivision, actor, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	return actor, &partner, result
}

/*
================
tokenNotices

The token notices among frames: chat notices naming the Devil's Spirit
(the notice text is UTF-16 on the wire).
================
*/
func tokenNotices(frames []wire.Frame) int {
	n := 0
	for _, frame := range frames {
		if frame.Opcode == chat.OpChatBroadcast && strings.Contains(string(frame.Payload), "D\x00e\x00v\x00i\x00l\x00") {
			n++
		}
	}
	return n
}

/*
================
partnerFrames
================
*/
func partnerFrames(result OpResult, id int64) []wire.Frame {
	var frames []wire.Frame
	for _, recipient := range result.Recipients {
		if recipient.CharacterID == id {
			frames = append(frames, recipient.Frames...)
		}
	}
	return frames
}

/*
================
TestNasrunTokensThroughAKill
================
*/
func TestNasrunTokensThroughAKill(t *testing.T) {
	for _, on := range []bool{false, true} {
		actor, partner, result := tokenKill(t, on, nil)
		want := int64(0)
		if on {
			want = nasrun.TokensEasy
		}
		if actor.NasrunTokens != want || partner.NasrunTokens != want {
			t.Fatalf("on=%v: actor %d and partner %d tokens, want %d each", on, actor.NasrunTokens, partner.NasrunTokens, want)
		}
		if got := tokenNotices(result.Frames); got != int(want) {
			t.Fatalf("on=%v: %d actor notices, want %d", on, got, want)
		}
		if got := tokenNotices(partnerFrames(result, partner.ID)); got != int(want) {
			t.Fatalf("on=%v: %d partner notices, want %d", on, got, want)
		}
		if on && actor.NasrunTokenDayCount != nasrun.TokensEasy {
			t.Fatalf("the day's count is not recorded: %d", actor.NasrunTokenDayCount)
		}
	}
}

/*
================
TestNasrunTokensOneAwardPerAccount

The partner dealt 99 of the 100 damage, so on a shared account it is the
top contributor and the only earner.
================
*/
func TestNasrunTokensOneAwardPerAccount(t *testing.T) {
	actor, partner, _ := tokenKill(t, true, func(_ int64, actor, partner *enterworld.Character) {
		actor.AccountID, partner.AccountID = "shared", "shared"
	})
	if actor.NasrunTokens != 0 || partner.NasrunTokens != nasrun.TokensEasy {
		t.Fatalf("actor %d and partner %d tokens, want 0 and %d", actor.NasrunTokens, partner.NasrunTokens, nasrun.TokensEasy)
	}
}

/*
================
TestNasrunTokensSkipTheLevelBand
================
*/
func TestNasrunTokensSkipTheLevelBand(t *testing.T) {
	high := int64(200)
	actor, partner, result := tokenKill(t, true, func(_ int64, _, partner *enterworld.Character) { partner.Level = &high })
	if partner.NasrunTokens != 0 || tokenNotices(partnerFrames(result, partner.ID)) != 0 {
		t.Fatalf("a contributor past the band earned %d", partner.NasrunTokens)
	}
	if actor.NasrunTokens != nasrun.TokensEasy {
		t.Fatalf("the actor in band earned %d", actor.NasrunTokens)
	}
}

/*
================
TestNasrunTokensStopAtTheDailyCap

A partner already at today's cap earns nothing and is told nothing; the
same count on yesterday's day restarts and pays.
================
*/
func TestNasrunTokensStopAtTheDailyCap(t *testing.T) {
	_, partner, result := tokenKill(t, true, func(today int64, _, partner *enterworld.Character) {
		partner.NasrunTokens, partner.NasrunTokenDay, partner.NasrunTokenDayCount = 30, today, nasrun.TokenDailyCap
	})
	if partner.NasrunTokens != 30 || tokenNotices(partnerFrames(result, partner.ID)) != 0 {
		t.Fatalf("a capped partner holds %d and got notices", partner.NasrunTokens)
	}
	_, partner, _ = tokenKill(t, true, func(today int64, _, partner *enterworld.Character) {
		partner.NasrunTokens, partner.NasrunTokenDay, partner.NasrunTokenDayCount = 30, today-1, nasrun.TokenDailyCap
	})
	if partner.NasrunTokens != 30+nasrun.TokensEasy || partner.NasrunTokenDayCount != nasrun.TokensEasy {
		t.Fatalf("yesterday's cap did not restart: balance %d, today %d", partner.NasrunTokens, partner.NasrunTokenDayCount)
	}
}
