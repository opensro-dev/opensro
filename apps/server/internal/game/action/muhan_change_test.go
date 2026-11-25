/*
===========================================================================

muhan_change_test.go - the Muhan change NPCs (port-only, #3349)

A change NPC is an arena world NPC (worldnpc.go). Talk at a paired one
casts its BUFF01 (msch word 7) with the form as the context RefObj, which
fills the transform block as a mask does; without the pairing it offers
nothing and nothing changes. The install refuses a pairing that does not
resolve, and a world NPC stands only in its own world definition.

===========================================================================
*/

package action

import (
	"bytes"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	changeNpcRef   = 3000
	changeNpcGID   = 241000
	changeFormRef  = 3001
	changeSkillID  = 34606
	changeNpcCode  = "NPC_MUHAN_CHANGE_SOLO_75_A"
	changeFormCode = "MOB_MUHAN_CHANGE_SOLO_75_A"
	changeSkill    = "SKILL_MUHAN_A_75_BUFF01"
	changeFormWalk = 20
	changeFormRun  = 80
)

/*
==================
changeFixture

The combat fixture with one change NPC standing beside the player as a
world NPC of the player's own world, its form in the monster template
and its BUFF01: the shipped mask skill under the Muhan codename with
msch word 7, as the v1.696 rows carry it. paired installs the pairing;
formLevel sets the form's level against the caster's 1.
==================
*/
func changeFixture(t *testing.T, paired bool, formLevel uint8) (*Runtime, *enterworld.Character, simulation.NpcDef) {
	t.Helper()
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	c.BattleUntilMs = 0

	form := monster.MonsterRef{
		RefObjID: changeFormRef, Codename: changeFormCode, Level: formLevel,
		MaxHP: 100, BodyRadius: 6, WalkSpeed: changeFormWalk, RunSpeed: changeFormRun,
	}
	monsters := simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{form.RefObjID: form}, nil))
	monsters.SetTimeSource(clock.Now)
	monsters.StartDivision(testDivision)
	rt.Monsters = monsters

	npc := simulation.NpcDef{
		ObjectID: changeNpcGID, RefObjID: changeNpcRef, TidWord: 0x0146, Codename: changeNpcCode,
		AuthoredSpawn: true, Spawn: simulation.Spawn{RegionID: 0x62A8, X: 963, Y: 20, Z: 458},
	}
	rt.NpcSpawn.Enabled = true
	rt.worldNpcs = []simulation.WorldNpc{{World: instance.ID(domain.CharacterWorldInstance(c)).Definition(), Npc: npc}}

	skills := rt.deps.SkillData().(staticSkillSource)
	mask, ok := shippedSkills(t).SkillByCodename("SKILL_ETC_TRANS_MONSTER_01")
	if !ok {
		t.Fatal("shipped mask skill missing")
	}
	buff := mask
	buff.ID, buff.Codename = changeSkillID, changeSkill
	buff.CastGate.MschMode = mschMuhanChange
	skills[buff.ID] = buff
	rt.deps.(*enterworld.Deps).Skills = codenameSkills{skills}

	if paired {
		if err := rt.ConfigureMuhanTransforms([]monster.MuhanTransform{{
			NpcCodename: changeNpcCode, SkillCodename: changeSkill, MonsterCodename: changeFormCode,
		}}); err != nil {
			t.Fatal(err)
		}
	}
	return rt, c, npc
}

/*
==================
selectChangeNpc

Selects the change NPC through the select handler and returns its answer.
==================
*/
func selectChangeNpc(t *testing.T, rt *Runtime, c *enterworld.Character) []wire.Frame {
	t.Helper()
	return rt.HandleObjectSelect(testDivision, c, wire.NewWriter(4).U32(changeNpcGID).Payload()).Frames
}

