/*
===========================================================================

permission_test.go - the master grants member rights (0x744E)

===========================================================================
*/
package guild_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/social/guild"
)

/*
================
TestMasterGrantsUnionChat

5C69F0: the master's grant of the union chat right (4) is stored and
batched to every member as 0x3B29 0x16 mask 0x10; a member who is not the
master is refused 0x1E on 0xB44E.
================
*/
func TestMasterGrantsUnionChat(t *testing.T) {
	t.Parallel()
	deps, _, alfa, berk, guildID := newTwoMemberGuildFixture(t)
	request := binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32([]byte{1}, mutatorBerkJID), guild.PermMaskUnionChat)
	refused := guild.HandlePermissionUpdate(deps, mutatorDivision, berk, request)
	if !bytes.Equal(refused.ErrorPayload, []byte{2, 0x1E}) {
		t.Fatalf("a member's grant answered % X (%s)", refused.ErrorPayload, refused.Refusal)
	}
	outcome := guild.HandlePermissionUpdate(deps, mutatorDivision, alfa, request)
	if outcome.Refusal != "" {
		t.Fatal(outcome.Refusal)
	}
	want := append([]byte{0x16, 1, 0x10}, request[1:]...)
	if !bytes.Equal(outcome.PushPayload, want) || len(outcome.MemberNames) != 2 {
		t.Fatalf("push % X to %v, want % X to both", outcome.PushPayload, outcome.MemberNames, want)
	}
	_, members, _ := deps.Guilds.Guild(mutatorDivision, guildID)
	for _, member := range members {
		if member.JID == mutatorBerkJID && member.PermMask != guild.PermMaskUnionChat {
			t.Fatalf("stored rights %#x", member.PermMask)
		}
	}
	if again := guild.HandlePermissionUpdate(deps, mutatorDivision, alfa, request); again.PushPayload != nil {
		t.Fatal("an unchanged grant was pushed")
	}
}
