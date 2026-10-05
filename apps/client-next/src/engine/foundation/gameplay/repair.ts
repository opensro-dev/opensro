/*
===========================================================================

repair.ts - equipment repair at a smith: what it costs and what it sends

The shop window's Repair button arms a repair cursor (CIFStore_OnRepairButton
5B1C00, cursor 0x96) and clicking an item sends 0x746F [u32 npcGid][u8 1]
[u8 slot]; Repair All (CIFStore_OnRepairAllButton 5B2B10) totals the cost of
every equipped and carried item, says there is nothing to repair when the
total is zero, and after its confirmation sends 0x746F [u32 npcGid][u8 2]
(CGInterface_SendNpcRepairRequest746F_758C 693860). 0xB46F answers [1] or
[2, category-13 notice]; durability and gold arrive on their own packets.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { WireFrame } from "@/engine/contracts/network";
import { itemMaxDurability } from "@/engine/foundation/ui/item-tooltip-stats";
import { constantNativeNotice } from "./native-notice";
import type { SystemNotice } from "./system-notices";

export const REPAIR_REQUEST_OPCODE = 0x746f;
export const REPAIR_RESPONSE_OPCODE = 0xb46f;
export const REPAIR_ONE_SLOT = 1;
export const REPAIR_ALL_SLOTS = 2;
// CSOItem_CalculateRepairCost clamps the per-point price here.
// The float32 9.99999968e+37f at C461B0.
const MAX_REPAIR_POINT_PRICE = 9.999999680285692e37;

/*
================
repairableItem

CGItemEquip_CanRepair's item half: equipment that is not an accessory
(family 5 or 12) or a job suit (7), with itemdata CanRepair.
================
*/
export function repairableItem( item: InventoryItem ): boolean {
	const word = item.typeFlags, family = word >>> 7 & 15;
	if ( (word & 2) !== 0 || (word & 0x1c) !== 0x0c || (word & 0x60) !== 0x20 ) return false;
	if ( family === 5 || family === 12 || family === 7 ) return false;
	return (item.tooltip?.fields.canRepair ?? 0) !== 0;
}

/*
================
itemRepairCost

CSOItem_CalculateRepairCost (789630): CostRepair / maximum per missing
point as a float32, at least 1, truncated over the missing points, plus
CostRevive for a broken item, which repairs from 1. 0 when nothing is
missing.
================
*/
export function itemRepairCost( item: InventoryItem ): number {
	if ( !repairableItem( item ) ) return 0;
	const fields = item.tooltip?.fields ?? {}, maximum = itemMaxDurability( item );
	let current = item.durability ?? 0, revive = 0;
	if ( current === 0 ) {
		current = 1;
		revive = fields.reviveCostB8 ?? 0;
	}
	const missing = maximum - current;
	if ( missing <= 0 || maximum <= 0 ) return 0;
	let price = Math.fround( ((fields.repairCostB4 ?? 0) >>> 0) / maximum );
	if ( !(price >= 1) ) price = 1;
	else if ( price > MAX_REPAIR_POINT_PRICE ) price = MAX_REPAIR_POINT_PRICE;
	return Math.trunc( missing * price ) + revive;
}

/*
================
repairAllCost

What Repair All charges: the cost of every equipped and carried item.
================
*/
export function repairAllCost( items: readonly InventoryItem[] ): number {
	let total = 0;
	for ( const item of items ) total += itemRepairCost( item );
	return total;
}

/*
================
repairRequest

0x746F for one slot (its inventory slot number) or for everything.
================
*/
export function repairRequest(
	npc: number,
	mode: typeof REPAIR_ONE_SLOT | typeof REPAIR_ALL_SLOTS,
	slot = 0
): WireFrame {
	if (
		!Number.isInteger( npc ) || npc < 1 || npc > 0xffffffff || !Number.isInteger( slot ) || slot < 0 || slot > 255
	) {
		throw Error( "Invalid repair request" );
	}
	const payload = new Uint8Array( mode === REPAIR_ONE_SLOT ? 6 : 5 );
	new DataView( payload.buffer ).setUint32( 0, npc, true );
	payload[4] = mode;
	if ( mode === REPAIR_ONE_SLOT ) payload[5] = slot;
	return { opcode: REPAIR_REQUEST_OPCODE, payload };
}

/*
================
repairNotice

0xB46F's refusal through the category-13 notice table (689420).
================
*/
export function repairNotice( opcode: number, p: Uint8Array ): SystemNotice | null {
	if ( opcode !== REPAIR_RESPONSE_OPCODE || p[0] === 1 ) return null;
	if ( p.length !== 2 || p[0] !== 2 ) throw Error( "Invalid repair refusal" );
	return constantNativeNotice( 13, p[1]! );
}
