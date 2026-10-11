/*
===========================================================================

unique-notices.ts - the 0x3058 event notice: unique appearances and kills

v1.150 74D451 registers 0x3058 -> 7508C0. Unlike the newer server's 0x300C
envelope, v1.150 starts with one kind byte; no live entity or GID is
required. Kinds 5 and 6 announce a unique's appearance and death; kinds
outside 5..10, and 8 and 9, show the category 0x11 notice whose code is
the kind (kind 1: a thief may not enter a town with its transport).

===========================================================================
*/
import { constantNativeNotice } from "./native-notice";
import type { SystemNotice } from "./system-notices";

// 7508C0's kinds: the unique announcements, and the category its other
// kinds' notices come from.
const KIND_UNIQUE_APPEARED = 5;
const KIND_UNIQUE_KILLED = 6;
const KIND_LAST_SPECIAL = 10;
const EVENT_NOTICE_CATEGORY = 0x11;

/*
================
UniqueReference
================
*/
interface UniqueReference {
	readonly symbol: string;
	readonly name: string;
}

/*
================
nativeWording

These five branches are literal comparisons in v1.150 750949..750A77; all
other authored references use the generic template, including new ones.
================
*/
function nativeWording( symbol: string ): string | undefined {
	switch ( symbol ) {
		case "SN_MOB_CH_TIGERWOMAN":
			return "TIGER_GIRL";
		case "SN_MOB_KK_ISYUTARU":
			return "IYUTARU";
		case "SN_MOB_OA_URUCHI":
			return "URRUCHI";
		case "SN_MOB_TK_BONELORD":
			return "BONELORD";
		case "SN_MOB_RM_TAHOMET":
			return "TAHOMET";
	}
	return undefined;
}

/*
================
uniqueReferences

The monster references a unique notice may name, from the bootstrap.
================
*/
export function uniqueReferences( value: unknown ): ReadonlyMap<number, UniqueReference> {
	const rows = (value as { refObjSnapshot?: unknown; })?.refObjSnapshot ?? [];
	if ( !Array.isArray( rows ) ) throw Error( "Invalid announcement references" );
	const refs = new Map<number, UniqueReference>();
	for ( const row of rows ) {
		if ( row.kind !== "monster" ) continue;
		if (
			!Number.isInteger( row.refObjId ) || row.refObjId <= 0 || row.refObjId > 0xffffffff ||
			typeof row.name !== "string" || typeof row.nameStrId !== "string"
		) {
			throw Error( "Invalid unique reference" );
		}
		refs.set( row.refObjId, { symbol: row.nameStrId, name: row.name } );
	}
	return refs;
}

/*
================
eventNotice

7508C0's other kinds: outside 5..10, and 8 and 9, the category 0x11 notice
of that code. Kind 10 also closes an open NPC shop and is not handled here;
kind 7 has its own branch.
================
*/
function eventNotice( kind: number ): SystemNotice | null {
	if ( kind < KIND_UNIQUE_APPEARED || kind > KIND_LAST_SPECIAL || kind === 8 || kind === 9 ) {
		return constantNativeNotice( EVENT_NOTICE_CATEGORY, kind );
	}
	return null;
}

/*
================
uniqueNotice

The notice a 0x3058 frame shows, or null for another opcode or a kind this
port does not present.
================
*/
export function uniqueNotice(
	opcode: number,
	p: Uint8Array,
	refs: ReadonlyMap<number, UniqueReference>
): SystemNotice | null {
	if ( opcode !== 0x3058 ) return null;
	if ( p.length === 0 ) throw Error( "Missing announcement subtype" );
	const kind = p[0]!;
	// 7508C0 reads only the kind for these; anything after it is ignored.
	if ( kind !== KIND_UNIQUE_APPEARED && kind !== KIND_UNIQUE_KILLED ) return eventNotice( kind );
	if ( p.length < 5 ) throw Error( "Truncated unique reference" );
	const view = new DataView( p.buffer, p.byteOffset, p.byteLength ), id = view.getUint32( 1, true );
	let killer: string | undefined;
	if ( kind === KIND_UNIQUE_APPEARED ) {
		if ( p.length !== 5 ) throw Error( "Trailing unique appearance bytes" );
	} else {
		if ( p.length < 7 ) throw Error( "Truncated unique killer" );
		const length = view.getUint16( 5, true );
		if ( p.length !== 7 + length ) throw Error( "Invalid unique killer length" );
		killer = new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( 7 ) );
	}
	const ref = refs.get( id );
	if ( !ref ) return null;
	// 750C0E et al. compare against the UTF-16 literal "???" at C0B1DC.
	const suffix = nativeWording( ref.symbol ), dead = kind === KIND_UNIQUE_KILLED, anonymous = killer === "???";
	const prefix = !dead ? "APPEAR" : anonymous ? "DEAD" : "ANYONE_DEAD";
	const args = !dead || anonymous ? (suffix ? [] : [ ref.name ]) : suffix ? [ killer! ] : [ killer!, ref.name ];
	return { key: `UIIT_MSG_${prefix}_${suffix ?? "UNIC"}`, value: 0, nativeType: 0, arguments: args, banner: true };
}
