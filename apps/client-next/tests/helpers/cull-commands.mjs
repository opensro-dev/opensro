/*
===========================================================================

cull-commands.mjs - the GPU cull commands of a fake geometry owner

A world or terrain layer test with a hand-made GeometryCommands spreads
these in: the renderer hands every terrain layer draw to the GPU cull
(index-cull.ts) and gives it the view each frame, which a fake that only
records uploads has no use for. A test of the cull itself drives the real
geometry owner instead.

===========================================================================
*/
// Dynamic: the source loader (native-source-loader.mjs) resolves @/ only once
// a test has imported it.
const { prepareViewFrustum } = await import( "../../src/engine/foundation/rendering/world-math.ts" );

export const CULL_COMMANDS = Object.freeze( {
	cullDraw() {},
	cullSource() {},
	cullRanges() {},
	cullView() {},
	cullObject() {},
	drawRevision: () => 0
} );

// The world object cull (object-cull.ts) as a model, kept per fixture: the
// geometry fake the world calls is the key. cullView records the planes the
// world actually sends, cullObject each draw's sphere and owner, and
// visibleDraws culls with those recorded planes after checking they are the
// prepared frame's own view and mirror: a world that stopped sending the
// view (or sent another) fails, not just one that culls wrongly.
const views = new WeakMap(), spheres = new WeakMap();
export const OBJECT_CULL_COMMANDS = Object.freeze( {
	cullView( main, mirror ) {
		views.set(
			this,
			mirror ? [ Float64Array.from( main ), Float64Array.from( mirror ) ] : [ Float64Array.from( main ) ]
		);
	},
	cullObject( draw, sphere ) {
		spheres.set( draw, { sphere: [ ...sphere ], owner: this } );
	},
	drawRevision: () => 0
} );
// object-cull.ts CULL_EPSILON.
const CULL_EPSILON = 1;

/*
================
submittedDraws

A prepared world's draws with its static runs back at their places (the
order the frame records them in).
================
*/
export function submittedDraws( prepared ) {
	const draws = prepared.draws, persistent = prepared.persistent, merged = [];
	let next = 0;
	for ( let k = 0; k < (persistent?.blocks.length ?? 0); k++ ) {
		for ( ; next < persistent.at[k]; next++ ) merged.push( draws[next] );
		merged.push( ...persistent.blocks[k] );
	}
	for ( ; next < draws.length; next++ ) merged.push( draws[next] );
	return merged;
}

/*
================
gpuSphereVisible

The cull shader's test: inside every normalized plane, grown by
CULL_EPSILON (planes as prepareViewFrustum lays them out).
================
*/
export function gpuSphereVisible( planes, sphere ) {
	for ( let at = 0; at < 30; at += 5 ) {
		const length = planes[at + 4] || 1;
		const distance = (planes[at] * sphere[0] + planes[at + 1] * sphere[1] + planes[at + 2] * sphere[2] +
			planes[at + 3]) / length;
		if ( distance < -(sphere[3] + CULL_EPSILON) ) return false;
	}
	return true;
}

/*
================
visibleDraws

The submitted draws the GPU cull keeps: a culled draw survives when its
sphere is inside the view or the mirror its owner last received, which
must be this frame's.
================
*/
export function visibleDraws( prepared ) {
	const expected = [ prepareViewFrustum( prepared.matrix ) ];
	if ( prepared.reflectionMatrix ) expected.push( prepareViewFrustum( prepared.reflectionMatrix ) );
	return submittedDraws( prepared ).filter( draw => {
		const culled = spheres.get( draw );
		if ( !culled ) return true;
		const sent = views.get( culled.owner );
		if ( !sent ) throw Error( "a GPU-culled draw was submitted but the world never sent the cull view" );
		if (
			sent.length !== expected.length ||
			sent.some( ( planes, index ) => planes.some( ( value, k ) => value !== expected[index][k] ) )
		) throw Error( "the cull view the world sent is not the prepared frame's" );
		return sent.some( planes => gpuSphereVisible( planes, culled.sphere ) );
	} );
}
