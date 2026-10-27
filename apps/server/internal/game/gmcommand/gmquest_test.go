/*
===========================================================================

gmquest_test.go - /INITQ, /RESETQ, /COMPQ and /REMOVEQ at the dispatcher

The frames ChatInput_HandleGmCommand (client @0x00509cf0) composes, the
privilege gate, the owner receiving the decoded request and the
acknowledgements NetMsg_OnGmCommandAck_B5B6 (client @0x00751ec0) reads.

===========================================================================
*/
package gmcommand

import (
	"bytes"
	"testing"
)

/*
================
questOwner

The action-owned quest port, recording what it was asked and answering with
a fixed verdict.
================
*/
type questOwner struct {
	calls   []string
	verdict bool
}

func (*questOwner) ToggleGMBodyStatus(string, string, uint8) bool { return false }

func (o *questOwner) InitQuestGM(_, name, quest string) bool {
	o.calls = append(o.calls, "init:"+name+":"+quest)
	return o.verdict
}

func (o *questOwner) RemoveQuestGM(_, name, quest string) bool {
	o.calls = append(o.calls, "remove:"+name+":"+quest)
	return o.verdict
}

func (o *questOwner) CompleteQuestGM(_, name, quest string) bool {
	o.calls = append(o.calls, "complete:"+name+":"+quest)
	return o.verdict
}

func (o *questOwner) ResetQuestsGM(_, name string) bool {
	o.calls = append(o.calls, "reset:"+name)
	return o.verdict
}

func TestQuestToolsReachTheOwnerWithTheDecodedCodename(t *testing.T) {
	for _, tc := range []struct {
		sub  uint8
		want string
	}{
		{SubInitQ, "init:Gm:QNO_CH_CHEF_1"},
		{SubRemoveQ, "remove:Gm:QNO_CH_CHEF_1"},
		{SubCompQ, "complete:Gm:QNO_CH_CHEF_1"},
	} {
		gm, _, _ := adminCharacters()
		owner := &questOwner{verdict: true}
		out := HandleGmCommand(testDeps(gm), &adminPresence{}, testDivision, gm, nameFrame(tc.sub, "QNO_CH_CHEF_1"), owner)
		if len(owner.calls) != 1 || owner.calls[0] != tc.want {
			t.Fatalf("sub 0x%02X called %v, want %q", tc.sub, owner.calls, tc.want)
		}
		if !bytes.Equal(out.Ack, []byte{AckResultOK, tc.sub}) {
			t.Fatalf("sub 0x%02X ack % X, want OK with no tail", tc.sub, out.Ack)
		}
	}
}

func TestResetQuestsTakesNoArgument(t *testing.T) {
	gm, _, _ := adminCharacters()
	owner := &questOwner{verdict: true}
	out := HandleGmCommand(testDeps(gm), &adminPresence{}, testDivision, gm, []byte{SubResetQ}, owner)
	if len(owner.calls) != 1 || owner.calls[0] != "reset:Gm" || !bytes.Equal(out.Ack, []byte{AckResultOK, SubResetQ}) {
		t.Fatalf("reset called %v ack % X", owner.calls, out.Ack)
	}
	owner.calls = nil
	out = HandleGmCommand(testDeps(gm), &adminPresence{}, testDivision, gm, []byte{SubResetQ, 0}, owner)
	if len(owner.calls) != 0 || out.Refusal == "" || out.Ack != nil {
		t.Fatalf("a reset frame with a trailing byte must be dropped: calls %v out %+v", owner.calls, out)
	}
}

func TestQuestToolRefusalAnswersResultTwo(t *testing.T) {
	gm, _, _ := adminCharacters()
	owner := &questOwner{}
	for _, payload := range [][]byte{nameFrame(SubInitQ, "QNO_CH_CHEF_1"), nameFrame(SubCompQ, "QNO_CH_CHEF_1"), nameFrame(SubRemoveQ, "QNO_CH_CHEF_1"), {SubResetQ}} {
		out := HandleGmCommand(testDeps(gm), &adminPresence{}, testDivision, gm, payload, owner)
		if want := []byte{AckResultFail, payload[0]}; !bytes.Equal(out.Ack, want) {
			t.Fatalf("sub 0x%02X ack % X, want % X", payload[0], out.Ack, want)
		}
	}
}

func TestQuestToolWithoutAPortOrAnEmptyNameIsRefused(t *testing.T) {
	gm, _, _ := adminCharacters()
	out := HandleGmCommand(testDeps(gm), &adminPresence{}, testDivision, gm, nameFrame(SubInitQ, "QNO_CH_CHEF_1"))
	if !bytes.Equal(out.Ack, []byte{AckResultFail, SubInitQ}) {
		t.Fatalf("no owner: ack % X", out.Ack)
	}
	owner := &questOwner{verdict: true}
	out = HandleGmCommand(testDeps(gm), &adminPresence{}, testDivision, gm, nameFrame(SubCompQ, ""), owner)
	if len(owner.calls) != 0 || !bytes.Equal(out.Ack, []byte{AckResultFail, SubCompQ}) {
		t.Fatalf("empty codename: calls %v ack % X", owner.calls, out.Ack)
	}
}

func TestQuestToolFrameWithTrailingBytesIsDropped(t *testing.T) {
	gm, _, _ := adminCharacters()
	owner := &questOwner{verdict: true}
	payload := append(nameFrame(SubRemoveQ, "QNO_CH_CHEF_1"), 0xFF)
	out := HandleGmCommand(testDeps(gm), &adminPresence{}, testDivision, gm, payload, owner)
	if len(owner.calls) != 0 || out.Refusal == "" || out.Ack != nil {
		t.Fatalf("a malformed frame must reach no owner: calls %v out %+v", owner.calls, out)
	}
}

func TestQuestToolsNeedPrivilege(t *testing.T) {
	_, player, _ := adminCharacters()
	owner := &questOwner{verdict: true}
	for _, payload := range [][]byte{nameFrame(SubInitQ, "QNO_CH_CHEF_1"), nameFrame(SubCompQ, "QNO_CH_CHEF_1"), nameFrame(SubRemoveQ, "QNO_CH_CHEF_1"), {SubResetQ}} {
		out := HandleGmCommand(testDeps(player), &adminPresence{}, testDivision, player, payload, owner)
		if out.Ack != nil || !out.PrivilegeDenied {
			t.Fatalf("sub 0x%02X from a player: %+v, want a silent denial", payload[0], out)
		}
	}
	if len(owner.calls) != 0 {
		t.Fatalf("a player's quest command reached the owner: %v", owner.calls)
	}
}
