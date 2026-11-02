/*
===========================================================================

runtime-errors.ts - per-source failure transitions for the runtime report

Report transitions, not frames. Recovery rearms the same fault; the map is
bounded by subsystem names rather than the number of distinct messages.

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
		}
	};
}