/*
==================
TestMuhanChangeTalkBecomesTheForm

Selecting a paired change NPC offers Talk; Talk fills the transform
block with the form, broadcasts its skin, and gives the form's speeds,
for the BUFF01's 900 s; retiring the instance ends it as a mask's does.
==================
*/
func TestMuhanChangeTalkBecomesTheForm(t *testing.T) {
	rt, c, npc := changeFixture(t, true, 1)
	var pushed []wire.Frame
	rt.PushDivisionPeerFrames = func(_ string, _ string, frames []wire.Frame) { pushed = append(pushed, frames...) }

	selected := selectChangeNpc(t, rt, c)
	want := wire.EncodeNpcObjectSelectResult(npc.ObjectID, simulation.NpcTalkFlagTalk, 0)
	if len(selected) != 1 || !bytes.Equal(selected[0].Payload, want) {
		t.Fatalf("select %+v, want the NPC result offering Talk", selected)
	}

	frames, refusal := rt.HandleNpcAction(testDivision, c, wire.NewWriter(8).U32(npc.ObjectID).U32(simulation.NpcTalkFlagTalk).Payload())
	if refusal != "" {
		t.Fatalf("talk refused: %s", refusal)
	}
	if c.TransformRefObjID != changeFormRef || c.TransformMode != 1 {
		t.Fatalf("transform %d mode %d", c.TransformRefObjID, c.TransformMode)
	}
	skin := wire.SkinChange{GID: enterworld.ObjectIDForCharacter(c), Skin: wire.TransformSkin{RefObjID: changeFormRef}}.Encode()
	for name, list := range map[string][]wire.Frame{"caster": frames, "observers": pushed} {
		if frame, ok := findFrame(list, wire.OpSkinChange); !ok || !bytes.Equal(frame.Payload, skin) {
			t.Fatalf("%s did not get the form's skin: %+v", name, list)
		}
	}
	if walk, run := worldSpeeds(rt, c); walk != changeFormWalk || run != changeFormRun {
		t.Fatalf("changed speeds %v/%v", walk, run)
	}
	effects := rt.effects.Snapshot(testDivision, c.Name)
	if len(effects) != 1 || effects[0].ExpiresAtMs != rt.Now().UnixMilli()+900000 {
		t.Fatalf("change effect %+v", effects)
	}

	rt.deps.Update(c, "test-end", func() bool {
		ended := rt.effects.RetireInstances(testDivision, c.Name, []uint32{effects[0].InstanceToken})
		rt.publishEndedEffects(testDivision, c, ended, rt.Now().UnixMilli())
		return true
	})
	if c.TransformRefObjID != 0 {
		t.Fatalf("form %d survived its instance", c.TransformRefObjID)
	}
}

/*
==================
TestMuhanChangeRefusals

The change keeps the mask's guards (a form above the caster 0x3008,
riding 0x3009, berserk 0x3031) and answers each with the skill error
and the function refusal; it offers nothing but Talk.
==================
*/
func TestMuhanChangeRefusals(t *testing.T) {
	for _, tc := range []struct {
		name  string
		level uint8
		mask  uint32
		setup func(*enterworld.Character)
		code  uint16
	}{
		{name: "form above the caster", level: 2, mask: simulation.NpcTalkFlagTalk, code: 0x3008},
		{name: "riding", level: 1, mask: simulation.NpcTalkFlagTalk, setup: func(c *enterworld.Character) {
			c.ActiveCOS = &enterworld.CharacterCOS{Mounted: true}
		}, code: 0x3009},
		{name: "berserk", level: 1, mask: simulation.NpcTalkFlagTalk, setup: func(c *enterworld.Character) { c.NativeBodyStatus = 1 }, code: 0x3031},
		{name: "shop", level: 1, mask: simulation.NpcTalkFlagShop},
	} {
		rt, c, npc := changeFixture(t, true, tc.level)
		selectChangeNpc(t, rt, c)
		if tc.setup != nil {
			tc.setup(c)
		}
		frames, refusal := rt.HandleNpcAction(testDivision, c, wire.NewWriter(8).U32(npc.ObjectID).U32(tc.mask).Payload())
		if refusal == "" || c.TransformRefObjID != 0 {
			t.Errorf("%s: admitted, transform %d", tc.name, c.TransformRefObjID)
			continue
		}
		want := npcFunctionRefusal(npcFunctionNotOffered)
		if tc.code != 0 {
			want = append(offensiveRefusal(tc.code).Frames, want...)
		}
		if len(frames) != len(want) {
			t.Errorf("%s: frames %+v, want %+v", tc.name, frames, want)
			continue
		}
		for i := range want {
			if frames[i].Opcode != want[i].Opcode || !bytes.Equal(frames[i].Payload, want[i].Payload) {
				t.Errorf("%s: frame %d %+v, want %+v", tc.name, i, frames[i], want[i])
			}
		}
	}
}

