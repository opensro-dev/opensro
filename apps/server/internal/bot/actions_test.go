package bot_test

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

func TestEncodeMovementToRoundTrips(t *testing.T) {
	body := bot.EncodeMovementTo(0x6B4F, 1201, 80, 355)
	if len(body) != simulation.ClientMovementRequestSize {
		t.Fatalf("movement body = %d bytes, want %d", len(body), simulation.ClientMovementRequestSize)
	}
	request, moveErr := simulation.DecodeClientMovementRequest(body)
	if moveErr != nil {
		t.Fatalf("decode movement: %v", moveErr)
	}
	if request.Mode != simulation.MovementAckDestinationMode || request.RegionID != 0x6B4F {
		t.Fatalf("decoded movement = %+v", request)
	}
	if request.X != 1201 || request.Y != 80 || request.Z != 355 {
		t.Fatalf("decoded coords = %v,%v,%v, want 1201,80,355", request.X, request.Y, request.Z)
	}
}

func TestEncodeBasicAttackRoundTrips(t *testing.T) {
	const gid uint32 = 0x00A1B2C3
	body := bot.EncodeBasicAttack(gid)
	if lane := wire.ClassifyTargetActionLane(body); lane != wire.TargetActionBasicAttack {
		t.Fatalf("attack lane = %v, want basic attack", lane)
	}
	engage, err := wire.DecodeBasicAttackEngage(body)
	if err != nil {
		t.Fatal(err)
	}
	if engage.TargetGid != gid {
		t.Fatalf("target gid = 0x%X, want 0x%X", engage.TargetGid, gid)
	}
}

// TestAttackerFromSkillCast guards issue #185: a monster's 0xB245 attack names
// the monster as the caster and the bot as the target. The helper reads that
// attacker. A player caster, a cast on another character and a refusal carry
// no attacker.
func TestAttackerFromSkillCast(t *testing.T) {
	const botGID uint32 = 100007
	monsterGID := domain.MonsterGIDBase + 123
	single := func(caster, target uint32) []byte {
		return wire.SkillCastSingleTargetResultFrame(wire.NewStationarySkillCastSingleTargetResult(
			wire.SkillCastSuccess{SkillId: 1, CasterGid: caster, InstanceToken: 9},
			target,
			[]wire.SkillCastTargetImpact{{Damage: 5}},
		)).Payload
	}
	// The frame layout evidence: the encoder derives OwnerOrTargetGid from the
	// target gid, so offset 14..18 holds the victim.
	if got := single(monsterGID, botGID); len(got) < 18 || got[0] != 0x01 {
		t.Fatalf("cast payload = %x, want an 18-byte 0x01 success prefix", got)
	}
	if gid, ok := bot.AttackerFromSkillCast(single(monsterGID, botGID), botGID); !ok || gid != monsterGID {
		t.Fatalf("attacker = %d, %v, want %d, true", gid, ok, monsterGID)
	}
	if _, ok := bot.AttackerFromSkillCast(single(domain.PlayerGIDBase+5, botGID), botGID); ok {
		t.Fatal("a player caster was treated as a monster aggressor")
	}
	if _, ok := bot.AttackerFromSkillCast(single(monsterGID, botGID+1), botGID); ok {
		t.Fatal("a cast on another target was treated as an attack on the bot")
	}
	if _, ok := bot.AttackerFromSkillCast([]byte{0x02, 0x39}, botGID); ok {
		t.Fatal("a refusal body was treated as an attack")
	}
	if _, ok := bot.AttackerFromSkillCast(nil, botGID); ok {
		t.Fatal("an empty body was treated as an attack")
	}
}

// TestEncodeObjectSelectPinsTheNativeBytes guards the 0x745A body [u32le gid]
// (issue #171). The oracle is the native trace tuple the object-select decoder
// pins.
func TestEncodeObjectSelectPinsTheNativeBytes(t *testing.T) {
	if bot.OpObjectSelectRequest != 0x745A {
		t.Fatalf("opcode = 0x%04X, want 0x745A", bot.OpObjectSelectRequest)
	}
	got := bot.EncodeObjectSelect(200001)
	want := []byte{0x41, 0x0D, 0x03, 0x00}
	if !bytes.Equal(got, want) {
		t.Fatalf("object select = % x, want % x", got, want)
	}
	gid, err := wire.DecodeObjectSelectRequest(got)
	if err != nil || gid != 200001 {
		t.Fatalf("decode = %d, %v, want 200001", gid, err)
	}
}

// TestEncodePortalPinsTheWire guards the 0x7495 gate-travel body
// [u32le gid][u8 kind=2][u32le destination] (issue #171).
func TestEncodePortalPinsTheWire(t *testing.T) {
	if bot.PortalOpcode != 0x7495 {
		t.Fatalf("opcode = 0x%04X, want 0x7495", bot.PortalOpcode)
	}
	got := bot.EncodePortal(252094, 2)
	want := []byte{0xBE, 0xD8, 0x03, 0x00, 0x02, 0x02, 0x00, 0x00, 0x00}
	if !bytes.Equal(got, want) {
		t.Fatalf("portal = % x, want % x", got, want)
	}
}
