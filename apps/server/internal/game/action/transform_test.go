/*
===========================================================================

transform_test.go - the monster mask

===========================================================================
*/

package action

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	maskMonsterRef   = 2000 // the monster the mask holds
	maskMonsterSkill = 9001 // its first default skill
	maskSlot         = 13
	maskWalk         = 16
	maskRun          = 70
)

// codenameSkills adds the codename lookup item use needs.
type codenameSkills struct{ staticSkillSource }

func (s codenameSkills) SkillByCodename(code string) (enterworld.SkillRow, bool) {
	for _, row := range s.staticSkillSource {
		if row.Codename == code {
			return row, true
		}
	}
	return enterworld.SkillRow{}, false
}

/*
==================
maskFixture

The combat fixture with a second, unspawned monster reference the mask
names, the shipped transform skill and mask item, and a filled mask in
bag slot 13 holding holding.
==================
*/
func maskFixture(t *testing.T, maskLevel uint8, holding uint32) (*Runtime, *fakeClock, *enterworld.Character, monster.Instance) {
	t.Helper()
	dir := gamedatatest.TextdataDir(t)
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	c.BattleUntilMs = 0

	fixture, _ := rt.Monsters.Reference(1933)
	mask := monster.MonsterRef{
		RefObjID: maskMonsterRef, Codename: "MOB_TEST_MASK", Level: maskLevel,
		MaxHP: 100, BodyRadius: 6, WalkSpeed: maskWalk, RunSpeed: maskRun,
		DefaultSkillIDs: [10]uint32{maskMonsterSkill},
	}
	spawn := monster.SpawnPoint{RefObjID: fixture.RefObjID, RegionID: 0x62A8, X: 963, Y: 20, Z: 458}
	monsters := simulation.NewMonsterState(monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{fixture.RefObjID: fixture, mask.RefObjID: mask},
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

	skills := rt.deps.SkillData().(staticSkillSource)
	transform, ok := shippedSkills(t).SkillByCodename("SKILL_ETC_TRANS_MONSTER_01")
	if !ok || !transformRow(transform) {
		t.Fatalf("shipped transform row %+v", transform.CastGate)
	}
	skills[transform.ID] = transform
	strike := skills[2]
	strike.ID, strike.Codename = maskMonsterSkill, "MSKILL_TEST_MASK"
	strike.RequiredWeaponKinds = [2]uint8{0xff, 0xff} // as every TID4 1 monster basic skill is
	skills[maskMonsterSkill] = strike
	rt.deps.(*enterworld.Deps).Skills = codenameSkills{skills}

	item, ok := enterworld.NewTextdataItems(dir).ItemRefByCodename("ITEM_ETC_TRANS_MONSTER")
	if !ok || !wire.IsMonsterCapsule(item.TypeFlags()) {
		t.Fatal("shipped mask item missing or not a capsule")
	}
	rt.deps.ItemReferences().(staticItemSource)[item.Codename] = item
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: maskSlot, RefObjID: item.RefObjID, Codename: item.Codename, TypeFlags: item.TypeFlags(),
		StackCount: 1, TransformRefObjID: holding,
	})
	return rt, clock, c, instances[0]
}

func useMask(rt *Runtime, c *enterworld.Character) OpResult {
	for _, row := range c.MissionInventory {
		if row.Slot == maskSlot {
			return rt.HandleItemUse(testDivision, c, []byte{maskSlot, byte(row.TypeFlags), byte(row.TypeFlags >> 8)})
		}
	}
	return rt.HandleItemUse(testDivision, c, []byte{maskSlot, 0, 0})
}

func hasMask(c *enterworld.Character) bool {
	for _, row := range c.MissionInventory {
		if row.Slot == maskSlot {
			return true
		}
	}
	return false
}

func findFrame(frames []wire.Frame, opcode uint16) (wire.Frame, bool) {
	for _, f := range frames {
		if f.Opcode == opcode {
			return f, true
		}
	}
	return wire.Frame{}, false
}

