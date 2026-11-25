/*
===========================================================================

animation-dispatch.ts - the animation key cursors (ADD670 / AE0450)

The presenter owns the animation installations; this module owns their
key cursors: how far each installation's clock has travelled, and which
clip ranges a frame must dispatch (sound, particles, material keys).
Pose sampling is a separate consumer and is not a substitute for key
dispatch. The cursor arithmetic mirrors the native dispatch bit for bit,
including its x87-shaped truncations; where the port deliberately
departs from a native quirk, the departure is marked at the rule.

===========================================================================
*/
import type { CharacterLayer } from "@/engine/contracts/character";
import type { AnimationActivation } from "./animation-activation";

/*
================
AnimationDispatch

One installation's dispatch for one frame: the clip ranges to fire and
the monotonic distance travelled through them, for audio expiry.
================
*/
export interface AnimationDispatch {
	readonly activation: AnimationActivation;
	readonly layer: CharacterLayer;
	readonly durationMs: number;
	readonly ranges: readonly (readonly [number, number])[];
	/** Monotonic distance travelled through dispatch ranges, for audio expiry. */
	readonly elapsedMs: number;
}
/*
================
createAnimationDispatch

The dispatch state for one presenter: the group cursor and one key
cursor per live installation.
================
*/
export function createAnimationDispatch() {
	let groupCursor = 0;
	const cursors = new Map<
		AnimationActivation,
		{ previous: number; source: number; elapsed: number; cursor: number; carry: number; }
	>();
	return {
		/*
================
step

Advance every installation by deltaMs and return the ranges this frame
dispatches: event lane first, then the timed lane, as natively.
================
		*/
		step(
			layers: readonly CharacterLayer[],
			deltaMs: number,
			duration: ( clip: string ) => number
		): readonly AnimationDispatch[] {
			if ( !Number.isInteger( deltaMs ) || deltaMs < 0 ) throw Error( "Invalid animation dispatch delta" );
			let weights = 0, length = 0, advance = 0;
			const durations = layers.map( layer => duration( layer.clip ) );
			for ( let i = 0; i < layers.length; i++ ) {
				const layer = layers[i]!, ms = durations[i]!;
				if (
					!layer.activation || !Number.isInteger( ms ) || ms < 0 || !Number.isFinite( layer.weight ) ||
					layer.weight < 0 || layer.weight > 1 || !Number.isFinite( layer.rate ?? 1 ) ||
					(layer.rate ?? 1) <= 0
				) throw Error( "Invalid animation dispatch installation" );
				if ( layer.lane !== "timed" || !layer.loop ) continue;
				const weight = Math.fround( layer.weight );
				weights = Math.fround( weights + weight );
				// Native truncates each weighted length before the integer accumulation.
				length = (length + Math.trunc( ms * weight )) >>> 0;
				advance = Math.fround( advance + deltaMs * weight );
			}
			const groupDuration = weights > 0 ? Math.trunc( length / weights ) : 0;
			groupCursor = groupDuration ? ((groupCursor + Math.trunc( advance / weights )) >>> 0) % groupDuration : 0;
			const keep = new Set<AnimationActivation>(), out: AnimationDispatch[] = [];
			// Native dispatches the event list before the timed list.
			for ( const lane of [ "event", "timed" ] as const ) {
				for ( let i = 0; i < layers.length; i++ ) {
					const layer = layers[i]!;
					if ( layer.lane !== lane ) continue;
					const activation = layer.activation!, ms = durations[i]!;
					if ( keep.has( activation ) ) throw Error( "Duplicate animation installation" );
					keep.add( activation );
					let row = cursors.get( activation );
					if ( !row ) {
						row = { previous: 0, source: 0, elapsed: 0, cursor: 0, carry: 0 };
						cursors.set( activation, row );
					}
					if ( !ms ) continue;
					const shared = lane === "timed" && layer.loop;
					let current: number;
					if ( shared ) {
						// AE05D0 owns this cursor: wrap the previous overflow, then accumulate
						// rate*delta while retaining the fraction. ADFFB0 constructs rate 1 and
						// cursor 0. The group clock is passed to AE0450 but only a state-0
						// installation rescales its phase from it (AE047C/AE0487), so a blend
						// changing groupDuration must never move an installation's position.
						// AE05F0's jbe preserves equality; AE05BC retains cursor % length.
						// Preserve the resulting full-cycle dispatch at the equality boundary.
						// Deliberate presentation deviation: AE05F0 wraps on `cursor > length`
						// while AE05BC retains `cursor % length`, so a cursor landing exactly on
						// the length is not wrapped yet reports a retained 0, and the next frame
						// dispatches a whole extra cycle - re-triggering every keyed effect in the
						// clip. The original rarely lands there because a 60 Hz truncated delta of
						// 16 ms divides few clip durations; a 4 ms delta at 240 Hz divides most, so
						// the rule reproduces at a frequency the original never exhibited. Wrapping
						// on `>=` keeps the cursor and the retained value consistent. Dispatch
						// ranges drive sound, particles and material keys only.
						if ( row.cursor >= ms ) row.cursor = row.cursor % ms;
						const value = row.carry + deltaMs * (layer.rate ?? 1), step = Math.trunc( value );
						row.carry = value - step;
						row.cursor = (row.cursor + step) >>> 0;
						current = row.cursor;
					} else current = Math.max( 0, Math.trunc( layer.time * 1000 ) );
					const ranges: [number, number][] = [];
					if ( layer.loop ) {
						// AE0503 branches on the installation cursor versus the retained cursor.
						// Accumulation only moves it backwards on a genuine modulo wrap.
						let remaining = shared ?
								(current >= row.previous ? current - row.previous : ms - row.previous + current) :
								Math.max( 0, current - row.source ),
							from = row.previous;
						while ( remaining > 0 ) {
							const end = Math.min( ms, from + remaining );
							if ( end > from ) ranges.push( [ from, end ] );
							remaining -= end - from;
							from = 0;
						}
						row.previous = current % ms;
						row.source = current;
					} else {
						const end = Math.min( ms, current );
						if ( end > row.previous ) ranges.push( [ row.previous, end ] );
						row.previous = end;
					}
					for ( const [from, to] of ranges ) row.elapsed += to - from;
					out.push( {
						activation,
						layer: shared ? { ...layer, time: (current % ms) / 1000 } : layer,
						durationMs: ms,
						ranges,
						elapsedMs: row.elapsed
					} );
				}
			}
			for ( const key of cursors.keys() ) if ( !keep.has( key ) ) cursors.delete( key );
			return out;
		},
		/*
================
reset

Drop every cursor; the next step starts each installation at zero.
================
		*/
		reset() {
			groupCursor = 0;
			cursors.clear();
		}
	};
}
