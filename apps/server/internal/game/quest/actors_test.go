package quest

import (
	"fmt"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"path/filepath"
	"testing"
)

// Cross-version joins use codenames, never newer numeric character IDs.
func validateQuestActors(def *Definition, codesInMedia map[string]bool) error {
	specs := []QuestSpec{def.QuestSpec}
	for _, stage := range def.Stages {
		specs = append(specs, stage.QuestSpec)
	}
	for _, spec := range specs {
		codes := []string{spec.StartNpcCodename, spec.EndNpcCodename, spec.DeliveryNpcCodename}
		codes = append(codes, spec.KillMonsterCodenames...)
		if spec.MonsterDrop != nil {
			codes = append(codes, spec.MonsterDrop.MonsterCodenames...)
		}
		for _, code := range codes {
			if code == "" {
				continue
			}
			if !codesInMedia[code] {
				return fmt.Errorf("quest %s actor %s absent from v1.150 characterdata", def.Codename, code)
			}
		}
	}
	return nil
}

// Validate against raw identity rows: the COS/monster loader legitimately
// rejects NPCs with zero health. Do this at build verification, not by parsing
// all character files a second time on every server startup.
func TestEveryQuestActorResolvesInPrimaryMedia(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	codes := make(map[string]bool)
	files, err := filepath.Glob(filepath.Join(dir, "characterdata*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		for _, row := range enterworld.ReadTextdataFile(file) {
			if len(row) > 2 && row[0] == "1" {
				codes[row[2]] = true
			}
		}
	}
	defs, err := LoadDefinitions(NewCatalog(dir), enterworld.NewTextdataItems(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) == 0 || defs.Len() == 0 {
		t.Fatal("primary media missing")
	}
	for _, def := range defs.All() {
		if err := validateQuestActors(def, codes); err != nil {
			t.Error(err)
		}
	}
}
