/*
===========================================================================

runtime-errors.ts - per-source failure transitions for the runtime report

Report transitions, not frames. Recovery rearms the same fault; the map is
bounded by subsystem names rather than the number of distinct messages.
A held source (updateHeld) reports standing instead of transition: a
failure that recovers inside the hold files nothing.

===========================================================================
*/

/*
================
createRuntimeErrors

Tracks each source's standing error. update reports a source only when
its error appears or changes; clearing rearms it, so a fault that
recovers and fails again reports again.
================
*/
export function createRuntimeErrors( report: ( source: string, error: string ) => void ) {
	const active = new Map<string, string>();
	const held = new Map<string, { error: string; since: number; reported: boolean; }>();
	return {
		/*
		================
		update
		================
		*/
		update( source: string, error: string | null | undefined ) {
			if ( !error ) {
				active.delete( source );
				return;
			}
			if ( active.get( source ) === error ) return;
			active.set( source, error );
			report( source, error );
		},
		/*
		================
		updateHeld

		For a source whose standing error is a retry in progress, not a
		verdict: report only once the same error has stood the whole hold
		(the caller passes its clock and the hold length). A recovery
		inside the hold files nothing and rearms; a different error
		restarts the hold; a reported error does not report again until
		it clears and stands anew. One reporting rule per source: a
		held source does not also go through update.
		================
		*/
		updateHeld( source: string, error: string | null | undefined, now: number, holdMs: number ) {
			if ( !error ) {
				held.delete( source );
				return;
			}
			const standing = held.get( source );
			if ( !standing || standing.error !== error ) {
				held.set( source, { error, since: now, reported: false } );
				return;
			}
			if ( standing.reported || now - standing.since < holdMs ) return;
			standing.reported = true;
			report( source, error );
		}
	};
}
