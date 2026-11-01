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
	// No player and no item: the text, then nameLen 0 and bodyLen 0.
	f := DropPlusNotificationFrame("A", NoticeLink{})
	want := []byte{DropPlusNoticeChannel, 1, 0, 65, 0, 0, 0, 0, 0}
	if f.Opcode != 0x3667 || !bytes.Equal(f.Payload, want) {
		t.Fatalf("drop/plus notification wire opcode=%04x payload=% X, want % X", f.Opcode, f.Payload, want)
	}
}

// TestDropPlusNotificationFrameLinksThePlayerAndItem pins the vector the
// client decodes: "A" naming Looter and three of an ETC potion, refObjID
// 0x01020304, in its inventory body ([u32 refObjID][u16 quantity]).
func TestDropPlusNotificationFrameLinksThePlayerAndItem(t *testing.T) {
	potion := ItemBody{RefObjID: 0x01020304, TypeFlags: PackTypeFlags(3, 3, 1, 1), Quantity: 3}
	f := DropPlusNotificationFrame("A", NoticeLink{Player: "Looter", Body: potion.Encode()})
	want := []byte{
		DropPlusNoticeChannel, 1, 0, 65, 0,
		6, 0, 'L', 'o', 'o', 't', 'e', 'r',
		6, 0, 0x04, 0x03, 0x02, 0x01, 3, 0,
	}
	if f.Opcode != 0x3667 || !bytes.Equal(f.Payload, want) {
		t.Fatalf("linked notice payload % X, want % X", f.Payload, want)
	}
}

func TestJobKillNotificationFrameUsesItsOwnChannel(t *testing.T) {
	f := JobKillNotificationFrame("A")
	if f.Opcode != 0x3667 || !bytes.Equal(f.Payload, []byte{JobKillNoticeChannel, 1, 0, 65, 0}) {
		t.Fatalf("job-kill notification wire opcode=%04x payload=%x", f.Opcode, f.Payload)
	}
}

func TestBreakNotificationFrameUsesItsOwnChannel(t *testing.T) {
	f := BreakNotificationFrame("A")
	if f.Opcode != 0x3667 || !bytes.Equal(f.Payload, []byte{BreakNoticeChannel, 1, 0, 65, 0}) {
		t.Fatalf("break notification wire opcode=%04x payload=%x", f.Opcode, f.Payload)
	}
}

func TestRenameNotificationFrameUsesItsOwnChannel(t *testing.T) {
	f := RenameNotificationFrame("A")
	if f.Opcode != 0x3667 || !bytes.Equal(f.Payload, []byte{RenameNoticeChannel, 1, 0, 65, 0}) {
		t.Fatalf("rename notification wire opcode=%04x payload=%x", f.Opcode, f.Payload)
	}
}
