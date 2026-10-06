/*
===========================================================================

movement-settlement.mjs - observe command admission before settled movement

Main-thread publications lag a posted worker command. An idle snapshot from
before that command cannot prove the new command finished. Callers returning
to an anchor supply the revision observed immediately before posting it.

===========================================================================
*/
const POLL_MS = 50;

/*
================
waitForMovementSettlement

A changed movement revision admits an instantaneous completed move too; the
probe does not require catching the transient moving or pending state.
================
*/
/**
 * @template {{ movementRevision?: number, moving?: boolean, pendingMoves: number }} T
 * @param {{ read: () => Promise<T>, pause: (ms: number) => Promise<unknown>, timeoutMs: number, afterRevision?: number }} options
 * @returns {Promise<T>}
 */
export async function waitForMovementSettlement( { read, pause, timeoutMs, afterRevision = undefined } ) {
	const deadline = Date.now() + timeoutMs;
	for ( ;; ) {
		const state = await read();
		const admitted = afterRevision === undefined || state.movementRevision !== afterRevision;
		if ( admitted && !state.moving && state.pendingMoves === 0 ) return state;
		if ( Date.now() >= deadline ) throw Error( "movement did not admit and settle" );
		await pause( POLL_MS );
	}
}
