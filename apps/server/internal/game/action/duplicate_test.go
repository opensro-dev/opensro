/*
===========================================================================

duplicate_test.go - the Rogue's Duplicate

===========================================================================
*/

package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const duplicateA1 = 8006 // SKILL_EU_ROG_TRANSFORMA_DUPLE_A_01: msch 2 60, dura 900000, skc 0 2 0

// duplicateFixture is a Rogue with Duplicate and Stealth learned and an
// allied player at arm's length wearing the fixture's sword.
func duplicateFixture(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character, *enterworld.Character) {
	t.Helper()
	rt, clock, c := concealmentFixture(t, duplicateA1, rogueStealthID)
	ally := nearbyCharacter(rt, c, 21, "ally", 1)
	shape := int64(3)
	ally.BodyShapeByte = &shape
	return rt, clock, c, ally
}

func castDuplicate(rt *Runtime, c, target *enterworld.Character) OpResult {
	return rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: duplicateA1, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(target)}.Encode())
}

/*
==================
TestDuplicateCopiesTheAllysLook

4F0040 / 4F0320: the Rogue takes the ally's model, record byte and worn
sword; observers get a player skin in 0x323A; the speeds stay the
Rogue's own; the next skill cast (skc 2) ends it.
==================
*/
func TestDuplicateCopiesTheAllysLook(t *testing.T) {
	rt, clock, c, ally := duplicateFixture(t)
	walk, run := worldSpeeds(rt, c)
	result := castDuplicate(rt, c, ally)
	if result.DiagnosticRefusal != "" || c.TransformMode != 2 {
		t.Fatalf("duplicate refused: %+v", result)
	}
	model := rt.deps.(*enterworld.Deps).CharacterModelRef(ally)
	if c.TransformRefObjID != model || c.TransformShape != 3 || c.TransformEquipment[6] != ally.MissionInventory[0].RefObjID {
		t.Fatalf("block ref %d shape %d equipment %v", c.TransformRefObjID, c.TransformShape, c.TransformEquipment)
	}
	skin, ok := findFrame(result.Broadcast, wire.OpSkinChange)
	want := wire.SkinChange{GID: enterworld.ObjectIDForCharacter(c), Skin: wire.TransformSkin{
		RefObjID: model, Player: true, Shape: 3, Equipment: c.TransformEquipment}}.Encode()
	if !ok || !bytes.Equal(skin.Payload, want) {
		t.Fatalf("skin frame %x, want %x", skin.Payload, want)
	}
	if w, r := worldSpeeds(rt, c); w != walk || r != run {
		t.Fatalf("speeds %v/%v changed from %v/%v", w, r, walk, run)
	}
	clock.Advance(2 * time.Second) // the Duplicate's own action closes
	rt.drainSkillFinalizes(clock.NowMs())
	if r := castSelf(rt, c, rogueStealthID); c.TransformMode != 0 || c.NativeBodyStatus != 6 {
		t.Fatalf("stealth did not end the duplicate: mode %d, %+v", c.TransformMode, r)
	}
}

/*
==================
TestSableDuplicateCopiesAnEnemyPlayer

The later rank (33156) is authored enemy-player-only, so the pair is
in a hostile relation (opposing capes, no party) that refuses other
targeted families. The traced Duplicate rule has no relation gate:
the Rogue takes the enemy's look under the family's own refusal list.
==================
*/
func TestSableDuplicateCopiesAnEnemyPlayer(t *testing.T) {
	row, ok := shippedSkills(t).SkillByID(duplicateA1)
	if !ok {
		t.Fatal("shipped duplicate row missing")
	}
	row.ID, row.Codename = 33156, "SKILL_EU_ROG_STEALTHA_CHANGE_B_03"
	row.Targets = enterworld.SkillTargets{Present: true, Required: true, Animal: true, EnemyP: true}
	row.Reqi = enterworld.SkillReqi{}
	row.RequiredWeaponKinds = [2]uint8{0xff, 0xff}
	p := scornOpponentPair(t, row)
	if !p.rt.hostilePlayerRelation(testDivision, p.c, p.m) {
		t.Fatal("fixture pair is not in a hostile relation")
	}
	result := p.cast(row.ID)
	if result.DiagnosticRefusal != "" || p.c.TransformMode != 2 {
		t.Fatalf("sable duplicate refused: %+v", result)
	}
	model := p.rt.deps.(*enterworld.Deps).CharacterModelRef(p.m)
	if p.c.TransformRefObjID != model {
		t.Fatalf("block ref %d, want the enemy's model %d", p.c.TransformRefObjID, model)
	}
	if _, ok := findFrame(result.Broadcast, wire.OpSkinChange); !ok {
		t.Fatalf("no skin frame in %+v", result.Frames)
	}
}

/*
==================
TestDuplicateRefusesSomePlayers

58D1D3: above the level word, a GM, a murderer and a rider are refused.
==================
*/
func TestDuplicateRefusesSomePlayers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*enterworld.Character)
		code  uint16
	}{
		{"above level 60", func(a *enterworld.Character) { level := int64(61); a.Level = &level }, 0x3035},
		{"a GM", func(a *enterworld.Character) { a.GMPrivilege = true }, 0x3039},
		{"a murderer", func(a *enterworld.Character) { a.PK = &domain.PKRecord{Penalty: 1} }, 0x3039},
		{"a rider", func(a *enterworld.Character) { a.ActiveCOS = &enterworld.CharacterCOS{Summoned: true, Mounted: true} }, 0x3039},
	} {
		rt, _, c, ally := duplicateFixture(t)
		tc.setup(ally)
		result := castDuplicate(rt, c, ally)
		want := offensiveRefusal(tc.code).Frames[0]
		if len(result.Frames) != 1 || !bytes.Equal(result.Frames[0].Payload, want.Payload) || c.TransformMode != 0 {
			t.Errorf("%s: %+v, want %#x", tc.name, result, tc.code)
		}
	}
	_ = simulation.WorldKey
}
