/*
===========================================================================

character-capacity.test.mjs - tests for characters.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createCharacters } = await import(
	sourceFileUrl( "src/engine/runtime/renderer/characters/characters.ts" ).href
);
const { CHARACTER_ASSEMBLIES, CHARACTER_MODELS } = await import(
	sourceFileUrl( "src/engine/foundation/animation/character-budget.ts" ).href
);
const I = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
const model = {
	nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
	images: [],
	clips: [ {
		name: "move",
		duration: 1,
		channels: [ {
			node: 0,
			path: "translation",
			interpolation: "LINEAR",
			times: Float32Array.of( 0, 1 ),
			values: Float32Array.of( 0, 0, 0, 1, 2, 3 )
		} ]
	} ],
	primitives: [ {
		name: "body",
		node: 0,
		image: -1,
		joints: [ 0 ],
		inverseBind: I(),
		geometry: {
			positions: Float32Array.of( -1, -1, 0, 1, -1, 0, 0, 1, 0 ),
			indices: Uint32Array.of( 0, 1, 2 ),
			transform: I()
		}
	} ]
};
/*
================
fixture
================
*/
function fixture() {
	let uploads = 0, releases = 0;
	const gpu = {
		upload( g ) {
			uploads++;
			return {
				count: g.instances.length / 16,
				instanceCount: g.instances.length / 16,
				indexCount: g.indices.length,
				instances: g.instances.slice(),
				bones: g.bones.slice()
			};
		},
		updateInstances( d, m ) {
			d.count = m.length / 16;
			d.instanceCount = d.count;
			d.instances = m.slice();
			return d;
		},
		updateBones( d, b ) {
			d.bones = b.slice();
		},
		release() {
			releases++;
		},
		updateTransform() {}
	};
	const owner = createCharacters();
	owner.model( "m", model, [] );
	return { owner, gpu, stats: () => ({ uploads, releases }) };
}
const actors = ( ids, t ) =>
	ids.map( gid => ({
		gid,
		model: "m",
		clip: "move",
		time: (t + gid / 100) % 1,
		loop: true,
		scale: 1,
		pose: { regionId: 257, x: gid, y: 0, z: 10, yaw: 0 }
	}) );
const visible = d => ({
	count: d.count,
	instances: d.instances.slice( 0, d.count * 16 ),
	bones: d.bones.slice( 0, d.count * 16 )
});

test("the CPU evaluation total keeps the work of evaluators that retired", () => {
	const f = fixture();
	try {
		f.owner.actors( actors( [ 1, 2, 3 ], .25 ) );
		f.owner.prepare( f.gpu, {}, 257 );
		const crowd = f.owner.stats();
		assert.ok( crowd.liveOwnedCpuEvaluations > 0, "the fixture evaluates on the CPU" );
		assert.equal( crowd.cpuEvaluations, crowd.liveOwnedCpuEvaluations );
		// Two peers leave: their evaluators retire, the live sum falls, the total does not.
		f.owner.actors( actors( [ 1 ], .5 ) );
		f.owner.prepare( f.gpu, {}, 257 );
		const alone = f.owner.stats();
		assert.ok( alone.liveOwnedCpuEvaluations < crowd.liveOwnedCpuEvaluations );
		assert.ok( alone.cpuEvaluations > crowd.cpuEvaluations, "the remaining peer's new evaluation adds on top" );
		// The peer changes model: its old evaluator is replaced, its work kept.
		f.owner.model( "m2", model, [] );
		f.owner.actors( actors( [ 1 ], .75 ).map( actor => ({ ...actor, model: "m2" }) ) );
		f.owner.prepare( f.gpu, {}, 257 );
		const replaced = f.owner.stats();
		assert.ok( replaced.cpuEvaluations > alone.cpuEvaluations );
		assert.ok(
			replaced.liveOwnedCpuEvaluations < replaced.cpuEvaluations,
			"the replaced evaluator's work is retired"
		);
		// Disposal clears every evaluator; the total still holds all of it.
		f.owner.dispose( f.gpu, null );
		const disposed = f.owner.stats();
		assert.equal( disposed.liveOwnedCpuEvaluations, 0 );
		assert.equal( disposed.cpuEvaluations, replaced.cpuEvaluations );
	} finally {
		f.owner.dispose( f.gpu, null );
	}
});

