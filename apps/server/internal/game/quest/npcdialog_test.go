package quest

import (
	"opensro.online/server/internal/testsupport/licensed"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

func TestEuropeanStarterQuestNpcOfferAndTalkCompletion(t *testing.T) {
	licensed.RequireGameData(t)
	defs, items := loadShippedDefinitions(t)
	rt, err := NewRuntime(&enterworld.Deps{Items: items}, defs, func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) {
		return nil, true
	})
	if err != nil {
		t.Fatal(err)
	}
	race, level := int64(enterworld.RaceEurope), int64(1)
	character := &enterworld.Character{Name: "EuStarter", RaceIndex: &race, Level: &level}

	// Lipria offers QTUTORIAL_EU only; the superseded chain stays closed.
	options := rt.OptionsForNpc(character, "NPC_EU_ADVICE")
	if len(options) != 1 || options[0].Codename != "QTUTORIAL_EU" || options[0].Complete {
		t.Fatalf("offer options = %+v", options)
	}
	if _, err := rt.StartQuest(character, "QNO_EU_TUTORIAL_1"); err == nil {
		t.Fatal("superseded tutorial accepted")
	}
	if _, err := rt.StartQuest(character, options[0].Codename); err != nil {
		t.Fatal(err)
	}
	token := stageToken("QTUTORIAL_EU", 0)
	options = rt.OptionsForNpc(character, "NPC_EU_ADVICE")
	found := false
	for _, option := range options {
		found = found || option.Codename == token
	}
	if !found {
		t.Fatalf("active options = %+v, want the first stage report", options)
	}
	if _, err := rt.AdvanceNpcQuest(character, token, "NPC_EU_ADVICE"); err != nil {
		t.Fatal(err)
	}
	if len(character.ActiveQuests) != 1 || character.ActiveQuests[0].Stage != 1 {
		t.Fatalf("quest state active=%+v", character.ActiveQuests)
	}
	if options := rt.OptionsForNpc(character, "NPC_EU_ADVICE"); len(options) != 0 {
		t.Fatalf("Lipria still offers during stage 2: %+v", options)
	}
}
