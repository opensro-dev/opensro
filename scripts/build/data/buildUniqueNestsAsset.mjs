/*
===========================================================================

buildUniqueNestsAsset.mjs - the world map's unique nests (port-only)

Port-only, not native: the v1.150 world map draws no unique nest. This step
projects the anchors the server's population template holds for uniques, so
the M window can show every candidate spawn of each unique as one sign.

The rule is the server's, read from the same tables
(apps/server/internal/game/world/monster):
	- LoadTemplate turns every npcpos.txt row (refObjID, regionID, x, y, z)
	  whose reference is a served monster into one nest;
	- LoadMonsterRefs keeps a characterdata row only when column 0 is 1 and
	  its packed TypeID word passes monsterTidGate ((W & 0x7FE) == 0x0C6);
	- LoadMonsterRefs refuses a rarity (column 15) above 0x0F, and
	  nestIsUnique (simulation/unique_states.go) reads it & 15 == 3:
	  template nests never carry an override.
The flag-gated port-only populations (Cap110, Pharaoh Tomb, temple) are
left out: the client cannot know a shard's switches, and they stand in
places the native world map does not draw.

Output: /assets/data/uniqueNests.json, { format, version, nests }, each nest
{ refObjId, regionId, x, z } in npcpos order.

===========================================================================
*/
import path from "node:path";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";
import { exportDataAsset } from "../shared/dataAssetExport.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { listTextDataShardNamesSync, readTextDataRowsSync } from "../shared/textDataIo.mjs";
import { publicRoot, retailTextdataRoot } from "../world/paths.mjs";

const FORMAT = "sro-unique-nests";
const VERSION = 1;
export const UNIQUE_NESTS_FILE = "uniqueNests.json";

// characterdata*.txt (monster/textdata.go col* constants)
const CHAR_COLUMN_SERVICE = 0;
const CHAR_COLUMN_ID = 1;
const CHAR_COLUMN_CHAR_BIT = 8;
const CHAR_COLUMN_TID1 = 9;
const CHAR_COLUMN_TID2 = 10;
const CHAR_COLUMN_TID3 = 11;
const CHAR_COLUMN_RARITY = 15;
const CHAR_COLUMN_MAX_HP = 59;
// monsterTidGate: the CICMonster class word (client 851420).
const MONSTER_TID_MASK = 0x7fe;
const MONSTER_TID_WORD = 0x0c6;
// LoadMonsterRefs keeps rarity 0..0x0F; nestIsUnique: rarity & 15 == 3.
const RARITY_MAX = 0x0f;
const RARITY_MASK = 15;
const RARITY_UNIQUE = 3;

// npcpos.txt: refObjID, regionID, x, y, z.
const NPCPOS_COLUMNS = 5;
const REGION_MASK = 0xffff;

/*
================
tidWord

The packed RefObj TypeID word (tidWordFromColumns).
================
*/
function tidWord( cols ) {
	const word = (Number( cols[CHAR_COLUMN_TID1] ) & 7) << 2 |
		(Number( cols[CHAR_COLUMN_TID2] ) & 3) << 5 |
		(Number( cols[CHAR_COLUMN_TID3] ) & 15) << 7;
	return Number( cols[CHAR_COLUMN_CHAR_BIT] ) !== 0 ? word | 2 : word;
}

/*
================
uniqueReferences

The served monster references whose rarity is unique, by RefObj id.
================
*/
export function uniqueReferences( characterRows ) {
	const ids = new Set();
	for ( const cols of characterRows ) {
		if ( cols[CHAR_COLUMN_SERVICE]?.trim() !== "1" || cols.length <= CHAR_COLUMN_MAX_HP ) continue;
		if ( (tidWord( cols ) & MONSTER_TID_MASK) !== MONSTER_TID_WORD ) continue;
		const rarity = Number( cols[CHAR_COLUMN_RARITY] );
		if ( !Number.isInteger( rarity ) || rarity < 0 || rarity > RARITY_MAX ) continue;
		if ( (rarity & RARITY_MASK) !== RARITY_UNIQUE ) continue;
		const id = Number( cols[CHAR_COLUMN_ID] );
		if ( Number.isSafeInteger( id ) && id > 0 ) ids.add( id );
	}
	return ids;
}

/*
================
uniqueNests

Every npcpos anchor of a unique reference. A unique with several anchors
keeps them all: each is a candidate spawn. Unparsable rows are skipped as
LoadSpawnPoints skips them.
================
*/
export function uniqueNests( npcposRows, uniques ) {
	const nests = [];
	for ( const cols of npcposRows ) {
		if ( cols.length < NPCPOS_COLUMNS ) continue;
		const [refObjId, region, x, y, z] = cols.slice( 0, NPCPOS_COLUMNS ).map( Number );
		if ( !uniques.has( refObjId ) ) continue;
		if ( !Number.isInteger( region ) || ![ x, y, z ].every( Number.isFinite ) ) continue;
		nests.push( { refObjId, regionId: region & REGION_MASK, x, z } );
	}
	return nests;
}

/*
================
readUniqueNestSources
================
*/
export function readUniqueNestSources( textdataRoot = retailTextdataRoot ) {
	const characterRows = [];
	for ( const shard of listTextDataShardNamesSync( textdataRoot, /^characterdata.*\.txt$/i ) ) {
		characterRows.push( ...readTextDataRowsSync( path.join( textdataRoot, shard ) ) );
	}
	return { characterRows, npcposRows: readTextDataRowsSync( path.join( textdataRoot, "npcpos.txt" ) ) };
}

/*
================
buildUniqueNestsAsset
================
*/
export async function buildUniqueNestsAsset( options = {} ) {
	const sources = options.sources ?? readUniqueNestSources();
	const nests = uniqueNests( sources.npcposRows, uniqueReferences( sources.characterRows ) );
	const { outPath } = exportDataAsset( {
		publicRoot: options.publicRoot ?? publicRoot,
		outputFileName: UNIQUE_NESTS_FILE,
		value: { format: FORMAT, version: VERSION, nests }
	} );
	await refreshPrecompressedSidecars( [ outPath ], { onlyWhenStale: true } );
	console.log( `[unique-nests] wrote ${nests.length} nest(s)` );
	return { outPath, nests: nests.length };
}

if ( isMainScript( import.meta.url ) ) await buildUniqueNestsAsset();
