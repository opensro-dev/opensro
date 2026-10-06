/*
===========================================================================

movement-continuity.mjs - whole-window motion evidence with bounded storage

Wraps the benchmark's existing frame observer. The normal recorder retains a
tail; these online totals preserve early discontinuities without an unbounded
array. This function is self-contained for Playwright serialization.

===========================================================================
*/

/*
================
installMovementContinuity

Horizontal excess compares distance with the observed movement speed and real
frame duration. Three-dimensional excess is reported separately because ground
slope can exceed an XZ speed budget. Neither value alone proves a correction.
================
*/
/** @param {{ target?: any, threshold?: number }} options */
export function installMovementContinuity( { target = globalThis, threshold = 0.5 } = {} ) {
	const root = /** @type {any} */ (target);
	const probe = root.__worldProbeFrameProfiler;
	if ( !probe?.movement ) throw Error( "continuity requires the existing movement observer" );
	if ( root.__recoveryContinuity ) throw Error( "continuity observer already installed" );
	const original = probe.movement;
	const REGION_SIZE = 1920, MAX_EVENTS = 32, MAX_CHANNEL_EVENTS = 8, LONG_FRAME_MS = 250;
	const names = [ "logical", "displayed", "body" ];
	let summary, previous, active = false;

	/*
	================
	reset
	================
	*/
	function reset() {
		previous = null;
		summary = {
			threshold,
			frames: 0,
			invalidSpeed: 0,
			invalidTime: 0,
			longFrames: 0,
			maxDtMs: 0,
			channels: Object.fromEntries( names.map( name => [ name, {
				pairs: 0,
				missing: 0,
				maxStepXZ: 0,
				maxStepXYZ: 0,
				maxExcessXZ: 0,
				maxExcessXYZ: 0,
				excessFramesXZ: 0,
				excessFramesXYZ: 0,
				stationaryPairs: 0,
				maxStationaryStepXZ: 0,
				stationaryExcessFramesXZ: 0,
				maxLongFrameExcessXZ: 0,
				longFrameExcessFramesXZ: 0,
				maxDeltaDifferenceXZ: 0,
				deltaDifferenceFramesXZ: 0,
				excessEvents: []
			} ] ) ),
			events: []
		};
		active = true;
	}

	/*
	================
	position
	================
	*/
	function position( pose ) {
		if ( !pose || ![ pose.regionId, pose.x, pose.y, pose.z ].every( Number.isFinite ) ) return null;
		return {
			x: (pose.regionId & 255) * REGION_SIZE + pose.x,
			y: pose.y,
			z: (pose.regionId >>> 8) * REGION_SIZE + pose.z
		};
	}

	/*
	================
	observe
	================
	*/
	probe.movement = function observe( sample ) {
		original.call( probe, sample );
		// measure() closes this window before serializing its large frame tail.
		// That transfer is harness overhead, not another measured game frame.
		if ( !active || root.__benchLoop === false ) return;
		summary.frames++;
		const game = root.__benchRuntime.gameplay();
		const entity = root.__benchRuntime.entities().find( row => row.gid === game.localGid );
		const speed = entity?.movementMode === 2 ? entity.walkSpeed : entity?.runSpeed;
		if ( !Number.isFinite( speed ) || speed < 0 ) {
			summary.invalidSpeed++;
			previous = null;
			return;
		}
		const current = {
			atMs: sample.atMs,
			workerAtMs: sample.workerAtMs,
			speed,
			revision: sample.revision,
			stationary: game.moving === false && game.pendingMoves === 0,
			logical: position( sample.logical ),
			displayed: position( sample.displayed ),
			body: position( sample.body?.pose )
		};
		if ( previous ) {
			const dt = current.atMs - previous.atMs;
			if ( !Number.isFinite( dt ) || dt <= 0 ) {
				summary.invalidTime++;
			} else {
				summary.maxDtMs = Math.max( summary.maxDtMs, dt );
				if ( dt > LONG_FRAME_MS ) summary.longFrames++;
				const stationary = previous.stationary && current.stationary;
				const expected = stationary ? 0 : Math.max( speed, previous.speed ) * dt / 1000;
				for ( const name of names ) {
					const a = previous[name], b = current[name], channel = summary.channels[name];
					if ( !a || !b ) {
						channel.missing++;
						continue;
					}
					channel.pairs++;
					const xz = Math.hypot( b.x - a.x, b.z - a.z ), xyz = Math.hypot( xz, b.y - a.y );
					const excessXZ = Math.max( 0, xz - expected ), excessXYZ = Math.max( 0, xyz - expected );
					channel.maxStepXZ = Math.max( channel.maxStepXZ, xz );
					channel.maxStepXYZ = Math.max( channel.maxStepXYZ, xyz );
					channel.maxExcessXZ = Math.max( channel.maxExcessXZ, excessXZ );
					channel.maxExcessXYZ = Math.max( channel.maxExcessXYZ, excessXYZ );
					if ( excessXZ > threshold ) channel.excessFramesXZ++;
					if ( excessXYZ > threshold ) channel.excessFramesXYZ++;
					if ( stationary ) {
						channel.stationaryPairs++;
						channel.maxStationaryStepXZ = Math.max( channel.maxStationaryStepXZ, xz );
						if ( excessXZ > threshold ) channel.stationaryExcessFramesXZ++;
					}
					if ( dt > LONG_FRAME_MS ) {
						channel.maxLongFrameExcessXZ = Math.max( channel.maxLongFrameExcessXZ, excessXZ );
						if ( excessXZ > threshold ) channel.longFrameExcessFramesXZ++;
					}
					let deltaDifferenceXZ = null;
					if ( previous.logical && current.logical ) {
						deltaDifferenceXZ = Math.hypot(
							(b.x - a.x) - (current.logical.x - previous.logical.x),
							(b.z - a.z) - (current.logical.z - previous.logical.z)
						);
						channel.maxDeltaDifferenceXZ = Math.max( channel.maxDeltaDifferenceXZ, deltaDifferenceXZ );
						if ( deltaDifferenceXZ > threshold ) channel.deltaDifferenceFramesXZ++;
					}
					if ( excessXYZ > threshold || deltaDifferenceXZ > threshold ) {
						const event = {
							channel: name,
							atMs: current.atMs,
							dtMs: dt,
							workerAtMs: current.workerAtMs,
							workerBeforeMs: previous.workerAtMs,
							expected,
							xz,
							xyz,
							excessXZ,
							excessXYZ,
							stationary,
							deltaDifferenceXZ,
							from: a,
							to: b,
							revisionBefore: previous.revision,
							revision: current.revision,
							transition: sample.transition ? { ...sample.transition } : null
						};
						summary.events.push( event );
						if ( excessXYZ > threshold ) {
							channel.excessEvents.push( event );
							channel.excessEvents.sort( ( a, b ) => b.excessXYZ - a.excessXYZ );
							if ( channel.excessEvents.length > MAX_CHANNEL_EVENTS ) {
								channel.excessEvents.length = MAX_CHANNEL_EVENTS;
							}
						}
						summary.events.sort( ( a, b ) =>
							Math.max( b.excessXYZ, b.deltaDifferenceXZ ?? 0 ) -
							Math.max( a.excessXYZ, a.deltaDifferenceXZ ?? 0 )
						);
						if ( summary.events.length > MAX_EVENTS ) summary.events.length = MAX_EVENTS;
					}
				}
			}
		}
		previous = current;
	};
	root.__recoveryContinuity = {
		reset,
		/*
		================
		snapshot
		================
		*/
		snapshot() {
			return structuredClone( summary );
		},
		/*
		================
		stop
		================
		*/
		stop() {
			active = false;
			return structuredClone( summary );
		}
	};
}
