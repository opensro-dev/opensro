/*
===========================================================================

creatable_monsters_test.go - the bake roster is every creatable monster

The GameWorld's populated set (nests and the unique summon closure) and
every script spawn must be inside the roster the bake reads, or the client
admits an entity it cannot draw (#369).

===========================================================================
*/
package main

import (
	"encoding/json"
	"io"
	"opensro.online/server/internal/testsupport/tier"
	"os"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/gamedata"
	"opensro.online/server/internal/testsupport/clientdata"
)

/*
================
TestCreatableRosterHoldsEveryRuntimeMonster
================
*/
func TestCreatableRosterHoldsEveryRuntimeMonster(t *testing.T) {
	tier.Long(t, "every runtime monster is creatable")
	clientdata.RequireGameData(t)
	dir, err := gamedata.ResolveTextdataDir()
	if err != nil {
		t.Fatal(err)
	}
	template := monster.LoadTemplate(dir)
	creatable := map[uint32]bool{}
	for _, ref := range template.CreatableRefs() {
		if ref.Structure {
			t.Fatalf("fortress structure %s in the monster roster", ref.Codename)
		}
		creatable[ref.RefObjID] = true
	}
	withSummons, err := enterworld.WithMonsterSummonReferences(template, enterworld.NewTextdataSkills(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range withSummons.SpawnableRefs() {
		if !creatable[ref.RefObjID] {
			t.Fatalf("populated monster %s is not creatable", ref.Codename)
		}
	}
	// Script spawns and GM-only rows: quest guardians, STRONG and level variants.
	byName := map[string]bool{}
	for _, ref := range template.CreatableRefs() {
		byName[ref.Codename] = true
	}
	for _, codename := range []string{"MOB_QT_01_ONG", "MOB_QT_02_PUNISHER_CLON", "MOB_QT_01_LADON", "MOB_CH_STRONG_TIGER", "MOB_CH_TIGERWOMAN_L2"} {
		if !byName[codename] {
			t.Fatalf("%s is not in the creatable roster", codename)
		}
	}
}

/*
================
TestCreatableMonstersQinShiTombGate

The roster export the bake reads (runCreatableMonsters) lists the Qin-Shi
Tomb references only with SRO_QINSHI_TOMB on, mirroring the GameWorld's
WithQinShiTomb append; flag off it is the retail table alone, so a flag-off
bake and a flag-off server agree on what can appear.
================
*/
func TestCreatableMonstersQinShiTombGate(t *testing.T) {
	tier.Long(t, "every Qin-Shi tomb monster is creatable behind its gate")
	clientdata.RequireGameData(t)

	run := func() monsterRosterOutput {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		drained := make(chan []byte, 1)
		go func() {
			data, _ := io.ReadAll(reader)
			drained <- data
		}()
		previous := os.Stdout
		os.Stdout = writer
		runErr := runCreatableMonsters(nil)
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		os.Stdout = previous
		if runErr != nil {
			t.Fatal(runErr)
		}
		var output monsterRosterOutput
		if err := json.Unmarshal(<-drained, &output); err != nil {
			t.Fatal(err)
		}
		return output
	}
	tombRefs := func(output monsterRosterOutput) int {
		count := 0
		for _, ref := range output.Refs {
			if strings.HasPrefix(ref.Codename, "MOB_TQ_") || strings.HasPrefix(ref.Codename, "MOB_QT_TQ_") {
				count++
			}
		}
		return count
	}

	t.Setenv("SRO_QINSHI_TOMB", "")
	off := run()
	if off.Count == 0 || tombRefs(off) != 0 {
		t.Fatalf("flag off: %d refs, %d of them Tomb", off.Count, tombRefs(off))
	}

	t.Setenv("SRO_QINSHI_TOMB", "on")
	on := run()
	if tombRefs(on) == 0 || on.Count <= off.Count {
		t.Fatalf("flag on: %d refs (%d Tomb) after %d flag off", on.Count, tombRefs(on), off.Count)
	}
	byName := map[string]bool{}
	for _, ref := range on.Refs {
		byName[ref.Codename] = true
	}
	if !byName["MOB_TQ_TOMBSOLDIER"] {
		t.Fatal("flag on: MOB_TQ_TOMBSOLDIER is not in the creatable roster")
	}
}
