/*
===========================================================================

item-tooltip-stone.ts - the alchemy and attribute stone rows

Native 557C50, reached from the 56B2E0 item dispatcher for etc items of
type 11 kinds 1, 2 and 7, writes the stone's sort, its target degree and
its attribute assimilation probability. The probability is the stone's
OptLevel byte (CSOItem +0x68, read by 7892B0), the same value the inventory
carries as `plus`. The server merges two stones only when that value is
equal, so this row is how a player tells two otherwise identical stones apart.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { TooltipRow } from "./tooltip-rows";

const ETC_FAMILY_MASK = 0x7e;
const ETC_FAMILY = 0x6c;
const STONE_TYPE = 11;
const MAGIC_STONE_KIND = 1;
const ATTRIBUTE_STONE_KIND = 2;
const MAGIC_STONE_EXTRA_KIND = 7;
const STONE_ROW_COLOR = 0xffffffff;

// The target words 557C50 tests, in its order; bit i of the published
// stoneTooltipMask marks word i. Mirrors stoneTooltipWords in the
// server's enterworld/stonefamily.go; change both together.
const STONE_TARGET_SYMBOLS = [
	"UIIT_STT_WEAPON_TYPE",
	"UIIT_STT_ARMOR_TYPE",
	"UIIT_STT_SHIELD",
	"UIIT_STT_ACCESSARY_TYPE",
	"UIIT_STT_ARMOR_POSITION_HEAD",
	"UIIT_STT_ARMOR_POSITION_SHOULDER",
	"UIIT_STT_ARMOR_POSITION_BREAST",
	"UIIT_STT_ARMOR_POSITION_LEG",
	"UIIT_STT_ARMOR_POSITION_HAND",
	"UIIT_STT_ARMOR_POSITION_FOOT",
	"UIIT_STT_ACCESSARY_EARRING",
	"UIIT_STT_ACCESSARY_NECKLACE",
	"UIIT_STT_ACCESSARY_RING",
	"UIIT_STT_HONOR_TYPE"
] as const;

/*
================
itemTooltipStone

Rows in native order: the sort line, the "Object item" row, the target
degree from Param1 (+0x29C, a byte), a blank line, then the assimilation
line built from "%s%s: %d%%" (5586C0..558719). The Object item row is
UIIT_STT_TARGET_ITEM plus the type symbols of every target word the
stone's text contains, in 557C50's test order, each followed by a space.
The text itself is item data the client never receives; the server
searches it (enterworld/stonefamily.go) and publishes the matching bits
as the stoneTooltipMask field. A stone without the field (an older
server) draws no row; a published zero draws the label alone, as 557C50
does when its text holds no word.
================
*/
export function itemTooltipStone(
	item: InventoryItem,
	text: ( symbol: string ) => string
): readonly TooltipRow[] {
	if ( (item.typeFlags & ETC_FAMILY_MASK) !== ETC_FAMILY ) return [];
	const t = (item.typeFlags >>> 7) & 15, k = (item.typeFlags >>> 11) & 31;
	if ( t !== STONE_TYPE ) return [];
	if ( k !== MAGIC_STONE_KIND && k !== ATTRIBUTE_STONE_KIND && k !== MAGIC_STONE_EXTRA_KIND ) return [];
	const sort = k === ATTRIBUTE_STONE_KIND ?
		"UIIT_STT_ITEM_TYPE_ALCHEMY_ATTRSTONE" :
		"UIIT_STT_ITEM_TYPE_ALCHEMY_MAGICSTONE";
	// 558602 reads Param1 as a byte before formatting "%s: %d%s".
	const degree = (item.tooltip?.fields["itemParam1_29c"] ?? 0) & 0xff;
	const rows: TooltipRow[] = [
		{ value: `${text( "UIIT_STT_ITEM_TYPE" )}: ${text( sort )}`, color: STONE_ROW_COLOR }
	];
	const targets = item.tooltip?.fields["stoneTooltipMask"];
	if ( targets !== undefined ) {
		// 55800B: "%s: " then each matching symbol and a space, untrimmed.
		let value = `${text( "UIIT_STT_TARGET_ITEM" )}: `;
		for ( let bit = 0; bit < STONE_TARGET_SYMBOLS.length; bit++ ) {
			if ( targets & (1 << bit) ) value += `${text( STONE_TARGET_SYMBOLS[bit]! )} `;
		}
		rows.push( { value, color: STONE_ROW_COLOR } );
	}
	rows.push(
		{
			value: `${text( "UIIT_STT_TARGET_ITEM_CLASS" )}: ${degree}${text( "UIIT_STT_CLASS" )}`,
			color: STONE_ROW_COLOR
		},
		{ value: " ", color: STONE_ROW_COLOR },
		{
			value: `${text( "PARAM_ATTR_ASSIMILATION" )}${text( "UIIT_STT_PROBABILITY" )}: ${(item.plus ?? 0) & 0xff}%`,
			color: STONE_ROW_COLOR
		}
	);
	return rows;
}
