/*
===========================================================================

combat-result.ts - native cast phases and independent damage result batches

Decode the whole packet before HP or presentation owners change. B245/B505
carry steering around a result batch; B3C6 mode 2 carries the batch directly.

===========================================================================
*/

import type { CastImpact, CastTargetResult, Pose } from "@/engine/contracts/gameplay";

const MAX_RESULT_RECORDS = 16384;
const RESULT_TARGETS = 1;
const RESULT_CORRECTION = 2;
const RESULT_TRAVEL = 8;
const PULSE_RESULTS = 2;

/*
================
resultReader

One bounded cursor owns nested reads. Both packet envelopes use the same
result grammar, as native 8E0190 does.
================
*/
function resultReader( payload: Uint8Array, offset: number ) {
	const view = new DataView( payload.buffer, payload.byteOffset, payload.byteLength );
	let cursor = offset;
	/*
	================
	need
	================
	*/
	function need( count: number ) {
		if ( cursor < 0 || cursor + count > payload.length ) throw Error( "Truncated cast result" );
	}
	/*
	================
	u8
	================
	*/
	function u8() {
		need( 1 );
		return view.getUint8( cursor++ );
	}
	/*
	================
	u16
	================
	*/
	function u16() {
		need( 2 );
		const value = view.getUint16( cursor, true );
		cursor += 2;
		return value;
	}
	/*
	================
	i16
	================
	*/
	function i16() {
		need( 2 );
		const value = view.getInt16( cursor, true );
		cursor += 2;
		return value;
	}
	/*
	================
	u32
	================
	*/
	function u32() {
		need( 4 );
		const value = view.getUint32( cursor, true );
		cursor += 4;
		return value;
	}
	/*
	================
	position
	================
	*/
	function position(): Omit<Pose, "angle"> {
		return { regionId: u16(), x: i16(), y: i16(), z: i16() };
	}
	/*
	================
	batch

	Stage count belongs to the whole batch, including targets introduced by a
	later linked stage. The wire order is target then impact.
	================
	*/
	function batch() {
		const stageCount = u8(), targets = u8();
		if ( stageCount * targets > MAX_RESULT_RECORDS ) throw Error( "Cast result capacity exceeded" );
		const results: CastTargetResult[] = [];
		for ( let targetIndex = 0; targetIndex < targets; targetIndex++ ) {
			const target = u32(), impacts: CastImpact[] = [];
			for ( let hit = 0; hit < stageCount; hit++ ) {
				const raw = u8(), type = raw & 127;
				let packed = 0, secondaryAmount = 0;
				let displacement: Omit<Pose, "angle"> | undefined;
				let auxiliary: readonly [number, number] | undefined;
				if ( type === 0 || type === 4 || type === 5 ) {
					packed = u32();
					secondaryAmount = u32();
					if ( type === 4 || type === 5 ) displacement = position();
				} else if ( type === 7 ) {
					packed = u32();
					auxiliary = [ u16(), u16() ];
				}
				impacts.push( {
					type,
					damage: packed >>> 8,
					flags: packed & 255,
					fatal: type !== 7 && !!(raw & 128),
					secondaryAmount,
					...(displacement ? { displacement } : {}),
					...(auxiliary ? { auxiliary } : {})
				} );
			}
			results.push( { target, impacts } );
		}
		return { results, stageCount };
	}
	/*
	================
	assertEnd
	================
	*/
	function assertEnd() {
		if ( cursor !== payload.length ) throw Error( "Invalid cast result length" );
	}
	return { u8, u32, position, batch, assertEnd };
}

/*
================
castPhase

8E0440 reads the target and steering mask before dispatching to 8E0190.
================
*/
export function castPhase( payload: Uint8Array, offset: number ) {
	const reader = resultReader( payload, offset );
	const target = reader.u32(), flags = reader.u8();
	if ( flags & ~(RESULT_TARGETS | RESULT_CORRECTION | RESULT_TRAVEL) ) return null;
	const batch = flags & RESULT_TARGETS ? reader.batch() : { results: [] as CastTargetResult[], stageCount: 0 };
	const travel = flags & RESULT_TRAVEL ? reader.position() : undefined;
	const correction = flags & RESULT_CORRECTION ? reader.position() : undefined;
	reader.assertEnd();
	return { target, flags, ...batch, travel, correction };
}

/*
================
skillPulse

7757CB reads mode 2, caster and skill before a bare result batch. Other B3C6
modes have different contracts and cannot be interpreted as linked attacks.
================
*/
export function skillPulse( payload: Uint8Array ) {
	const reader = resultReader( payload, 0 );
	if ( reader.u8() !== PULSE_RESULTS ) return null;
	const caster = reader.u32(), skill = reader.u32();
	const batch = reader.batch();
	reader.assertEnd();
	return {
		caster,
		skill,
		phase: {
			target: batch.results[0]?.target ?? 0,
			flags: RESULT_TARGETS,
			...batch,
			travel: undefined,
			correction: undefined
		}
	};
}
