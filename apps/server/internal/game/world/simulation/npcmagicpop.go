/*
===========================================================================

npcmagicpop.go - the Magic Pop machine placements missing from npcpos.txt

The v1.150 npcpos.txt table places no NPC_CH_GACHA_MACHINE (RefObj 9251), so
the shipped world had Magic Pop tickets and a working roll path but no machine
to hand them to. The later server places the machine through its nest table
(Tab_RefNest); this file carries those placements for the towns v1.150 has and
appends them to the NPC world roster beside the npcpos rows.

===========================================================================
*/
package simulation

const (
	// magicPopMachineCodename is RefObj 9251, the machine the Magic Pop
	// reference data binds (see npcGachaMachines).
	magicPopMachineCodename = "NPC_CH_GACHA_MACHINE"

	// magicPopGIDBase starts the machine band inside the static NPC range:
	// above any npcpos row (the roster is a few hundred NPCs) and below the
	// teleport gates at 250000 + RefObjID. Identities stay stable when npcpos
	// rows are added.
	magicPopGIDBase uint32 = 240000

	// magicPopOperatorCodename is RefObj 9252, Exchanger Gori, who swaps a result
	// voucher for its recorded reward (talk row 0x28).
	magicPopOperatorCodename = "NPC_CH_GACHA_OPERATOR"

	// magicPopOperatorGIDBase starts Gori's band above the machines' four ids.
	magicPopOperatorGIDBase uint32 = magicPopGIDBase + 16
)

// worldNpcLookups are the reference joins every static NPC row shares,
// loaded once per roster build.
type worldNpcLookups struct {
	fortresses    map[string]uint32
	storeGroups   map[string][]NpcTalkStoreGroup
	fixtureByCode map[string]NpcDef
}

/*
================
magicPopPlacements

Tab_RefNest placements of NPC_CH_GACHA_MACHINE in the later retail server's nest
table, restricted to the towns that exist in v1.150: Jangan (25000), Donwhang
(26265), Hotan (23687) and Samarkand (27244). The fifth later placement, region
23088, is a town the v1.150 world has no NPC in, so it is left out. Every
position has a recovered facing in npc_headings_generated.go, and the
published-roster heading test keeps them joined.

INFERENCE: the later nest table is the evidence that the original placed one
machine per town; the v1.150 positions are taken from it unchanged.
================
*/
var magicPopPlacements = [...]Spawn{
	{RegionID: 25000, X: 1612.97, Y: 0, Z: 1193.0},
	{RegionID: 26265, X: 543.91, Y: -106.74, Z: 1533.36},
	{RegionID: 23687, X: 1652.38, Y: 243.82, Z: 776.57},
	{RegionID: 27244, X: 913.69, Y: 180, Z: 1828.76},
}

/*
================
magicPopOperatorPlacements

Tab_RefNest placements of NPC_CH_GACHA_OPERATOR, taken from the facings the same
later table recovered (npc_headings_generated.go keeps position and facing
joined at a tenth of a unit). The same four v1.150 towns as the machines; the
fifth, region 23088, is left out for the reason given above.

INFERENCE: as for the machines, the v1.150 positions are the later nest table's
unchanged. Gori stands at his own nest position, not beside the machine.
================
*/
var magicPopOperatorPlacements = [...]Spawn{
	{RegionID: 25000, X: 977.3, Y: -32.6, Z: 727.6},
	{RegionID: 26265, X: 1162.7, Y: -106.7, Z: 1385.8},
	{RegionID: 23687, X: 1002.8, Y: 244.0, Z: 489.6},
	{RegionID: 27244, X: 586.7, Y: 180, Z: 1414.7},
}

/*
================
newWorldNpc

One static NPC row from its reference data and placement: identity, facing,
shop groups and the service and talk words derived from them.
================
*/
func newWorldNpc(ref npcWorldRef, objectID uint32, spawn Spawn, lookups worldNpcLookups) NpcDef {
	spawn.Angle, _ = npcPlacementHeading(ref.codename, spawn.RegionID, spawn.X, spawn.Y, spawn.Z)
	row := NpcDef{
		ObjectID:          objectID,
		RefObjID:          ref.refObjID,
		FortressID:        lookups.fortresses[ref.fortress],
		TidWord:           ref.tidWord,
		Codename:          ref.codename,
		NameStrID:         ref.nameID,
		Name:              ref.name,
		ModelPath:         ref.model,
		Level:             ref.level,
		MaxHP:             ref.maxHP,
		BaseSpeechSymbol:  ref.base,
		QuestSpeechSymbol: ref.quest,
		AuthoredSpawn:     true,
		Spawn:             spawn,
		WalkSpeed:         ref.walk,
		RunSpeed:          ref.run,
		ScaleDenom:        ref.scale,
	}
	row.NpcTalkStoreGroups = cloneNpcStoreGroups(lookups.storeGroups[row.Codename])
	// Rebirth destinations are server gameplay policy and are not present in
	// the four shipped refshop tables. Preserve the pinned guide policy only;
	// shop rows are always rebuilt from authored data above.
	if pinned, found := lookups.fixtureByCode[row.Codename]; found {
		row.RebirthPoint = pinned.RebirthPoint
	}
	row.Services = ResolveNpcServices(row)
	row.TalkFlags = ResolveNpcTalkFlags(row)
	return row
}

/*
================
appendMagicPopMachines

Appends one machine and one operator per placement. A reference table without
a row (a partial data set) appends nothing for it rather than inventing a
model-less NPC.
================
*/
func appendMagicPopMachines(roster []NpcDef, refs map[uint32]npcWorldRef, lookups worldNpcLookups) []NpcDef {
	byCodename := map[string]npcWorldRef{}
	for _, ref := range refs {
		if ref.codename == magicPopMachineCodename || ref.codename == magicPopOperatorCodename {
			byCodename[ref.codename] = ref
		}
	}
	if machine, found := byCodename[magicPopMachineCodename]; found {
		for index, spawn := range magicPopPlacements {
			roster = append(roster, newWorldNpc(machine, magicPopGIDBase+uint32(index), spawn, lookups))
		}
	}
	if operator, found := byCodename[magicPopOperatorCodename]; found {
		for index, spawn := range magicPopOperatorPlacements {
			roster = append(roster, newWorldNpc(operator, magicPopOperatorGIDBase+uint32(index), spawn, lookups))
		}
	}
	return roster
}
