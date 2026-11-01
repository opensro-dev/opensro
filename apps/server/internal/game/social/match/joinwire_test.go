package match

// Byte-exact pins for the match-JOIN wire codec (joinwire.go). The
// fixtures mirror the re-harness matchJoinParity builders, so the Go
// encoders and the REAL client folds
// (CPSMission_OnPartyMatchingJoinReq0x75BF /
// SEH_CNetProcessSecond_RegisterPacketHandlers_769e30 /
// CPSMission_OnPartyMatchingJoinResponse0xB5BF /
// CNetProcess_OnResponse0xB592) agree on every byte.

import (
	"bytes"
	"testing"
)

func TestDecodeJoinRequestPinsTheU32Body(t *testing.T) {
	entryID, err := DecodeJoinRequest(u32le(0xDEAD0042))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if entryID != 0xDEAD0042 {
		t.Fatalf("entryID = %#x, want 0xDEAD0042", entryID)
	}
}

func TestDecodeJoinRequestRefusesWrongLengths(t *testing.T) {
	if _, err := DecodeJoinRequest([]byte{1, 2, 3}); err == nil {
		t.Fatal("short body accepted")
	}
	if _, err := DecodeJoinRequest([]byte{1, 2, 3, 4, 5}); err == nil {
		t.Fatal("trailing byte accepted")
	}
}

