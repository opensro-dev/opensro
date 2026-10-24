/*
===========================================================================

party-member.ts - the shared 75DB30 party member record

The roster (social.ts) and the matching approval (party-matching.ts)
decode the same masked member record off the wire. This module owns its
layout - mask bits, field order, string budget - so neither consumer
guesses where a row ends and the following bytes stay put.

===========================================================================
*/
import type { PartyMember } from "./social-roster";
export type { PartyMember } from "./social-roster";

/*
================
unknownPartyMember

The base a 0x3E58 type-6 row decodes over when the roster does not hold
its member: the row is consumed and dropped (761870's setters skip it).
================
*/
export function unknownPartyMember( id: number ): PartyMember {
	return { id, name: "", model: 0, level: 0, status: 0, region: 0, x: 0, y: 0, z: 0, war: 0 };
}

/*
================
readPartyMember

Shared 75DB30 member record: roster and matching approval use the same wire owner.
================
*/
export function readPartyMember( p: Uint8Array, start = 0, old?: PartyMember ): { member: PartyMember; end: number; } {
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	let at = start;
	/*
	================
	take
	================
	*/
	function take( n: number ) {
		if ( at + n > p.length ) throw Error( "Truncated party member" );
		const out = at;
		at += n;
		return out;
	}
	const u8 = () => v.getUint8( take( 1 ) ),
		u16 = () => v.getUint16( take( 2 ), true ),
		u32 = () => v.getUint32( take( 4 ), true ),
		i16 = () => v.getInt16( take( 2 ), true );
	const mask = u8();
	const member: {
		id: number;
		name: string;
		model: number;
		level: number;
		status: number;
		region: number;
		x: number;
		y: number;
		z: number;
		war: number;
		guild?: string;
		native41?: number;
		primaryMastery?: number;
		secondaryMastery?: number;
	} = { id: 0, name: "", model: 0, level: 0, status: 0, region: 0, x: 0, y: 0, z: 0, war: 0, ...old };
	if ( mask & 16 ) member.id = u32();
	if ( mask & 1 ) {
		const n = u16();
		if ( n > 2048 ) throw Error( "Social string budget" );
		const offset = take( n );
		member.name = new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( offset, offset + n ) );
		member.model = u32();
	}
	if ( mask & 2 ) member.level = u8();
	if ( mask & 4 ) member.status = u8();
	if ( mask & 32 ) {
		member.region = u16();
		member.x = i16();
		member.y = i16();
		member.z = i16();
		member.war = u32();
	}
	if ( mask & 64 ) {
		const n = u16();
		if ( n > 2048 ) throw Error( "Social string budget" );
		const offset = take( n );
		member.guild = new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( offset, offset + n ) );
	}
	if ( mask & 128 ) member.native41 = u8();
	if ( mask & 8 ) {
		member.primaryMastery = u32();
		member.secondaryMastery = u32();
	}
	if ( !member.id ) throw Error( "Missing party member identity" );
	return { member, end: at };
}
