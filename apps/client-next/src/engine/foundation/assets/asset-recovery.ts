/*
===========================================================================

asset-recovery.ts - typed transient failures and bounded stream recovery

Transport marks recoverable failures before crossing the worker boundary.
Each stream owns one recovery clock; frame time drives retries without
another timer or polling task. Permanent asset validation never opts in.

===========================================================================
*/
const MAX_AUTOMATIC_RETRIES = 8;

/*
================
assetFailure
================
*/
export function assetFailure( message: string, transient = false ): Error & { transient: boolean; } {
	return Object.assign( new Error( message ), { transient } );
}

/*
================
transientAssetFailure
================
*/
export function transientAssetFailure( error: unknown ): boolean {
	return error instanceof Error && "transient" in error && error.transient === true;
}

/*
================
createAssetRecovery

Eight automatic retries cover a prolonged interruption with capped backoff.
After exhaustion the failure stays explicit; manual retry or a new network
availability event can start a fresh bounded sequence.
================
*/
export function createAssetRecovery() {
	const retryDelaysMs = [ 2000, 5000, 10000, 30000 ] as const;
	let transient = false, attempts = 0, retryAt = Infinity;
	return {
		/*
		================
		failed
		================
		*/
		failed( error: unknown, nowMs: number ) {
			transient = transientAssetFailure( error );
			retryAt = transient && attempts < MAX_AUTOMATIC_RETRIES ?
				nowMs + retryDelaysMs[Math.min( attempts++, retryDelaysMs.length - 1 )]! :
				Infinity;
		},
		/*
		================
		due
		================
		*/
		due( nowMs: number ) {
			return transient && nowMs >= retryAt;
		},
		/*
		================
		reconnecting
		================
		*/
		reconnecting() {
			return transient && Number.isFinite( retryAt );
		},
		/*
		================
		transient
		================
		*/
		transient() {
			return transient;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			transient = false;
			attempts = 0;
			retryAt = Infinity;
		}
	};
}
