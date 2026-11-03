/*
===========================================================================

npcmagicpop_test.go - the Magic Pop machine is placed in the static NPC world

===========================================================================
*/
package simulation

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
TestAppendMagicPopMachinesPlacesOnePerTown

The machine row comes from reference data alone, so a synthetic reference row
proves the placements, identities, facing and Magic Pop service without the
licensed data set.
================
*/
func TestAppendMagicPopMachinesPlacesOnePerTown(t *testing.T) {
	refs := map[uint32]npcWorldRef{9251: {
		refObjID: 9251, tidWord: 0x0146, codename: magicPopMachineCodename, scale: 1,
	}, 9252: {
		refObjID: 9252, tidWord: 0x0146, codename: magicPopOperatorCodename, scale: 1,
	}}
	roster := appendMagicPopMachines(nil, refs, worldNpcLookups{})
	if len(roster) != len(magicPopPlacements)+len(magicPopOperatorPlacements) {
		t.Fatalf("appended %d NPCs, want %d", len(roster), len(magicPopPlacements)+len(magicPopOperatorPlacements))
	}
	if err := ValidateNpcRoster(roster); err != nil {
		t.Fatal(err)
	}
	for index, npc := range roster {
		placements, want := magicPopPlacements[:], NpcTalkFlagGachaMachine
		if npc.Codename == magicPopOperatorCodename {
			placements, want, index = magicPopOperatorPlacements[:], NpcTalkFlagGachaOperator, index-len(magicPopPlacements)
		}
		if npc.Spawn.RegionID != placements[index].RegionID {
			t.Fatalf("%s %d region = %d", npc.Codename, index, npc.Spawn.RegionID)
		}
		if npc.TalkFlags != want {
			t.Fatalf("NPC %d talk flags %#x, want %#x", index, npc.TalkFlags, want)
		}
		if _, found := npcPlacementHeading(npc.Codename, npc.Spawn.RegionID, npc.Spawn.X, npc.Spawn.Y, npc.Spawn.Z); !found {
			t.Fatalf("machine %d has no recovered facing", index)
		}
	}
}

/*
================
TestAppendMagicPopMachinesSkipsMissingReference
================
*/
func TestAppendMagicPopMachinesSkipsMissingReference(t *testing.T) {
	if got := appendMagicPopMachines(nil, map[uint32]npcWorldRef{}, worldNpcLookups{}); len(got) != 0 {
		t.Fatalf("appended %d machines without a reference row", len(got))
	}
}

/*
================
TestShippedWorldHasMagicPopMachines

The published roster carries the machines beside the npcpos rows: unique
identities in the machine band, each with the Magic Pop talk row, and the
npcpos identities below it are untouched.
================
*/
func TestShippedWorldHasMagicPopMachines(t *testing.T) {
	tier.Long(t, "the shipped world roster carries its magic-pop machines")
	clientdata.RequireGameData(t)
	roster := LoadNpcWorldRoster(gamedatatest.TextdataDir(t))
	regions, operators := map[uint16]bool{}, map[uint16]bool{}
	for _, npc := range roster {
		if npc.Codename == magicPopOperatorCodename {
			if npc.ObjectID < magicPopOperatorGIDBase || npc.ObjectID >= domain.GroundItemGIDBase ||
				npc.TalkFlags&NpcTalkFlagGachaOperator == 0 || npc.ModelPath == "" {
				t.Fatalf("operator %d: talk flags %#x model %q", npc.ObjectID, npc.TalkFlags, npc.ModelPath)
			}
			operators[npc.Spawn.RegionID] = true
			continue
		}
		if npc.Codename != magicPopMachineCodename {
			if npc.ObjectID >= magicPopGIDBase {
				t.Fatalf("%s id %d intrudes on the machine band", npc.Codename, npc.ObjectID)
			}
			continue
		}
		if npc.ObjectID < magicPopGIDBase || npc.ObjectID >= domain.GroundItemGIDBase {
			t.Fatalf("machine id %d outside its band", npc.ObjectID)
		}
		if npc.TalkFlags&NpcTalkFlagGachaMachine == 0 || npc.ModelPath == "" {
			t.Fatalf("machine %d: talk flags %#x model %q", npc.ObjectID, npc.TalkFlags, npc.ModelPath)
		}
		regions[npc.Spawn.RegionID] = true
	}
	for _, spawn := range magicPopPlacements {
		if !regions[spawn.RegionID] {
			t.Fatalf("no machine in region %d", spawn.RegionID)
		}
	}
	for _, spawn := range magicPopOperatorPlacements {
		if !operators[spawn.RegionID] {
			t.Fatalf("no operator in region %d", spawn.RegionID)
		}
	}
}
