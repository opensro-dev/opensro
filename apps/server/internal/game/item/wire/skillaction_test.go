package wire

import (
	"bytes"
	"errors"
	"testing"
)

// Goldens for the three unique wire layouts of the 0x72CD skill-action
// family: sub_6fcd50's flag-0x00/0x01 in-place bodies and sub_878100's
// flag-0x02 ground-location body.
func TestSkillActionGoldens(t *testing.T) {
	noTarget := SkillAction{ActionId: 0x00001234}
	got := noTarget.Encode()
	wantNoTarget := []byte{0x01, 0x04, 0x34, 0x12, 0x00, 0x00, 0x00}
	if !bytes.Equal(got, wantNoTarget) {
		t.Fatalf("no-target = % X, want % X", got, wantNoTarget)
	}

	withTarget := SkillAction{ActionId: 0x0000ABCD, HasTarget: true, TargetGid: 300001}
	got = withTarget.Encode()
	wantWithTarget := []byte{0x01, 0x04, 0xCD, 0xAB, 0x00, 0x00, 0x01, 0xE1, 0x93, 0x04, 0x00}
	if !bytes.Equal(got, wantWithTarget) {
		t.Fatalf("with-target = % X, want % X", got, wantWithTarget)
	}

	groundTarget := SkillAction{ActionId: 0x00005678, HasGroundTarget: true,
		Region: 0x62A8, GroundX: 960, GroundY: 20, GroundZ: 458}
	got = groundTarget.Encode()
	wantGroundTarget := []byte{0x01, 0x04, 0x78, 0x56, 0x00, 0x00, 0x02,
		0xA8, 0x62, 0xC0, 0x03, 0x14, 0x00, 0xCA, 0x01}
	if !bytes.Equal(got, wantGroundTarget) {
		t.Fatalf("ground-location = % X, want % X", got, wantGroundTarget)
	}

	for _, form := range []SkillAction{noTarget, withTarget, groundTarget} {
		decoded, err := DecodeSkillAction(form.Encode())
		if err != nil {
			t.Fatalf("round trip of %+v failed: %v", form, err)
		}
		if decoded != form {
			t.Fatalf("round trip = %+v, want %+v", decoded, form)
		}
		if !IsSkillActionPayload(form.Encode()) {
			t.Fatalf("IsSkillActionPayload rejected %+v", form)
		}
	}
}

func TestSkillActionDecodeRejectsMalformedPayloads(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
	}{
		{"empty", nil},
		{"pickup interact form", []byte{0x01, 0x02, 0x01, 0xE1, 0x93, 0x04, 0x00}},
		{"pickup cancel", []byte{0x02}},
		{"wrong lead", []byte{0x03, 0x04, 0x01, 0x00, 0x00, 0x00, 0x00}},
		{"wrong leg (pickup-shaped second byte)", []byte{0x01, 0x02, 0x34, 0x12, 0x00, 0x00, 0x00}},
		{"attack-family second byte", []byte{0x01, 0x03, 0x01, 0xE1, 0x93, 0x04, 0x00}},
		{"bad target flag", []byte{0x01, 0x04, 0x34, 0x12, 0x00, 0x00, 0x03}},
		{"truncated action id", []byte{0x01, 0x04, 0x34, 0x12}},
		{"has-target missing gid", []byte{0x01, 0x04, 0x34, 0x12, 0x00, 0x00, 0x01, 0xE1}},
		{"ground flag with no location", []byte{0x01, 0x04, 0x34, 0x12, 0x00, 0x00, 0x02}},
		{"ground location truncated", []byte{0x01, 0x04, 0x34, 0x12, 0x00, 0x00, 0x02, 0xA8, 0x62, 0xC0, 0x03, 0x14, 0x00}},
		{"trailing on no-target", []byte{0x01, 0x04, 0x34, 0x12, 0x00, 0x00, 0x00, 0xFF}},
		{"trailing on with-target", []byte{0x01, 0x04, 0xCD, 0xAB, 0x00, 0x00, 0x01, 0xE1, 0x93, 0x04, 0x00, 0xFF}},
		{"trailing on ground-location", []byte{0x01, 0x04, 0x78, 0x56, 0x00, 0x00, 0x02, 0xA8, 0x62, 0xC0, 0x03, 0x14, 0x00, 0xCA, 0x01, 0xFF}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := DecodeSkillAction(testCase.payload); err == nil {
				t.Fatalf("payload % X decoded, want a refusal", testCase.payload)
			}
			if IsSkillActionPayload(testCase.payload) {
				t.Fatalf("IsSkillActionPayload accepted % X", testCase.payload)
			}
		})
	}

	if _, err := DecodeSkillAction([]byte{0x01, 0x04, 0x34}); !errors.Is(err, ErrShortPayload) {
		t.Fatalf("short action id = %v, want ErrShortPayload", err)
	}
	if _, err := DecodeSkillAction([]byte{0x01, 0x04, 0x34, 0x12, 0x00, 0x00, 0x00, 0xFF}); !errors.Is(err, ErrTrailingBytes) {
		t.Fatalf("long no-target = %v, want ErrTrailingBytes", err)
	}
}

// Witnessed-red: skill-action bytes must never decode as pickup.
func TestPickupDecodeRejectsSkillActionShapes(t *testing.T) {
	skillForms := [][]byte{
		{0x01, 0x04, 0x34, 0x12, 0x00, 0x00, 0x00},
		{0x01, 0x04, 0xCD, 0xAB, 0x00, 0x00, 0x01, 0xE1, 0x93, 0x04, 0x00},
		{0x01, 0x04, 0x78, 0x56, 0x00, 0x00, 0x02, 0xA8, 0x62, 0xC0, 0x03, 0x14, 0x00, 0xCA, 0x01},
	}
	for _, payload := range skillForms {
		if _, err := DecodeTargetInteract(payload); err == nil {
			t.Fatalf("pickup decode accepted skill-action % X", payload)
		}
	}
}