/*
==================
TestMonsterMaskTransformsUntilItsInstanceEnds

493C30 -> 4F00F0: the mask is spent, 0x323A names the monster to every
observer, walk and run become the monster's; ending the instance
(4F0210) restores the player's speeds and clears the skin, and so does
death.
==================
*/
func TestMonsterMaskTransformsUntilItsInstanceEnds(t *testing.T) {
	for _, end := range []string{"retire", "death"} {
		rt, clock, c, _ := maskFixture(t, 1, maskMonsterRef)
		result := useMask(rt, c)
		if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 {
			t.Fatalf("mask use refused: %+v", result)
		}
		if hasMask(c) || c.TransformRefObjID != maskMonsterRef {
			t.Fatalf("mask kept %v, transform %d", hasMask(c), c.TransformRefObjID)
		}
		skin, ok := findFrame(result.Broadcast, wire.OpSkinChange)
		want := wire.SkinChange{GID: enterworld.ObjectIDForCharacter(c), Skin: wire.TransformSkin{RefObjID: maskMonsterRef}}.Encode()
		if !ok || !bytes.Equal(skin.Payload, want) {
			t.Fatalf("observers did not get the skin: %+v", result.Broadcast)
		}
		if walk, run := worldSpeeds(rt, c); walk != maskWalk || run != maskRun {
			t.Fatalf("transformed speeds %v/%v", walk, run)
		}
		effects := rt.effects.Snapshot(testDivision, c.Name)
		if len(effects) != 1 || effects[0].ExpiresAtMs != clock.NowMs()+900000 {
			t.Fatalf("transform effect %+v", effects)
		}

		rt.deps.Update(c, "test-end", func() bool {
			if end == "death" {
				rt.retireBodyEffectsOnDeath(testDivision, c)
			} else {
				ended := rt.effects.RetireInstances(testDivision, c.Name, []uint32{effects[0].InstanceToken})
				rt.publishEndedEffects(testDivision, c, ended, clock.NowMs())
			}
			return true
		})
		if c.TransformRefObjID != 0 || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
			t.Fatalf("%s: transform %d survived", end, c.TransformRefObjID)
		}
		if walk, run := worldSpeeds(rt, c); walk != float32(simulation.WalkSpeed) || run != float32(simulation.RunSpeed) {
			t.Fatalf("%s: speeds %v/%v after the transform", end, walk, run)
		}
	}
}

/*
==================
TestMonsterMaskRefusals

493C30 answers 2 before casting; 58DE46 refusals reach the player as
their 0xB070 code followed by item-use result 5. Nothing is spent.
==================
*/
func TestMonsterMaskRefusals(t *testing.T) {
	for _, tc := range []struct {
		name    string
		level   uint8
		holding uint32
		setup   func(*Runtime, *enterworld.Character)
		skill   uint16 // 0: no skill error
		item    uint8
	}{
		{name: "empty mask", level: 1, holding: 0, item: 2},
		{name: "body mode 4", level: 1, holding: maskMonsterRef, setup: func(_ *Runtime, c *enterworld.Character) { c.NativeBodyStatus = 4 }, item: 2},
		{name: "unknown monster", level: 1, holding: 4242, skill: 0x3006, item: 5},
		{name: "monster above the caster", level: 2, holding: maskMonsterRef, skill: 0x3008, item: 5},
		{name: "berserk", level: 1, holding: maskMonsterRef, setup: func(_ *Runtime, c *enterworld.Character) { c.NativeBodyStatus = 1 }, skill: 0x3031, item: 5},
		{name: "riding", level: 1, holding: maskMonsterRef, setup: func(_ *Runtime, c *enterworld.Character) {
			c.ActiveCOS = &enterworld.CharacterCOS{Mounted: true}
		}, skill: 0x3009, item: 5},
		{name: "seated", level: 1, holding: maskMonsterRef, setup: func(rt *Runtime, c *enterworld.Character) {
			rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
				func(w *simulation.WorldState) { w.Sitting = true })
		}, skill: 0x3009, item: 5},
	} {
		rt, _, c, _ := maskFixture(t, tc.level, tc.holding)
		if tc.setup != nil {
			tc.setup(rt, c)
		}
		result := useMask(rt, c)
		var frames []wire.Frame
		if tc.skill != 0 {
			frames = append(frames, offensiveRefusal(tc.skill).Frames...)
		}
		frames = append(frames, itemUseFailure(tc.item).Frames...)
		if len(result.Frames) != len(frames) {
			t.Errorf("%s: frames %+v, want %+v", tc.name, result.Frames, frames)
			continue
		}
		for i := range frames {
			if result.Frames[i].Opcode != frames[i].Opcode || !bytes.Equal(result.Frames[i].Payload, frames[i].Payload) {
				t.Errorf("%s: frame %d %+v, want %+v", tc.name, i, result.Frames[i], frames[i])
			}
		}
		if !hasMask(c) || c.TransformRefObjID != 0 {
			t.Errorf("%s: mask spent or transform set", tc.name)
		}
	}
}

