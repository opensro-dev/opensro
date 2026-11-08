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
	const pulse = decodePetBondPulse( p );
	return pulse.damage === undefined ? undefined : { gid: pulse.bear, damage: pulse.damage, linked: true };
}

/*
================
decodePetBondPulse

The one reader of the port-only Guardian's Bond pulse (0x3FB4): {u32 pet gid,
u32 bearer gid, u8 kind} with kind 1 (transfer) or 2, and the 13-byte paid
form appending the transferred u32 amount, which only a transfer carries.
The bearer's damage text (damageFeedback) and the pet's pose cue (entities.ts)
both read it here, so a change to the pulse lands in one place.
================
*/
export function decodePetBondPulse(
	p: Uint8Array
): { gid: number; bear: number; kind: 1 | 2; damage?: number; } {
	if ( p.length !== 9 && p.length !== 13 ) throw Error( "Invalid pet bond pulse length" );
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	const gid = v.getUint32( 0, true ), bear = v.getUint32( 4, true ), kind = p[8]!;
	if ( !gid || !bear || (kind !== 1 && kind !== 2) || (p.length === 13 && kind !== 1) ) {
		throw Error( "Invalid pet bond pulse" );
	}
	// The throw above proves the kind; TypeScript does not carry it out of the test.
	const known = kind as 1 | 2;
	return p.length === 13 ? { gid, bear, kind: known, damage: v.getUint32( 9, true ) } : { gid, bear, kind: known };
}
