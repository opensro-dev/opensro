package enterworld

import (
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

// DefaultSiegeFortressDataRows is the exact enabled v1.150 projection of
// textdata/siegefortress.txt loaded by client 7F22D0 case 0x36 into
// CGlobalDataManager+0x488.
//
// The last source column is deliberately retained: client 5D8930 resolves the
// selected fortress official's effective RefObj codename and client 7D4B90
// matches it against the record's final +0x20 wstring (character data begins
// at +0x24). Dropping that column makes the 0x34 fortress-application row a
// silent no-op even though the NPC and menu render correctly.
func DefaultSiegeFortressDataRows() []SiegeFortressDataRow {
	return []SiegeFortressDataRow{
		{
			FortressID:      1,
			Icon:            "etc/fort_jangan.ddj",
			CodeName:        "FORTRESS_JANGAN",
			NameStrID:       "SN_FORTRESS_JANGAN",
			OfficialNpcCode: "NPC_CH_FORTRESS_OFFICIAL",
			RequestFee:      5000000,
			TaxTargets:      63,
		},
	}
}

// constantinopleSiegeFortressRow is the v1.696 siegefortress.txt
// Constantinople row (id 4), the projection DefaultSiegeFortressDataRows
// makes for Jangan. Port-only, not native: it joins the list only while
// SRO_FORT_CONSTANTINOPLE is on, the switch that also builds the
// fortress's regions and event zones; off is exactly the shipped list.
var constantinopleSiegeFortressRow = SiegeFortressDataRow{
	FortressID:      4,
	Icon:            "etc/fort_constantinople.ddj",
	CodeName:        "FORTRESS_CONSTANTINOPLE",
	NameStrID:       "SN_FORTRESS_CONSTANTINOPLE",
	OfficialNpcCode: "NPC_EU_FORTRESS_OFFICIAL",
	RequestFee:      5000000,
	TaxTargets:      63,
}

/*
================
siegeFortressRows

The shipped rows with each official's RefObjID resolved from character
data. While the Constantinople switch is on the fortress joins the
list (port-only, not native); the authority catalog and the world's
structures come from the built tables, not from this projection.
================
*/
func siegeFortressRows(deps *Deps) []SiegeFortressDataRow {
	rows := DefaultSiegeFortressDataRows()
	if monster.ConstantinopleEnabled() {
		rows = append(rows, constantinopleSiegeFortressRow)
	}
	if deps == nil {
		return rows
	}
	characters, ok := deps.Items.(CharacterRefSource)
	if !ok {
		return rows
	}
	for i := range rows {
		if ref, found := characters.CharacterRefByCodename(rows[i].OfficialNpcCode); found && ref != nil {
			rows[i].OfficialRefObjID = ref.RefObjID
		}
	}
	return rows
}

// DefaultGameWorldDataRows projects the browser's fortress-name fields from
// the complete shipped catalog, including its native allocation inputs.
func DefaultGameWorldDataRows() []GameWorldDataRow {
	definitions := instance.Shipped()
	rows := make([]GameWorldDataRow, len(definitions))
	for i, definition := range definitions {
		rows[i] = GameWorldDataRow{GameWorldID: uint16(definition.ID), CodeName: definition.CodeName, WarName: definition.Strings[0]}
	}
	return rows
}
