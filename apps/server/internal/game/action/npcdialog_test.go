package action

import (
	"errors"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

type fullQuestBag struct{}

func TestUnfinishedQuestDialogueCannotMutateQuest(t *testing.T) {
	c := testCharacter()
	rt := selectTestRuntime(c)
	rt.NpcSpawn = enterworld.NpcSpawnConfig{Enabled: true, AtPlayer: true}
	npc := rt.NpcRoster[0]
	rt.Selected.Set(testDivision, c.Name, npc.ObjectID)
	rt.NpcDialogs.Put(testDivision, c.Name, npcDialogSession{NpcGID: npc.ObjectID, NpcCode: npc.Codename, Stage: npcDialogOptions, Options: []NpcQuestOption{{Codename: "QUEST", PromptSymbol: "ONGOING", Informational: true}}})
	rt.NpcQuests.Accept = func(*enterworld.Character, string) ([]wire.Frame, error) {
		t.Fatal("informational row accepted quest")
		return nil, nil
	}
	frames, refusal := rt.HandleNpcDialogResponse(testDivision, c, []byte{5})
	if refusal != "" || len(frames) != 1 || string(frames[0].Payload) != string(wire.EncodeNpcDialogSymbol("ONGOING")) {
		t.Fatalf("%v %q", frames, refusal)
	}
	if _, refusal := rt.HandleNpcDialogResponse(testDivision, c, []byte{2}); refusal == "" {
		t.Fatal("informational row retained Yes")
	}
}

/*
================
TestSideTalkRecordsItsLineThroughFinish

A side-talk row speaks its line once and records it as heard through the
quest owner (89FDA0's pending bit); a refusal answers the NPC's base line.
================
*/
func TestSideTalkRecordsItsLineThroughFinish(t *testing.T) {
	for _, refused := range []bool{false, true} {
		c := testCharacter()
		rt := selectTestRuntime(c)
		rt.NpcSpawn = enterworld.NpcSpawnConfig{Enabled: true, AtPlayer: true}
		npc := rt.NpcRoster[0]
		rt.Selected.Set(testDivision, c.Name, npc.ObjectID)
		rt.NpcDialogs.Put(testDivision, c.Name, npcDialogSession{NpcGID: npc.ObjectID, NpcCode: npc.Codename, DefaultSymbol: "BASE",
			Stage: npcDialogOptions, Options: []NpcQuestOption{{Codename: "side-talk:QUEST~1", PromptSymbol: "SLAVE", SideTalk: true}}})
		var heard []string
		rt.NpcQuests.Finish = func(_ *enterworld.Character, codename, npcCodename string) ([]wire.Frame, error) {
			heard = append(heard, codename+"@"+npcCodename)
			if refused {
				return nil, errors.New("already heard")
			}
			return nil, nil
		}
		frames, refusal := rt.HandleNpcDialogResponse(testDivision, c, []byte{5})
		want := "SLAVE"
		if refused {
			want = "BASE"
		}
		if refusal != "" || len(frames) != 1 || string(frames[0].Payload) != string(wire.EncodeNpcDialogSymbol(want)) {
			t.Fatalf("refused=%v: %v %q", refused, frames, refusal)
		}
		if len(heard) != 1 || heard[0] != "side-talk:QUEST~1@"+npc.Codename {
			t.Fatalf("side talk recorded %v", heard)
		}
		if _, ok := rt.NpcDialogs.Get(testDivision, c.Name); ok {
			t.Fatal("side talk kept the conversation open")
		}
	}
}

func TestQuestAcceptanceAndDenialUseAuthoredResponses(t *testing.T) {
	for _, choice := range []byte{2, 3} {
		c := testCharacter()
		rt := selectTestRuntime(c)
		rt.NpcSpawn = enterworld.NpcSpawnConfig{Enabled: true, AtPlayer: true}
		npc := rt.NpcRoster[0]
		rt.Selected.Set(testDivision, c.Name, npc.ObjectID)
		rt.NpcDialogs.Put(testDivision, c.Name, npcDialogSession{NpcGID: npc.ObjectID, NpcCode: npc.Codename, DefaultSymbol: "BASE", Stage: npcDialogConfirm, Pending: NpcQuestOption{Codename: "QUEST", AcceptResponseSymbol: "ACCEPT", DenyResponseSymbol: "DENY"}})
		calls := 0
		rt.NpcQuests.Accept = func(*enterworld.Character, string) ([]wire.Frame, error) { calls++; return nil, nil }
		frames, refusal := rt.HandleNpcDialogResponse(testDivision, c, []byte{choice})
		want, count := "DENY", 0
		if choice == 2 {
			want, count = "ACCEPT", 1
		}
		if refusal != "" || len(frames) != 1 || string(frames[0].Payload) != string(wire.EncodeNpcDialogSymbol(want)) || calls != count {
			t.Fatalf("choice %d: %v %q calls %d", choice, frames, refusal, calls)
		}
		if _, exists := rt.NpcDialogs.Get(testDivision, c.Name); exists {
			t.Fatal("response retained stale confirmation")
		}
	}
}

func TestNpcEligibilityRefusalConfirmClosesAndReselectionRefreshesOffers(t *testing.T) {
	c := testCharacter()
	rt := selectTestRuntime(c)
	rt.NpcSpawn = enterworld.NpcSpawnConfig{Enabled: true, AtPlayer: true}
	rt.NpcRoster[0].TalkFlags |= simulation.NpcTalkFlagTalk
	rt.NpcRoster[0].BaseSpeechSymbol = "NPC_BS"
	npc := rt.NpcRoster[0]
	rt.Selected.Set(testDivision, c.Name, npc.ObjectID)
	eligible, calls := true, 0
	rt.NpcQuests.Options = func(string, *enterworld.Character, string) []NpcQuestOption {
		if !eligible {
			return nil
		}
		return []NpcQuestOption{{Codename: "Q_TEST", TitleSymbol: "SN_Q_TEST", PromptSymbol: "SN_TALK_Q_TEST"}}
	}
	rt.NpcQuests.Accept = func(*enterworld.Character, string) ([]wire.Frame, error) {
		calls++
		return nil, errors.New("prerequisite changed after offer")
	}
	if _, refusal := rt.HandleNpcAction(testDivision, c, npcActionBody(npc.ObjectID, simulation.NpcTalkFlagTalk)); refusal != "" {
		t.Fatal(refusal)
	}
	if _, refusal := rt.HandleNpcDialogResponse(testDivision, c, []byte{5}); refusal != "" {
		t.Fatal(refusal)
	}
	eligible = false
	frames, refusal := rt.HandleNpcDialogResponse(testDivision, c, []byte{2})
	if refusal != "" || len(frames) != 1 || string(frames[0].Payload) != string(wire.EncodeNpcDialogSymbol("NPC_BS")) {
		t.Fatalf("refusal left client waiting: %v %q", frames, refusal)
	}
	if _, exists := rt.NpcDialogs.Get(testDivision, c.Name); exists {
		t.Fatal("stale confirmation survived")
	}
	if _, refusal := rt.HandleNpcDialogResponse(testDivision, c, []byte{2}); refusal == "" || calls != 1 {
		t.Fatal("duplicate Yes reached mutation owner")
	}
	frames, refusal = rt.HandleNpcDialogResponse(testDivision, c, []byte{1})
	if refusal != "" || len(frames) != 1 || frames[0].Opcode != wire.OpTalkCloseResult || string(frames[0].Payload) != "\x01" {
		t.Fatalf("Confirm did not close informational dialogue: %v %q", frames, refusal)
	}
	if _, selected := rt.Selected.Get(testDivision, c.Name); selected {
		t.Fatal("Confirm retained server selection")
	}
	if duplicate, reason := rt.HandleNpcDialogResponse(testDivision, c, []byte{1}); len(duplicate) != 0 || reason == "" {
		t.Fatal("duplicate Confirm reopened conversation")
	}
	eligible = true
	rt.Selected.Set(testDivision, c.Name, npc.ObjectID)
	frames, refusal = rt.HandleNpcAction(testDivision, c, npcActionBody(npc.ObjectID, simulation.NpcTalkFlagTalk))
	if refusal != "" || len(frames) != 1 || frames[0].Payload[0] != 4 {
		t.Fatalf("Reselection did not rebuild eligible offers: %v %q", frames, refusal)
	}
	rt.NpcDialogs.Clear(testDivision, c.Name)
	rt.Selected.Clear(testDivision, c.Name)
	if frames, refusal := rt.HandleNpcDialogResponse(testDivision, c, []byte{1}); refusal == "" || len(frames) != 0 {
		t.Fatal("Confirm reopened an unselected NPC")
	}
}

func (fullQuestBag) Error() string          { return "inventory full" }
func (fullQuestBag) DialogueSymbol() string { return "SN_TALK_QNO_CH_POTION_1_05" }

func TestNpcRewardRefusalUsesAuthoredTextAndReopenCanRetry(t *testing.T) {
	c := testCharacter()
	rt := selectTestRuntime(c)
	rt.NpcSpawn = enterworld.NpcSpawnConfig{Enabled: true, AtPlayer: true}
	rt.NpcRoster[0].TalkFlags |= simulation.NpcTalkFlagTalk
	npc := rt.NpcRoster[0]
	rt.Selected.Set(testDivision, c.Name, npc.ObjectID)
	rt.NpcDialogs.Put(testDivision, c.Name, npcDialogSession{NpcGID: npc.ObjectID, NpcCode: npc.Codename, Stage: npcDialogConfirm, Pending: NpcQuestOption{Codename: "Q_TEST", Complete: true}})
	calls := 0
	rt.NpcQuests.Finish = func(_ *enterworld.Character, code, npcCode string) ([]wire.Frame, error) {
		if code != "Q_TEST" || npcCode != npc.Codename {
			t.Fatal("lost selected NPC identity")
		}
		calls++
		return nil, fullQuestBag{}
	}
	frames, refusal := rt.HandleNpcDialogResponse(testDivision, c, []byte{2})
	if refusal != "" || len(frames) != 1 || string(frames[0].Payload) != string(wire.EncodeNpcDialogSymbol(fullQuestBag{}.DialogueSymbol())) {
		t.Fatalf("inventory refusal disappeared: %v %q", frames, refusal)
	}
	if calls != 1 {
		t.Fatal("reward owner not called")
	}
	if _, exists := rt.NpcDialogs.Get(testDivision, c.Name); exists {
		t.Fatal("stale confirmation survived refusal")
	}
	if _, refusal := rt.HandleNpcDialogResponse(testDivision, c, []byte{2}); refusal == "" || calls != 1 {
		t.Fatal("replayed stale confirmation")
	}
}

func npcActionBody(gid, mask uint32) []byte {
	return wire.NewWriter(8).U32(gid).U32(mask).Payload()
}

func TestNpcDialogQuestSessionIsSelectionBound(t *testing.T) {
	character := testCharacter()
	rt := selectTestRuntime(character)
	rt.NpcSpawn = enterworld.NpcSpawnConfig{Enabled: true, AtPlayer: true}
	rt.NpcRoster[0].BaseSpeechSymbol = "NPC_BS"
	rt.NpcRoster[0].QuestSpeechSymbol = "NPC_PS"
	rt.NpcRoster[0].TalkFlags |= simulation.NpcTalkFlagTalk
	gid := rt.NpcRoster[0].ObjectID
	rt.Selected.Set(testDivision, character.Name, gid)

	accepted := ""
	rt.NpcQuests = NpcQuestHooks{
		Options: func(string, *enterworld.Character, string) []NpcQuestOption {
			return []NpcQuestOption{{Codename: "Q_TEST", TitleSymbol: "SN_Q_TEST", PromptSymbol: "SN_TALK_Q_TEST"}}
		},
		Accept: func(_ *enterworld.Character, codename string) ([]wire.Frame, error) {
			accepted = codename
			return []wire.Frame{{Opcode: 0x31ED, Payload: []byte{1}}}, nil
		},
	}

	frames, refusal := rt.HandleNpcAction(testDivision, character, npcActionBody(gid, simulation.NpcTalkFlagTalk))
	if refusal != "" || len(frames) != 1 || frames[0].Opcode != wire.OpNpcDialog || frames[0].Payload[0] != 4 {
		t.Fatalf("talk open = frames %#v refusal %q, want kind-4", frames, refusal)
	}
	frames, refusal = rt.HandleNpcDialogResponse(testDivision, character, []byte{5})
	if refusal != "" || len(frames) != 1 || frames[0].Payload[0] != 3 || accepted != "" {
		t.Fatalf("option = frames %#v refusal %q accepted %q, want confirm only", frames, refusal, accepted)
	}
	frames, refusal = rt.HandleNpcDialogResponse(testDivision, character, []byte{2})
	if refusal != "" || accepted != "Q_TEST" || len(frames) != 2 || frames[0].Opcode != 0x31ED || frames[1].Payload[0] != 1 {
		t.Fatalf("confirm = frames %#v refusal %q accepted %q", frames, refusal, accepted)
	}

	// Re-open, then replace selection. A delayed response must not mutate.
	rt.Selected.Set(testDivision, character.Name, gid)
	_, _ = rt.HandleNpcAction(testDivision, character, npcActionBody(gid, simulation.NpcTalkFlagTalk))
	rt.Selected.Set(testDivision, character.Name, gid+1)
	accepted = ""
	if _, refusal = rt.HandleNpcDialogResponse(testDivision, character, []byte{5}); refusal == "" || accepted != "" {
		t.Fatalf("stale response refusal=%q accepted=%q", refusal, accepted)
	}
}

/*
================
TestQuestOfferPagesTurnBeforeTheQuestion

Rahid 5's offer pages through four story prompts, each with one reply row,
before the authored offer asks for acceptance. Only the reply row turns a
page; acceptance is reachable only after the last page.
================
*/
func TestQuestOfferPagesTurnBeforeTheQuestion(t *testing.T) {
	c := testCharacter()
	rt := selectTestRuntime(c)
	rt.NpcSpawn = enterworld.NpcSpawnConfig{Enabled: true, AtPlayer: true}
	npc := rt.NpcRoster[0]
	rt.Selected.Set(testDivision, c.Name, npc.ObjectID)
	pages := []NpcDialogPage{{PromptSymbol: "P1", ReplySymbol: "R1"}, {PromptSymbol: "P2", ReplySymbol: "R2"}}
	rt.NpcDialogs.Put(testDivision, c.Name, npcDialogSession{NpcGID: npc.ObjectID, NpcCode: npc.Codename, DefaultSymbol: "BASE",
		Stage: npcDialogOptions, Options: []NpcQuestOption{{Codename: "QUEST", PromptSymbol: "OFFER", AcceptResponseSymbol: "ACCEPT", Pages: pages}}})
	accepted := 0
	rt.NpcQuests.Accept = func(*enterworld.Character, string) ([]wire.Frame, error) { accepted++; return nil, nil }
	for _, page := range pages {
		frames, refusal := rt.HandleNpcDialogResponse(testDivision, c, []byte{npcDialogFirstRow})
		want := wire.EncodeNpcDialogOptions(page.PromptSymbol, []string{page.ReplySymbol})
		if refusal != "" || len(frames) != 1 || string(frames[0].Payload) != string(want) {
			t.Fatalf("page %s: %v %q", page.PromptSymbol, frames, refusal)
		}
		if _, refusal := rt.HandleNpcDialogResponse(testDivision, c, []byte{2}); refusal == "" || accepted != 0 {
			t.Fatal("a page accepted the quest")
		}
	}
	frames, refusal := rt.HandleNpcDialogResponse(testDivision, c, []byte{npcDialogFirstRow})
	if refusal != "" || len(frames) != 1 || string(frames[0].Payload) != string(wire.EncodeNpcDialogConfirm("OFFER")) {
		t.Fatalf("last page did not open the offer: %v %q", frames, refusal)
	}
	frames, refusal = rt.HandleNpcDialogResponse(testDivision, c, []byte{2})
	if refusal != "" || accepted != 1 || string(frames[len(frames)-1].Payload) != string(wire.EncodeNpcDialogSymbol("ACCEPT")) {
		t.Fatalf("offer after pages did not accept: %v %q %d", frames, refusal, accepted)
	}
}

/*
================
TestQuestOfferAcceptRowAcceptsWithoutAQuestion

QNO_WC_POTION_4's _01 shows one NEXT row and NEXT accepts (897680): no
yes/no confirm opens, and only that row is answered.
================
*/
func TestQuestOfferAcceptRowAcceptsWithoutAQuestion(t *testing.T) {
	c := testCharacter()
	rt := selectTestRuntime(c)
	rt.NpcSpawn = enterworld.NpcSpawnConfig{Enabled: true, AtPlayer: true}
	npc := rt.NpcRoster[0]
	rt.Selected.Set(testDivision, c.Name, npc.ObjectID)
	rt.NpcDialogs.Put(testDivision, c.Name, npcDialogSession{NpcGID: npc.ObjectID, NpcCode: npc.Codename, DefaultSymbol: "BASE",
		Stage: npcDialogOptions, Options: []NpcQuestOption{{Codename: "QUEST", PromptSymbol: "OFFER", AcceptResponseSymbol: "ACCEPT", AcceptRowSymbol: "NEXT"}}})
	accepted := 0
	rt.NpcQuests.Accept = func(*enterworld.Character, string) ([]wire.Frame, error) { accepted++; return nil, nil }
	frames, refusal := rt.HandleNpcDialogResponse(testDivision, c, []byte{npcDialogFirstRow})
	if refusal != "" || len(frames) != 1 || string(frames[0].Payload) != string(wire.EncodeNpcDialogOptions("OFFER", []string{"NEXT"})) {
		t.Fatalf("offer did not show its accept row: %v %q", frames, refusal)
	}
	if _, refusal := rt.HandleNpcDialogResponse(testDivision, c, []byte{npcDialogConfirmYes}); refusal == "" || accepted != 0 {
		t.Fatal("a confirm yes answered the accept row")
	}
	frames, refusal = rt.HandleNpcDialogResponse(testDivision, c, []byte{npcDialogFirstRow})
	if refusal != "" || accepted != 1 || string(frames[len(frames)-1].Payload) != string(wire.EncodeNpcDialogSymbol("ACCEPT")) {
		t.Fatalf("the accept row did not accept: %v %q %d", frames, refusal, accepted)
	}
	if _, open := rt.NpcDialogs.Get(testDivision, c.Name); open {
		t.Fatal("acceptance kept the conversation open")
	}
}
