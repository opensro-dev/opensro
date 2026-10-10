/*
===========================================================================

silk_test.go - the port's /SILK GM command (operator tooling)

===========================================================================
*/
package gmcommand

import (
	"encoding/binary"
	"testing"
)

/*
================
silkOwner

Records the grant the lane asks for.
================
*/
type silkOwner struct {
	calls          int
	sender, target string
	amount         uint32
	refuse         bool
}

func (o *silkOwner) ToggleGMBodyStatus(string, string, uint8) bool { return false }

func (o *silkOwner) GrantGMSilk(_, sender, target string, amount uint32) (uint32, bool) {
	o.calls++
	o.sender, o.target, o.amount = sender, target, amount
	return 100500, !o.refuse
}

/*
================
silkFrame

{u8 0xF0, u16 len + name, u32 amount}, laid out by hand.
================
*/
func silkFrame(name string, amount uint32) []byte {
	frame := append(nameFrame(SubGrantSilk, name), 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(frame[len(frame)-4:], amount)
	return frame
}

/*
================
TestGMSilkGrantReachesTheOwnerAndAcksTheBalance
================
*/
func TestGMSilkGrantReachesTheOwnerAndAcksTheBalance(t *testing.T) {
	sender := gmChar("Gm", true, 0x1234)
	owner := &silkOwner{}
	outcome := HandleGmCommand(testDeps(sender), &stubPresence{}, testDivision, sender, silkFrame("Tester", 100000), owner)
	if owner.calls != 1 || owner.sender != "Gm" || owner.target != "Tester" || owner.amount != 100000 {
		t.Fatalf("owner saw %+v", owner)
	}
	if want := []byte{AckResultOK, SubGrantSilk, 0x94, 0x88, 0x01, 0x00}; string(outcome.Ack) != string(want) {
		t.Fatalf("ack % X, want % X", outcome.Ack, want)
	}
	owner.refuse = true
	if outcome := HandleGmCommand(testDeps(sender), &stubPresence{}, testDivision, sender, silkFrame("Tester", 5), owner); string(outcome.Ack) != string([]byte{AckResultFail, SubGrantSilk}) {
		t.Fatalf("refused grant ack % X", outcome.Ack)
	}
}

/*
================
TestNonGMSilkGrantIsSilentAndGrantsNothing
================
*/
func TestNonGMSilkGrantIsSilentAndGrantsNothing(t *testing.T) {
	sender := gmChar("Player", false, 0x1234)
	owner := &silkOwner{}
	outcome := HandleGmCommand(testDeps(sender), &stubPresence{}, testDivision, sender, silkFrame("Player", 1000000), owner)
	if outcome.Ack != nil || !outcome.PrivilegeDenied || owner.calls != 0 {
		t.Fatalf("non-GM /SILK: ack % X, denied %v, grants %d", outcome.Ack, outcome.PrivilegeDenied, owner.calls)
	}
}

/*
================
TestDecodeSilkRefusesShortOrTrailingFrames
================
*/
func TestDecodeSilkRefusesShortOrTrailingFrames(t *testing.T) {
	good := silkFrame("Tester", 7)
	if request, err := DecodeGmCommand(good); err != nil || request.Name != "Tester" || request.Silk != 7 {
		t.Fatalf("decode %+v, %v", request, err)
	}
	if _, err := DecodeGmCommand(good[:len(good)-1]); err == nil {
		t.Fatal("a short /SILK frame decoded")
	}
	if _, err := DecodeGmCommand(append(good, 0)); err == nil {
		t.Fatal("a /SILK frame with trailing bytes decoded")
	}
}
