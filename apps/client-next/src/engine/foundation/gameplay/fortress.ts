/*
===========================================================================

fortress.ts - fortress world state and incoming service responses

Owns the world-war projection used by appearance and music. Service replies
are decoded in full before replacing the last response in the snapshot.

===========================================================================
*/
import { fortressServiceReply, FORTRESS_SERVICE_REPLY, type FortressServiceReply } from "./fortress-services";
import type { WireFrame } from "@/engine/contracts/network";
/*
================
FortressState
================
*/
export interface FortressState {
	readonly service?: FortressServiceReply;
	readonly localKills?: number;
	readonly localDeaths?: number;
	readonly role?: number;
	readonly worldId: number;
	readonly worlds: readonly { id: number; code: string; }[];
	readonly fortresses: readonly { id: number; code: string; icon?: string; }[];
	readonly wars: readonly { id: number; name: string; flags: number; }[];
	readonly registered: readonly number[];
	readonly listId: number;
}
/*
================
fortressBootstrap
================
*/
export function fortressBootstrap( value: unknown ): FortressState {
	const b = value as {
		localPlayerEntry?: { fortressWorld?: number; };
		gameWorldData?: { gameWorldId: number; warName: string; }[];
		siegeFortressData?: { fortressId: number; codeName: string; icon?: string; }[];
	};
	const worlds = (b.gameWorldData ?? []).map( r => ({ id: r.gameWorldId, code: r.warName }) ),
		fortresses = (b.siegeFortressData ?? []).map( r => ({ id: r.fortressId, code: r.codeName, icon: r.icon }) );
	for ( const rows of [ worlds, fortresses ] ) {
		if (
			rows.length > 65536 || new Set( rows.map( r => r.id ) ).size !== rows.length ||
			rows.some( r => !Number.isInteger( r.id ) || r.id < 0 || r.id > 0xffffffff || typeof r.code !== "string" )
		) throw Error( "Invalid fortress reference table" );
	}
	// 77B220 -> 862B80: preserve the complete packed word, then resolve its low u16.
	const worldId = b.localPlayerEntry?.fortressWorld ?? 0x10001;
	if ( !Number.isInteger( worldId ) || worldId < 0 || worldId > 0xffffffff ) {
		throw Error( "Invalid packed fortress world" );
	}
	return { worldId, worlds, fortresses, wars: [], registered: [], listId: 0 };
}
/*
================
fortressPacket
================
*/
export function fortressPacket( state: FortressState, frame: WireFrame ): FortressState | null {
	if ( frame.opcode === FORTRESS_SERVICE_REPLY ) {
		const service = fortressServiceReply( frame );
		return service ? { ...state, service } : null;
	}
	if ( frame.opcode !== 0x3887 ) return null;
	const p = frame.payload, v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	let o = 0;
	/*
 ================
 take
 ================
 */
	function take( n: number ) {
		if ( o + n > p.length ) throw Error( "Truncated fortress frame" );
		const at = o;
		o += n;
		return at;
	}
	const u8 = () => v.getUint8( take( 1 ) ),
		u16 = () => v.getUint16( take( 2 ), true ),
		u32 = () => v.getUint32( take( 4 ), true );
	/*
 ================
 str
 ================
 */
	function str() {
		const n = u16(), at = take( n );
		return new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( at, at + n ) );
	}
	const subtype = u8();
	let next = state;
	if ( subtype === 0 ) {
		const count = u8(), wars: FortressState["wars"][number][] = [];
		for ( let i = 0; i < count; i++ ) {
			const id = u32(), name = str();
			for ( let j = 0; j < 4; j++ ) u32();
			if ( u8() === 1 ) u32();
			if ( u8() === 1 ) u32();
			const at = wars.findIndex( r => r.id === id ), row = { id, name, flags: 0 };
			if ( at < 0 ) wars.push( row );
			else wars[at] = row;
		}
		const flags = u8(), listId = u32();
		next = { ...state, wars: wars.map( r => ({ ...r, flags }) ), listId };
	} else if ( subtype === 8 ) {
		const id = u32(), name = str();
		for ( let j = 0; j < 4; j++ ) u32();
		next = { ...state, wars: state.wars.map( r => r.id === id ? { ...r, name } : r ) };
	} else if ( subtype === 0x0c || subtype === 0x0d ) {
		const id = u32();
		u8();
		next = { ...state, listId: subtype === 0x0c ? id : 0 };
	} else if ( subtype === 0x11 ) {
		u32();
		const localKills = u32(), localDeaths = u32();
		next = { ...state, localKills, localDeaths };
	} else if ( subtype === 0x12 ) {
		u32();
		next = { ...state, role: u8() };
	} else if ( subtype === 0x10 ) {
		u32();
		const count = u8(), registered: number[] = [];
		for ( let i = 0; i < count; i++ ) {
			const id = u32();
			if ( id && !registered.includes( id ) ) registered.push( id );
		}
		next = { ...state, registered };
	} else if ( [ 2, 6, 0x31, 0x32, 0x33, 0x34 ].includes( subtype ) ) {
		const bit = subtype === 2 || subtype === 6 ? 1 : subtype < 0x33 ? 4 : 2,
			set = subtype === 2 || subtype === 0x31 || subtype === 0x33;
		// 7E2100 uses XOR on the off arm, not AND-NOT.
		next = { ...state, wars: state.wars.map( r => ({ ...r, flags: set ? r.flags | bit : r.flags ^ bit }) ) };
	} else if ( ![ 1, 3, 4, 5, 9 ].includes( subtype ) ) return null;
	if ( o !== p.length ) throw Error( "Trailing fortress frame bytes" );
	return next;
}
/*
================
fortressActive
================
*/
export function fortressActive( state: FortressState ): boolean {
	const code = state.worlds.find( r => r.id === (state.worldId & 65535) )?.code,
		id = state.fortresses.find( r => r.code === code )?.id;
	return id !== undefined && ((state.wars.find( r => r.id === id )?.flags ?? 0) & 1) !== 0;
}
/*
================
fortressMusicActive
================
*/
export function fortressMusicActive( state: FortressState ): boolean {
	const code = state.worlds.find( r => r.id === (state.worldId & 65535) )?.code;
	const id = state.fortresses.find( r => r.code === code )?.id;
	return !!id && ((state.wars.find( r => r.id === id )?.flags ?? 0) & 1) !== 0;
}
// 76CDD3 chooses 3 AFTER start flags; 76D156 chooses 0 BEFORE end flags.
// Other fortress messages do not implicitly recompute the latched music mode.
/*
================
fortressMusicMode
================
*/
export function fortressMusicMode(
	mode: number,
	before: FortressState,
	after: FortressState,
	subtype: number
): number {
	if ( subtype === 2 && fortressMusicActive( after ) ) return 3;
	if ( subtype === 6 && fortressMusicActive( before ) ) return 0;
	return mode;
}
// 828150 includes equality with the local guild even when absent from +94.
/*
================
fortressStatus
================
*/
export function fortressStatus(
	state: FortressState,
	local: number,
	target: number,
	allies: readonly number[]
): number {
	if ( !fortressActive( state ) ) return 0xcd;
	if ( !local || !target ) return 0xc8;
	if ( state.registered.includes( local ) ) return state.registered.includes( target ) ? 0xc9 : 0xcc;
	if ( state.registered.includes( target ) ) return 0xca;
	return local === target || allies.includes( target ) ? 0xcb : 0xcc;
}
