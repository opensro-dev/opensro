/*
===========================================================================

portItemRows.mjs - port-only item rows appended to the itemdata projections

Port-only, not native (owner, 2026-10-14): the attack-pet expansion's
equipment (SRO_ATTACK_PET_EXPANSION). Every pet wears one gear family in
three slots (collar, harness, charm) of degrees 1-9, dropped by monsters
with random blue options. The rows come from the pet design's one source,
the server's embedded companion/petdesign.json (#1631), read in place.
v1.150 has no pet equipment, so each piece is a plain carry item cloned
from a native no-use ETC row (TypeID 3/3/9/0) with an owned32x32 gear icon; its blue
options ride the item body like equipment (item/wire IsPetGearRef). The
server's companion package gives the pieces their meaning.

One list feeds the server bundle, the client presentation and the English
text, so every product sees the same reference IDs and names.

===========================================================================
*/
import fs from "node:fs";
import path from "node:path";
import { serverSourceRoot } from "../world/paths.mjs";

// The pet design under the server tree; the fingerprint's server-pet-design root.
export const PET_DESIGN_PATH = path.join( "internal", "game", "companion", "petdesign.json" );
const design = JSON.parse( fs.readFileSync( path.join( serverSourceRoot(), PET_DESIGN_PATH ), "utf8" ) );

// The native template: an ETC item with no use, no stats and a ground model.
export const PORT_ITEM_TEMPLATE = "ITEM_ETC_E051021_REDPEARL";
// Logical resource identity for our owned GLB; no retail BSR is replaced.
export const PORT_PET_DROP_MODEL = "item/etc/drop_pet_caravan_collar.bsr";

// itemdata columns (0-based) rewritten in each clone; see enterworld itemrefs.go.
const COL_REF_OBJ_ID = 1;
const COL_CODENAME = 2;
const COL_NATIVE_NAME = 3;
const COL_NAME_SYMBOL = 5;
const COL_DESC_SYMBOL = 6;
const COL_PRICE = 26;
const COL_SELL_PRICE = 31;
const COL_ICON = 54;
const COL_DROP_MODEL = 53;
const COL_MAX_STACK = 57;

/*
================
PORT_ITEMS

Every pet gear row: ID, codename, English name and description, icon and
gold price (sell price a tenth).
================
*/
export const PORT_ITEMS = Object.freeze(
	design.gear.flatMap( degree =>
		degree.items.flatMap( item =>
			[ 0, 1, 2, 3 ].map( seal => ({
				id: item.id + seal * 10,
				seal,
				slot: item.slot,
				degree: degree.degree,
				codename: item.codename + [ "", "_STAR", "_MOON", "_SUN" ][seal],
				icon: `item\\pet\\${item.slot.toLowerCase()}_${String( degree.degree ).padStart( 2, "0" )}.ddj`,
				price: 5000 * degree.degree * degree.degree,
				name: `${item.name} (Degree ${degree.degree})`,
				description: `Attack-pet gear (${item.slot}). Up to ${degree.maxBlues} blues.`
			}) )
		)
	)
);

/*
================
PORT_ITEM_TEXT

The English name and description symbols of every port row.
================
*/
export const PORT_ITEM_TEXT = Object.freeze( Object.fromEntries( PORT_ITEMS.flatMap( item => [
	[ "SN_" + item.codename, item.name ],
	[ "SN_" + item.codename + "_TT_DESC", item.description ]
] ) ) );

/*
================
portItemColumns

The port rows cloned from the template's columns.
================
*/
export function portItemColumns( template ) {
	return PORT_ITEMS.map( item => {
		const columns = [ ...template ];
		columns[0] = "1";
		columns[COL_REF_OBJ_ID] = String( item.id );
		columns[COL_CODENAME] = item.codename;
		columns[COL_NATIVE_NAME] = item.codename;
		columns[COL_NAME_SYMBOL] = "SN_" + item.codename;
		columns[COL_DESC_SYMBOL] = "SN_" + item.codename + "_TT_DESC";
		columns[COL_PRICE] = String( item.price );
		columns[COL_SELL_PRICE] = String( item.price / 10 );
		columns[COL_ICON] = item.icon;
		columns[COL_DROP_MODEL] = PORT_PET_DROP_MODEL;
		columns[COL_MAX_STACK] = "1";
		columns[15] = item.seal ? "2" : "0";
		columns[61] = String( (item.degree - 1) * 3 + (item.seal || 1) );
		return columns;
	} );
}

/*
================
appendPortItemRecords

Appends the port rows right after the template record of one itemdata
file's text. Idempotent: a text that already holds them is unchanged.
================
*/
export function appendPortItemRecords( text ) {
	if ( text.includes( "\t" + PORT_ITEMS[0].codename + "\t" ) ) return text;
	return text.replace( /[^\r]+/g, record => {
		const fields = record.split( "\t" );
		if ( fields[0].trim() !== "1" || fields[COL_CODENAME] !== PORT_ITEM_TEMPLATE ) return record;
		return record + portItemColumns( fields ).map( columns => "\r\n" + columns.join( "\t" ) ).join( "" );
	} );
}
