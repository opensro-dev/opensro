/**
 * Waits for every promise to settle, then throws the first rejection. A
 * writer fan-out must never return (and let its caller release the build
 * lock) while sibling writes are still running, which Promise.all does on
 * the first failure.
 */
export async function settleAll( promises ) {
	const values = [];
	for ( const outcome of await Promise.allSettled( promises ) ) {
		if ( outcome.status === "rejected" ) throw outcome.reason;
		values.push( outcome.value );
	}
	return values;
}

/**
 * Run `worker` over `items` with at most `limit` jobs in flight.
 * Results retain input order even when workers finish out of order. After a
 * failure no new item starts, and the call rejects only once the items
 * already running have finished.
 */
export async function mapWithConcurrency( items, limit, worker ) {
	const results = new Array( items.length );
	let nextIndex = 0, failed = false;
	const laneCount = Math.max( 1, Math.min( normalizeConcurrency( limit ), items.length ) );
	const lanes = Array.from( { length: laneCount }, async () => {
		while ( !failed ) {
			const index = nextIndex;
			nextIndex += 1;
			if ( index >= items.length ) return;
			try {
				results[index] = await worker( items[index], index );
			} catch ( error ) {
				failed = true;
				throw error;
			}
		}
	} );
	await settleAll( lanes );
	return results;
}

/**
 * A shared bound for work started from several places at once: run(task)
 * starts task when fewer than `limit` tasks are running, else queues it.
 * Callers spread over many groups share one budget instead of one each.
 */
export function createLimiter( limit ) {
	const capacity = normalizeConcurrency( limit );
	const waiting = [];
	let running = 0;
	const next = () => {
		if ( running >= capacity || waiting.length === 0 ) return;
		running += 1;
		const { task, resolve, reject } = waiting.shift();
		Promise.resolve()
			.then( task )
			.then( resolve, reject )
			.finally( () => {
				running -= 1;
				next();
			} );
	};
	return ( task ) =>
		new Promise( ( resolve, reject ) => {
			waiting.push( { task, resolve, reject } );
			next();
		} );
}

function normalizeConcurrency( value ) {
	return Number.isFinite( value ) ? Math.max( 1, Math.floor( value ) ) : 1;
}
