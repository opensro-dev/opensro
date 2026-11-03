/*
===========================================================================

npcrefusal_test.go - every refused NPC function request answers

A refused 0x7338 must retire the client's pending function request with
0xB338 kind 2 and the branch's reason code; no branch may return silence.

===========================================================================
*/

package action

import (
	"errors"
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
TestNpcWithdrawalWithoutServiceIsRefused

The NPC grants the withdrawal row but no quest owner is wired: the
generic invalid-request code retires the request.
================
*/
func TestNpcWithdrawalWithoutServiceIsRefused(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.NpcRoster[0].TalkFlags = simulation.NpcTalkFlagWithdrawSkill
	frames, refusal := rt.HandleNpcAction(testDivision, c, npcActionBody(17, simulation.NpcTalkFlagWithdrawSkill))
	assertNpcFunctionRefusal(t, frames, refusal, wire.ErrCodeInvalidRequest)
}

/*
================
TestNpcWithdrawalRefusedByOwnerIsRefused

The quest owner refuses the player (under its level gate, say): the
generic invalid-request code retires the request.
================
*/
func TestNpcWithdrawalRefusedByOwnerIsRefused(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.NpcRoster[0].TalkFlags = simulation.NpcTalkFlagWithdrawSkill
	rt.NpcQuests = NpcQuestHooks{
		OpenResuscitation: func(*enterworld.Character, string) ([]wire.Frame, error) {
			return nil, errors.New("resuscitation service unavailable")
		},
	}
	frames, refusal := rt.HandleNpcAction(testDivision, c, npcActionBody(17, simulation.NpcTalkFlagWithdrawSkill))
	assertNpcFunctionRefusal(t, frames, refusal, wire.ErrCodeInvalidRequest)
}

/*
================
TestNpcWithdrawalOpensThroughTheQuestOwner

The row's click reaches the quest owner for the bound NPC and returns
its frames unchanged: the native 0x3230 open.
================
*/
func TestNpcWithdrawalOpensThroughTheQuestOwner(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.NpcRoster[0].TalkFlags = simulation.NpcTalkFlagWithdrawSkill
	opened := ""
	rt.NpcQuests = NpcQuestHooks{
		OpenResuscitation: func(_ *enterworld.Character, npcCodename string) ([]wire.Frame, error) {
			opened = npcCodename
			return []wire.Frame{{Opcode: 0x3230}}, nil
		},
	}
	frames, refusal := rt.HandleNpcAction(testDivision, c, npcActionBody(17, simulation.NpcTalkFlagWithdrawSkill))
	if refusal != "" || opened != rt.NpcRoster[0].Codename || len(frames) != 1 || frames[0].Opcode != 0x3230 {
		t.Fatalf("withdrawal open = %+v / %q (npc %q)", frames, refusal, opened)
	}
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

/*
================
TestGachaOpenWithoutCatalogueIsRefused

The handler itself, called with no catalogue, answers the generic
invalid-request refusal; the routed path is pinned above.
================
*/
func TestGachaOpenWithoutCatalogueIsRefused(t *testing.T) {
	rt, c, _ := gachaFixture(t)
	rt.GachaCatalog = nil
	frames, refusal := rt.HandleGachaNpcAction(testDivision, c, npcActionBody(17, gacha.InteractionFlagGacha))
	assertNpcFunctionRefusal(t, frames, refusal, wire.ErrCodeInvalidRequest)
}

/*
================
TestGachaOpenWithMalformedPayloadIsRefused

A Magic Pop action whose body does not decode answers the generic
invalid-request refusal instead of leaving the request pending.
================
*/
func TestGachaOpenWithMalformedPayloadIsRefused(t *testing.T) {
	rt, c, _ := gachaFixture(t)
	frames, refusal := rt.HandleGachaNpcAction(testDivision, c, []byte{17, 0, 0})
	assertNpcFunctionRefusal(t, frames, refusal, wire.ErrCodeInvalidRequest)
}

/*
================
TestGachaOpenWithUnsupportedFlagsIsRefused

The dispatcher routes only the Magic Pop flag here, but the handler
itself refuses any other flag set rather than answering silence.
================
*/
func TestGachaOpenWithUnsupportedFlagsIsRefused(t *testing.T) {
	rt, c, _ := gachaFixture(t)
	frames, refusal := rt.HandleGachaNpcAction(testDivision, c, npcActionBody(17, 0x20000))
	assertNpcFunctionRefusal(t, frames, refusal, wire.ErrCodeInvalidRequest)
}

/*
================
TestGachaOpenForUnselectedNpcIsRefused

The machine named is not the bound selection: the native no-object
answer (code 3) retires the request.
================
*/
func TestGachaOpenForUnselectedNpcIsRefused(t *testing.T) {
	rt, c, _ := gachaFixture(t)
	rt.Selected.Set(testDivision, c.Name, 18)
	frames, refusal := rt.HandleGachaNpcAction(testDivision, c, npcActionBody(17, gacha.InteractionFlagGacha))
	assertNpcFunctionRefusal(t, frames, refusal, selectObjectNotFound)
}

/*
================
TestGachaOpenForNpcWithoutSetIsRefused

The bound NPC is live but has no Magic Pop set: the not-offered answer
(code 5) retires the request.
================
*/
func TestGachaOpenForNpcWithoutSetIsRefused(t *testing.T) {
	rt, c, _ := gachaFixture(t)
	rt.NpcRoster[0].RefObjID = 9999
	frames, refusal := rt.HandleGachaNpcAction(testDivision, c, npcActionBody(17, gacha.InteractionFlagGacha))
	assertNpcFunctionRefusal(t, frames, refusal, npcFunctionNotOffered)
}
