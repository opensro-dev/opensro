/*
===========================================================================

interact_test.go - target-action packet boundaries and native goldens

Follow, attack and pickup must remain distinct even when malformed input
shares their prefix. Round trips alone cannot establish those boundaries.

===========================================================================
*/
package wire

import (
	"bytes"
	"errors"
	"testing"
)

/*
================
TestTargetInteractGoldens

Client 698B45 emits the pickup form and bare cancellation.
================
*/
func TestTargetInteractGoldens(t *testing.T) {
	interact := TargetInteract{Gid: 300001}
	got := interact.Encode()
	want := []byte{0x01, 0x02, 0x01, 0xE1, 0x93, 0x04, 0x00}
	if !bytes.Equal(got, want) {
		t.Fatalf("interact = % X, want % X", got, want)
	}

	cancel := TargetInteract{Cancel: true}
	if got := cancel.Encode(); !bytes.Equal(got, []byte{0x02}) {
		t.Fatalf("cancel = % X, want the bare 02", got)
	}

	for _, form := range []TargetInteract{interact, cancel} {
		decoded, err := DecodeTargetInteract(form.Encode())
		if err != nil {
			t.Fatalf("round trip of %+v failed: %v", form, err)
		}
		if decoded != form {
			t.Fatalf("round trip = %+v, want %+v", decoded, form)
		}
	}
}

/*
================
TestBasicAttackEngageGoldens
================
*/
func TestBasicAttackEngageGoldens(t *testing.T) {
	tests := []struct {
		name string
		form BasicAttackEngage
		want []byte
	}{
		{
			name: "world double-click or control-click",
			form: BasicAttackEngage{TargetGid: 400001},
			want: []byte{0x01, 0x01, 0x01, 0x81, 0x1A, 0x06, 0x00},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := testCase.form.Encode()
			if !bytes.Equal(got, testCase.want) {
				t.Fatalf("engage = % X, want % X", got, testCase.want)
			}
			decoded, err := DecodeBasicAttackEngage(got)
			if err != nil || decoded != testCase.form {
				t.Fatalf("round trip = %+v, %v; want %+v", decoded, err, testCase.form)
			}
			if _, err := DecodeTargetInteract(got); err == nil {
				t.Fatal("basic attack aliased the ground-item decoder")
			}
		})
	}
}

/*
================
TestBasicAttackEngageRejectsOther72CDLegs
================
*/
func TestBasicAttackEngageRejectsOther72CDLegs(t *testing.T) {
	for name, payload := range map[string][]byte{
		"empty":            nil,
		"follow":           FollowTarget{TargetGid: 400001}.Encode(),
		"pickup":           {0x01, 0x02, 0x01, 0x81, 0x1A, 0x06, 0x00},
		"skill":            {0x01, 0x04, 0x02, 0x00, 0x00, 0x00, 0x00},
		"wrong actor kind": {0x01, 0x01, 0x02, 0x81, 0x1A, 0x06, 0x00},
		"zero gid":         {0x01, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00},
		"trailing":         {0x01, 0x01, 0x01, 0x81, 0x1A, 0x06, 0x00, 0xFF},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeBasicAttackEngage(payload); err == nil {
				t.Fatalf("payload % X decoded, want refusal", payload)
			}
		})
	}
}

/*
================
TestFollowTargetNativeGoldenAndFamilyIsolation
================
*/
func TestFollowTargetNativeGoldenAndFamilyIsolation(t *testing.T) {
	form := FollowTarget{TargetGid: 400001}
	want := []byte{1, 3, 1, 0x81, 0x1A, 6, 0}
	if !bytes.Equal(form.Encode(), want) {
		t.Fatalf("follow = % X, want % X", form.Encode(), want)
	}
	if got, err := DecodeFollowTarget(want); err != nil || got != form {
		t.Fatalf("follow round trip = %+v, %v", got, err)
	}
	for _, payload := range [][]byte{
		nil, want[:6], append(append([]byte{}, want...), 0),
		FollowTarget{}.Encode(), BasicAttackEngage{TargetGid: 400001}.Encode(),
		TargetInteract{Gid: 400001}.Encode(), {1, 3, 2, 0x81, 0x1A, 6, 0},
	} {
		if _, err := DecodeFollowTarget(payload); err == nil {
			t.Fatalf("follow admitted % X", payload)
		}
	}
	if _, err := DecodeTargetInteract(want); err == nil {
		t.Fatal("follow entered pickup conversation")
	}
}

/*
================
TestTargetInteractDecodeRejectsMalformedPayloads
================
*/
func TestTargetInteractDecodeRejectsMalformedPayloads(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
	}{
		{"empty", nil},
		{"unknown lead byte", []byte{0x03, 0x02, 0x01, 0x00, 0x00, 0x00, 0x00}},
		{"wrong ground-leg discriminator", []byte{0x01, 0x01, 0x01, 0xE1, 0x93, 0x04, 0x00}},
		{"wrong item-kind discriminator", []byte{0x01, 0x02, 0x02, 0xE1, 0x93, 0x04, 0x00}},
		{"truncated gid", []byte{0x01, 0x02, 0x01, 0xE1, 0x93}},
		{"trailing byte on cancel", []byte{0x02, 0x00}},
		{"trailing byte on interact", []byte{0x01, 0x02, 0x01, 0xE1, 0x93, 0x04, 0x00, 0xFF}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := DecodeTargetInteract(testCase.payload); err == nil {
				t.Fatalf("payload % X decoded, want a refusal", testCase.payload)
			}
		})
	}

	// The length errors specifically surface the shared reader sentinels.
	if _, err := DecodeTargetInteract([]byte{0x01, 0x02, 0x01, 0xE1}); !errors.Is(err, ErrShortPayload) {
		t.Fatalf("short gid = %v, want ErrShortPayload", err)
	}
	if _, err := DecodeTargetInteract([]byte{0x02, 0x00}); !errors.Is(err, ErrTrailingBytes) {
		t.Fatalf("long cancel = %v, want ErrTrailingBytes", err)
	}
}