/*
==================
TestTransformedPlayerStrikesWithTheMonsterSkill

59E650: a transformed player's basic attack is the monster's first
default skill, even though it names a weapon the player does not hold;
untransformed, the same engage uses the sword's base attack.
==================
*/
func TestTransformedPlayerStrikesWithTheMonsterSkill(t *testing.T) {
	for _, transformed := range []bool{false, true} {
		rt, _, c, target := maskFixture(t, 1, maskMonsterRef)
		if transformed {
			if r := useMask(rt, c); r.Frames[0].Payload[0] != 1 {
				t.Fatalf("mask use refused: %+v", r)
			}
		}
		r := rt.HandleTargetInteract(testDivision, c, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
		want := uint32(2)
		if transformed {
			want = maskMonsterSkill
		}
		cast, ok := findFrame(r.Frames, wire.OpSkillCastResult)
		id := make([]byte, 4)
		binary.LittleEndian.PutUint32(id, want)
		if !ok || !bytes.Contains(cast.Payload, id) {
			t.Fatalf("transformed %v: cast %+v does not use skill %d (%s)", transformed, r.Frames, want, r.DiagnosticRefusal)
		}
	}
}

// The transform is runtime state; the character record never stores it.
func TestTransformIsNotPersisted(t *testing.T) {
	c := domain.Character{TransformRefObjID: maskMonsterRef}
	if data, err := json.Marshal(c); err != nil || bytes.Contains(data, []byte("ransform")) {
		t.Fatalf("transform serialized: %s %v", data, err)
	}
}

/*
==================
TestAMaskedPlayerHasOnlyItsBasicAttack

4AE601 / 515B8E: while transformed a skill press answers 0x3030 and
berserk refuses; the same presses work once the mask ends.
==================
*/
func TestAMaskedPlayerHasOnlyItsBasicAttack(t *testing.T) {
	rt, clock, c, _ := maskFixture(t, 1, maskMonsterRef)
	learnShipped(t, rt, c, rogueStealthID)
	c.BattleUntilMs = 0
	if r := useMask(rt, c); r.Frames[0].Payload[0] != 1 {
		t.Fatalf("mask use refused: %+v", r)
	}
	press := castSelf(rt, c, rogueStealthID)
	if want := offensiveRefusal(0x3030).Frames[0]; len(press.Frames) != 1 || !bytes.Equal(press.Frames[0].Payload, want.Payload) {
		t.Fatalf("skill press while masked: %+v", press.Frames)
	}
	c.BerserkPoints = 5
	if r := rt.HandleBerserk(testDivision, c, []byte{1}); c.NativeBodyStatus == 1 || len(r.Frames) != 1 || r.Frames[0].Payload[1] != 5 {
		t.Fatalf("berserk while masked: body %d frames %+v", c.NativeBodyStatus, r.Frames)
	}

	rt.deps.Update(c, "test-end", func() bool { return rt.endTransform(testDivision, c, clock.NowMs()) })
	if c.TransformMode != 0 {
		t.Fatal("endTransform left the mode")
	}
	if r := castSelf(rt, c, rogueStealthID); c.NativeBodyStatus != 6 {
		t.Fatalf("stealth after the mask ended: %+v", r)
	}
}

/*
==================
TestMountingEndsTheMask

CGObjPC_MountCOSAndBroadcast (4EC68F) retires an msch 1 instance.
==================
*/
func TestMountingEndsTheMask(t *testing.T) {
	c := testCharacter()
	rt, clock := newTestRuntime(c, testCosSource(testItems()))
	row := enterworld.SkillRow{ID: 7126, Group: 405, EffectDurationMs: 900000, EffectDurationPresent: true,
		CastGate: enterworld.SkillCastGate{MschPresent: true, MschMode: 1}}
	if _, ok := rt.commitCharacterEffect(testDivision, c, row, 77, statuseffect.StateActive, false, EffectPresentation{Phase: 2, TransformRefObjID: maskMonsterRef}, clock.NowMs()); !ok || c.TransformMode != 1 {
		t.Fatal("transform did not install")
	}
	c.ActiveCOS = &enterworld.CharacterCOS{GID: 0x00C00003, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 87829, Summoned: true}
	rt.HandleCosCommand(testDivision, c, wire.NewWriter(5).U32(c.ActiveCOS.GID).U8(wire.CosCommandMountTag).Payload())
	if !c.ActiveCOS.Mounted || c.TransformMode != 0 || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
		t.Fatalf("mounted %v, transform %d, effects %+v", c.ActiveCOS.Mounted, c.TransformMode, rt.effects.Snapshot(testDivision, c.Name))
	}
}

/*
==================
TestALoadingAreaEndsTheMask

Both transforms' descriptions end them on entering a loading area; the
GM warp's re-entry is one such load.
==================
*/
func TestALoadingAreaEndsTheMask(t *testing.T) {
	rt, _, c, _ := maskFixture(t, 1, maskMonsterRef)
	if r := useMask(rt, c); r.Frames[0].Payload[0] != 1 {
		t.Fatalf("mask use refused: %+v", r)
	}
	c.GMPrivilege = true
	rt.PushCharacterFrames = func(string, string, []wire.Frame) {}
	rt.PushDivisionPeerFrames = func(string, string, []wire.Frame) {}
	deps := rt.deps.(*enterworld.Deps)
	deps.CanEnterWorldRegion = func(*enterworld.Character, uint16) bool { return true }
	deps.SpawnTerrainHeight = func(uint16, float64, float64) (float64, bool) { return 20, true }
	rt.WarpGM(testDivision, c.Name, wire.Position{RegionID: 0x62A8, X: 900, Y: 20, Z: 400})
	if c.TransformMode != 0 || c.TransformRefObjID != 0 || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
		t.Fatalf("transform survived the load: mode %d effects %+v", c.TransformMode, rt.effects.Snapshot(testDivision, c.Name))
	}
}
