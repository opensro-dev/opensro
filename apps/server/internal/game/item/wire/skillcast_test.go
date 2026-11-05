package wire

import (
	"bytes"
	"math"
	"testing"
)

func testSkillCastFacingPoint(t *testing.T) SkillCastFacingPoint {
	t.Helper()
	point, ok := NewSkillCastFacingPoint(0x62AA, 812.68, 75.08, 392.90)
	if !ok {
		t.Fatal("test facing point was rejected")
	}
	return point
}

// The finalize token must echo the authoritative result frame. A token miss
// is consumed silently by the client's SkillInstanceRegistry_Find lookup
// and never closes the client bracket.
func TestSkillCastFinalizeGolden(t *testing.T) {
	finalize := SkillCastFinalize{InstanceToken: 0x00C0FFEE}
	gotFinalize := finalize.Encode()
	wantFinalize := []byte{0x02, 0x00, 0xEE, 0xFF, 0xC0, 0x00}
	if !bytes.Equal(gotFinalize, wantFinalize) {
		t.Fatalf("finalize = % X, want % X", gotFinalize, wantFinalize)
	}
}

func TestSkillCastSingleTargetResultGolden(t *testing.T) {
	result := NewSkillCastSingleTargetResult(
		SkillCastSuccess{
			BtResult:      0,
			SkillId:       1,
			CasterGid:     100003,
			InstanceToken: 2,
		},
		400001,
		[]SkillCastTargetImpact{{
			ResultFlags:     0x12,
			Damage:          47,
			Fatal:           true,
			SecondaryAmount: 9,
		}},
		testSkillCastFacingPoint(t),
	)
	got := result.Encode()
	want := []byte{
		0x01, 0x00, // success, btResult
		0x01, 0x00, 0x00, 0x00, // skillId
		0xA3, 0x86, 0x01, 0x00, // casterGid
		0x02, 0x00, 0x00, 0x00, // instanceToken
		0x81, 0x1A, 0x06, 0x00, // ownerOrTargetGid = targetGid
		0x09,       // steeringFlags: target-list and facing-point blocks follow
		0x01, 0x01, // one impact row, one target
		0x81, 0x1A, 0x06, 0x00, // targetGid 400001
		0x80,                   // type 0 + fatal transition bit
		0x12, 0x2F, 0x00, 0x00, // flags 0x12 + applied damage 47
		0x09, 0x00, 0x00, 0x00, // secondary amount
		0xAA, 0x62, // facing region
		0x2C, 0x03, // facing X = trunc(812.68)
		0x4B, 0x00, // facing Y = trunc(75.08)
		0x88, 0x01, // facing Z = trunc(392.90)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("single-target result = % X, want % X", got, want)
	}
	if len(got) != 42 {
		t.Fatalf("single-target result length = %d, want exactly 42", len(got))
	}

	saturated := NewSkillCastSingleTargetResult(
		SkillCastSuccess{BtResult: 0, SkillId: 1, CasterGid: 100003, InstanceToken: 2},
		400001,
		[]SkillCastTargetImpact{{ResultFlags: 0x12, Damage: MaxSkillActionDamage + 1, Fatal: true, SecondaryAmount: 9}},
		testSkillCastFacingPoint(t),
	).Encode()
	if !bytes.Equal(saturated[26:30], []byte{0x12, 0xFF, 0xFF, 0xFF}) {
		t.Fatalf("saturated packed result = % X, want flags + 24-bit max", saturated[26:30])
	}
}

// The authoritative result and close constructors preserve the bracket's
// token identity while allowing runtime policy to deliver them at different
// times. There is deliberately no constructor for a visual-only success.
func TestSkillCastFrameConstructorsPreserveBracketToken(t *testing.T) {
	success := SkillCastSingleTargetResultFrame(NewSkillCastSingleTargetResult(
		SkillCastSuccess{
			SkillId:       0x1234,
			CasterGid:     100003,
			InstanceToken: 42,
		},
		400001,
		[]SkillCastTargetImpact{{
			ResultFlags: 1,
			Damage:      6,
		}},
		testSkillCastFacingPoint(t),
	))
	finalize := SkillCastFinalizeFrame(42)
	if success.Opcode != OpSkillCastResult || finalize.Opcode != OpSkillEffectControl {
		t.Fatalf("opcodes = [0x%04X 0x%04X], want [0xB245 0xB505]", success.Opcode, finalize.Opcode)
	}
	if !bytes.Equal(success.Payload[10:14], finalize.Payload[2:6]) {
		t.Fatalf("finalize token % X does not echo the success token % X",
			finalize.Payload[2:6], success.Payload[10:14])
	}
	if success.Payload[1] != 0 {
		t.Fatalf("btResult = %d, want the pinned 0", success.Payload[1])
	}
	if !bytes.Equal(success.Payload[14:18], success.Payload[21:25]) {
		t.Fatalf("ownerOrTargetGid = % X, targetGid = % X; one target must own both views",
			success.Payload[14:18], success.Payload[21:25])
	}
	if success.Payload[18] != 9 || len(success.Payload) != 42 {
		t.Fatalf("authoritative result shape = flags %d, length %d; want 9 and 42",
			success.Payload[18], len(success.Payload))
	}
}

func TestSkillCastSingleTargetMultiImpactGolden(t *testing.T) {
	result := NewSkillCastSingleTargetResult(
		SkillCastSuccess{SkillId: 2, CasterGid: 100003, InstanceToken: 7},
		400001,
		[]SkillCastTargetImpact{
			{ResultFlags: 1, Damage: 11},
			{ResultFlags: 2, Damage: 13, Fatal: true},
		},
		testSkillCastFacingPoint(t),
	)
	got := result.Encode()
	if len(got) != 51 {
		t.Fatalf("two-impact result length = %d, want 51", len(got))
	}
	if got[19] != 2 || got[20] != 1 {
		t.Fatalf("impact/target counts = %d/%d, want 2/1", got[19], got[20])
	}
	if !bytes.Equal(got[21:25], []byte{0x81, 0x1A, 0x06, 0x00}) {
		t.Fatalf("target gid = % X, want one shared target column", got[21:25])
	}
	if got[25] != 0 || got[34] != 0x80 {
		t.Fatalf("impact fatal tags = %#x/%#x, want 0/0x80", got[25], got[34])
	}
}

func TestSkillCastFacingPointAdmissionAndOwnership(t *testing.T) {
	point, ok := NewSkillCastFacingPoint(0x62AA, -12.9, 0.99, 32767.99)
	if !ok {
		t.Fatal("representable finite point was rejected")
	}
	encoded := point.writeTo(NewWriter(8)).Payload()
	want := []byte{0xAA, 0x62, 0xF4, 0xFF, 0x00, 0x00, 0xFF, 0x7F}
	if !bytes.Equal(encoded, want) {
		t.Fatalf("facing point = % X, want truncation-toward-zero % X", encoded, want)
	}

	for _, invalid := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 32768, -32769} {
		if _, admitted := NewSkillCastFacingPoint(0x62AA, invalid, 0, 0); admitted {
			t.Fatalf("invalid facing coordinate %v was admitted", invalid)
		}
	}

	deferred := NewSkillCastSingleTargetResult(
		SkillCastSuccess{SkillId: 1, CasterGid: 100003, InstanceToken: 2},
		400001,
		[]SkillCastTargetImpact{{Damage: 1}},
		SkillCastFacingPoint{},
	)
	defer func() {
		if recover() == nil {
			t.Fatal("single-target result without its facing publication did not fail closed")
		}
	}()
	_ = deferred.Encode()
}
