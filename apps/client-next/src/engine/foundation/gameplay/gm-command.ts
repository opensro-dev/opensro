/*
===========================================================================

gm-command.ts - the GM console's 0x75B6 requests and 0xB5B6 replies

The client's ChatInput_HandleGmCommand (509CF0) composes one 0x75B6 frame
per slash line; 751EC0 consumes the 0xB5B6 reply. Only arms whose wire
layout was verified in the client are composed here. Context-dependent
object and waypoint commands need their own reference authority; this
module never sends fabricated IDs.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";

const OP_GM_COMMAND = 0x75b6;
const MAX_ITEM_REFERENCES = 65536;
const MAX_GM_NAME_BYTES = 128;
// The port's /SILK name amount (operator tooling, not native): subcommand
// 0xF0, past the native table's last command (0x30).
const GM_GRANT_SILK = 0xf0;
const MAX_SILK_GRANT = 1_000_000;

/*
================
GmReply
================
*/
export interface GmReply {
	readonly text?: string;
	readonly key?: string;
	readonly console: boolean;
}

/*
================
GmMonsterReference

A monster row the GM typed by codename, with its static type byte (client
record +0xA0).
================
*/
export interface GmMonsterReference {
	readonly refObjId: number;
	readonly monsterType: number;
}

/*
================
GmItemReference
================
*/
export interface GmItemReference {
	readonly refObjId: number;
	readonly codename: string;
	readonly typeFlags: number;
	readonly maxStack: number;
}

/*
================
gmItemReferences

The /MAKEITEM catalog, validated at the worker boundary.
================
*/
export function gmItemReferences( value: unknown ): Map<string, GmItemReference> {
	const rows = (value as { itemCommandReferences?: unknown; })?.itemCommandReferences ?? [];
	if ( !Array.isArray( rows ) || rows.length > MAX_ITEM_REFERENCES ) throw Error( "Invalid GM item catalog" );
	const result = new Map<string, GmItemReference>();
	for ( const row of rows ) {
		const r = row as GmItemReference;
		if (
			!r || typeof r.codename !== "string" || !r.codename.startsWith( "ITEM_" ) || r.codename.length > 256 ||
			!Number.isInteger( r.refObjId ) || r.refObjId <= 0 || r.refObjId > 0xffffffff ||
			!Number.isInteger( r.typeFlags ) || r.typeFlags < 0 || r.typeFlags > 65535 ||
			!Number.isInteger( r.maxStack ) || r.maxStack < 1 || r.maxStack > 65535 ||
			result.has( r.codename )
		) throw Error( "Invalid GM item reference" );
		result.set( r.codename, r );
	}
	return result;
}