test("the culling census measures how far admitted spheres sit outside the view", () => {
	const f = fixture();
	// Column-major orthographic view: clip x = sx * (x - cx), y = y / 100,
	// z = z / 100, so the frustum is cx - 1/sx <= x <= cx + 1/sx.
	const ortho = ( sx, cx ) => {
		const m = new Float32Array( 16 );
		m[0] = sx;
		m[5] = .01;
		m[10] = .01;
		m[12] = -sx * cx;
		m[15] = 1;
		return m;
	};
	try {
		assert.equal( f.owner.stats( true ).cullSlack, undefined, "no census before a main frame" );
		f.owner.actors( actors( [ 1 ], .25 ) );
		f.owner.prepare( f.gpu, {}, 257, ortho( .01, 0 ) );
		const inside = f.owner.stats( true ).cullSlack;
		assert.equal( inside.admitted, 1 );
		assert.equal( inside.bodies, 1 );
		assert.equal( inside.centreInside, 1 );
		assert.deepEqual( inside.outsideShares, [ 0, 0, 0, 0 ] );
		const radius = inside.meanRadius;
		assert.ok( radius > 0 );
		// The right plane at x = 1 - 0.6 radius: the centre (x = 1) is 0.6 of a
		// radius outside it, so the sphere is still admitted.
		const sx = .01, cx = 1 - radius * .6 - 1 / sx;
		f.owner.prepare( f.gpu, {}, 257, ortho( sx, cx ) );
		const straddling = f.owner.stats( true ).cullSlack;
		assert.equal( straddling.admitted, 1 );
		assert.equal( straddling.centreInside, 0 );
		assert.deepEqual( straddling.outsideShares, [ 0, 0, 1, 0 ] );
		assert.equal( f.owner.stats().cullSlack, undefined, "ordinary stats skip the census" );
	} finally {
		f.owner.dispose( f.gpu, null );
	}
});

test("the ordinary census never evaluates poses; only the posed census reads palettes", () => {
	const f = fixture();
	// The shared fixture is unskinned, which the posed census counts as unknown
	// (visible). A skinned copy, every vertex fully on joint 0, exercises palettes.
	const skinned = {
		...model,
		primitives: model.primitives.map( primitive => ({
			...primitive,
			geometry: {
				...primitive.geometry,
				joints: Uint16Array.of( 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0 ),
				weights: Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 )
			}
		}) )
	};
	f.owner.model( "s", skinned, [] );
	const view = ( sx, cx ) => new Float32Array( [ sx, 0, 0, 0, 0, .01, 0, 0, 0, 0, .01, 0, -sx * cx, 0, 0, 1 ] );
	// GPU bone preparation takes every palette, so prepare leaves poses deferred.
	f.gpu.prepareGpuBones = () => true;
	try {
		f.owner.actors( [ ...actors( [ 1 ], .25 ), { ...actors( [ 2 ], .25 )[0], model: "s" } ] );
		f.owner.prepare( f.gpu, {}, 257, view( .01, 0 ) );
		const deferred = f.owner.stats().cpuEvaluations;
		const ordinary = f.owner.stats( true ).cullSlack;
		assert.equal( ordinary.posedHidden, undefined, "posed fields only on the posed census" );
		assert.equal( f.owner.stats().cpuEvaluations, deferred, "stats(true) stays non-evaluating" );
		const inside = f.owner.stats( true, true ).cullSlack;
		assert.ok( f.owner.stats().cpuEvaluations > deferred, "only the posed census materializes poses" );
		assert.equal( inside.posedUnknown, 1, "the unskinned body is unknown, counted visible" );
		assert.equal( inside.posedHidden, 0, "the skinned body is in view" );
		// Right plane at x = 1: the skinned body's feet sphere at x = 2 still
		// reaches it, but its posed triangle (x 1.27..3.27) lies wholly beyond.
		f.owner.prepare( f.gpu, {}, 257, view( 1, 0 ) );
		const beyond = f.owner.stats( true, true ).cullSlack;
		assert.equal( beyond.posedHidden, 1, "admitted by the sphere, hidden once posed" );
		assert.equal( beyond.posedUnknown, 1 );
	} finally {
		f.owner.dispose( f.gpu, null );
	}
});

