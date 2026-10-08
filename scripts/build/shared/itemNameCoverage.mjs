import { readdirSync } from "node:fs";
import path from "node:path";
import { readTextDataRowsSync } from "./textDataIo.mjs";

// A new retail gap must stop publication, not become another invisible tooltip.
// This checks all active refs, and all authored item titles (including inactive
// content). Descriptions are optional body text and deliberately excluded.
export function assertItemNameCoverage( textdataRoot, entries ) {
	const missing = [];
	const valid = value => typeof value === "string" && value.trim() && !/^(?:-|xxx|null|0)$/i.test( value.trim() );
	let activeItems = 0;
	for ( const file of readdirSync( textdataRoot ).filter( name => /^itemdata.*\.txt$/i.test( name ) ) ) {
		for ( const row of readTextDataRowsSync( path.join( textdataRoot, file ) ) ) {
			if ( row[0].trim() !== "1" || row.length < 9 ) continue;
			activeItems++;
			const symbol = row[5] === "xxx" && /^ITEM_ETC_GOLD_0[1-3]$/.test( row[2] ) ? "SN_ITEM_ETC_GOLD" : row[5];
			if ( !valid( entries[symbol] ) ) missing.push( `${row[1]} ${row[2]} -> ${symbol}` );
		}
	}
	for ( const [symbol, value] of Object.entries( entries ) ) {
		if ( symbol.startsWith( "SN_ITEM_" ) && !symbol.includes( "_TT_DESC" ) && !valid( value ) ) {
			missing.push( symbol );
		}
	}
	if ( missing.length ) throw Error( `Missing item English titles (${missing.length}):\n${missing.join( "\n" )}` );
	return {
		activeItems,
		itemTitles:
			Object.keys( entries ).filter( symbol => symbol.startsWith( "SN_ITEM_" ) && !symbol.includes( "_TT_DESC" ) )
				.length
	};
}
