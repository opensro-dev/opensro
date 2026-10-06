/*
===========================================================================

items.mjs - the dashboard's read-only item catalog and item artwork

The catalog is the server's own item export (tools/item-catalog.mjs, run by
`pnpm task build server-game-data`); names and descriptions come from the
published client text and artwork from the published client images. It is
loaded once, lazily, and a failed load is retried on the next request.

===========================================================================
*/
import { clientPublicPath, generatedPath } from "../../../scripts/lib/generatedRoot.mjs";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";

// file: URLs into the generated tree (scripts/lib/generatedRoot.mjs). The icon
// root keeps its trailing separator: icon paths resolve against it.
export const ITEM_CATALOG_URL = pathToFileURL( generatedPath( "observatory", "items.json" ) );
const ITEM_NAMES_URL = pathToFileURL( clientPublicPath( "assets", "text", "textdataname.en.json" ) );
const ITEM_ICON_ROOT_URL = pathToFileURL(
	clientPublicPath( "assets", "images", "Media_extracted", "icon" ) + path.sep
);
const CATALOG_VERSION = 1;
const MAX_CATALOG_ITEMS = 65536;

/*
================
itemIconPath

The authored item artwork a catalog icon names, or null for anything that
is not a plain path under item/.
================
*/
export function itemIconPath( icon ) {
	const path = String( icon ?? "" ).replaceAll( "\\", "/" ).replace( /\.ddj$/i, ".png" );
	return /^item\/[a-z0-9_/-]+\.png$/i.test( path ) && !path.includes( ".." ) ? path : null;
}

/*
================
parseCatalog
================
*/
function parseCatalog( raw, names ) {
	const data = JSON.parse( raw );
	const text = JSON.parse( names ).entries;
	if ( data.version !== CATALOG_VERSION || !Array.isArray( data.items ) || data.items.length > MAX_CATALOG_ITEMS ) {
		throw new Error( "Invalid item catalog" );
	}
	const ids = new Set();
	for ( const item of data.items ) {
		if (
			!Number.isSafeInteger( item.id ) || item.id <= 0 || ids.has( item.id ) ||
			!/^ITEM_[A-Z0-9_]+$/i.test( item.codename )
		) {
			throw new Error( "Invalid item identity" );
		}
		ids.add( item.id );
		item.iconPath = itemIconPath( item.icon );
		item.icon = item.iconPath ? "/api/item-icon/" + item.id : "";
		item.description = text[item.descriptionSymbol] ?? "";
	}
	return data;
}

/*
================
createItems
================
*/
export function createItems() {
	let pending;

	function load() {
		if ( !pending ) {
			pending = Promise.all( [ readFile( ITEM_CATALOG_URL, "utf8" ), readFile( ITEM_NAMES_URL, "utf8" ) ] )
				.then( ( [raw, names] ) => parseCatalog( raw, names ) )
				.catch( error => {
					pending = null;
					throw error;
				} );
		}
		return pending;
	}

	return {
		async catalog() {
			const data = await load();
			return { ...data, items: data.items.map( ( { iconPath, ...item } ) => item ) };
		},
		async icon( id ) {
			const item = (await load()).items.find( item => item.id === id );
			if ( !item?.iconPath ) return null;
			try {
				return await readFile( new URL( item.iconPath, ITEM_ICON_ROOT_URL ) );
			} catch ( error ) {
				if ( error.code === "ENOENT" ) return null;
				throw error;
			}
		}
	};
}
