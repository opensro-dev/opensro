import nameCatalog from "./itemNameCompletions.json" with { type: "json" };

// Product English, not recovered native fallback behavior. The catalog records
// the source and basis for every title, including explicitly labelled unreleased
// placeholders. Raw retail data stays untouched. One policy feeds both products.
export const ITEM_TEXT_COMPLETIONS = Object.freeze( {
	...Object.fromEntries( Object.entries( nameCatalog ).map( ( [symbol, entry] ) => [ symbol, entry.english ] ) ),
	SN_ITEM_MALL_MOVE_SPEED_UP_50_TT_DESC: "Increases moving speed by 50% for 60 minutes.",
	SN_ITEM_MALL_MOVE_SPEED_UP_100_TT_DESC: "Increases moving speed by 100% for 60 minutes.",
	SN_ITEM_ETC_SPEED_UP_BASIC_TT_DESC: "Increases moving speed when used."
} );

// Absent columns are malformed records, not cells eligible for translation.
function isMissingItemText( value ) {
	return typeof value === "string" && /^(?:-|xxx|null|0)?$/i.test( value.trim() );
}

export function completeItemText( entries ) {
	for ( const [symbol, english] of Object.entries( ITEM_TEXT_COMPLETIONS ) ) {
		if ( Object.hasOwn( entries, symbol ) ? isMissingItemText( entries[symbol] ) : nameCatalog[symbol]?.absent ) {
			entries[symbol] = english;
		}
	}
	return entries;
}

// Change only the English cells in the generated server projection, preserving
// BOM, record separators, bare-LF cell contents and every other language column.
export function completeItemTextProjection( bytes ) {
	const utf16 = bytes[0] === 0xff && bytes[1] === 0xfe;
	const encoding = utf16 ? "utf16le" : "utf8";
	const seen = new Set();
	let text = bytes.toString( encoding ).replace( /[^\r]+/g, record => {
		const columns = record.split( "\t" ), symbol = columns[1]?.trim();
		if ( record.trimStart().startsWith( "//" ) ) return record;
		seen.add( symbol );
		if ( !Object.hasOwn( ITEM_TEXT_COMPLETIONS, symbol ) || !isMissingItemText( columns[8] ) ) return record;
		columns[8] = ITEM_TEXT_COMPLETIONS[symbol];
		return columns.join( "\t" );
	} );
	for ( const [symbol, entry] of Object.entries( nameCatalog ) ) {
		if ( "absent" in entry && entry.absent && !seen.has( symbol ) ) {
			if ( !text.endsWith( "\r\n" ) ) text += "\r\n";
			text += [ "1", symbol, "", "", "", "", "", "", entry.english, "", "" ].join( "\t" ) + "\r\n";
		}
	}
	return Buffer.from( text, encoding );
}

// Retail currency rows share the invalid key "xxx". Repair only their name
// reference in the generated projection; prices, IDs and mechanics are intact.
export function completeItemReferenceProjection( bytes ) {
	const encoding = bytes[0] === 0xff && bytes[1] === 0xfe ? "utf16le" : "utf8";
	return Buffer.from(
		bytes.toString( encoding ).replace( /[^\r]+/g, record => {
			if ( record.trimStart().startsWith( "//" ) ) return record;
			const fields = record.split( "\t" );
			if ( fields[0].trim() === "1" && /^ITEM_ETC_GOLD_0[1-3]$/.test( fields[2] ) && fields[5] === "xxx" ) {
				fields[5] = "SN_ITEM_ETC_GOLD";
				return fields.join( "\t" );
			}
			return record;
		} ),
		encoding
	);
}
