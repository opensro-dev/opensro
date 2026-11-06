/*
===========================================================================

damage-feedback.ts - display-only periodic and paid Bond damage bodies

HP stays owned by vitals updates. Bond is a port-only pet extension.
===========================================================================
*/
import type { EnvironmentalDamage } from "@/engine/contracts/combat-feedback";

/*
================
damageFeedback

74D9FD -> 74FE80: periodic damage is cosmetic. Legacy Bond pulses carry no amount.
================
*/
export function damageFeedback(
	op: number,
	p: Uint8Array
): Pick<EnvironmentalDamage, "gid" | "damage" | "linked"> | undefined {
	if ( op === 0x3128 ) {
		if ( p.length !== 8 ) throw Error( "Invalid periodic damage notification" );
		const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
		return { gid: v.getUint32( 0, true ), damage: v.getUint32( 4, true ) };
	}
	if ( op !== 0x3fb4 ) throw Error( "Invalid damage feedback opcode" );
	if ( p.length !== 9 && p.length !== 13 ) throw Error( "Invalid pet bond pulse length" );
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	if (
		!v.getUint32( 0, true ) || !v.getUint32( 4, true ) || (p[8] !== 1 && p[8] !== 2) ||
		(p.length === 13 && p[8] !== 1)
	) throw Error( "Invalid pet bond pulse" );
	return p.length === 13 ? { gid: v.getUint32( 4, true ), damage: v.getUint32( 9, true ), linked: true } : undefined;
}
