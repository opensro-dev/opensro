/*
===========================================================================

npcstyria_test.go - the arena manager stands only while its switch is on

===========================================================================
*/
package simulation

import (
	"testing"

	"opensro.online/server/internal/game/world/styria"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestArenaManagerStandsOnlyWhenEnabled

Port-only (#3648): with SRO_STYRIA_CLASH on, the roster gains the
arena manager at its six authored stands, carrying the manager
service and a talk row; off, no stand exists and the roster is the
shipped one.
================
*/
func TestArenaManagerStandsOnlyWhenEnabled(t *testing.T) {
	clientdata.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)

	t.Setenv(styria.Env, "off")
	for _, npc := range LoadNpcWorldRoster(dir) {
		if npc.Codename == styria.ManagerCodename {
			t.Fatal("the arena manager stands while the switch is off")
		}
	}

	t.Setenv(styria.Env, "on")
	stands := 0
	for _, npc := range LoadNpcWorldRoster(dir) {
		if npc.Codename != styria.ManagerCodename {
			continue
		}
		stands++
		if npc.RefObjID != styria.ManagerRefObjID {
			t.Fatalf("manager ref = %d, want %d", npc.RefObjID, styria.ManagerRefObjID)
		}
		if !npc.Services.Has(NpcServiceArenaManager) {
			t.Fatalf("manager services = %v, want the arena manager service", npc.Services)
		}
		if npc.TalkFlags&NpcTalkFlagTalk == 0 {
			t.Fatal("the manager offers no talk row")
		}
		if npc.BaseSpeechSymbol != styria.ManagerSpeech {
			t.Fatalf("manager speech = %q, want %q", npc.BaseSpeechSymbol, styria.ManagerSpeech)
		}
	}
	if stands != len(styria.ManagerPlacements) {
		t.Fatalf("manager stands = %d, want %d", stands, len(styria.ManagerPlacements))
	}
}
