/*
===========================================================================

blended-modifiers.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
async function load( path ) {
	return import( sourceFileUrl( path ).href );
}
const {
	createModelAnimation,
	createAnimationEmission,
	modelAnimationParticles,
	createAnimationDispatch,
	animationActivation
} = {
	...(await load( "src/engine/foundation/animation/model-animation.ts" )),
	...(await load( "src/engine/foundation/animation/animation-emission.ts" )),
	...(await load( "src/engine/foundation/animation/animation-dispatch.ts" )),
	...(await load( "src/engine/foundation/animation/animation-activation.ts" ))
};
const selector = { set: "default", state: 0 };
const binding = [ { ...selector, clip: "stand" }, { set: "sword", state: 2, clip: "run" } ];
const layer = ( activation, clip = "stand", time = 0, lane = "timed", weight = 1 ) => ({
	clip,
	activation,
	time,
	lane,
	weight,
	loop: true
});
const raw = {
	kind: 1,
	stateId: 0,
	animationSetName: "default",
	baseWords: [ 1056964608, 1, 48, 4294967295, 0, 0 ],
	entries: [ {
		field00: 1,
		effectPath: "system/test.efp",
		boneName: "",
		vector3c: [ 0, 0, 0 ],
		field4c: 100,
		flags50: [ 0, 0, 0 ],
		flag53: 0
	} ]
};
test("zero-weight installations select queues, outgoing keys survive, and missing named sets fall back as a whole", () => {
	const clock = createAnimationDispatch(),
		selection = createModelAnimation(),
		a = animationActivation( 0 ),
		b = animationActivation( .1 ),
		sets = [ selector, { set: "default", state: 2 } ];
	const step = ( layers, dt ) => selection.step( clock.step( layers, dt, () => 1000 ), binding, sets );
	assert.deepEqual( step( [ layer( a ) ], 50 ).selected, selector );
	const blend = step( [ layer( a ), layer( b, "run", 0, "timed", 0 ) ], 100 );
	assert.deepEqual( blend.selected, { set: "default", state: 2 } );
	assert.equal( blend.dispatch.length, 2 );
	assert.deepEqual( blend.dispatch[0].ranges, [ [ 50, 150 ] ] );
	const end = step( [ layer( a ) ], 0 );
	assert.equal( end.selected, null, "removing primary returns to ambient, not the outgoing set" );
	const repeat = step( [ layer( animationActivation( .2 ) ) ], 0 );
	assert.deepEqual( repeat.selected, selector );
	const same = step( [ layer( animationActivation( .3 ) ) ], 0 );
	assert.deepEqual( same.restarted, [ selector ] );
	selection.reset();
	assert.equal( step( [], 0 ).selected, null );
});
test("particle wrappers reject endpoint/LOD/daylight keys, retain inactive instances and detach repeated tails", () => {
	let serial = -1;
	const owner = createAnimationEmission( () => serial-- ), sets = modelAnimationParticles( [ raw ] );
	const body = {
		gid: 1,
		modifierId: 9,
		model: "body",
		pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
		clip: "stand",
		time: 0,
		loop: true,
		scale: 1
	};
	const frame = ( from, to, selected = selector, restarted = [] ) => ({
		selected,
		revision: 0,
		restarted,
		dispatch: [ { selector, ranges: [ [ from, to ] ] } ]
	});
	const matrix = () => ({
		regionId: 257,
		matrix: Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 12, 34, 56, 1 )
	});
	let age = 0;
	const step = ( time, modelAnimation, lod = 0, night = true ) =>
		owner.step(
			[ { actor: { ...body, modelAnimation, animationLod: { fraction: lod, crowded: false } }, sets } ],
			time,
			() => true,
			20,
			night,
			matrix,
			() => age,
			() => 1
		);
	assert.equal( step( 0, frame( 0, 100 ) ).length, 0 );
	assert.equal( step( .1, frame( 100, 101 ), 1 ).length, 0 );
	const first = step( .2, frame( 100, 101 ) );
	assert.equal( first.length, 1 );
	assert.equal( first[0].loop, false );
	const hidden = step( .3, frame( 101, 102, null ) );
	assert.equal( hidden[0].gid, first[0].gid );
	assert.equal( hidden[0].deferredParticle.lodHidden, true );
	const repeat = step( .4, frame( 0, 50, selector, [ selector ] ) );
	assert.equal( repeat.length, 1 );
	assert.equal( repeat[0].gid, first[0].gid );
	assert.equal( repeat[0].attachment, undefined );
	assert.deepEqual( [ repeat[0].pose.x, repeat[0].pose.y, repeat[0].pose.z ], [ 12, 34, 56 ] );
	const newInstance = step( .5, frame( 100, 101 ) );
	assert.equal( newInstance.length, 2 );
	assert.notEqual( newInstance[0].gid, first[0].gid );
	age = 1.1;
	assert.equal( step( 1.5, frame( 101, 102 ) ).length, 1 );
	owner.reset();
	assert.equal( step( 2, frame( 101, 102 ) ).length, 0 );
	const nightSets = modelAnimationParticles( [ {
		...raw,
		entries: [ { ...raw.entries[0], flags50: [ 0, 1, 0 ] } ]
	} ] );
	const actors = owner.step(
		[ { actor: { ...body, modelAnimation: frame( 100, 101 ) }, sets: nightSets } ],
		3,
		() => true,
		20,
		false,
		matrix,
		() => 0,
		() => 1
	);
	assert.equal( actors.length, 0 );
});
