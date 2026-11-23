import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { Progression } from "../gameplay/progression";

export interface EquipmentIdentity {
	readonly country?: number;
	readonly sex?: number;
}
function equipment( flags: number ) {
	return (flags & 0x7e) === 0x2c;
}
function body( flags: number ) {
	return equipment( flags ) && [ 1, 2, 3, 9, 10, 11 ].includes( flags >>> 7 & 15 );
}
function clothes( flags: number ) {
	return [ 1, 9 ].includes( flags >>> 7 & 15 );
}
// 5666E3..56674A: full-mask refusal, then repeat without durability bit 4.
// This is a visual overlay; it must not disable dragging, selling or tooltips.
export function itemEquipmentOverlay(
	item: InventoryItem,
	progression: Progression,
	identity: EquipmentIdentity,
	worn: readonly InventoryItem[]
): "icon_disable" | "icon_item_broken" | "icon_item_warning" | null {
	if ( !equipment( item.typeFlags ) ) return null;
	const f = item.tooltip?.fields;
	if ( !f ) return null;
	const quadPass = ( mastery: boolean ) => {
		let pass = true;
		for ( let i = 1; i <= 4; i++ ) {
			const type = f["reqLevelType" + i] ?? 0, required = f["requiredLevel" + (i === 1 ? "" : i)] ?? 0;
			if ( mastery ? type <= 10 : type !== 1 ) continue;
			const level = mastery ? progression.masteries.find( m => m.id === type )?.level : progression.level;
			// Native missing mastery records do not participate; country is separate.
			if ( level === undefined ) continue;
			if ( Math.min( 255, Math.max( 0, level ) ) >= required ) return true;
			pass = false;
		}
		return pass;
	};
	const denied = (f.reqStr ?? 0) > (progression.stats?.strength ?? 0) ||
		(f.reqInt ?? 0) > (progression.stats?.intellect ?? 0) ||
		!quadPass( false ) || !quadPass( true ) ||
		(identity.sex !== undefined && f.reqGender !== undefined && f.reqGender !== 2 &&
			identity.sex !== f.reqGender) ||
		(body( item.typeFlags ) &&
			worn.some( row =>
				row.slot < 6 && body( row.typeFlags ) && clothes( row.typeFlags ) !== clothes( item.typeFlags )
			)) ||
		(identity.country !== undefined && f.country !== undefined && f.country !== 3 &&
			identity.country !== f.country);
	if ( denied ) return "icon_disable";
	if ( (f.maxDurability ?? 0) <= 0 ) return null;
	if ( item.durability === 0 ) return "icon_item_broken";
	return item.durability !== undefined && item.durability <= 6 && ![ 5, 12 ].includes( item.typeFlags >>> 7 & 15 ) ?
		"icon_item_warning" :
		null;
}

/*
================
wearerSex

The sex selector an item's ReqGender is compared against. Native
draws read it off the live character: the slot renderer's 789C60
call (5666E3) takes the wearer from the local player's current
model (+0x1AC) on every draw, so a gender change re-marks the items
at once. The login roster's gender (creation's 0 male, 1 female) is
only the stand-in for before the local actor is known; it predates
any in-world change and must never override the live value.
================
*/
export function wearerSex( liveSex: number | undefined, rosterGender: number | undefined ): number | undefined {
	if ( liveSex !== undefined ) return liveSex;
	return rosterGender === 0 ? 1 : rosterGender === 1 ? 0 : undefined;
}

// 68EBB5: timer 3 runs every 80ms; 6875F0 cycles 0..16. 5667CE maps to 16 atlas frames.
export function equipmentWarningUv( phase: number ): readonly [number, number, number, number] {
	const frame = Math.trunc( (phase % 17) / 17 * 16 );
	return [ (frame % 8) / 8, Math.floor( frame / 8 ) / 4, 1 / 8, 1 / 4 ];
}
