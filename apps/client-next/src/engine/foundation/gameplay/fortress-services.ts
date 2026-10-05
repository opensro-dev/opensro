/*
===========================================================================

fortress-services.ts - the v1.150 fortress staff wire contract

Client 703130 writes requests; 754A40 reads replies. Monetary values remain
decimal strings across the gameplay snapshot to preserve signed 64-bit values.
This module owns wire validation, not guild permissions or durable state.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";

export const FORTRESS_SERVICE_REQUEST = 0x71e1;
export const FORTRESS_SERVICE_REPLY = 0xb1e1;
const MAX_SERVICE_ACTION = 0x18;
const MIN_INT64 = "-9223372036854775808";
const MAX_INT64 = "9223372036854775807";

/*
================
FortressServiceRequest

The construction completion (0xA) has no target field on the wire. Other
actions name either an NPC or, for 0x16/0x17, the object being removed.
================
*/
export interface FortressServiceRequest {
	readonly action: number;
	readonly target?: number;
	readonly fortress?: number;
	readonly reference?: number;
	readonly word?: number;
	readonly flag?: number;
	readonly gold?: string;
	// Item common +1A8 at 703321: collection clamps to the reference stack cap.
	readonly stackLimit?: number;
}

/*
================
FortressServiceReply

Fields are present only when that native response carries them. The consumer
must use action/result rather than retaining fields from a previous reply.
================
*/
export interface FortressServiceReply {
	readonly action: number;
	readonly result: 1 | 2;
	readonly error?: number;
	readonly fortress?: number;
	readonly taxRate?: number;
	readonly gold?: string;
	readonly flags?: number;
	readonly schedules?: readonly (readonly number[])[];
	readonly applicants?: readonly { readonly name: string; readonly level: number; readonly side: number; }[];
	readonly registered?: boolean;
	readonly side?: number;
	readonly reference?: number;
	readonly level?: number;
	readonly hp?: number;
	readonly producing?: boolean;
	readonly quantity?: number;
	readonly ready?: number;
	readonly productionTime?: string;
	readonly gate?: number;
	readonly structures?: readonly { readonly reference: number; readonly hp: number; }[];
}

/*
================
fortressServiceRequest
================
*/
export function fortressServiceRequest( request: FortressServiceRequest ): WireFrame {
	const bytes: number[] = [];
	/*
	================
	uint
	================
	*/
	function uint( value: number | undefined, width: number ) {
		if ( value === undefined || !Number.isInteger( value ) || value < 0 || value >= 2 ** (width * 8) ) {
			throw Error( "Invalid fortress request value" );
		}
		for ( let i = 0; i < width; i++ ) bytes.push( value >>> (i * 8) & 255 );
	}
	const action = request.action;
	if ( !Number.isInteger( action ) || action < 0 || action > MAX_SERVICE_ACTION ) {
		throw Error( "Invalid fortress request action" );
	}
	if ( action !== 0x0a ) uint( request.target, 4 );
	uint( action, 1 );
	if ( action !== 6 && action !== 9 ) uint( request.fortress, 4 );
	switch ( action ) {
		case 1:
			if (
				request.word === undefined || !Number.isInteger( request.word ) || request.word < -20 ||
				request.word > 20
			) {
				throw Error( "Invalid fortress tax rate" );
			}
			uint( request.word & 65535, 2 );
			break;
		case 2: {
			if ( request.gold === undefined || !/^-?\d+$/.test( request.gold ) ) throw Error( "Invalid fortress gold" );
			const value = BigInt( request.gold );
			if ( value < BigInt( MIN_INT64 ) || value > BigInt( MAX_INT64 ) ) throw Error( "Invalid fortress gold" );
			const encoded = BigInt.asUintN( 64, value );
			for ( let i = 0n; i < 8n; i++ ) bytes.push( Number( encoded >> (i * 8n) & 255n ) );
			break;
		}
		case 4:
		case 7:
		case 8:
			uint( request.flag, 1 );
			break;
		case 0x0b:
			uint( request.reference, 4 );
			uint( request.flag, 1 );
			break;
		case 0x0a:
		case 0x0c:
		case 0x0f:
		case 0x13:
			uint( request.reference, 4 );
			break;
		case 0x0e:
		case 0x12:
			uint( request.reference, 4 );
			uint( request.word, 2 );
			break;
		case 0x10:
		case 0x14:
			if (
				request.stackLimit === undefined || !Number.isInteger( request.stackLimit ) ||
				request.stackLimit < 0 || request.stackLimit > 65535 || request.word === undefined ||
				!Number.isInteger( request.word ) || request.word < 0 || request.word > 65535
			) throw Error( "Invalid fortress collection quantity" );
			uint( request.reference, 4 );
			uint( Math.min( request.word, request.stackLimit ), 2 );
			break;
		case 0x15:
			uint( request.word, 2 );
			break;
	}
	return { opcode: FORTRESS_SERVICE_REQUEST, payload: Uint8Array.from( bytes ) };
}