func TestDecodeJoinAnswerPinsTheSub6fe370Body(t *testing.T) {
	payload := concat(u32le(7), u32le(3), []byte{2})
	answer, err := DecodeJoinAnswer(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := JoinAnswer{EchoA: 7, EchoB: 3, Answer: JoinAnswerNoReply}
	if answer != want {
		t.Fatalf("decoded %+v, want %+v", answer, want)
	}
}

func TestDecodeJoinAnswerRefusesWrongLengths(t *testing.T) {
	if _, err := DecodeJoinAnswer(concat(u32le(1), u32le(2))); err == nil {
		t.Fatal("missing answer byte accepted")
	}
	if _, err := DecodeJoinAnswer(concat(u32le(1), u32le(2), []byte{1, 9})); err == nil {
		t.Fatal("trailing byte accepted")
	}
}

func TestEncodePartyJoinNotifyPinsTheSub75ea70ReadOrder(t *testing.T) {
	memberInfo := []byte{0x37, 0xAA, 0xBB} // opaque tail - the seam owns it
	got := EncodePartyJoinNotify75BF(9, 4, PartyApplicant{Primary: 257, Secondary: 273, JobClass: 4}, memberInfo)
	want := concat(
		u32le(9),   // a = requestID (echoed by 0x30FA)
		u32le(4),   // b = entryID (echoed by 0x30FA)
		u32le(0),   // c - unpinned semantics, zero floor
		u32le(257), // d = highest trained mastery
		u32le(273), // e = second mastery reference, never listing purpose
		[]byte{4},  // f = ordinary active job class
		memberInfo, // the CPartyMemberInfo_ReadMasked masked member record
	)
	if !bytes.Equal(got, want) {
		t.Fatalf("notify = % X, want % X", got, want)
	}
}

func TestEncodeMentorJoinNotifyPinsTheSub769e30ReadOrder(t *testing.T) {
	got := EncodeMentorJoinNotify7592(2, 5, 20, 1907, "Alice")
	want := concat(
		u32le(2),           // echoA -> record+0x2c -> msgbox +0xa7c
		u32le(5),           // echoB -> record+0x30 -> msgbox +0xa80
		u32le(0),           // f00 - unread by the kind-0xe configure
		u32le(0),           // f04 - unread by the kind-0xe configure
		[]byte{20, 20},     // f08/f09 - the "%d(%d)" level pair
		u32le(1907),        // f0c - RefObjID (race resolve GlobalDataManager_GetCharCosDataById+0x9c)
		narrowStr("Alice"), // the UIIT_STT_TC_JOIN_REQUEST body name
	)
	if !bytes.Equal(got, want) {
		t.Fatalf("notify = % X, want % X", got, want)
	}
}

func TestEncodeJoinAckPinsTheOuter1Details(t *testing.T) {
	cases := []struct {
		detail uint8
		want   []byte
	}{
		{JoinAckComplete, []byte{1, 1}},
		{JoinAckRefused, []byte{1, 0}},
		{JoinAckNoReply, []byte{1, 2}},
	}
	for _, c := range cases {
		if got := EncodeJoinAck(c.detail); !bytes.Equal(got, c.want) {
			t.Fatalf("ack(%d) = % X, want % X", c.detail, got, c.want)
		}
	}
}

func TestJoinTableParkTakeAndDisplacement(t *testing.T) {
	table := newJoinTable()
	parked, _, displaced := table.park(joinKindParty, "div", "Hero", "Alice", 4)
	if displaced {
		t.Fatal("first park reported a displacement")
	}
	if parked.requestID != 1 {
		t.Fatalf("first requestID = %d, want 1", parked.requestID)
	}
	// A second request toward the SAME owner displaces the first (the
	// owner's pane stores exactly one request - client
	// CIFPartyMatchReqJoin_SetRequest overwrites).
	second, old, displaced := table.park(joinKindParty, "div", "Hero", "Cara", 4)
	if !displaced || old.joinerName != "Alice" {
		t.Fatalf("second park displaced %+v (displaced=%v), want Alice's request", old, displaced)
	}
	if second.requestID != 2 {
		t.Fatalf("second requestID = %d, want 2", second.requestID)
	}
	// The displaced request must be unanswerable.
	if _, ok := table.take(joinKindParty, "div", "Hero", parked.requestID, 4); ok {
		t.Fatal("displaced request still answerable")
	}
	// Wrong echoes / kind / owner never consume.
	if _, ok := table.take(joinKindParty, "div", "Hero", second.requestID, 5); ok {
		t.Fatal("wrong entry echo consumed the pending request")
	}
	if _, ok := table.take(joinKindMentor, "div", "Hero", second.requestID, 4); ok {
		t.Fatal("wrong kind consumed the pending request")
	}
	if _, ok := table.take(joinKindParty, "div", "Alice", second.requestID, 4); ok {
		t.Fatal("a non-owner consumed the pending request")
	}
	got, ok := table.take(joinKindParty, "div", "Hero", second.requestID, 4)
	if !ok || got.joinerName != "Cara" {
		t.Fatalf("take = %+v (ok=%v), want Cara's request", got, ok)
	}
	// Consumed means gone.
	if _, ok := table.take(joinKindParty, "div", "Hero", second.requestID, 4); ok {
		t.Fatal("consumed request answered twice")
	}
}

func TestJoinTableJoinerRuleAndLifecycleDrop(t *testing.T) {
	table := newJoinTable()
	table.park(joinKindParty, "div", "Hero", "Alice", 4)
	table.park(joinKindMentor, "div", "Mira", "Bob", 9)
	if !table.hasJoiner("div", "alice") {
		t.Fatal("case-insensitive joiner lookup missed Alice")
	}
	if table.hasJoiner("other", "Alice") {
		t.Fatal("joiner lookup crossed the division")
	}
	// Alice disconnecting drops her OUTGOING request silently (no
	// orphan - she was the joiner).
	if orphans := table.dropByCharacter("div", "Alice"); len(orphans) != 0 {
		t.Fatalf("joiner-side drop orphaned %d request(s), want 0", len(orphans))
	}
	if table.hasJoiner("div", "Alice") {
		t.Fatal("Alice's request survived her drop")
	}
	// Mira (an OWNER) disconnecting orphans Bob's request - the caller
	// acks him detail-2.
	orphans := table.dropByCharacter("div", "Mira")
	if len(orphans) != 1 || orphans[0].joinerName != "Bob" {
		t.Fatalf("owner-side drop orphaned %+v, want Bob's request", orphans)
	}
}