/*
==================
TestMuhanChangeOffOffersNothing

Without the pairing (SRO_MUHAN off) a change NPC offers no function and
a Talk at it changes nothing.
==================
*/
func TestMuhanChangeOffOffersNothing(t *testing.T) {
	rt, c, npc := changeFixture(t, false, 1)
	selected := selectChangeNpc(t, rt, c)
	if want := wire.EncodeNpcObjectSelectResult(npc.ObjectID, 0, 0); len(selected) != 1 || !bytes.Equal(selected[0].Payload, want) {
		t.Fatalf("unpaired select answered %+v, want the NPC result offering nothing", selected)
	}
	if _, refusal := rt.HandleNpcAction(testDivision, c, wire.NewWriter(8).U32(npc.ObjectID).U32(simulation.NpcTalkFlagTalk).Payload()); refusal == "" {
		t.Fatal("unpaired talk was admitted")
	}
	if c.TransformRefObjID != 0 {
		t.Fatalf("transform %d without the pairing", c.TransformRefObjID)
	}
}

/*
==================
TestWorldNpcStandsInItsOwnWorldOnly

A world NPC of another definition is neither published to this world
nor selectable from it; its own world publishes it.
==================
*/
func TestWorldNpcStandsInItsOwnWorldOnly(t *testing.T) {
	rt, c, npc := changeFixture(t, true, 1)
	own := domain.CharacterWorldInstance(c)
	if npcs := rt.WorldNpcs("", own); len(npcs) != 1 || npcs[0].ObjectID != npc.ObjectID {
		t.Fatalf("own world publishes %+v, want the change NPC", npcs)
	}
	rt.worldNpcs[0].World++
	if npcs := rt.WorldNpcs("", own); len(npcs) != 0 {
		t.Fatalf("another world's NPC published here: %+v", npcs)
	}
	if selected := selectChangeNpc(t, rt, c); len(selected) == 1 &&
		bytes.Equal(selected[0].Payload, wire.EncodeNpcObjectSelectResult(npc.ObjectID, simulation.NpcTalkFlagTalk, 0)) {
		t.Fatal("another world's NPC was selectable here")
	}
}

/*
==================
TestMuhanChangeInstallRefusesUnresolvedRows

A pairing whose NPC, form or skill is missing, or whose skill is not an
msch word 7 change, refuses the boot.
==================
*/
func TestMuhanChangeInstallRefusesUnresolvedRows(t *testing.T) {
	good := monster.MuhanTransform{NpcCodename: changeNpcCode, SkillCodename: changeSkill, MonsterCodename: changeFormCode}
	for _, tc := range []struct {
		name string
		row  monster.MuhanTransform
		want string
	}{
		{name: "npc", row: monster.MuhanTransform{NpcCodename: "NPC_MUHAN_CHANGE_X", SkillCodename: changeSkill, MonsterCodename: changeFormCode}, want: "npc"},
		{name: "form", row: monster.MuhanTransform{NpcCodename: changeNpcCode, SkillCodename: changeSkill, MonsterCodename: "MOB_MUHAN_CHANGE_X"}, want: "form"},
		{name: "skill", row: monster.MuhanTransform{NpcCodename: changeNpcCode, SkillCodename: "SKILL_MUHAN_X", MonsterCodename: changeFormCode}, want: "skill"},
		{name: "mask word", row: monster.MuhanTransform{NpcCodename: changeNpcCode, SkillCodename: "SKILL_ETC_TRANS_MONSTER_01", MonsterCodename: changeFormCode}, want: "msch"},
	} {
		rt, _, _ := changeFixture(t, false, 1)
		skills := rt.deps.SkillData().(codenameSkills)
		mask, _ := shippedSkills(t).SkillByCodename("SKILL_ETC_TRANS_MONSTER_01")
		skills.staticSkillSource[mask.ID] = mask
		err := rt.ConfigureMuhanTransforms([]monster.MuhanTransform{good, tc.row})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err %v, want one naming %q", tc.name, err, tc.want)
		}
	}
}
