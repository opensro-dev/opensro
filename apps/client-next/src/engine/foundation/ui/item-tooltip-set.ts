/*
===========================================================================

item-tooltip-set.ts - the set rows of an equipment tooltip

Port-only, not native (the Experimental "Set bonus on item tooltips" row):
the v1.150 client has no set rows. The layout follows what the later client
shows beside a set piece: the set name with the worn pieces out of the total,
every piece of the set (worn ones bright, the rest dim), then each tier as
"<name>(<pieces>)" with its effects as "<label> : <value> <unit>", the tier in
force in colour and the others dim. The names and labels are the later
client's own text (SN_ITEM_11_SET_*, PARAM_REINFORCE_ITEM_SET, ...), merged
into the build's text tables; the English fallbacks below cover a build that
lacks them. The original draws this as a second panel to the right of the item;
the port draws it as a block at the end of the one panel.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import { type EquipmentSet, SET_OPTION_REINFORCE_ITEM, SET_OPTION_RESIST_ALL } from "../gameplay/equipment-sets";
import type { TooltipRow } from "./tooltip-rows";

// The equipment slots below this index are the worn ones (the bag follows).
const FIRST_BAG_SLOT = 13;
const COLOR_NAME = 0xff9ce86b;
const COLOR_WORN = 0xffffffff;
const COLOR_DIM = 0xff6b7684;
const COLOR_EFFECT = 0xff2ee6f0;

// English names of the later client's set symbols, for a build without its text.
const SET_NAMES = {
	SN_ITEM_11_SET_A_DEFENSE: "Destruction",
	SN_ITEM_11_SET_B_DEFENSE: "Immortality",
	SN_ITEM_11_SET_A_ACCESSORY: "Myth",
	SN_ITEM_11_SET_B_ACCESSORY: "Legend",
	SN_ITEM_11_SET_A_WEAPON: "Power",
	SN_ITEM_11_SET_B_WEAPON: "Fight",
	SN_ITEM_11_SET_A_SHIELD: "Protection",
	SN_ITEM_11_SET_B_SHIELD: "Guard"
} as const;

/*
================
known

The client's text for a symbol, or the fallback when it has none (an unknown
symbol comes back empty or as itself).
================
*/
function known( text: ( symbol: string ) => string, symbol: string, fallback: string ): string {
	const value = symbol ? text( symbol ) : "";
	return value && value !== symbol ? value : fallback;
}

/*
================
setName

The set's display name.
================
*/
function setName( set: EquipmentSet, text: ( symbol: string ) => string ): string {
	return known( text, set.name, (SET_NAMES as Readonly<Record<string, string>>)[set.name] ?? "" );
}

/*
================
wornSetPieces

The number of worn pieces of each set group: every item in an equipment
slot that names a set in its setId native field.
================
*/
export function wornSetPieces( items: readonly InventoryItem[] ): ReadonlyMap<number, number> {
	const counts = new Map<number, number>();
	for ( const item of items ) {
		if ( item.slot >= FIRST_BAG_SLOT ) continue;
		const id = item.tooltip?.fields.setId ?? 0;
		if ( id > 0 ) counts.set( id, (counts.get( id ) ?? 0) + 1 );
	}
	return counts;
}

/*
================
wornSetSlots

The worn positions (TypeID4) of each set group's worn pieces.
================
*/
export function wornSetSlots( items: readonly InventoryItem[] ): ReadonlyMap<number, ReadonlySet<number>> {
	const slots = new Map<number, Set<number>>();
	for ( const item of items ) {
		if ( item.slot >= FIRST_BAG_SLOT ) continue;
		const id = item.tooltip?.fields.setId ?? 0;
		if ( id <= 0 ) continue;
		let row = slots.get( id );
		if ( !row ) slots.set( id, row = new Set() );
		row.add( (item.typeFlags >>> 11) & 31 );
	}
	return slots;
}

