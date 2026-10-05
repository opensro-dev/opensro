/*
===========================================================================

npcworlddata_test.go - the shipped NPC world roster

Conversation and shop authority, merchant tabs and quest greetings of the
shipped NPC roster.

===========================================================================
*/
package simulation

import (
	"path/filepath"
	"testing"

	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestShippedNpcWorldRosterCarriesConversationAndShopAuthority
================
*/
func TestShippedNpcWorldRosterCarriesConversationAndShopAuthority(t *testing.T) {
	dir := licensed.RetailTextdataDir(t)
	roster := LoadNpcWorldRoster(dir)
	if len(roster) < 150 {
		t.Skipf("shipped NPC world data unavailable (loaded %d rows)", len(roster))
	}
	byCode := make(map[string]NpcDef)
	for _, npc := range roster {
		if npc.ObjectID == 0 || !npc.AuthoredSpawn {
			t.Fatalf("%s is not a stable authored world row: %+v", npc.Codename, npc)
		}
		byCode[npc.Codename] = npc
	}
	lipria := byCode["NPC_EU_ADVICE"]
	if lipria.BaseSpeechSymbol != "SN_NPC_EU_ADVICE_BS" || lipria.TalkFlags&NpcTalkFlagTalk == 0 {
		t.Fatalf("Lipria conversation contract = %+v", lipria)
	}
	for _, code := range []string{"NPC_EU_SMITH", "NPC_CH_SMITH", "NPC_EU_ARMOR"} {
		npc := byCode[code]
		if len(npc.NpcTalkStoreGroups) == 0 || npc.TalkFlags&NpcTalkFlagShop == 0 {
			t.Fatalf("%s has no authored shop projection: %+v", code, npc)
		}
		if got := npc.NpcTalkStoreGroups[0].StoreGroupID; got != npc.RefObjID {
			t.Fatalf("%s menu storeGroupId0c = %d, want character RefObjID %d", code, got, npc.RefObjID)
		}
	}
}

/*
================
TestPublishedMerchantBranchesCoverAllTabs
================
*/
func TestPublishedMerchantBranchesCoverAllTabs(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	roster := LoadNpcWorldRoster(dir)
	if len(roster) < 150 {
		t.Fatal("published merchant roster missing")
	}
	observed := map[string]int{}
	for _, npc := range roster {
		branches := map[int32]int{}
		for _, group := range npc.NpcTalkStoreGroups {
			for _, tab := range group.Tabs {
				if tab.GroupID <= 0 || tab.GroupLabelSymbol == "" {
					t.Fatalf("%s has ungrouped tab %+v", npc.Codename, tab)
				}
				branches[tab.GroupID]++
				if branches[tab.GroupID] > 4 {
					t.Fatalf("%s branch %d exceeds authored tab strip", npc.Codename, tab.GroupID)
				}
			}
		}
		observed[npc.Codename] = len(branches)
	}
	for code, count := range map[string]int{"NPC_CH_ARMOR": 2, "NPC_EU_ARMOR": 2, "NPC_KT_ARMOR": 4} {
		if observed[code] != count {
			t.Fatalf("%s branches=%d want %d", code, observed[code], count)
		}
	}
}

/*
================
TestNpcQuestGreetingsNeverBorrowMissingNpcChatPS
================
*/
func TestNpcQuestGreetingsNeverBorrowMissingNpcChatPS(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	roster := LoadNpcWorldRoster(dir)
	text := map[string]bool{}
	for _, row := range readNpcTabbed(filepath.Join(dir, "textquest.txt")) {
		if len(row) > 2 {
			text[row[1]] = true
		}
	}
	found := 0
	for _, npc := range roster {
		if npc.QuestSpeechSymbol != "" && !text[npc.QuestSpeechSymbol] {
			t.Fatalf("%s unresolved greeting %s", npc.Codename, npc.QuestSpeechSymbol)
		}
		if npc.Codename == "NPC_EU_ADVICE" || npc.Codename == "NPC_EU_ADVICE2" || npc.Codename == "NPC_EU_ADVICE3" {
			found++
			if npc.QuestSpeechSymbol != "SN_"+npc.Codename+"_QS" {
				t.Fatalf("guide greeting %+v", npc)
			}
		}
	}
	if found != 3 {
		t.Fatalf("guide coverage %d", found)
	}
}
