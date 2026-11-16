/*
===========================================================================

whisper-block.ts - the whisper block list

The blocked names the bootstrap carries, the 0x766F request that adds or
removes one, and its answer.

===========================================================================
*/

import type { WireFrame } from "@/engine/contracts/network";

/*
================
blockedWhisperers
================
*/
export function blockedWhisperers( value: unknown ): readonly string[] {
	const rows = (value as { character?: { blockedWhisperers?: unknown; }; })?.character?.blockedWhisperers ?? [];
	if (
		!Array.isArray( rows ) || rows.length > 255 ||
		rows.some( name =>
			typeof name !== "string" || !name || name.includes( "\0" ) || new TextEncoder().encode( name ).length >= 128
		) || new Set( rows ).size !== rows.length
	) throw Error( "Invalid blocked names" );
	return [ ...rows ];
}

/*
================
whisperBlockRequest
================
*/
export function whisperBlockRequest( name: string, blocked: boolean ): WireFrame {
	const bytes = new TextEncoder().encode( name );
	if ( typeof blocked !== "boolean" || !bytes.length || bytes.length >= 128 || name.includes( "\0" ) ) {
		throw Error( "Invalid block request" );
	}
	const payload = new Uint8Array( bytes.length + 3 );
	payload[0] = blocked ? 1 : 2;
	new DataView( payload.buffer ).setUint16( 1, bytes.length, true );
	payload.set( bytes, 3 );
	return { opcode: 0x766f, payload };
}

/*
================
whisperBlockResult

771550: zero is success; 1..3 show these precise messages; 4 has no UI effect.
================
*/
export function whisperBlockResult( payload: Uint8Array ) {
	if ( payload.length < 2 || ![ 1, 2 ].includes( payload[0]! ) ) throw Error( "Invalid block result" );
	const mode = payload[0]!, result = payload[1]!;
	if ( result ) {
		if ( payload.length !== 2 ) throw Error( "Trailing block result" );
		return {
			mode,
			result,
			key: ({
				1: "UIIT_MSG_COSPETERR_PETNAME_SUMENESS",
				2: "UIIT_MSG_ALIAS_ACTION_ERR_NOTALLOW",
				3: "UIIT_STT_BLOCKMAN_DELETE_LISTFULL"
			} as Record<number, string>)[result]
		};
	}
	if ( payload.length < 4 ) throw Error( "Truncated block name" );
	const length = new DataView( payload.buffer, payload.byteOffset, payload.byteLength ).getUint16( 2, true );
	if ( !length || length >= 128 || payload.length !== 4 + length ) throw Error( "Invalid block name" );
	const name = new TextDecoder( "utf-8", { fatal: true } ).decode( payload.subarray( 4 ) );
	if ( name.includes( "\0" ) ) throw Error( "Invalid block name" );
	return { mode, result, name };
}
