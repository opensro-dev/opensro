/*
===========================================================================

item-tooltip-set.ts - the set bonus rows of an equipment tooltip

Port-only, not native (the Experimental "Set bonus on item tooltips" row):
the v1.150 client has no set rows and the original text for these lines is
not in its data, so the wording is the port's own. The block names the item's
set, how many of its pieces are worn, and every tier with its effect, the
tier in force drawn bright and the rest dim.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import { type EquipmentSet, SET_OPTION_REINFORCE_ITEM, SET_OPTION_RESIST_ALL } from "../gameplay/equipment-sets";
import type { TooltipRow } from "./tooltip-rows";

// The equipment slots below this index are the worn ones (the bag follows).
const FIRST_BAG_SLOT = 13;
const COLOR_HEADING = 0xffffd953;
const COLOR_ACTIVE = 0xff6ce675;
const COLOR_INACTIVE = 0xff8a8a8a;
const COLOR_PIECES = 0xffefdaa4;

// Port-written names for the overlay's text symbols; the symbol itself is
// preferred when the client has text for it.
const SET_NAMES = {
	SN_ITEM_11_SET_A_DEFENSE: "Ruin Set",
	SN_ITEM_11_SET_B_DEFENSE: "Immortal Set",
	SN_ITEM_11_SET_A_ACCESSORY: "Myth Set",
	SN_ITEM_11_SET_B_ACCESSORY: "Legend Set"
} as const;

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

One tier option as a line; an option the port does not know prints nothing.
================
*/
function optionText( option: number, value: number ): string {
	if ( option === SET_OPTION_REINFORCE_ITEM ) return `Reinforce +${value} on each worn piece`;
	if ( option === SET_OPTION_RESIST_ALL ) return `Abnormal state resistance +${value}`;
	return "";
}

/*
================
itemTooltipSet

The set block of one item's tooltip; empty when the item names no set the
shard published. The tier in force is the highest the worn count reaches
(tiers replace each other, they never add).
================
*/
export function itemTooltipSet(
	item: InventoryItem,
	sets: readonly EquipmentSet[],
	worn: ReadonlyMap<number, number>,
	text: ( symbol: string ) => string
): readonly TooltipRow[] {
	const id = item.tooltip?.fields.setId ?? 0;
	const set = id > 0 ? sets.find( row => row.id === id ) : undefined;
	if ( !set || !set.tiers.length ) return [];
	const count = worn.get( id ) ?? 0,
		total = set.tiers[set.tiers.length - 1]!.pieces,
		active = set.tiers.filter( tier => count >= tier.pieces ).at( -1 ),
		named = set.name ? text( set.name ) : "",
		name = (named && named !== set.name ? named : (SET_NAMES as Readonly<Record<string, string>>)[set.name]) ||
			"Set";
	const rows: TooltipRow[] = [
		{ value: `${name} (${count}/${total})`, color: COLOR_HEADING, strong: true }
	];
	for ( const tier of set.tiers ) {
		const color = tier === active ? COLOR_ACTIVE : COLOR_INACTIVE;
		rows.push( { value: `${tier.pieces} pieces:`, color: tier === active ? COLOR_PIECES : COLOR_INACTIVE } );
		for ( const option of tier.options ) {
			const line = optionText( option.option, option.value );
			if ( line ) rows.push( { value: "  " + line, color } );
		}
	}
	return rows;
}