test("the culling census averages the active radius over lone bodies, not mounted riders", () => {
	const f = fixture();
	try {
		f.owner.actors( actors( [ 1 ], .25 ) );
		f.owner.prepare( f.gpu, {}, 257, new Float32Array( [ .01, 0, 0, 0, 0, .01, 0, 0, 0, 0, .01, 0, 0, 0, 0, 1 ] ) );
		const alone = f.owner.stats( true ).cullSlack;
		// A rider mounted on gid 1: a body (no attachment) whose chain is not lone.
		const rows = actors( [ 1, 2 ], .25 );
		rows[1] = { ...rows[1], mountedOn: 1 };
		f.owner.actors( rows );
		f.owner.prepare( f.gpu, {}, 257, new Float32Array( [ .01, 0, 0, 0, 0, .01, 0, 0, 0, 0, .01, 0, 0, 0, 0, 1 ] ) );
		const mounted = f.owner.stats( true ).cullSlack;
		assert.equal( mounted.bodies, 2 );
		assert.equal( mounted.meanActiveRadius, alone.meanActiveRadius, "the rider does not dilute the lone average" );
	} finally {
		f.owner.dispose( f.gpu, null );
	}
});

test("pose eligibility diagnostics are copied, deduplicate sharing and never evaluate poses", () => {
	const f = fixture();
	try {
		const rows = actors( [ 1, 2, 3 ], .25 );
		rows[1].time = rows[0].time;
		rows[2].bodyVolume = { index: 3, female: false };
		f.owner.actors( rows );
		f.owner.prepare( f.gpu, {}, 257 );
		const before = f.owner.stats();
		assert.equal( before.poseEligibility, undefined );
		const snapshot = f.owner.stats( true );
		assert.deepEqual( snapshot.poseEligibility, {
			actors: 3,
			unique: 2,
			gpuSamples: 1,
			linearSamples: 1,
			sharedPaletteSamples: 0,
			clothSamples: 0,
			gpuPaletteSamples: 0
		} );
		snapshot.poseEligibility.gpuSamples = 999;
		assert.equal( f.owner.stats( true ).poseEligibility.gpuSamples, 1 );
		assert.equal( f.owner.stats().liveOwnedCpuEvaluations, before.liveOwnedCpuEvaluations );
		assert.equal( f.owner.stats().poseEvaluations, before.poseEvaluations );
	} finally {
		f.owner.dispose( f.gpu, null );
	}
});

test("GPU palette census distinguishes model eligibility from device availability", () => {
	const f = fixture();
	try {
		f.owner.model( "skinned", {
			...model,
			primitives: model.primitives.map( primitive => ({
				...primitive,
				geometry: {
					...primitive.geometry,
					joints: new Uint32Array( 12 ),
					weights: Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 )
				}
			}) )
		}, [] );
		const rows = actors( [ 1, 2 ], .25 );
		rows[1].model = "skinned";
		f.owner.actors( rows );
		f.owner.prepare( f.gpu, {}, 257 );
		assert.deepEqual( f.owner.stats( true ).poseEligibility, {
			actors: 2,
			unique: 2,
			gpuSamples: 2,
			linearSamples: 2,
			sharedPaletteSamples: 1,
			clothSamples: 0,
			gpuPaletteSamples: 1
		} );
	} finally {
		f.owner.dispose( f.gpu, null );
	}
});

test("batch census follows actual emitted groups on fresh and retained frames", () => {
	const f = fixture(), samples = [], counters = {}, rows = actors( [ 1, 2, 3 ], .25 );
	rows[0].opacity = .5;
	f.owner.profile( {
		renderBegin() {},
		renderMark() {},
		characterBegin() {},
		characterMark() {},
		characterCount( name, count = 1 ) {
			counters[name] = count;
		},
		characterBatch( variant, count, draws ) {
			samples.push( { variant, count, draws } );
		}
	} );
	try {
		f.owner.actors( rows );
		for ( let frame = 0; frame < 2; frame++ ) {
			samples.length = 0;
			const output = f.owner.prepare( f.gpu, {}, 257 );
			assert.deepEqual( samples, [
				{ variant: "\0fade", count: 1, draws: 1 },
				{ variant: "", count: 2, draws: 1 }
			] );
			assert.equal( samples.reduce( ( sum, row ) => sum + row.draws, 0 ), output.length );
			assert.equal( counters["character-candidates"], 3 );
			assert.equal( counters["character-visible-candidates"], 3 );
			assert.equal( counters["character-needed-poses"], 3 );
			assert.equal( counters["character-particle-needed-poses"], 0 );
		}
		f.owner.actors( [] );
		samples.length = 0;
		f.owner.prepare( f.gpu, {}, 257 );
		assert.deepEqual( samples, [] );
	} finally {
		f.owner.dispose( f.gpu, null );
	}
});