/*
================
fortressServiceReply

74F4F0 reads each schedule as sixteen bytes; 754A40 reads a one-byte
refusal, unlike the v1.188 server's two-byte 0x28xx code.
================
*/
export function fortressServiceReply( frame: WireFrame ): FortressServiceReply | null {
	if ( frame.opcode !== FORTRESS_SERVICE_REPLY ) return null;
	const payload = frame.payload, view = new DataView( payload.buffer, payload.byteOffset, payload.byteLength );
	let offset = 0;
	/*
	================
	take
	================
	*/
	function take( count: number ) {
		if ( offset + count > payload.length ) throw Error( "Truncated fortress service reply" );
		const start = offset;
		offset += count;
		return start;
	}
	const u8 = () => view.getUint8( take( 1 ) );
	const u16 = () => view.getUint16( take( 2 ), true );
	const u32 = () => view.getUint32( take( 4 ), true );
	const i64 = () => view.getBigInt64( take( 8 ), true ).toString();
	/*
	================
	schedule
	================
	*/
	function schedule(): number[] {
		const words: number[] = [];
		for ( let i = 0; i < 8; i++ ) words.push( u16() );
		return words;
	}
	/*
	================
	text
	================
	*/
	function text() {
		const length = u16(), start = take( length );
		return new TextDecoder( "utf-8", { fatal: true } ).decode( payload.subarray( start, start + length ) );
	}
	const action = u8(), result = u8();
	if ( action > MAX_SERVICE_ACTION ) throw Error( "Invalid fortress reply action" );
	if ( result !== 1 && result !== 2 ) throw Error( "Invalid fortress reply result" );
	let reply: FortressServiceReply = { action, result };
	if ( result === 2 ) {
		reply = { ...reply, error: u8() };
	} else {
		switch ( action ) {
			case 0:
				reply = { ...reply, fortress: u32(), taxRate: view.getInt16( take( 2 ), true ), gold: i64() };
				break;
			case 1:
				reply = { ...reply, taxRate: view.getInt16( take( 2 ), true ) };
				break;
			case 2:
				reply = { ...reply, gold: i64() };
				break;
			case 3:
			case 4:
				reply = { ...reply, flags: u8() };
				break;
			case 5: {
				const schedules = [ schedule(), schedule() ], count = u8();
				const applicants: { name: string; level: number; side: number; }[] = [];
				for ( let i = 0; i < count; i++ ) applicants.push( { name: text(), level: u8(), side: u8() } );
				reply = { ...reply, schedules, applicants };
				break;
			}
			case 6: {
				const schedules = [ schedule() ], registered = u8() !== 0;
				reply = registered ?
					{ ...reply, schedules, registered, fortress: u32(), side: u8() } :
					{ ...reply, schedules, registered };
				break;
			}
			case 7:
			case 8:
				reply = { ...reply, registered: action === 7, fortress: u32(), side: u8() };
				break;
			case 0x0a:
			case 0x0f:
			case 0x13:
				reply = { ...reply, fortress: u32(), reference: u32() };
				break;
			case 0x0b:
				reply = { ...reply, fortress: u32(), reference: u32(), level: u8() };
				break;
			case 0x0c:
				reply = { ...reply, fortress: u32(), reference: u32(), hp: u32() };
				break;
			case 0x0d:
			case 0x11: {
				const fortress = u32(), producing = u8() !== 0;
				reply = producing ?
					{
						...reply,
						fortress,
						producing,
						reference: u32(),
						quantity: u16(),
						ready: u8(),
						productionTime: i64()
					} :
					{ ...reply, fortress, producing };
				break;
			}
			case 0x0e:
			case 0x12:
				reply = { ...reply, fortress: u32(), reference: u32(), quantity: u16(), productionTime: i64() };
				break;
			case 0x10:
			case 0x14:
				reply = { ...reply, fortress: u32(), reference: u32(), quantity: u16() };
				break;
			case 0x15:
				reply = { ...reply, fortress: u32(), reference: u32(), gate: u16() };
				break;
			case 0x17:
				reply = { ...reply, reference: u32() };
				break;
			case 0x18: {
				const fortress = u32(), count = u8(), structures: { reference: number; hp: number; }[] = [];
				for ( let i = 0; i < count; i++ ) structures.push( { reference: u32(), hp: u32() } );
				reply = { ...reply, fortress, structures };
				break;
			}
		}
	}
	if ( offset !== payload.length ) throw Error( "Trailing fortress service reply bytes" );
	return reply;
}
