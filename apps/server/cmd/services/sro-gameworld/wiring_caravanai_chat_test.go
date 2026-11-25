/*
===========================================================================

wiring_caravanai_chat_test.go - slice 4 (#3704): agents speak in local
chat, and their chat commands answer as themselves

A spoken line becomes a gid-authored All-chat frame for the viewers who
can see the agent and nobody else; the chat command intake binds,
answers as the named agent, notices when there is no such agent, and
stays silent at the rate limit.

===========================================================================
*/
package main

import (
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/caravanai"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/chat"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestCaravanChatFramesReachVisibleViewers
================
*/
func TestCaravanChatFramesReachVisibleViewers(t *testing.T) {
	near := peerTestViewer()
	far := peerTestViewer()
	far.CharacterID = 43
	far.Pose = simulation.Spawn{RegionID: 23687, X: 100, Z: 100}
	agent := caravanPeerAgent(false, caravanai.Pose{})
	lines := []caravanChatLine{
		{agentName: "[AI]Trader_Jin", text: "Fresh silk, fair prices."},
		{agentName: "[AI]Ghost", text: "boo"},
	}

	out := caravanChatFrames("div", []caravanPeerViewer{near, far}, []caravanai.AgentEntity{agent}, lines)
	if len(out) != 1 {
		t.Fatalf("expected one addressed batch (the near viewer, the real agent), got %d", len(out))
	}
	batch := out[0]
	if batch.OnlyCharacterID != near.CharacterID || batch.DivisionID != "div" {
		t.Fatalf("the batch must address the near viewer in the division, got %+v", batch)
	}
	if len(batch.Frames) != 1 || batch.Frames[0].Opcode != chat.OpChatBroadcast {
		t.Fatalf("expected one chat broadcast frame, got %+v", batch.Frames)
	}
	want := chat.EncodeChatBroadcastGid(chat.ChatTypeAll, simulation.PlayerObjectID(agent.CharacterID), "Fresh silk, fair prices.")
	if string(batch.Frames[0].Payload) != string(want) {
		t.Fatalf("the frame must be the gid-authored line for the agent, got %x want %x", batch.Frames[0].Payload, want)
	}
}

/*
================
TestCaravanCommandsPortAnswersAsTheAgent
================
*/
func TestCaravanCommandsPortAnswersAsTheAgent(t *testing.T) {
	rt := caravanai.NewRuntime(caravanai.Config{Enabled: true, BrainMode: caravanai.BrainHardcoded}, nil, caravanai.WorldHooks{})
	port := caravanCommandsPort(rt)
	hunter := &enterworld.Character{Name: "Scout", Job: domain.CharacterJob{Type: domain.JobHunter}}
	other := &enterworld.Character{Name: "Other", Job: domain.CharacterJob{Type: domain.JobHunter}}

	speaker, reply, handled := port("div", hunter, "!accept [AI]Trader_Jin")
	if !handled || speaker != "[AI]Trader_Jin" || reply == "" {
		t.Fatalf("an accepted command answers as the agent, got speaker=%q reply=%q handled=%v", speaker, reply, handled)
	}
	if speaker, reply, handled := port("div", hunter, "!memory [AI]Trader_Jin"); !handled || speaker != "" || reply != "" {
		t.Fatalf("the rate limit's answer is silent, got speaker=%q reply=%q handled=%v", speaker, reply, handled)
	}
	if speaker, reply, handled := port("div", other, "!accept [AI]Ghost"); !handled || speaker != "" || !strings.Contains(reply, "No AI Trader") {
		t.Fatalf("an unknown agent answers as a notice, got speaker=%q reply=%q handled=%v", speaker, reply, handled)
	}
	if _, _, handled := port("div", other, "just chatting"); handled {
		t.Fatal("ordinary chat is not claimed")
	}
}
