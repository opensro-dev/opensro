/*
===========================================================================

return-scroll.ts - item-owned delay rows for return and structure repair

The successful receipt starts the native CIFDelayInfo row before the spent
stack disappears. Repair also binds its source-effect token for cancellation.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import { isReturnScroll } from "./travel";

/*
================
ReturnScrollCast
================
*/
export interface ReturnScrollCast {
	readonly refObjId: number;
	readonly name: string;
	readonly startedAtMs: number;
	readonly durationMs: number;
	readonly skillId?: number;
	readonly token?: number;
}

/*
================
returnScrollCast

755E40 and 6B1B30: mode 0 reads the return parameter; mode 4 follows the
repair kit's first associated skill and reads its authored duration.
================
*/
export function returnScrollCast( item: InventoryItem | undefined, now: number ): ReturnScrollCast | undefined {
	if ( !item ) return undefined;
	const repair = (item.typeFlags & 0x7fe) === 0xec && item.typeFlags >>> 11 === 10;
	if ( !repair && !isReturnScroll( item.typeFlags ) ) return undefined;
	const fields = item.tooltip?.fields;
	const duration = repair ? fields?.useSkillDurationMs : fields?.itemParam1_29c;
	if ( duration === undefined || !Number.isInteger( duration ) || duration < 0 || duration > 0xffffffff ) {
		throw Error( "Missing item delay duration authority" );
	}
	const skillId = repair ? fields?.useSkillId : undefined;
	if ( repair && (!Number.isInteger( skillId ) || !skillId || skillId < 0 || skillId > 0xffffffff) ) {
		throw Error( "Missing repair skill authority" );
	}
	return {
		refObjId: item.refObjId,
		name: item.name ?? "",
		startedAtMs: now,
		durationMs: duration,
		...(repair ? { skillId } : {})
	};
}
