/*
===========================================================================

particle-command-frames.ts - native EFP source-line schedule frame expansion

Expands a CEESourceListLineState schedule (start, spacing, end and byte1
mode flags, native CEESourceListLineState_ResolveFrameWindow / CEESourceListLineState_ScheduleCommands) into the discrete integer frames
within an element's lifetime at which the particle command fires.

===========================================================================
*/

export interface ParticleOperation {
	readonly name: string;
	readonly flags?: number;
	readonly byte1?: number;
	readonly start?: number;
	readonly spacing?: number;
	readonly end?: number;
	readonly parameter?: {
		readonly kind?: string;
		readonly value?: unknown;
		readonly left?: unknown;
		readonly right?: unknown;
	} | null;
}

/*
================
particleCommandFrames

B0C290 / B143C0: `start` is the first frame (sampleStart0c), `spacing` is the
sample period (sampleStep10), and `end` is the schedule end frame (sampleEnd14).
Percent start/end truncate BEFORE multiplying by the element lifetime.
================
*/
export function particleCommandFrames( op: ParticleOperation, total: number ): number[] {
	const flags = op.byte1 ?? 0, start = op.start ?? 0, spacing = op.spacing ?? 1, end = op.end ?? 0;
	if (
		!Number.isInteger( total ) || total <= 0 || total > 1200 || !Number.isInteger( flags ) || flags < 0 ||
		flags > 255 || ![ start, spacing, end ].every( Number.isFinite )
	) {
		throw Error( "Invalid particle command schedule" );
	}
	const first = flags & 1 ? Math.trunc( start / 100 ) * total : Math.trunc( start );
	const period = flags & 2 ? Math.fround( total * spacing / 100 ) : spacing;
	let last: number;
	switch ( (flags >>> 2) % 5 ) {
		case 0:
			last = Math.trunc( end );
			break;
		case 1:
			last = Math.trunc( end / 100 ) * total;
			break;
		case 2:
			last = first + Math.trunc( end );
			break;
		case 3:
			last = first - Math.trunc( end / -100 ) * total;
			break;
		default:
			if ( end < 1 ) return [];
			last = first + Math.trunc( end * period );
	}
	last = Math.min( last, total - 1 );
	if ( first > last || period < 1e-6 ) return [];
	const count = Math.trunc( (last - first) / period ) + 1;
	if ( first < 0 || !Number.isSafeInteger( count ) || count > 4096 ) {
		throw Error( "Particle command schedule exceeds budget" );
	}
	return Array.from( { length: count }, ( _, i ) => Math.trunc( first + i * period ) );
}
