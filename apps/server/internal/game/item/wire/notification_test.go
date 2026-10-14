package wire

import (
	"bytes"
	"testing"
)

func TestNotificationFrameNativeTextOnly(t *testing.T) {
	f := NotificationFrame("A\U0001f600")
	if f.Opcode != 0x3667 || !bytes.Equal(f.Payload, []byte{7, 3, 0, 65, 0, 0x3d, 0xd8, 0, 0xde}) {
		t.Fatalf("notification wire opcode=%04x payload=%x", f.Opcode, f.Payload)
	}
}

func TestDropPlusNotificationFrameUsesItsOwnChannel(t *testing.T) {
	f := DropPlusNotificationFrame("A")
	if f.Opcode != 0x3667 || !bytes.Equal(f.Payload, []byte{DropPlusNoticeChannel, 1, 0, 65, 0}) {
		t.Fatalf("drop/plus notification wire opcode=%04x payload=%x", f.Opcode, f.Payload)
	}
}

func TestJobKillNotificationFrameUsesItsOwnChannel(t *testing.T) {
	f := JobKillNotificationFrame("A")
	if f.Opcode != 0x3667 || !bytes.Equal(f.Payload, []byte{JobKillNoticeChannel, 1, 0, 65, 0}) {
		t.Fatalf("job-kill notification wire opcode=%04x payload=%x", f.Opcode, f.Payload)
	}
}
