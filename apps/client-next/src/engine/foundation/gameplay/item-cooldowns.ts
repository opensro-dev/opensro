/*
===========================================================================

item-cooldowns.ts - native recovery and cure category cooldowns

Receipts start these clocks. Inventory and quickslot controls share the same
categories, independent of the stack or reference ID used.

===========================================================================
*/
/*
================
RecoveryCategory
================
*/
export type RecoveryCategory = 1 | 2 | 3;
/*
================
PotionCategory
================
*/
export type PotionCategory = RecoveryCategory | 4 | 5 | 6 | 7 | 13 | 14 | 15;
/*
================
ItemCooldown
================
*/
export interface ItemCooldown {
	readonly category: PotionCategory | 18;
	readonly group?: number;
	readonly refObjId?: number;
	readonly startedAtMs: number;
	readonly durationMs: number;
}
const ORDINARY_RECOVERY_MS = 1000;
const EUROPEAN_RECOVERY_MS = 15000;
const PERCENT_RECOVERY_MS = 4000;
const UNIVERSAL_PILL_MS = 20000;

// SRO_Client 755E40 / 565400: recovery lanes are TID4, not bag slot or reference ID.
/*
================
recoveryCategory
================
*/
export function recoveryCategory( typeFlags: number ): RecoveryCategory | null {
	const category = typeFlags >>> 11;
	return (typeFlags & 0x7fe) === 0xec && category >= 1 && category <= 3 ? category as RecoveryCategory : null;
}
/*
================
itemCooldown
================
*/
export function itemCooldown(
	rows: readonly ItemCooldown[],
	target: number | {
		readonly typeFlags: number;
		readonly refObjId: number;
		readonly tooltip?: { readonly fields: Readonly<Record<string, number>>; };
	},
	now: number
): ItemCooldown | undefined {
	const typeFlags = typeof target === "number" ? target : target.typeFlags;
	const category = potionCategory( typeFlags );
	const group = typeof target === "number" ? 0 : target.tooltip?.fields.useCooldownGroup524 ?? 0;
	return rows.find( row =>
		now < row.startedAtMs + row.durationMs &&
		(row.category === category || typeof target !== "number" && category === null && row.category === 18 &&
				(group ? row.group === group : !row.group && row.refObjId === target.refObjId))
	);
}
// Native receipt 755FF4..756084. The server independently owns its reuse guard.
/*
================
recoveryCooldownMs
================
*/
export function recoveryCooldownMs(
	category: RecoveryCategory,
	fields: Readonly<Record<string, number>>,
	country: number,
	abnormal: number
): number {
	if ( country !== 0 && country !== 1 ) throw Error( "Invalid recovery cooldown country" );
	const percentage = (fields.itemParam2_2a0 ?? 0) !== 0 || (fields.itemParam4_2a8 ?? 0) !== 0;
	const duration = percentage ? PERCENT_RECOVERY_MS : country === 0 ? ORDINARY_RECOVERY_MS : EUROPEAN_RECOVERY_MS;
	return duration +
		((category === 1 && (abnormal & 0x200000) !== 0 || category === 2 && (abnormal & 0x400000) !== 0) ?
			PERCENT_RECOVERY_MS :
			0);
}

/*
================
potionCategory

6961B0 admission, 755E40 receipts and 565400 icons use these categories.
Revival, berserk and structure repair do not have a potion category timer.
================
*/
export function potionCategory( typeFlags: number ): PotionCategory | null {
	const recovery = recoveryCategory( typeFlags );
	if ( recovery !== null ) return recovery;
	const subtype = typeFlags >>> 11;
	if ( (typeFlags & 0x7fe) === 0xec ) {
		switch ( subtype ) {
			case 4:
				return 4;
			case 5:
				return 5;
			case 7:
				return 6;
			case 9:
				return 7;
		}
	}
	if ( (typeFlags & 0x7fe) === 0x16c ) {
		switch ( subtype ) {
			case 1:
				return 13;
			case 6:
				return 14;
			case 7:
				return 15;
		}
	}
	return null;
}
/*
================
potionCooldownMs

756090..7562A1: companion recovery and both cure races use fixed clocks.
================
*/
export function potionCooldownMs(
	category: PotionCategory,
	fields: Readonly<Record<string, number>>,
	country: number | undefined,
	abnormal: number
): number {
	if ( category <= 3 ) {
		if ( country === undefined ) throw Error( "Missing recovery cooldown country" );
		return recoveryCooldownMs( category as RecoveryCategory, fields, country, abnormal );
	}
	return category === 13 ? UNIVERSAL_PILL_MS : ORDINARY_RECOVERY_MS;
}
