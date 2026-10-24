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

/*
================
itemTooltipStone

Rows in native order: the sort line, the target degree from Param1
(+0x29C, a byte), a blank line, then the assimilation line built from
"%s%s: %d%%" (5586C0..558719). Native also prints an "Object item" row
from the item's description strings (+0x2EC, +0x340); the client
reference rows do not carry those strings, so that row is not drawn.
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
	return [
		{ value: `${text( "UIIT_STT_ITEM_TYPE" )}: ${text( sort )}`, color: STONE_ROW_COLOR },
		{
			value: `${text( "UIIT_STT_TARGET_ITEM_CLASS" )}: ${degree}${text( "UIIT_STT_CLASS" )}`,
			color: STONE_ROW_COLOR
		},
		{ value: " ", color: STONE_ROW_COLOR },
		{
			value: `${text( "PARAM_ATTR_ASSIMILATION" )}${text( "UIIT_STT_PROBABILITY" )}: ${(item.plus ?? 0) & 0xff}%`,
			color: STONE_ROW_COLOR
		}
	];
}
