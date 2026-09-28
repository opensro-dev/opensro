/*
===========================================================================

targetaction_test.go - native command-family dispatch and strict boundaries

Every composer has one owner. These tests prevent semantic aliasing of
follow, combat, pickup, structures and effect cancellation.

===========================================================================
*/
package wire

import (
	"bytes"
	"testing"
)

/*
================
TestTargetActionLaneCoversEveryNative72CDComposerFamily
================
*/
func TestTargetActionLaneCoversEveryNative72CDComposerFamily(t *testing.T) {
	forms := []struct {
		name    string
		payload []byte
		lane    TargetActionLane
	}{
		{"cancel", []byte{0x02}, TargetActionCancel},
		{"basic attack", BasicAttackEngage{TargetGid: 0x11223344}.Encode(), TargetActionBasicAttack},
		{"pickup", TargetInteract{Gid: 0x11223344}.Encode(), TargetActionGroundItemPickup},
		{"follow", FollowTarget{TargetGid: 0x11223344}.Encode(), TargetActionFollow},
		{"malformed follow remains follow-owned", []byte{1, 3}, TargetActionFollow},
		{"skill no target", SkillAction{ActionId: 0x12345678}.Encode(), TargetActionSkill},
		{"skill entity", SkillAction{ActionId: 0x12345678, HasTarget: true, TargetGid: 0x11223344}.Encode(), TargetActionSkill},
		{"skill ground", SkillAction{ActionId: 0x12345678, HasGroundTarget: true, Region: 0x62a8, GroundX: 960, GroundY: 20, GroundZ: 458}.Encode(), TargetActionSkill},
		{"fortress structure", FortressStructureInteract{TargetGid: 0x11223344}.Encode(), TargetActionFortressStructure},
		{"cancel active effect by skill ID", CancelActiveEffectRequest{EffectID: 0x12345678}.Encode(), TargetActionCancelActiveEffect},
		{"cancel active effect instance", CancelActiveEffectRequest{EffectID: 0x12345678, InstanceToken: 0x11223344}.Encode(), TargetActionCancelActiveEffect},
	}

	for _, form := range forms {
		t.Run(form.name, func(t *testing.T) {
			if got := ClassifyTargetActionLane(form.payload); got != form.lane {
				t.Fatalf("lane = %d, want %d for % X", got, form.lane, form.payload)
			}
		})
	}
}

/*
================
TestFortressStructureInteractGoldenDoesNotAliasCancelOrPickup
================
*/
func TestFortressStructureInteractGoldenDoesNotAliasCancelOrPickup(t *testing.T) {
	form := FortressStructureInteract{TargetGid: 0x11223344}
	want := []byte{0x02, 0x01, 0x01, 0x44, 0x33, 0x22, 0x11}
	if got := form.Encode(); !bytes.Equal(got, want) {
		t.Fatalf("encode = % X, want % X", got, want)
	}
	decoded, err := DecodeFortressStructureInteract(want)
	if err != nil || decoded != form {
		t.Fatalf("round trip = %+v, %v; want %+v", decoded, err, form)
	}
	if _, err := DecodeTargetInteract(want); err == nil {
		t.Fatal("fortress structure form aliased cancel/pickup")
	}
}

/*
================
TestCancelActiveEffectGoldensAndNonzeroOptionalGate
================
*/
func TestCancelActiveEffectGoldensAndNonzeroOptionalGate(t *testing.T) {
	forms := []struct {
		form CancelActiveEffectRequest
		want []byte
	}{
		{
			CancelActiveEffectRequest{EffectID: 0x12345678},
			[]byte{0x01, 0x05, 0x78, 0x56, 0x34, 0x12, 0x00},
		},
		{
			CancelActiveEffectRequest{EffectID: 0x12345678, InstanceToken: 0x11223344},
			[]byte{0x01, 0x05, 0x78, 0x56, 0x34, 0x12, 0x44, 0x33, 0x22, 0x11, 0x00},
		},
	}
	for _, row := range forms {
		got := row.form.Encode()
		if !bytes.Equal(got, row.want) {
			t.Fatalf("encode = % X, want % X", got, row.want)
		}
		decoded, err := DecodeCancelActiveEffectRequest(got)
		if err != nil || decoded != row.form {
			t.Fatalf("round trip = %+v, %v; want %+v", decoded, err, row.form)
		}
	}

	for name, payload := range map[string][]byte{
		"zero optional": {0x01, 0x05, 1, 0, 0, 0, 0, 0, 0, 0, 0},
		"wrong tail":    {0x01, 0x05, 1, 0, 0, 0, 1},
		"truncated":     {0x01, 0x05, 1, 0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeCancelActiveEffectRequest(payload); err == nil {
				t.Fatalf("payload % X decoded, want refusal", payload)
			}
		})
	}

	highBit := []byte{0x01, 0x05, 1, 0, 0, 0, 0, 0, 0, 0x80, 0}
	decoded, err := DecodeCancelActiveEffectRequest(highBit)
	if err != nil || decoded.InstanceToken != 0x80000000 {
		t.Fatalf("high-bit nonzero optional = %+v, %v; machine JBE admits it", decoded, err)
	}
}
