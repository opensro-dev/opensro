/*
===========================================================================

monstercapture_test.go - Monster Mask on a corpse

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/testsupport/gamedatatest"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const monsterMaskA1 = 7995 // SKILL_EU_ROG_TRANSFORMA_MASK_A_01: mcap 20 10364, 52 MP

// ItemRefByID lets the capture resolve its mcap item in tests.
func (s staticItemSource) ItemRefByID(id uint32) (*enterworld.ItemRef, bool) {
	for _, ref := range s {
		if ref.RefObjID == id {
			return ref, true
		}
	}
	return nil, false
}

/*
==================
corpseFixture

The combat fixture with its monster rebuilt from mutate, the shipped mask
skill learned and the shipped Essence of the Dead known. dead leaves the
monster a retained corpse.
==================
*/
func corpseFixture(t *testing.T, dead bool, mutate func(*monster.MonsterRef)) (*Runtime, *fakeClock, *enterworld.Character, monster.Instance) {
	t.Helper()
	dir := gamedatatest.TextdataDir(t)
	rt, clock, c, _ := newCombatTestRuntime(t, 100)
	ref, _ := rt.Monsters.Reference(1933)
	mutate(&ref)
	spawn := monster.SpawnPoint{RefObjID: ref.RefObjID, RegionID: 0x62A8, X: 963, Y: 20, Z: 458}
	monsters := simulation.NewMonsterState(monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{ref.RefObjID: ref},
		[]monster.NestRow{{SpawnPoint: spawn, RetailEvidence: true, MaxCount: 1}},
	))
	monsters.SetTimeSource(clock.Now)
	monsters.StartDivision(testDivision)
	monsters.AdvancePopulation(monsters.CurrentTimeMillis())
	rt.Monsters = monsters
	instances := monsters.InstancesInRegions(testDivision, []uint16{0x62A8})
	if len(instances) != 1 {
		t.Fatalf("%d monsters", len(instances))
	}
	if dead {
		if hit, ok := monsters.ApplyDamage(testDivision, instances[0].Gid, instances[0].CurrentHP); !ok || hit.CurrentHP != 0 {
			t.Fatalf("corpse fixture: %+v", hit)
		}
	}
	learnShipped(t, rt, c, monsterMaskA1)
	item, ok := enterworld.NewTextdataItems(dir).ItemRefByCodename("ITEM_ETC_TRANS_MONSTER")
	if !ok {
		t.Fatal("shipped mask item missing")
	}
	rt.deps.ItemReferences().(staticItemSource)[item.Codename] = item
	instance, _ := monsters.Get(testDivision, instances[0].Gid)
	return rt, clock, c, instance
}

func castCapture(rt *Runtime, c *enterworld.Character, gid uint32) OpResult {
	return rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: monsterMaskA1, HasTarget: true, TargetGid: gid}.Encode())
}

/*
==================
TestMonsterMaskDropsTheEssenceAtTheRogue

593F63 -> 4AA680: absorbing a level-20 corpse costs 52 MP, starts the
cooldown and puts one Essence holding that monster on the ground, owned
by the Rogue.
==================
*/
func TestMonsterMaskDropsTheEssenceAtTheRogue(t *testing.T) {
	rt, _, c, corpse := corpseFixture(t, true, func(r *monster.MonsterRef) { r.Level = 20 })
	mp := int64(100)
	c.CurrentMP = &mp
	result := castCapture(rt, c, corpse.Gid)
	cast, ok := findFrame(result.Broadcast, wire.OpSkillCastResult)
	if !ok || result.DiagnosticRefusal != "" {
		t.Fatalf("capture refused: %+v", result)
	}
	if want := wire.SkillCastAtTargetFrame(wire.SkillCastSuccess{SkillId: monsterMaskA1, CasterGid: enterworld.ObjectIDForCharacter(c), InstanceToken: 1, OwnerOrTargetGid: corpse.Gid}); len(cast.Payload) != len(want.Payload) {
		t.Fatalf("cast %x", cast.Payload)
	}
	drops := rt.CharacterGroundItems(testDivision, c)
	if len(drops) != 1 || drops[0].Codename != "ITEM_ETC_TRANS_MONSTER" || drops[0].TransformRefObjID != corpse.Ref.RefObjID ||
		drops[0].OwnerJID != enterworld.ObjectIDForCharacter(c) {
		t.Fatalf("essence %+v", drops)
	}
	if *c.CurrentMP != 48 {
		t.Fatalf("MP %d, want 100-52", *c.CurrentMP)
	}
	if again := castCapture(rt, c, corpse.Gid); len(rt.CharacterGroundItems(testDivision, c)) != 1 {
		t.Fatalf("second capture inside the cooldown: %+v", again)
	}
}

/*
==================
TestMonsterMaskRefusesTheWrongCorpse

58D2F4: a living target, a hunter (TID4 3) and a
monster above the cap are all refused, and nothing drops.
==================
*/
func TestMonsterMaskRefusesTheWrongCorpse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		dead   bool
		mutate func(*monster.MonsterRef)
		code   uint16
	}{
		{"alive", false, func(r *monster.MonsterRef) { r.Level = 5 }, 0x3006},
		{"hunter", true, func(r *monster.MonsterRef) { r.Level = 5; r.TypeID4 = 3 }, 0x3006},
		{"above the cap", true, func(r *monster.MonsterRef) { r.Level = 21 }, 0x3035},
	} {
		rt, _, c, corpse := corpseFixture(t, tc.dead, tc.mutate)
		result := castCapture(rt, c, corpse.Gid)
		want := offensiveRefusal(tc.code).Frames[0]
		got, ok := findFrame(result.Frames, wire.OpSkillCastResult)
		if !ok || string(got.Payload) != string(want.Payload) {
			t.Errorf("%s: frames %+v, want %#x", tc.name, result.Frames, tc.code)
		}
		if drops := rt.CharacterGroundItems(testDivision, c); len(drops) != 0 {
			t.Errorf("%s: dropped %+v", tc.name, drops)
		}
	}
}

// 58D2F4's grade test: a champion corpse (grade 1) answers 0x3033, and the
// grade bits are the low nibble only.
func TestMonsterMaskRefusesAnyGradeButNormal(t *testing.T) {
	capture := enterworld.SkillMonsterCapture{Pinned: true, MaxLevel: 20, ItemRefObjID: 10364}
	corpse := monster.Instance{Ref: monster.MonsterRef{Level: 5, TypeID4: 1}}
	corpse.Nest.HasRarityOverride = true
	for grade, want := range map[uint8]uint16{0: 0, 1: 0x3033, 3: 0x3033, 0x10: 0} {
		corpse.Nest.RarityOverride = grade
		if got := captureTargetRefusal(corpse, capture); got != want {
			t.Errorf("grade %#x: %#x, want %#x", grade, got, want)
		}
	}
}