/*
================
gmReply

B5B6 / 751EC0. The failure byte is passed to category 0 of 689420, not to
a GM-specific error table. Most failure codes are deliberately silent.
================
*/
export function gmReply( p: Uint8Array ): GmReply | null {
	if ( !p.length ) throw Error( "Truncated GM reply" );
	const result = p[0];
	if ( result !== 1 && result !== 2 ) return null;
	if ( p.length < 2 ) throw Error( "Truncated GM reply command" );
	const code = p[1]!;
	const string = ( failure = false ) => {
		if ( p.length < 4 ) throw Error( "Truncated GM reply string" );
		const size = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint16( 2, true );
		if ( p.length !== 4 + size + Number( failure ) ) throw Error( "Invalid GM reply string length" );
		return new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( 4, 4 + size ) );
	};
	if ( code === 0x19 || code === 0x1a ) {
		const value = string( result === 2 );
		const marked = (result === 1 && code === 0x1a) || (result === 2 && code === 0x19);
		return { console: true, text: (result === 1 ? "-> " : "Failed. -> ") + (marked ? "*" : "") + value };
	}
	if ( result === 1 && code === 1 ) return { console: false, text: string() };
	if ( code === GM_GRANT_SILK ) {
		// The port's /SILK answer: the recipient's new balance, or a refusal.
		if ( result === 2 ) {
			if ( p.length !== 2 ) throw Error( "Trailing GM silk refusal bytes" );
			return { console: true, text: "Failed. -> silk grant refused" };
		}
		if ( p.length !== 6 ) throw Error( "Invalid GM silk reply" );
		const balance = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 2, true );
		return { console: true, text: "-> silk granted, balance " + balance };
	}
	if ( result === 1 && code === 4 ) {
		if ( p.length !== 14 ) throw Error( "Invalid GM world status" );
		const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
		return {
			console: false,
			text: `Player:${v.getInt32( 2, true )}, NPC_Mob:${v.getInt32( 6, true )}, DroppedItem:${
				v.getInt32( 10, true )
			}`
		};
	}
	if ( p.length !== 2 ) throw Error( "Trailing GM reply bytes" );
	if ( code === 0x20 || (result === 1 && code === 0x21) ) {
		return { console: false, text: "SiegeManager MSG Result - " + (result === 1 ? "Ok." : "Fail.") };
	}
	if ( result === 2 ) {
		const key = ({
			3: "UIIT_STT_ERR_COMMON_INVALID_TARGET",
			4: "UIIT_STT_ERR_COMMON_TOO_FAR",
			5: "UIIT_STT_ERR_COMMON_INVALID_OPERATION"
		} as Record<number, string>)[code];
		if ( key ) return { console: false, key };
	}
	return null;
}

