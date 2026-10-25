/*
===========================================================================

uniquedamagerank_test.go - the damage ranking shown when a unique dies

Port-only, not native: off, no unique kill sends 0x3FBC; on, every ranked
contributor receives the top rows and its own place, and nothing else does.

===========================================================================
*/
package action

import (
	"fmt"
	"math"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
rankActor

A roster entry for a named character, in the given party (nil for none).
================
*/
func rankActor(id int64, party *RewardParty) (uint32, rewardActor) {
	c := &enterworld.Character{ID: id, Name: fmt.Sprintf("player%d", id)}
	return enterworld.ObjectIDForCharacter(c), rewardActor{character: c, party: party}
}

/*
================
TestUniqueDamageRankingOrdersPlayersAndParties
================
*/
func TestUniqueDamageRankingOrdersPlayersAndParties(t *testing.T) {
	first, second := &RewardParty{Order: 7}, &RewardParty{Order: 3}
	actors := map[uint32]rewardActor{}
	gid := map[int64]uint32{}
	for id, party := range map[int64]*RewardParty{1: first, 2: first, 3: second, 4: nil, 5: nil} {
		g, a := rankActor(id, party)
		actors[g], gid[id] = a, g
	}
	const absentGID = 999_999
	rows := []simulation.MonsterContribution{
		{CreditGID: gid[1], Damage: 300},
		{CreditGID: gid[2], Damage: 300},
		{CreditGID: gid[3], Damage: 700},
		{CreditGID: gid[4], Damage: 50},
		{CreditGID: gid[5], Damage: 50},
		{CreditGID: absentGID, Damage: 1000},
	}
	places, total := uniqueDamageRanking(rows, actors)
	if total != 2400 {
		t.Fatalf("total %d, want 2400 (the absent contributor counts)", total)
	}
	// Party 3's one member (700) outranks party 7's two (600 together), and
	// equal damage keeps the lower GID first.
	want := []struct {
		id        int64
		damage    uint32
		partyRank uint8
	}{{3, 700, 1}, {1, 300, 2}, {2, 300, 2}, {4, 50, 0}, {5, 50, 0}}
	if len(places) != len(want) {
		t.Fatalf("%d places, want %d (the absent contributor has no row)", len(places), len(want))
	}
	for i, w := range want {
		p := places[i]
		if p.character.ID != w.id || p.damage != w.damage || p.partyRank != w.partyRank {
			t.Fatalf("place %d: id %d damage %d party %d, want %+v", i+1, p.character.ID, p.damage, p.partyRank, w)
		}
	}

	// Two capped dword sums add without wrapping.
	_, big := uniqueDamageRanking([]simulation.MonsterContribution{
		{CreditGID: gid[1], Damage: math.MaxUint32},
		{CreditGID: gid[2], Damage: math.MaxUint32},
	}, actors)
	if big != 2*math.MaxUint32 {
		t.Fatalf("total %d wrapped", big)
	}
}

/*
================
uniqueKillImpact

A fatal impact on a monster of the given rarity grade with one row per
roster player, the player with the highest id dealing the most.
================
*/
func uniqueKillImpact(grade uint8, players int) (simulation.MonsterDamageResult, rewardRoster, []*enterworld.Character) {
	roster := rewardRoster{actors: map[uint32]rewardActor{}}
	impact := simulation.MonsterDamageResult{Fatal: true, Instance: monster.Instance{Ref: monster.MonsterRef{RefObjID: 1954, MonsterType: grade}}}
	var characters []*enterworld.Character
	for id := int64(1); id <= int64(players); id++ {
		g, a := rankActor(id, nil)
		roster.actors[g] = a
		characters = append(characters, a.character)
		impact.Contributions = append(impact.Contributions, simulation.MonsterContribution{CreditGID: g, Damage: uint32(id) * 10})
	}
	return impact, roster, characters
}

/*
================
TestUniqueDamageRankFramesFollowTheFlagAndGrade
================
*/
func TestUniqueDamageRankFramesFollowTheFlagAndGrade(t *testing.T) {
	rt := &Runtime{}
	impact, roster, characters := uniqueKillImpact(uniqueRarityGrade, 14)
	actor := characters[0]
	if frames, others := rt.uniqueDamageRankFrames(actor, roster, impact); frames != nil || others != nil {
		t.Fatal("the flag off sent a ranking")
	}
	rt.UniqueDamageRank = true
	for _, grade := range []uint8{0, 1, 8} {
		other, otherRoster, _ := uniqueKillImpact(grade, 3)
		if frames, others := rt.uniqueDamageRankFrames(actor, otherRoster, other); frames != nil || others != nil {
			t.Fatalf("grade %d sent a ranking", grade)
		}
	}
	nonFatal := impact
	nonFatal.Fatal = false
	if frames, others := rt.uniqueDamageRankFrames(actor, roster, nonFatal); frames != nil || others != nil {
		t.Fatal("a nonfatal hit sent a ranking")
	}

	frames, others := rt.uniqueDamageRankFrames(actor, roster, impact)
	if len(frames) != 1 || len(others) != len(characters)-1 {
		t.Fatalf("%d actor frames and %d recipients, want 1 and %d", len(frames), len(others), len(characters)-1)
	}
	var top []wire.UniqueDamageRow
	for id := int64(14); id > 2; id-- {
		top = append(top, wire.UniqueDamageRow{Name: fmt.Sprintf("player%d", id), Damage: uint32(id) * 10})
	}
	const total = 10 * 14 * 15 / 2
	// The actor dealt the least: fourteenth, below the twelve rows.
	want := wire.EncodeUniqueDamageRank(wire.UniqueDamageRank{RefObjID: 1954, Total: total, Rows: top, SelfRank: 14, SelfDamage: 10})
	if frames[0].Opcode != wire.OpUniqueDamageRank || string(frames[0].Payload) != string(want) {
		t.Fatalf("actor frame %#x % x, want % x", frames[0].Opcode, frames[0].Payload, want)
	}
	for _, recipient := range others {
		id := recipient.CharacterID
		want := wire.EncodeUniqueDamageRank(wire.UniqueDamageRank{RefObjID: 1954, Total: total, Rows: top, SelfRank: uint16(15 - id), SelfDamage: uint32(id) * 10})
		if len(recipient.Frames) != 1 || string(recipient.Frames[0].Payload) != string(want) {
			t.Fatalf("recipient %d frames %+v, want its own place", id, recipient.Frames)
		}
	}
}

/*
================
TestUniqueDamageRankThroughAKill

Through the real kill settlement (HandleTargetInteract): a partner's
earlier damage and the actor's fatal blow each earn one ranking, the
actor's on its reply and the partner's addressed to it; off, neither.
================
*/
func TestUniqueDamageRankThroughAKill(t *testing.T) {
	for _, on := range []bool{false, true} {
		rt, _, actor, target := newCombatTestRuntime(t, 100)
		rt.UniqueDamageRank = on
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
		source[testDivision] = append(source[testDivision], &partner)
		hits := rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, 100, []simulation.MonsterDamagePlan{
			{GID: target.Gid, Damage: 99, CreditGID: enterworld.ObjectIDForCharacter(&partner)},
		})
		if len(hits) != 1 || hits[0].Fatal {
			t.Fatal("nonfatal contribution setup failed")
		}
		rt.DropRoll = constantDropRoll(0)
		result := rt.HandleTargetInteract(testDivision, actor, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
		actorRanks := 0
		for _, frame := range result.Frames {
			if frame.Opcode == wire.OpUniqueDamageRank {
				actorRanks++
			}
		}
		partnerRanks := 0
		for _, recipient := range result.Recipients {
			for _, frame := range recipient.Frames {
				if frame.Opcode != wire.OpUniqueDamageRank {
					continue
				}
				if recipient.CharacterID != partner.ID {
					t.Fatalf("on=%v: a ranking addressed to %d", on, recipient.CharacterID)
				}
				partnerRanks++
			}
		}
		want := 0
		if on {
			want = 1
		}
		if actorRanks != want || partnerRanks != want {
			t.Fatalf("on=%v: actor %d and partner %d rankings, want %d each", on, actorRanks, partnerRanks, want)
		}
	}
}