/*
================
tooltipSetViewer

The set input of an item tooltip: the published groups and the bag they are counted
in, or nothing. The Experimental row decides: off, or a shard that publishes no sets,
leaves the tooltip as the native client draws it. The references are the inputs' own,
so a caller can memoise on them.
================
*/
export function tooltipSetViewer(
	enabled: boolean,
	groups: readonly EquipmentSet[] | undefined,
	equipment: readonly InventoryItem[] | undefined
): { readonly groups: readonly EquipmentSet[]; readonly equipment: readonly InventoryItem[]; } | undefined {
	if ( !enabled || !groups?.length ) return undefined;
	return { groups, equipment: equipment ?? [] };
}

/*
================
optionText

One tier option as "<label> : <value> <unit>"; an option the port does not know prints nothing.
================
*/
function optionText( option: number, value: number, text: ( symbol: string ) => string ): string {
	if ( option === SET_OPTION_REINFORCE_ITEM ) {
		return `${known( text, "PARAM_REINFORCE_ITEM_SET", "Set enforcement" )} : ${value} ${
			known( text, "PARAM_REINFORCE", "enhancement" )
		}`;
	}
	if ( option === SET_OPTION_RESIST_ALL ) {
		return `${known( text, "PARAM_RESIST_ALL_SET", "Increase all resistance" )} : ${value} ${
			known( text, "PARAM_INCREASE", "Increase" )
		}`;
	}
	return "";
}

/*
================
itemSetOf

The published set an item belongs to; undefined otherwise. A set without tiers
(the weapon and shield sets) is published for its name only.
================
*/
function itemSetOf( item: InventoryItem, sets: readonly EquipmentSet[] ): EquipmentSet | undefined {
	const id = item.tooltip?.fields.setId ?? 0;
	return id > 0 ? sets.find( row => row.id === id ) : undefined;
}

/*
================
itemTooltipSetName

The line under an item's name that names its set (the later client's green row),
or nothing when the item is no published set's.
================
*/
export function itemTooltipSetName(
	item: InventoryItem,
	sets: readonly EquipmentSet[],
	text: ( symbol: string ) => string
): TooltipRow | undefined {
	const set = itemSetOf( item, sets ), name = set ? setName( set, text ) : "";
	return name ? { value: name, color: COLOR_NAME } : undefined;
}

/*
================
itemTooltipSet

The set block of one item's tooltip; empty when the item names no set the
shard published, or a set without tiers (a weapon or shield set shows only its
name under the title). The tier in force is the highest the worn count reaches
(tiers replace each other, they never add). Like the later client's panel it
lists the tiers reached and the next one to reach, not the ones beyond (an
unworn set shows only its first tier).
================
*/
export function itemTooltipSet(
	item: InventoryItem,
	sets: readonly EquipmentSet[],
	equipment: readonly InventoryItem[],
	text: ( symbol: string ) => string
): readonly TooltipRow[] {
	const set = itemSetOf( item, sets );
	if ( !set || !set.tiers.length ) return [];
	const count = wornSetPieces( equipment ).get( set.id ) ?? 0,
		worn = wornSetSlots( equipment ).get( set.id ),
		total = set.tiers[set.tiers.length - 1]!.pieces,
		active = set.tiers.filter( tier => count >= tier.pieces ).at( -1 ),
		name = setName( set, text );
	const rows: TooltipRow[] = [ { value: `${name} (${count}/${total})`, color: COLOR_NAME, strong: true } ];
	for ( const piece of set.pieces ) {
		const label = known( text, piece.name, "" );
		if ( label ) {
			rows.push(
				worn?.has( piece.slot ) ?
					{ value: label, color: COLOR_WORN, strong: true } :
					{ value: label, color: COLOR_DIM }
			);
		}
	}
	const next = set.tiers.findIndex( tier => tier.pieces > count );
	for ( const tier of next < 0 ? set.tiers : set.tiers.slice( 0, next + 1 ) ) {
		const on = tier === active;
		rows.push( { value: " ", color: 0 } );
		rows.push( { value: `${name}(${tier.pieces})`, color: on ? COLOR_NAME : COLOR_DIM, strong: on } );
		for ( const option of tier.options ) {
			const line = optionText( option.option, option.value, text );
			if ( line ) rows.push( { value: line, color: on ? COLOR_EFFECT : COLOR_DIM, strong: on } );
		}
	}
	return rows;
}
