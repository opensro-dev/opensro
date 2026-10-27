package enterworld

import "testing"

// W3 base falsifiers for the 0x7427 stall-network leave handler; W5
// extends. The wire contract is the client 702960 asm truth: the body is
// EMPTY (ctor -> submit with no AppendBytes), so exactly len==0 accepts
// and any payload byte is a shape the retail client cannot compose.
// No reply frames exist on this plane at all (0xB427 is a 1-byte native
// sink with no browser consumer), so the handler's whole visible surface
// is the accept/refuse decision.

func TestStallNetworkLeaveAcceptsExactlyTheEmptyBody(t *testing.T) {
	character := &Character{ID: 3, Name: "asd2"}
	if err := HandleStallNetworkLeave(character, []byte{}); err != nil {
		t.Errorf("empty body refused: %v", err)
	}
	if err := HandleStallNetworkLeave(character, nil); err != nil {
		t.Errorf("nil (zero-length) body refused: %v", err)
	}
}

func TestStallNetworkLeaveRefusesPayloadBytes(t *testing.T) {
	character := &Character{ID: 3, Name: "asd2"}
	for _, payload := range [][]byte{{0x00}, {0x01}, {0x27, 0x74}, make([]byte, 16)} {
		if err := HandleStallNetworkLeave(character, payload); err == nil {
			t.Errorf("payload % X accepted, native body is empty", payload)
		}
	}
}

func TestStallNetworkLeaveRefusesNoCharacter(t *testing.T) {
	if err := HandleStallNetworkLeave(nil, []byte{}); err == nil {
		t.Error("nil character accepted")
	}
}
