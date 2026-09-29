/*
===========================================================================

englishCorrections.mjs - reviewed corrections to authored v1.150 English

The client catalogs and server projection share this policy. Match both the
symbol and original cell so changed source data cannot silently receive an
obsolete correction. Native captures and the extracted archive stay intact.

===========================================================================
*/

const CORRECTIONS = {
	"textdataname.txt": {
		SN_NPC_CH_SOLDIER_EA2: [ "Solder Sangnam [Teleport]", "Soldier Sangnam [Teleport]" ],
		// The Korean source says southern landing/dock, not a rock formation.
		SN_ZONE_25031_2: [ "Karakoram South Sock", "Karakoram South Dock" ],
		SN_ITEM_QNO_CH_EUROPE_3_02: [ "Blood Devil 's leaf", "Blood Devil's leaf" ]
	},
	"textquest.txt": {
		SN_CON_QSP_ALL_POTION_3_01: [ "Collect  Purification Seed (%d)", "Collect Purification Seed (%d)" ],
		SN_CON_QSP_ALL_POTION_4: [ "Collect  Purification Fruit (%d)", "Collect Purification Fruit (%d)" ]
	}
};

/*
================
correctedEnglish

Accept already-corrected projections too; applying the build twice must not
change output. New source wording requires a human review of this table.
================
*/
export function correctedEnglish( fileName, key, english ) {
	const correction = CORRECTIONS[fileName.toLowerCase()]?.[key];
	if ( !correction ) return english;
	const [before, after] = correction;
	if ( english === after ) return english;
	if ( english !== before ) throw new Error( `Stale English correction ${fileName}:${key}` );
	return after;
}
