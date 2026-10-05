/*
===========================================================================

fortress-return.ts - the action window's "Return to fortress" (1015)

CGInterface_ExecuteActionCommand (695420) case 1015 sends 0x7025 [u32
fortress] for the fortress its guild occupies
(GlobalDataManager_FindFortressIdByOwnerName 7E24B0), refusing itself with
notice 0x1F/7 when there is none and 0x1F/8 while the portal cooldown
runs (CIFActionTabPanel +0x390). The server's refusal is 0xB025 [u8 2][u8
code] (CNetProcess_OnFortressReturnResult0xB025 7674C0); the cooldown
arrives as 0x3792 [u8 2][u8 5][u32 seconds]
(CNetProcessSecond_OnTimedJobState3792 766E30).

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import type { FortressState } from "./fortress";

export const ACTION_FORTRESS_RETURN = 1015;
export const OP_FORTRESS_RETURN = 0x7025;
export const OP_FORTRESS_RETURN_RESULT = 0xb025;
export const OP_TIMED_JOB_STATE = 0x3792;
// CGInterface_ShowSystemNotification category of the portal stone texts.
export const FORTRESS_PORTAL_NOTICE_CATEGORY = 0x1f;
export const FORTRESS_PORTAL_NO_FORTRESS = 7;
export const FORTRESS_PORTAL_NOT_YET = 8;
const RESULT_REFUSED = 2;
const TIMED_JOB_OWNER = 2;
const TIMED_JOB_FORTRESS_PORTAL = 5;

/*
================
fortressReturnRequest

The 0x7025 frame for the guild's fortress, or the 0x1F code the client
refuses with before sending.
================
*/
export function fortressReturnRequest(
	state: FortressState,
	guildName: string | undefined,
	cooldownRemainingMs: number
): { readonly frame: WireFrame; } | { readonly code: number; } {
	const owned = guildName ? state.wars.find( row => row.name === guildName ) : undefined;
	if ( !owned ) return { code: FORTRESS_PORTAL_NO_FORTRESS };
	if ( cooldownRemainingMs > 0 ) return { code: FORTRESS_PORTAL_NOT_YET };
	const payload = new Uint8Array( 4 );
	new DataView( payload.buffer ).setUint32( 0, owned.id, true );
	return { frame: { opcode: OP_FORTRESS_RETURN, payload } };
}

/*
================
fortressPortalCooldown

The seconds a 0x3792 kind-5 frame starts the portal cooldown with; null
for any other frame.
================
*/
export function fortressPortalCooldown( frame: WireFrame ): number | null {
	const p = frame.payload;
	if ( frame.opcode !== OP_TIMED_JOB_STATE || p[0] !== TIMED_JOB_OWNER || p[1] !== TIMED_JOB_FORTRESS_PORTAL ) {
		return null;
	}
	if ( p.length !== 6 ) throw Error( "Invalid fortress portal cooldown" );
	return Math.max( 0, new DataView( p.buffer, p.byteOffset, p.byteLength ).getInt32( 2, true ) );
}

/*
================
fortressReturnRefusal

The 0x1F code of a refused return; null for anything else.
================
*/
export function fortressReturnRefusal( frame: WireFrame ): number | null {
	const p = frame.payload;
	if ( frame.opcode !== OP_FORTRESS_RETURN_RESULT || p[0] !== RESULT_REFUSED ) return null;
	if ( p.length !== 2 ) throw Error( "Invalid fortress return result" );
	return p[1]!;
}