/*
================
Batch variants on retained snapshots

Compare a reused renderer with a fresh owner after optional fields disappear
and after the same gid switches models. Snapshot identity cannot imply a key hit.
================
*/
test("retained actor batches follow model and material variant changes", () => {
	const f = fixture(), rows = actors( [ 1, 2 ], .25 );
	f.owner.model( "other", model, [] );
	try {
		for ( let phase = 0; phase < 12; phase++ ) {
			rows[0].model = phase % 4 === 1 ? "other" : "m";
			rows[0].opacity = phase % 4 === 2 ? .5 : undefined;
			rows[0].materialTint = phase % 4 === 3 ? [ .25, .5, 1 ] : undefined;
			f.owner.actors( rows );
			const actual = f.owner.prepare( f.gpu, {}, 257 );
			const oracle = fixture();
			try {
				oracle.owner.model( "other", model, [] );
				oracle.owner.actors( rows );
				assert.deepEqual( actual.map( visible ), oracle.owner.prepare( oracle.gpu, {}, 257 ).map( visible ) );
			} finally {
				oracle.owner.dispose( oracle.gpu, null );
			}
			if ( phase === 5 ) f.owner.invalidate();
		}
	} finally {
		f.owner.dispose( f.gpu, null );
	}
});

test("sharing a pose does not discard an actors reusable evaluation storage", () => {
	const f = fixture();
	let creations = 0;
	try {
		for ( let frame = 0; frame < 40; frame++ ) {
			const rows = actors( [ 1, 2 ], frame / 100 );
			if ( frame % 2 ) rows[1].time = rows[0].time;
			f.owner.actors( rows );
			const actual = f.owner.prepare( f.gpu, {}, 257 );
			creations += f.owner.stats().poseCreations;
			const oracle = fixture();
			try {
				oracle.owner.actors( rows );
				assert.deepEqual( actual.map( visible ), oracle.owner.prepare( oracle.gpu, {}, 257 ).map( visible ) );
			} finally {
				oracle.owner.dispose( oracle.gpu, null );
			}
		}
		assert.equal(
			creations,
			2,
			"two resident actors should not repeatedly recreate storage when exact poses merge and split"
		);
		f.owner.actors( [] );
		f.owner.prepare( f.gpu, {}, 257 );
		f.owner.actors( actors( [ 1, 2 ], .8 ) );
		f.owner.prepare( f.gpu, {}, 257 );
		assert.equal( f.owner.stats().poseCreations, 2, "retired actors must not leave retained scratch storage" );
		f.owner.invalidate();
		f.owner.prepare( f.gpu, {}, 257 );
		assert.equal( f.owner.stats().poseCreations, 0, "device recovery preserves live CPU poses" );
	} finally {
		f.owner.dispose( f.gpu, null );
	}
});
test("the owned model count follows admission, eviction and disposal", () => {
	const { gpu } = fixture(), owner = createCharacters();
	const admit = ( from, to ) => {
		for ( let i = from; i < to; i++ ) owner.model( "m" + i, model, [] );
	};
	try {
		admit( 0, CHARACTER_MODELS );
		assert.throws( () => owner.model( "extra", model, [] ), /residency exceeds budget/ );
		// Evict all but one; the freed count admits a full budget again.
		owner.retain( [ "m0" ] );
		owner.actors( [] );
		owner.prepare( gpu, {}, 257 );
		admit( CHARACTER_MODELS, 2 * CHARACTER_MODELS - 1 );
		// A full budget of retained models still refuses one more.
		owner.retain( [
			"m0",
			...Array.from( { length: CHARACTER_MODELS - 1 }, ( _, i ) => "m" + (CHARACTER_MODELS + i) )
		] );
		assert.throws( () => owner.model( "extra", model, [] ), /residency exceeds budget/ );
	} finally {
		owner.dispose( gpu, null );
	}
});
test("a hidden session relieves the assembly budget without a prepare pass (BUG-070)", () => {
	// A hidden tab presents actors but draws nothing, so no prepare pass
	// retires residency. Assemblies nothing retains give way at the budget;
	// their draws are released by the next visible pass.
	const f = fixture();
	try {
		f.owner.retain( [ "m", "a0" ] );
		f.owner.assembly( "a0", "m", [] );
		f.owner.actors( [ { ...actors( [ 1 ], 0 )[0], model: "a0" } ] );
		f.owner.prepare( f.gpu, {}, 257 );
		assert.ok( f.stats().uploads > 0, "the drawn assembly owns draws" );
		f.owner.retain( [ "m" ] );
		f.owner.actors( [] );
		for ( let i = 1; i < CHARACTER_ASSEMBLIES; i++ ) f.owner.assembly( "a" + i, "m", [] );
		const releases = f.stats().releases;
		assert.doesNotThrow( () => f.owner.assembly( "extra", "m", [] ) );
		assert.equal( f.stats().releases, releases, "no GPU release happens outside a prepare pass" );
		f.owner.prepare( f.gpu, {}, 257 );
		assert.ok( f.stats().releases > releases, "the next prepare pass releases the retired draws" );
		// Retained assemblies are never relieved: a full retained budget refuses.
		const all = Array.from( { length: CHARACTER_ASSEMBLIES }, ( _, i ) => "r" + i );
		f.owner.retain( [ "m", ...all ] );
		for ( const id of all ) f.owner.assembly( id, "m", [] );
		assert.throws( () => f.owner.assembly( "more", "m", [] ), /assembly residency exceeds budget/ );
	} finally {
		f.owner.dispose( f.gpu, null );
	}
});
test("capacity changes retire obsolete batches before allocating replacement bands", () => {
	const owner = createCharacters();
	owner.model( "a", model, [] );
	owner.model( "b", model, [] );
	let held = 0, peak = 0;
	const gpu = {
		upload( g ) {
			const count = g.instances.length / 16;
			held += count;
			peak = Math.max( peak, held );
			return { count };
		},
		release( d ) {
			held -= d.count;
		},
		updateInstances: d => d,
		updateBones() {}
	};
	const population = (
		first,
		n,
		second,
		m
	) => [
		...actors( Array.from( { length: n }, ( _, i ) => i + 1 ), 0 ).map( a => ({ ...a, model: first }) ),
		...actors( Array.from( { length: m }, ( _, i ) => i + 100 ), 0 ).map( a => ({ ...a, model: second }) )
	];
	try {
		owner.actors( population( "a", 8, "b", 4 ) );
		owner.prepare( gpu, {}, 257 );
		assert.equal( held, 12 );
		owner.actors( population( "b", 8, "a", 4 ) );
		owner.prepare( gpu, {}, 257 );
		assert.equal( held, 12 );
		assert.equal( peak, 12 );
	} finally {
		owner.dispose( gpu, null );
	}
	assert.equal( held, 0 );
});
test("capacity retains geometry through membership changes with exact fresh-frame transforms and palettes", () => {
	const f = fixture();
	try {
		for ( let frame = 0; frame < 80; frame++ ) {
			const count = frame % 3 === 0 ? 7 : frame % 3 === 1 ? 5 : 8,
				rows = actors( Array.from( { length: count }, ( _, i ) => 1 + i + (frame % 2) * 10 ), frame / 80 );
			f.owner.actors( rows );
			const actual = f.owner.prepare( f.gpu, {}, 257 );
			const oracle = fixture();
			try {
				oracle.owner.actors( rows );
				const expected = oracle.owner.prepare( oracle.gpu, {}, 257 );
				assert.deepEqual( actual.map( visible ), expected.map( visible ), `frame ${frame}` );
			} finally {
				oracle.owner.dispose( oracle.gpu, null );
			}
		}
		assert.deepEqual( f.stats(), { uploads: 1, releases: 0 } );
		f.owner.actors( actors( Array.from( { length: 9 }, ( _, i ) => i + 1 ), .5 ) );
		assert.equal( f.owner.prepare( f.gpu, {}, 257 )[0].count, 9 );
		assert.equal( f.stats().uploads, 2 );
		f.owner.invalidate();
		f.owner.prepare( f.gpu, {}, 257 );
		assert.equal( f.stats().uploads, 3 );
	} finally {
		f.owner.dispose( f.gpu, null );
	}
});
