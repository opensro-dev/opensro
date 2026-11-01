/*
===========================================================================

npcrefusal_test.go - every refused NPC function request answers

A refused 0x7338 must retire the client's pending function request with
0xB338 kind 2 and the branch's reason code; no branch may return silence.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/gacha"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
assertNpcFunctionRefusal

One 0xB338 kind-2 frame carrying code, and nothing else.
================
*/
func assertNpcFunctionRefusal(t *testing.T, frames []wire.Frame, refusal string, code uint8) {
	t.Helper()
	if refusal == "" || len(frames) != 1 || frames[0].Opcode != wire.OpNpcInteractionAck ||
		string(frames[0].Payload) != string(wire.EncodeNpcInteractionRefusal(code)) {
		t.Fatalf("refusal = %+v / %q, want one B338 kind-2 code %d", frames, refusal, code)
	}
}

/*
================
TestNpcFunctionForUnboundObjectIsRefused

The request names an NPC that is not the bound selection: the native
no-object answer (code 3) retires it.
================
*/
func TestNpcFunctionForUnboundObjectIsRefused(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.Selected.Set(testDivision, c.Name, 18)
	frames, refusal := rt.HandleNpcAction(testDivision, c, npcActionBody(17, simulation.NpcTalkFlagShop))
	assertNpcFunctionRefusal(t, frames, refusal, selectObjectNotFound)
}

/*
================
TestNpcFunctionForOutOfScopeNpcIsRefused

The bound NPC is not live in the viewer's scope: code 3 again.
================
*/
func TestNpcFunctionForOutOfScopeNpcIsRefused(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.NpcRoster = nil
	frames, refusal := rt.HandleNpcAction(testDivision, c, npcActionBody(17, simulation.NpcTalkFlagShop))
	assertNpcFunctionRefusal(t, frames, refusal, selectObjectNotFound)
}

/*
================
TestNpcTalkWithoutSpeechIsRefused

The NPC grants talk but has no base-speech line: the talk entry resolves
to no dialog, so code 3 retires the request.
================
*/
func TestNpcTalkWithoutSpeechIsRefused(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.NpcRoster[0].TalkFlags = simulation.NpcTalkFlagTalk
	frames, refusal := rt.HandleNpcAction(testDivision, c, npcActionBody(17, simulation.NpcTalkFlagTalk))
	assertNpcFunctionRefusal(t, frames, refusal, selectObjectNotFound)
}

/*
================
TestNpcTalkWithBrokenQuestRowIsRefused

A quest row with an incomplete symbol contract is a server-side defect:
the generic invalid-request code retires the request.
================
*/
func TestNpcTalkWithBrokenQuestRowIsRefused(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.NpcRoster[0].TalkFlags = simulation.NpcTalkFlagTalk
	rt.NpcRoster[0].BaseSpeechSymbol = "NPC_BS"
	rt.NpcQuests = NpcQuestHooks{
		Options: func(string, *enterworld.Character, string) []NpcQuestOption {
			return []NpcQuestOption{{Codename: "Q_BROKEN", TitleSymbol: "", PromptSymbol: "SN_TALK_Q_BROKEN"}}
		},
	}
	frames, refusal := rt.HandleNpcAction(testDivision, c, npcActionBody(17, simulation.NpcTalkFlagTalk))
	assertNpcFunctionRefusal(t, frames, refusal, wire.ErrCodeInvalidRequest)
}

/*
================
TestNpcStorageWithoutAuthorityIsRefused

The NPC grants storage but no storage authority is configured: the
generic invalid-request code retires the request.
================
*/
func TestNpcStorageWithoutAuthorityIsRefused(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.NpcRoster[0].TalkFlags = simulation.NpcTalkFlagStorage
	frames, refusal := rt.HandleNpcAction(testDivision, c, npcActionBody(17, simulation.NpcTalkFlagStorage))
	assertNpcFunctionRefusal(t, frames, refusal, wire.ErrCodeInvalidRequest)
}

/*
================
TestNpcMagicOptionWithoutAlchemyIsRefused

The NPC grants the magic-option function but no alchemy catalogue is
configured: the generic invalid-request code retires the request.
================
*/
func TestNpcMagicOptionWithoutAlchemyIsRefused(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.NpcRoster[0].TalkFlags = simulation.NpcTalkFlagMagicOption
	frames, refusal := rt.HandleNpcAction(testDivision, c, npcActionBody(17, simulation.NpcTalkFlagMagicOption))
	assertNpcFunctionRefusal(t, frames, refusal, wire.ErrCodeInvalidRequest)
}

/*
================
TestNpcFunctionWithNoOwnerIsRefused

The NPC grants a mask no gameplay module owns: the not-offered code
retires the request rather than leaving it pending.
================
*/
func TestNpcFunctionWithNoOwnerIsRefused(t *testing.T) {
	const unownedMask uint32 = 0x40000000
	rt, c := merchantFixture(t)
	rt.NpcRoster[0].TalkFlags = unownedMask
	frames, refusal := rt.HandleNpcAction(testDivision, c, npcActionBody(17, unownedMask))
	assertNpcFunctionRefusal(t, frames, refusal, npcFunctionNotOffered)
}

/*
================
TestGachaRequestWithoutCatalogueIsRefused

A Magic Pop click routed with no gacha catalogue configured answers the
generic invalid-request refusal instead of leaving the client's pending
function request hanging on silence.
================
*/
func TestGachaRequestWithoutCatalogueIsRefused(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.GachaCatalog = nil
	frames, refusal := rt.routeNpcAction(testDivision, c, gacha.InteractionFlagGacha, npcActionBody(17, gacha.InteractionFlagGacha))
	assertNpcFunctionRefusal(t, frames, refusal, wire.ErrCodeInvalidRequest)
}