/*
================
gmRequest

The verified name and scalar arms of 509CF0. monster resolves a typed
codename the way the native client reads its own character data.
================
*/
export function gmRequest(
	line: string,
	items: ReadonlyMap<string, GmItemReference> = new Map(),
	heading = 0,
	monster: ( codename: string ) => GmMonsterReference | undefined = () => undefined
): WireFrame | null {
	const tokens = line.trim().split( /\s+/ ), name = tokens[0], arg = tokens[1] ?? "";
	// Retail 50B303: WP sends u8 16, u16 region, float32 XYZ, u16 heading.
	// Coordinate convenience uses that authority instead of 50BEE0's local warp.
	if ( name === "/warp" ) {
		if ( tokens.length !== 5 ) return null;
		const [region, x, y, z] = tokens.slice( 1 ).map( Number );
		if (
			!Number.isInteger( region ) || region! <= 0 || region! > 65535 ||
			![ x, y, z ].every( v => Number.isFinite( v ) && Number.isFinite( Math.fround( v! ) ) ) ||
			!Number.isInteger( heading ) || heading < 0 || heading > 65535
		) return null;
		const payload = new Uint8Array( 17 ), v = new DataView( payload.buffer );
		payload[0] = 16;
		v.setUint16( 1, region!, true );
		v.setFloat32( 3, x!, true );
		v.setFloat32( 7, y!, true );
		v.setFloat32( 11, z!, true );
		v.setUint16( 15, heading, true );
		return { opcode: OP_GM_COMMAND, payload };
	}
	if ( name === "/MAKEITEM" && tokens.length === 3 ) {
		const ref = items.get( arg );
		if ( !ref ) return null;
		// 50A838 reads the parsed integer's low byte BEFORE clamping.
		const amount = (Number.parseInt( tokens[2]!, 10 ) || 0) & 255, stackable = (ref.typeFlags & 0x60) === 0x60;
		const parameter = stackable ? Math.min( ref.maxStack, Math.max( 1, amount ) ) & 255 : Math.min( 12, amount );
		const payload = Uint8Array.of( 7, 0, 0, 0, 0, parameter );
		new DataView( payload.buffer ).setUint32( 1, ref.refObjId, true );
		return { opcode: OP_GM_COMMAND, payload };
	}
	// 50A3D3 /LOADMONSTER codename count [CHAMP|GIANT|NORMAL]: subcmd 6, u32 ref,
	// u8 count (low byte, 1..255), u8 type. Without a type token 50A4A6 sends the
	// record's +0xA0 byte; an unrecognized token leaves 0 (50A49F).
	if ( name === "/LOADMONSTER" && (tokens.length === 3 || tokens.length === 4) ) {
		const ref = monster( arg );
		if ( !ref ) return null;
		const parsed = (Number.parseInt( tokens[2]!, 10 ) || 0) & 255, count = parsed <= 1 ? 1 : parsed;
		const word = tokens[3]?.toUpperCase();
		const type = tokens.length === 3 ?
			ref.monsterType & 255 :
			word === "NORMAL" ?
			0 :
			word === "GIANT" ?
			4 :
			word === "CHAMP" ?
			1 :
			0;
		const payload = Uint8Array.of( 6, 0, 0, 0, 0, count, type );
		new DataView( payload.buffer ).setUint32( 1, ref.refObjId, true );
		return { opcode: OP_GM_COMMAND, payload };
	}
	if ( name === "/SILK" ) {
		if ( tokens.length !== 3 || !/^[0-9]+$/.test( tokens[2]! ) ) return null;
		const amount = Number( tokens[2] ), bytes = new TextEncoder().encode( arg );
		if ( amount < 1 || amount > MAX_SILK_GRANT ) return null;
		if ( !bytes.length || bytes.length >= MAX_GM_NAME_BYTES || arg.includes( " " ) ) {
			throw Error( "Invalid GM name" );
		}
		const payload = new Uint8Array( 3 + bytes.length + 4 ), view = new DataView( payload.buffer );
		payload[0] = GM_GRANT_SILK;
		view.setUint16( 1, bytes.length, true );
		payload.set( bytes, 3 );
		view.setUint32( 3 + bytes.length, amount, true );
		return { opcode: OP_GM_COMMAND, payload };
	}
	const named = ({
		"/FINDUSER": 1,
		"/TOTOWN": 3,
		"/MOVETOUSER": 8,
		"/BAN": 13,
		"/RECALLUSER": 17,
		"/RECALLGUILD": 18,
		"/LIENAME": 25,
		"/REALNAME": 26
	} as Record<string, number>)[name ?? ""];
	if ( named !== undefined ) {
		if ( tokens.length !== 2 ) return null;
		const bytes = new TextEncoder().encode( arg );
		if ( !bytes.length || bytes.length >= MAX_GM_NAME_BYTES || arg.includes( "\0" ) ) {
			throw Error( "Invalid GM name" );
		}
		const payload = new Uint8Array( 3 + bytes.length );
		payload[0] = named;
		new DataView( payload.buffer ).setUint16( 1, bytes.length, true );
		payload.set( bytes, 3 );
		return { opcode: OP_GM_COMMAND, payload };
	}
	const simple = ({ "/GOTOWN": 2, "/WORLDSTATUS": 4, "/INVISIBLE": 14, "/INVINCIBLE": 15 } as Record<
		string,
		number
	>)[name ?? ""];
	if ( simple !== undefined ) {
		if ( simple >= 14 && tokens.length !== 1 ) return null;
		return { opcode: OP_GM_COMMAND, payload: Uint8Array.of( simple ) };
	}
	if ( name === "/SETTIME" && tokens.length === 2 ) {
		const hour = Number.parseInt( arg, 10 ) || 0;
		return { opcode: OP_GM_COMMAND, payload: Uint8Array.of( 10, hour < 0 || hour >= 24 ? 0 : hour ) };
	}
	if ( name === "/flagworld" && tokens.length === 2 ) {
		return { opcode: OP_GM_COMMAND, payload: Uint8Array.of( 0x30, Number.parseInt( arg, 10 ) || 0 ) };
	}
	if ( name === "/INSTANCE" && tokens.length === 3 ) {
		const payload = new Uint8Array( 5 ), view = new DataView( payload.buffer );
		payload[0] = 19;
		view.setUint16( 1, Number.parseInt( arg, 10 ) || 0, true );
		view.setUint16( 3, Number.parseInt( tokens[2]!, 10 ) || 0, true );
		return { opcode: OP_GM_COMMAND, payload };
	}
	return null;
}
