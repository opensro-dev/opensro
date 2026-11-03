package quest

import (
	"encoding/hex"
	"encoding/json"
	"opensro.online/server/internal/testsupport/clientdata"
	"os"
	"testing"
)

func TestTutorialWireFixtureMatchesAuthorityStages(t *testing.T) {
	clientdata.RequireGameData(t)
	data, err := os.ReadFile("tutorial_wire_fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		QuestID uint32
		Frames  []struct {
			Stage       uint16
			Progress    uint32
			Op          byte
			Description string
			PayloadHex  string
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	root, _ := loadTestDefinitions(t).ByCodename("QTUTORIAL_CH")
	for _, row := range fixture.Frames {
		var payload []byte
		if row.Op == QuestUpdateOpComplete {
			payload = EncodeQuestUpdateComplete(root.RefID)
		} else {
			def, ok := definitionAtStage(root, row.Stage)
			if !ok || def.ContentsSymbol != row.Description {
				t.Fatal("stage identity mismatch")
			}
			record := BuildActiveQuestRecord(def, row.Progress)
			if row.Op == QuestUpdateOpInsert {
				payload = EncodeQuestUpdateInsert(record)
			} else {
				payload = EncodeQuestUpdateUpdate(record)
			}
		}
		if hex.EncodeToString(payload) != row.PayloadHex {
			t.Fatalf("stage %d op %d wire differs from shared client fixture", row.Stage, row.Op)
		}
	}
}
