/*
===========================================================================

buildLevelDataAsset.mjs - publish authored progression and restoration prices

Native CLevelData supplies experience, mastery and job thresholds. CDropGoldData
supplies the resuscitation gold basis through dg.txt, not levelgold.txt. Publish
both by level so the browser quote and server transaction use the same tables.

===========================================================================
*/
import fs from "node:fs";
import path from "node:path";
import { exportDataAsset } from "../shared/dataAssetExport.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { readTextDataLinesSync, splitTextDataRow } from "../shared/textDataIo.mjs";
import { retailTextdataRoot, publicRoot } from "../world/paths.mjs";

const LEVEL_COLUMNS = 9;
const GOLD_COLUMNS = 3;

/*
================
readNumericTable

Preserve empty cells: collapsing tabs shifts every subsequent native column.
Reject malformed prices at publication rather than offering a free operation.
================
*/
function readNumericTable( filename, columns ) {
	const source = path.join( retailTextdataRoot, filename );
	return readTextDataLinesSync( source ).map( ( line, index ) => {
		const cells = splitTextDataRow( line.trim() );
		if ( cells.length !== columns || cells.some( cell => cell.trim() === "" ) ) {
			throw Error( `[level-data] ${source}:${index + 1}: expected exactly ${columns} native columns` );
		}
		const values = cells.map( Number );
		// Job thresholds use -1 for unavailable ranks. Preserve those authored
		// sentinels; the individual price consumer validates its positive basis.
		if ( values.some( value => !Number.isSafeInteger( value ) ) || values[0] <= 0 ) {
			throw Error( `[level-data] ${source}:${index + 1}: invalid native numeric field` );
		}
		return values;
	} );
}

/*
================
buildLevelDataAsset

7E0F20 returns the level payload; 7E0650 returns the drop-gold payload.
Their different layouts must not be confused by the similarly named records.
================
*/
export function buildLevelDataAsset() {
	const source = path.join( retailTextdataRoot, "leveldata.txt" );
	if ( !fs.existsSync( source ) ) {
		console.warn( `[level-data] source missing (${source}) - skipping` );
		return { written: false, records: 0 };
	}
	const gold = new Map();
	for ( const [level, minimum] of readNumericTable( "dg.txt", GOLD_COLUMNS ) ) {
		if ( gold.has( level ) || minimum <= 0 ) throw Error( `[level-data] invalid drop-gold level ${level}` );
		gold.set( level, minimum );
	}
	const table = {};
	let rows = 0;
	for ( const cells of readNumericTable( "leveldata.txt", LEVEL_COLUMNS ) ) {
		const [level, expRequired, masteryTrainSpCost] = cells;
		if ( table[level] ) throw Error( `[level-data] duplicate level ${level}` );
		table[level] = {
			expRequired,
			masteryTrainSpCost,
			expOrbDivisor: cells[5],
			jobExpTrader: cells[6],
			jobExpThief: cells[7],
			jobExpHunter: cells[8],
			withdrawalGoldBasis: gold.get( level )
		};
		rows++;
	}
	const { outPath } = exportDataAsset( { publicRoot, outputFileName: "levelData.json", value: table } );
	console.log( `[level-data] wrote ${rows} level rows -> ${path.relative( publicRoot, outPath )}` );
	return { written: true, records: rows, outPath };
}

if ( isMainScript( import.meta.url ) ) {
	buildLevelDataAsset();
}
