/*
===========================================================================

siteTooltip.mjs - an item instance's tooltip, as the game prints it

The entry of the website's tooltip bundle (catalogue tooltip/item-tooltip.mjs)
and of the catalogue's own items.json tooltips: one function over the
client's unmodified itemTooltip, item-tooltip-stats and item-tooltip-magic,
so a profile shows the numbers the game shows. The bundle is a pure ES
module for the browser: no globals, no side effects at import, no fetches;
every input arrives as an argument.

An instance is { refObjId, plus, variance, magic, durability }: variance the
native u64 as a decimal string (5-bit fields), magic the native encoded
u64 decimal strings (param id in the low 16 bits, value in the high dword),
as the public API's worn items carry them. Requirements read as met: a
profile has no viewer to compare against.

===========================================================================
*/
import { itemTooltip } from "../../../apps/client-next/src/engine/foundation/ui/item-tooltip.ts";
import { itemMaxDurability } from "../../../apps/client-next/src/engine/foundation/ui/item-tooltip-stats.ts";
import { itemMagicDefinitions } from "../../../apps/client-next/src/engine/foundation/ui/item-tooltip-magic.ts";
import {
	itemMagicReferences,
	itemTooltipReference
} from "../../../apps/client-next/src/engine/foundation/gameplay/item-tooltip-reference.ts";

// TOOLTIP_FORMAT names this function's inputs and outputs; a change to
// either bumps it, so the site can refuse a bundle it does not understand.
export const TOOLTIP_FORMAT = 1;

// The bag's first slot: the tooltip reads an item instance in a slot.
const TOOLTIP_SLOT = 13;
// A requirement value no item asks for: requirements read as met (white).
const MET = 0xffff;
// The highest reinforcement the client accepts in an item body.
const MAX_PLUS = 255;

/*
================
createMagicTable

The magic-option definitions (the catalogue's tooltip/magic.json rows), as
the table itemTooltipRows reads. Build it once per page.
================
*/
export function createMagicTable( rows ) {
	return itemMagicReferences( rows );
}

/*
================
itemTooltipRows

The tooltip rows of one instance of item (its tooltip/items/<id>.json):
[{ text, color: "#rrggbb", strong }], a blank line as { text: "" }. A
missing durability shows the item's maximum, as it drops.
================
*/
export function itemTooltipRows( instance, item, magicTable, text ) {
	const plus = instance.plus ?? 0, magic = instance.magic ?? [], variance = instance.variance ?? "0";
	if ( !Number.isInteger( plus ) || plus < 0 || plus > MAX_PLUS ) throw Error( "Invalid item plus" );
	if ( typeof variance !== "string" || !/^[0-9]{1,20}$/.test( variance ) ) throw Error( "Invalid item variance" );
	if ( !Array.isArray( magic ) || magic.some( m => typeof m !== "string" || !/^[0-9]{1,20}$/.test( m ) ) ) {
		throw Error( "Invalid item magic options" );
	}
	const tooltip = itemTooltipReference( item.fields, item.descriptionSymbol );
	const base = {
		slot: TOOLTIP_SLOT,
		refObjId: instance.refObjId,
		typeFlags: item.typeFlags,
		quantity: 1,
		plus,
		durability: 0,
		variance,
		magic,
		name: item.name,
		tooltip,
		magicReferences: itemMagicDefinitions( magic, magicTable, tooltip?.fields.itemClass )
	};
	const durability = instance.durability ?? itemMaxDurability( base );
	const masteries = [];
	for ( let i = 1; i <= 4; i++ ) {
		const type = tooltip?.fields["reqLevelType" + i];
		if ( type !== undefined && type > 10 ) masteries.push( { id: type, level: MET } );
	}
	const progression = { level: MET, stats: { strength: MET, intellect: MET }, masteries };
	// The race and sex rows read as met too: the wearer is whoever may wear it.
	const country = tooltip?.fields.country, sex = tooltip?.fields.reqGender;
	const identity = {
		...(country === 0 || country === 1 ? { country } : {}),
		...(sex === 0 || sex === 1 ? { sex } : {})
	};
	return itemTooltip( { ...base, durability }, progression, text, identity ).map( row =>
		row.value.trim() === "" ?
			{ text: "" } :
			{
				text: row.value,
				color: "#" + (row.color & 0xffffff).toString( 16 ).padStart( 6, "0" ),
				...(row.strong ? { strong: true } : {})
			}
	);
}
