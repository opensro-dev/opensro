/*
===========================================================================

pet-name.ts - naming a pet: the request, its refusals and the new name

A pet (COS band 3 or 4) without a name shows the COS info window's Name
button (CIFCOSInfo_OnNameButton 6A5990, gate +0x38C). The name is checked
by the client (CIFCos_OnNameDialogResult 6A59E0: 2..12 characters and
CStringCheck_IsAllowedName 790900), confirmed once ("can not change any
more") and sent (CIFCOSInfo_OnNameBoxResult_Send7571 6A5570):

	C->S 0x7571  u32 cos gid, u16 length, ASCII name
	S->C 0xB571  u8 2, u16 code (a refusal only; success sends no 0xB571)
	S->C 0x3508  u32 cos gid, u8 5, u16 length, ASCII name (the new name, to
	             the owner and to everyone near: 77A570 case 5)

The server (v1.188 0x7117 handler 511BB0) refuses with 0x440E (the name
rules: 12:14 NOTPUT), 0x440F (already used: 12:15 SUMENESS), 3 (not the
caller's summoned pet) and 5 (a bad length, or a pet already named).

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import { checkedNameError, type NameRules } from "@/engine/foundation/ui/character-create";

export const PET_NAME_REQUEST = 0x7571;
export const PET_NAME_REFUSAL = 0xb571;
export const COS_STATE_UPDATE = 0x3508;
// 0x3508's case byte for a new pet name (77A570 case 5).
export const COS_STATE_NAME = 5;
// 6A59E0's length bounds.
export const PET_NAME_MIN = 2;
export const PET_NAME_MAX = 12;

// The server's refusal codes.
const REFUSAL_NAME_RULES = 0x440e;
const REFUSAL_NAME_TAKEN = 0x440f;
const REFUSAL_NOT_SUMMONED = 3;

/*
================
refusalNotice

The notice a refusal code shows; any other code (5 among them) is an
invalid operation.
================
*/
function refusalNotice( code: number ): string {
	switch ( code ) {
		case REFUSAL_NAME_RULES:
			return "UIIT_MSG_COSPETERR_PETNAME_NOTPUT";
		case REFUSAL_NAME_TAKEN:
			return "UIIT_MSG_COSPETERR_PETNAME_SUMENESS";
		case REFUSAL_NOT_SUMMONED:
			return "UIIT_STT_ERR_COMMON_INVALID_TARGET";
		default:
			return "UIIT_STT_ERR_COMMON_INVALID_OPERATION";
	}
}

/*
================
petNameError

6A59E0's own check before anything is sent: the length, then the
character-name rules. The notice key on a refusal, or null.
================
*/
export function petNameError( name: string, rules: NameRules ): string | null {
	if ( name.length < PET_NAME_MIN || name.length > PET_NAME_MAX ) return "UIIT_MSG_COSPETERR_PETNAME_NOTPUT";
	return checkedNameError( name, rules ) ? "UIIT_MSG_COSPETERR_PETNAME_NOTPUT" : null;
}

/*
================
petNameRequest

The 0x7571 frame. The name rules admit ASCII only, so one byte a character.
================
*/
export function petNameRequest( gid: number, name: string ): WireFrame {
	if ( !Number.isInteger( gid ) || gid <= 0 || gid > 0xffffffff ) throw Error( "Invalid pet name target" );
	const bytes = new TextEncoder().encode( name );
	if ( bytes.length !== name.length || bytes.length < PET_NAME_MIN || bytes.length > PET_NAME_MAX ) {
		throw Error( "Invalid pet name" );
	}
	const payload = new Uint8Array( 6 + bytes.length ), v = new DataView( payload.buffer );
	v.setUint32( 0, gid, true );
	v.setUint16( 4, bytes.length, true );
	payload.set( bytes, 6 );
	return { opcode: PET_NAME_REQUEST, payload };
}

/*
================
petNameRefusal

The notice key of a 0xB571 refusal; null for any other frame.
================
*/
export function petNameRefusal( frame: WireFrame ): string | null {
	if ( frame.opcode !== PET_NAME_REFUSAL ) return null;
	const p = frame.payload;
	if ( p.length !== 3 || p[0] !== 2 ) throw Error( "Invalid pet name refusal" );
	const code = p[1]! | p[2]! << 8;
	return refusalNotice( code );
}

/*
================
petNameUpdate

A 0x3508 case 5 frame's pet and new name; null for any other frame. The
name is player text: it decodes leniently, so a stored name can never fail
a viewer's session; the layout stays strict.
================
*/
export function petNameUpdate( frame: WireFrame ): { gid: number; name: string; } | null {
	const p = frame.payload;
	if ( frame.opcode !== COS_STATE_UPDATE || p[4] !== COS_STATE_NAME ) return null;
	if ( p.length < 7 ) throw Error( "Truncated pet name update" );
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength ), n = v.getUint16( 5, true );
	if ( p.length !== 7 + n || n > PET_NAME_MAX ) throw Error( "Invalid pet name update" );
	const gid = v.getUint32( 0, true );
	if ( !gid ) throw Error( "Invalid pet name update" );
	return { gid, name: new TextDecoder( "utf-8" ).decode( p.subarray( 7 ) ) };
}
