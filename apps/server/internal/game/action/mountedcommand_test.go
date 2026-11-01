/*
===========================================================================

mountedcommand_test.go - on-foot attack and skill commands while mounted

===========================================================================
*/

package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
==================
TestMountedPlayerCommandsAreRefused

The native command step refuses both command kinds with the kind-3 notice and leaves the
target untouched; dismounted, the same attack lands.
==================
*/
func TestMountedPlayerCommandsAreRefused(t *testing.T) {
	rt, _, c, target := newCombatTestRuntime(t, 10000)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: 900, Summoned: true, Mounted: true, CurrentHP: 100}
	want := []byte{0x03, 0x00, 0x04}

	for name, payload := range map[string][]byte{
		"attack": wire.BasicAttackEngage{TargetGid: target.Gid}.Encode(),
		"skill":  wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode(),
	} {
		r := rt.HandleTargetInteract(testDivision, c, payload)
		if len(r.Frames) != 1 || r.Frames[0].Opcode != wire.OpActionState || !bytes.Equal(r.Frames[0].Payload, want) {
			t.Fatalf("%s while mounted: %+v", name, r.Frames)
		}
		if after, _ := rt.Monsters.Get(testDivision, target.Gid); after.CurrentHP != target.CurrentHP {
			t.Fatalf("%s while mounted struck the target", name)
		}
	}

	c.ActiveCOS.Mounted = false
	r := rt.HandleTargetInteract(testDivision, c, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	if len(r.Frames) == 0 || r.Frames[0].Opcode != wire.OpSkillCastResult {
		t.Fatalf("dismounted attack refused: %+v", r.Frames)
	}
}
