/*
===========================================================================

return-scroll.ts - item-owned delay rows for return and structure repair

The successful receipt starts the native CIFDelayInfo row before the spent
stack disappears. Repair also binds its source-effect token for cancellation.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import { isReturnScroll } from "./travel";

// The server's port-only SRO_FAST_DEN_RETURN divides the Bandit Den scroll's
// countdown by this (apps/server action/denreturn.go).
const DEN_RETURN_SPEEDUP = 2;

/*
================
ReturnScrollCast

skillId marks the repair kit's kind-4 delay row, which raises CIFDelayInfo
+0x36C; gameplay's interactionBlocked (67D090) reads it. A later delay
kind that raises the flag (1 or 3) joins that predicate when it is ported.
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
fastDenReturn is the server's port-only rule: the Bandit Den scroll, the
one return scroll whose Param2 is 0 (teleport mode 2), counts down half its
Param1, as the server does.
================
*/
export function returnScrollCast(
	item: InventoryItem | undefined,
	now: number,
	fastDenReturn = false
): ReturnScrollCast | undefined {
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
	const den = !repair && fastDenReturn && fields?.itemParam2_2a0 === 0;
	return {
		refObjId: item.refObjId,
		name: item.name ?? "",
		startedAtMs: now,
		durationMs: den ? Math.trunc( duration / DEN_RETURN_SPEEDUP ) : duration,
		...(repair ? { skillId } : {})
	};
}
