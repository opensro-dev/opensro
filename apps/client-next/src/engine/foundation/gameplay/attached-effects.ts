/*
===========================================================================

attached-effects.ts - native attached-effect identities and reference admission

The bootstrap owns packet layout flags. Normalize compact optional fields here
before either source or recipient decoders consume them; never guess a layout
from the packet length. Server instance tokens own retirement.

===========================================================================
*/

const MAX_EFFECT_REFERENCES = 65536;
const MAX_WIRE_U32 = 0xffffffff;
const ATTACHED_EFFECT_HEADER_BYTES = 12;
const WIRE_U32_BYTES = 4;
const HAWK_COMMAND_BYTES = 10;
const HAWK_DAMAGE_MASK = 0x7fff;
const HAWK_FATAL_BIT = 0x8000;

/*
================
AttachedEffect

One instance may carry a source subject and a display timer. Neither grants
the client authority to remove it when the timer reaches zero.
================
*/
export interface AttachedEffect {
	readonly cancellationRequestedAtMs?: number;
	readonly gid: number;
	readonly skill: number;
	readonly token: number;
	readonly phase: number;
	readonly subject?: { readonly gid: number; readonly name: string; };
	readonly extra?: number;
	readonly receivedAtMs?: number;
	readonly remainingMs?: number;
	readonly durationMs?: number;
	readonly restored?: boolean;
	readonly hawk?: { readonly revision: number; readonly target: number; readonly damage: number; };
}
// 74DC91 registers 357A -> 775510 -> 8E2D30: instance u32, target
// u32, packed damage u16. This is neither a cast nor an entity spawn.
/*
================
hawkCommand

Decode one retained hawk's target/result notification atomically.
================
*/
export function hawkCommand( payload: Uint8Array ) {
	if ( payload.length !== HAWK_COMMAND_BYTES ) throw Error( "Invalid summoned-hawk command" );
	const view = new DataView( payload.buffer, payload.byteOffset, payload.byteLength );
	return { token: view.getUint32( 0, true ), target: view.getUint32( 4, true ), damage: view.getUint16( 8, true ) };
}
// 8E38A3: the high bit fills result+8 (fatal), not result+A (critical).
/*
================
hawkResult

Expand the native damage word into the shared result grammar.
================
*/
export function hawkResult( packed: number ) {
	return {
		type: 0,
		flags: 1,
		damage: packed & HAWK_DAMAGE_MASK,
		fatal: !!(packed & HAWK_FATAL_BIT),
		secondaryAmount: 0
	};
}
/*
================
AttachedEffectReference

Required discriminators stay explicit; optional bootstrap flags encode false
by omission to keep the complete reference table within its admission budget.
================
*/
export interface AttachedEffectReference {
	readonly linkedSkillId?: number;
	readonly cancellationDeferred?: boolean;
	readonly nameHit?: boolean;
	readonly status: boolean;
	readonly effectRider: boolean;
	readonly effectDurationMs?: number;
	readonly zeroEffectDuration?: boolean;
	readonly hideDetectionBuff?: boolean;
	readonly indefiniteBuffTimer?: boolean;
	readonly huntingPoint?: boolean;
	readonly stealthDuration?: boolean;
}
/*
================
attachedEffectReferences

Validate the complete authority table before replacing live references.
776695 gates the B5ED rider on RPBU/STDU; 7767C1 gates only minimap tracking
on hntp. Neither flag is required for a generic linked source decoration.
================
*/
export function attachedEffectReferences(
	rows: readonly ({ id: number; } & AttachedEffectReference)[]
): ReadonlyMap<number, AttachedEffectReference> {
	const refs = new Map<number, AttachedEffectReference>();
	if ( rows.length > MAX_EFFECT_REFERENCES ) throw Error( "Attached effect reference capacity exceeded" );
	for ( const row of rows ) {
		for ( const field of [ "status", "effectRider" ] as const ) {
			if ( typeof row[field] !== "boolean" ) {
				throw Error(
					`Invalid attached effect reference ${row.id}: ${field} must be boolean (received ${typeof row[
						field
					]})`
				);
			}
		}
		if (
			!Number.isInteger( row.id ) || row.id <= 0 || row.id > MAX_WIRE_U32 ||
			(row.effectDurationMs !== undefined &&
				(!Number.isInteger( row.effectDurationMs ) || row.effectDurationMs < 0 ||
					row.effectDurationMs > MAX_WIRE_U32)) ||
			refs.has( row.id )
		) throw Error( "Invalid attached effect reference" );
		for (
			const field of [
				"huntingPoint",
				"stealthDuration",
				"nameHit",
				"zeroEffectDuration",
				"hideDetectionBuff",
				"indefiniteBuffTimer",
				"cancellationDeferred"
			] as const
		) {
			if ( row[field] !== undefined && typeof row[field] !== "boolean" ) {
				throw Error( "Invalid detection effect authority" );
			}
		}
		if (
			row.linkedSkillId !== undefined &&
			(!Number.isInteger( row.linkedSkillId ) || row.linkedSkillId < 0 || row.linkedSkillId > MAX_WIRE_U32)
		) throw Error( "Invalid linked skill reference" );
		refs.set( row.id, {
			linkedSkillId: row.linkedSkillId,
			cancellationDeferred: row.cancellationDeferred,
			hideDetectionBuff: row.hideDetectionBuff,
			indefiniteBuffTimer: row.indefiniteBuffTimer,
			zeroEffectDuration: row.zeroEffectDuration,
			nameHit: row.nameHit,
			status: row.status,
			effectRider: row.effectRider,
			huntingPoint: row.huntingPoint ?? false,
			stealthDuration: row.stealthDuration ?? false,
			...(row.effectDurationMs !== undefined ? { effectDurationMs: row.effectDurationMs } : {})
		} );
	}
	return refs;
}
// 776450: efta (+274) adds a byte; RPBU/STDU/DTDR (+2dc/+2ec/+300)
// add one u32 in total. Never infer skill configuration from packet length.
/*
================
attachedEffect

Decode the recipient half using its admitted native layout flags.
================
*/
export function attachedEffect(
	payload: Uint8Array,
	refs: ReadonlyMap<number, AttachedEffectReference>
): AttachedEffect {
	if ( payload.length < ATTACHED_EFFECT_HEADER_BYTES ) throw Error( "Invalid attached effect packet" );
	const v = new DataView( payload.buffer, payload.byteOffset, payload.byteLength ),
		ref = refs.get( v.getUint32( 4, true ) );
	if ( !ref ) throw Error( "Missing attached effect reference authority" );
	const hasPhase = ref.status;
	if (
		payload.length !==
			ATTACHED_EFFECT_HEADER_BYTES + Number( hasPhase ) + WIRE_U32_BYTES * Number( ref.effectRider )
	) throw Error( "Invalid attached effect packet" );
	const gid = v.getUint32( 0, true ),
		skill = v.getUint32( 4, true ),
		token = v.getUint32( 8, true ),
		phase = hasPhase ? v.getUint8( 12 ) : 2;
	if ( !gid || !skill ) throw Error( "Invalid attached effect identity" );
	return {
		gid,
		skill,
		token,
		phase,
		...(payload.length >= 16 ? { extra: v.getUint32( hasPhase ? 13 : 12, true ) } : {})
	};
}
// 7759B0: count followed by instance tokens, not skill or entity IDs.
/*
================
endedEffectTokens

Validate the complete counted teardown before retiring any effect.
================
*/
export function endedEffectTokens( payload: Uint8Array ): readonly number[] {
	const count = payload[0];
	if ( count === undefined || payload.length !== 1 + count * WIRE_U32_BYTES ) {
		throw Error( "Invalid effect teardown packet" );
	}
	const v = new DataView( payload.buffer, payload.byteOffset, payload.byteLength );
	return Array.from( { length: count }, ( _, i ) => v.getUint32( 1 + i * WIRE_U32_BYTES, true ) );
}

// 6E5D40 adds the rider to dura for newly applied local buff timers. Only
// server teardown retires an effect; an exhausted UI timer is not authority.
/*
================
effectRemainingMs

Expose a clamped display timer without changing the retained effect lifetime.
================
*/
export function effectRemainingMs( effect: AttachedEffect, nowMs: number ): number | null {
	return effect.remainingMs === undefined ?
		null :
		Math.max( 0, effect.remainingMs - Math.max( 0, nowMs - (effect.receivedAtMs ?? nowMs) ) );
}
