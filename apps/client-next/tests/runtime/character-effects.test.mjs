/*
===========================================================================

character-effects.test.mjs - tests for random.ts, program.ts, effects.ts,
effects.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { createEffectPrograms } = await import( "../../src/engine/runtime/assets/worker/effects/program/program.ts" );
const { createEffectDecoder } = await import( "../../src/engine/runtime/assets/worker/effects/effects.ts" );
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { nativeHeadingYaw } = await import( "../../src/engine/foundation/math/angles.ts" );
const encode = value => new TextEncoder().encode( JSON.stringify( value ) );
function catalog() {
	return {
		framesPerSecond: 20,
		effects: {
			"hit.efp": {
				scale: 1,
				root: {
					name: "hit",
					children: [],
					globalData: { totalFrames: 10 },
					preProgram: [],
					postEmitterProgram: [],
					emitterProgram: [ {
						name: "StaticEmit",
						parameter: {
							kind: "EFStaticEmit",
							value: { min: 0, max: 1, minParticles: 1, burstRate: 1, spawnRate: 1 }
						}
					} ],
					viewCommand: { name: "ViewNone" },
					lifeCommand: { name: "NormalTimeExtinct" },
					renderCommand: { name: "RenderMesh" },
					renderProgram: [ { name: "SetGraphScale", parameter: { value: [ [ 1, 1, 1 ], [ 3, 3, 3 ] ] } }, {
						name: "SetGraphDiffuse",
						parameter: { value: [ [ 255, 128, 0, 200 ], [ 255, 128, 0, 0 ] ] }
					}, { name: "TextureSlide", parameter: { value: [ [ 0, 0, 0.5, 1 ], [ 0.5, 0, 0.5, 1 ] ] } } ],
					resource: {
						srcBlend: 5,
						dstBlend: 2,
						backFaceType: 1,
						// Native default stage 0: MODULATE(TEXTURE, DIFFUSE) for colour and alpha.
						srcTextureArg1: 2,
						srcTextureArg2: 0,
						srcTextureOp: 4,
						dstTextureArg1: 2,
						dstTextureArg2: 0,
						dstTextureOp: 4,
						meshes: [ { path: "mesh.bms", textures: [ "texture.ddj" ] } ]
					}
				}
			}
		},
		meshes: {
			"mesh.bms": {
				positions: [ 0, 0, 0, 1, 0, 0, 0, 1, 0 ],
				normals: [ 0, 0, 1, 0, 0, 1, 0, 0, 1 ],
				uvs: [ 0, 0, 1, 0, 0, 1 ],
				indices: [ 0, 1, 2 ]
			}
		},
		textures: { "texture.png": "/assets/images/texture.png" }
	};
}
test("authored effect mesh, scale, blend and atlas frames compile without generated substitute geometry", () => {
	const decoded = createEffectPrograms().decode( encode( catalog() ), "hit.efp" ),
		model = decoded.model,
		p = model.primitives[0];
	assert.equal( model.clips[0].duration, 0.5 );
	assert.deepEqual( [ ...model.clips[0].channels[0].values ], [ 1, 1, 1, 3, 3, 3 ] );
	assert.deepEqual( defined( p.geometry.material ).blendPair, { source: 5, destination: 2 } );
	assert.equal( defined( p.materialFrames ).colors.at( -1 ), 0 );
	assert.deepEqual( [ ...defined( p.materialFrames ).windows.slice( -4 ) ], [ 0.5, 1, 0.5, 0 ] );
	assert.deepEqual( decoded.imagePaths, [ "/assets/images/texture.png" ] );
	assert.deepEqual( [ ...p.geometry.indices ], [ 0, 1, 2 ] );
});

test("native plates use the retail quad and carry explicit camera-facing behavior", () => {
	const value = catalog(), node = value.effects["hit.efp"].root;
	node.renderCommand.name = "RenderPlate";
	node.viewCommand.name = "ViewBillboard";
	node.resource.meshes[0].path = "";
	const { model } = createEffectPrograms().decode( encode( value ), "hit.efp" ), p = model.primitives[0];
	assert.equal( p.billboard, "camera" );
	assert.deepEqual( [ ...p.geometry.positions ], [ -.5, .5, 0, .5, .5, 0, .5, -.5, 0, -.5, -.5, 0 ] );
	assert.deepEqual( [ ...p.geometry.indices ], [ 0, 1, 2, 0, 2, 3 ] );
	assert.deepEqual( [ ...p.geometry.uvs ], [ 0, 0, 1, 0, 1, 1, 0, 1 ] );
	node.viewCommand.name = "ViewUnsupported";
	assert.throws( () => createEffectPrograms().decode( encode( value ), "hit.efp" ), /lifecycle\/view/ );
	node.viewCommand.name = "ViewBillboard";
	Object.assign( node.emitterProgram[0].parameter.value, { minParticles: 12, max: 10 } );
	const repeated = createEffectPrograms().decode( encode( value ), "hit.efp" ).model;
	assert.deepEqual(
		defined( repeated.primitives[0].emission ).births,
		Array.from( { length: 10 }, ( _, i ) => i / 20 )
	);
	assert.equal( repeated.primitives.length, 1 );
	assert.equal( repeated.clips[0].duration, .95 );
});

test("RenderLinkDPipe and RenderLinkObj admit with plate geometry and ribbon treatment", () => {
	for ( const cmd of [ "RenderLinkDPipe", "RenderLinkObj" ] ) {
		const value = catalog(), node = value.effects["hit.efp"].root;
		node.renderCommand.name = cmd;
		node.resource.meshes[0].path = "";
		const { model } = createEffectPrograms().decode( encode( value ), "hit.efp" ), p = model.primitives[0];
		assert.ok( p.ribbon );
		assert.equal( p.ribbon.fps, 20 );
		assert.deepEqual( [ ...p.geometry.positions ], [ -.5, .5, 0, .5, .5, 0, .5, -.5, 0, -.5, -.5, 0 ] );
		assert.deepEqual( [ ...p.geometry.indices ], [ 0, 1, 2, 0, 2, 3 ] );
	}
});

test("authored scale animation does not overwrite the enclosing effect scale", () => {
	const value = catalog();
	value.effects["hit.efp"].scale = 2;
	const { model } = createEffectPrograms().decode( encode( value ), "hit.efp" );
	assert.deepEqual( model.nodes[0].scale, [ 2, 2, 2 ] );
	assert.equal( model.clips[0].channels[0].node, 1 );
	assert.equal( model.nodes[1].parent, 0 );
	assert.deepEqual( [ ...model.clips[0].channels[0].values ], [ 1, 1, 1, 3, 3, 3 ] );
});

test("SetBANPos and SetBANRot map frame tables to translation and rotation animation channels", () => {
	const value = catalog(), node = value.effects["hit.efp"].root;
	node.renderProgram.push( {
		name: "SetBANPos",
		parameter: { kind: "FrameBANPosition", value: [ [ 1, 2, 3 ], [ 4, 5, 6 ] ] }
	} );
	node.renderProgram.push( {
		name: "SetBANRot",
		parameter: {
			kind: "FrameBANRotation",
			value: [ [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ], [
				0,
				0,
				-1,
				0,
				0,
				1,
				0,
				0,
				1,
				0,
				0,
				0,
				0,
				0,
				0,
				1
			] ]
		}
	} );
	const { model } = createEffectPrograms().decode( encode( value ), "hit.efp" );
	const posChannel = model.clips[0].channels.find( c => c.path === "translation" );
	const rotChannel = model.clips[0].channels.find( c => c.path === "rotation" );
	assert.ok( posChannel );
	assert.equal( posChannel.interpolation, "LINEAR" );
	assert.deepEqual( [ ...posChannel.values ], [ 1, 2, 3, 4, 5, 6 ] );
	assert.ok( rotChannel );
	assert.equal( rotChannel.interpolation, "LINEAR" );
	assert.equal( rotChannel.values.length, 8 );
	assert.ok( Math.abs( rotChannel.values[3] - 1 ) < 1e-5 );
});

test("Phase 6 native blend pairs, leaf anchor, lifetime budget, and inert rotation admit cleanly", () => {
	// Every pair D3D9 defines draws with its own factors (CEFEffect_Render
	// B153A0 sets SRCBLEND/DESTBLEND from the resource), 9/9 included.
	const blends = [ [ 5, 7 ], [ 4, 3 ], [ 1, 6 ], [ 9, 9 ] ];
	for ( const [src, dst] of blends ) {
		const value = catalog();
		value.effects["hit.efp"].root.resource.srcBlend = src;
		value.effects["hit.efp"].root.resource.dstBlend = dst;
		const decoded = createEffectPrograms().decode( encode( value ), "hit.efp" );
		assert.deepEqual( defined( decoded.model.primitives[0].geometry.material ).blendPair, {
			source: src,
			destination: dst
		} );
	}
	// A factor outside D3DBLEND, or a source-only factor as the destination.
	for ( const [src, dst] of [ [ 16, 6 ], [ 5, 12 ], [ 0, 2 ] ] ) {
		const invalid = catalog();
		invalid.effects["hit.efp"].root.resource.srcBlend = src;
		invalid.effects["hit.efp"].root.resource.dstBlend = dst;
		assert.throws(
			() => createEffectPrograms().decode( encode( invalid ), "hit.efp" ),
			/Undefined native effect blend/
		);
	}

	const leafVal = catalog();
	const leafNode = leafVal.effects["hit.efp"].root;
	leafNode.emitterProgram = [];
	leafNode.lifeCommand = { name: "NeverExtinct" };
	leafNode.renderCommand = { name: "RenderNone" };
	leafNode.children = [];
	leafNode.globalData = { totalFrames: 0 };
	const leafDecoded = createEffectPrograms().decode( encode( leafVal ), "hit.efp" );
	assert.equal( leafDecoded.model.primitives.length, 0 );

	const longVal = catalog();
	longVal.effects["hit.efp"].root.globalData.totalFrames = 803;
	const longDecoded = createEffectPrograms().decode( encode( longVal ), "hit.efp" );
	assert.equal( longDecoded.model.clips[0].duration, 803 / 20 );
	const excessiveVal = catalog();
	excessiveVal.effects["hit.efp"].root.globalData.totalFrames = 0;
	assert.throws(
		() => createEffectPrograms().decode( encode( excessiveVal ), "hit.efp" ),
		/Effect lifetime exceeds budget/
	);

	const inertVal = catalog();
	inertVal.effects["hit.efp"].root.renderProgram.push( {
		name: "SetPosition",
		parameter: { value: [ 0, 0, 10 ] }
	} );
	inertVal.effects["hit.efp"].root.renderProgram.push( {
		name: "SetRotation",
		flags: 2,
		start: 0,
		end: 0,
		step: 0,
		parameter: { right: [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] }
	} );
	const inertDecoded = createEffectPrograms().decode( encode( inertVal ), "hit.efp" );
	assert.ok( inertDecoded.model );
	assert.equal( inertDecoded.model.nodes.length, 2 );
	assert.ok( !inertDecoded.model.nodes.some( n => n.name.includes( ":rotation" ) ) );
	assert.deepEqual( inertDecoded.model.nodes[1].translation, [ 0, 0, 10 ] );
});

test("MOV_UP uses native zero arc coefficients and works within a dungeon region", () => {
	const catalog = createEffectDecoder().decode( encode( {
		"1": {
			authoredShotAnimationNames: [ "ANI_ATTACK1" ],
			authoredStages: [ {
				startKeepRotation: true,
				startAddHeight: false,
				targetKeepRotation: true,
				targetAddHeight: false,
				animationPhase: "SHOT",
				startEvent: 0,
				actionType: "AT_MOV_1TAR",
				objectResourcePath: "bolt.bsr",
				startOffset: [ 0, 0, 0 ],
				move: { kind: "MOV_UP", delay: 0, startSpeed: 100, endSpeed: 100 },
				param: [ 1000, 30, 90 ],
				createCount: 1
			} ]
		}
	} ) );
	let id = 0;
	const jobs = new Map();
	const effects = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, decode ) {
				jobs.set(
					++id,
					decode === "effects" ?
						{ kind: "effects", catalog } :
						{
							kind: "bytes",
							buffer: encode( {
								format: "sro-skill-stage-models",
								models: {
									"bolt.bsr": { glb: "/assets/bolt.glb", clips: [ "stand" ], clipLoop: true }
								}
							} ).buffer
						}
				);
				return id;
			},
			take( id ) {
				const r = jobs.get( id );
				jobs.delete( id );
				return r;
			},
			cancel( id ) {
				jobs.delete( id );
			}
		},
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	const entities = [ { gid: 1, regionId: 0x8001, x: -50, y: 0, z: 4000, heading: 0 }, {
			gid: 2,
			regionId: 0x8001,
			x: 50,
			y: 0,
			z: 4000,
			heading: 0
		} ],
		cast = { token: 1, caster: 1, target: 2, skill: 1 },
		gameplay = { casts: [] };
	const step = ( now, triggers = [] ) => effects.step( entities, gameplay, now, () => true, () => 10, triggers );
	step( 0 );
	step( .1 );
	step( .2 );
	gameplay.casts = [ cast ];
	step( 1, [ { cast, phase: "SHOT", event: 0, at: 1 } ] );
	assert.deepEqual( step( 1.5 )[0].pose, { regionId: 0x8001, x: 0, y: 0, z: 4000, yaw: Math.PI / 2 } );
	assert.equal( effects.error(), null );
	assert.deepEqual( step( 2.01 ), [] );
	effects.dispose();
});
test("unsupported particle operations and malformed mesh indices fail admission", () => {
	const unsupported = catalog();
	unsupported.effects["hit.efp"].root.renderProgram.push( { name: "UnsupportedOp" } );
	assert.throws(
		() => createEffectPrograms().decode( encode( unsupported ), "hit.efp" ),
		/Unsupported effect operation/
	);
	const malformed = catalog();
	malformed.meshes["mesh.bms"].indices[2] = 9;
	assert.throws( () => createEffectPrograms().decode( encode( malformed ), "hit.efp" ), /Invalid effect geometry/ );
});
test("cold explicit callbacks queue once and admitted effects expire", () => {
	let id = 0;
	const jobs = new Map(),
		effects = createCharacterEffects(
			{
				available: () => 4,
				request( url, limit, decode ) {
					jobs.set(
						++id,
						decode === "effects" ?
							{
								kind: "effects",
								catalog: {
									"1": {
										clips: [ "attack1" ],
										stages: [ {
											resource: "hit.efp",
											damageEvent: true,
											startEvent: 1,
											action: "AT_DMG_POS",
											move: "MOV_NONE",
											bone: null,
											offset: [ 0, 10, 0 ],
											life: 0,
											sound: null,
											count: 1,
											scripts: []
										} ]
									}
								}
							} :
							{ kind: "bytes", buffer: encode( { format: "sro-skill-stage-models", models: {} } ).buffer }
					);
					return id;
				},
				take( id ) {
					const result = jobs.get( id );
					jobs.delete( id );
					return result;
				},
				cancel( id ) {
					jobs.delete( id );
				}
			},
			"http://localhost",
			() => {},
			createPresentationRandom( 1 )
		);
	const entity = { gid: 2, refObjId: 1, regionId: 257, x: 10, y: 20, z: 30, heading: 0 },
		gameplay = { casts: [], localGid: 1 };
	const step = time =>
		effects.step(
			[ entity ],
			gameplay,
			time,
			() => true,
			() => 0.5,
			gameplay.casts.map( cast => ({ cast, phase: "SHOT", event: 1, at: time }) )
		);
	gameplay.casts = [ { token: 1, caster: 1, target: 2, skill: 1 } ];
	step( 0 );
	step( 0.1 );
	step( 0.2 );
	assert.equal( step( 0.3 ).length, 1 );
	assert.deepEqual( step( .8 ), [] );
	gameplay.casts = [ { token: 2, caster: 1, target: 2, skill: 1 } ];
	const actors = step( 1 );
	assert.equal( actors.length, 1 );
	assert.equal( actors[0].pose.y, 30 );
	assert.equal( step( 1.1 ).length, 1 );
	assert.equal( step( 1.6 ).length, 0 );
	effects.reset();
	effects.dispose();
});
test("resource-less stages remain resource-less and animation names survive catalog projection", () => {
	const record = createEffectDecoder().decode(
		encode( {
			"1": {
				damageEffectPath: "hit.efp",
				authoredShotAnimationNames: [ "ANI_ATTACK1" ],
				authoredStages: [ {
					startKeepRotation: true,
					startAddHeight: false,
					targetKeepRotation: true,
					targetAddHeight: false,
					animationPhase: "SHOT",
					damageEvent: true,
					startEvent: 1,
					actionType: "AT_DMG_POS",
					startOffset: [ 0, 0, 0 ],
					move: { kind: "MOV_NONE" }
				} ]
			}
		} )
	)["1"];
	assert.deepEqual( record.clips, [ "attack1" ] );
	assert.equal( record.stages[0].resource, null );
});

test("multi-target callbacks fan out victim effects once and retain one caster effect", () => {
	let id = 0;
	const jobs = new Map(), sounds = [];
	const stage = damageEvent => ({
		resource: "hit.efp",
		damageEvent,
		startEvent: 1,
		action: "AT_DMG_POS",
		move: "MOV_NONE",
		bone: null,
		offset: [ 0, 0, 0 ],
		life: 10,
		sound: "hit.wav",
		count: 1,
		scripts: []
	});
	const effects = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, decode ) {
				jobs.set(
					++id,
					decode === "effects" ?
						{
							kind: "effects",
							catalog: { "1": { clips: [ "attack1" ], stages: [ stage( false ), stage( true ) ] } }
						} :
						{ kind: "bytes", buffer: encode( { format: "sro-skill-stage-models", models: {} } ).buffer }
				);
				return id;
			},
			take( id ) {
				const value = jobs.get( id );
				jobs.delete( id );
				return value;
			},
			cancel( id ) {
				jobs.delete( id );
			}
		},
		"http://localhost",
		value => sounds.push( value ),
		createPresentationRandom( 1 )
	);
	const entities = [ 1, 2, 3 ].map( gid => ({ gid, regionId: 257, x: gid * 10, y: 0, z: 0, heading: 0 }) ),
		gameplay = { casts: [] };
	const step = ( time, triggers = [] ) => effects.step( entities, gameplay, time, () => true, () => 10, triggers );
	step( 0 );
	step( .1 );
	step( .2 );
	const cast = {
			token: 1,
			caster: 1,
			target: 2,
			skill: 1,
			results: [ { target: 2, impacts: [ { damage: 10 } ] }, { target: 3, impacts: [ { damage: 20 } ] } ]
		},
		trigger = { cast, phase: "SHOT", event: 1, at: 1 };
	gameplay.casts = [ cast ];
	assert.deepEqual( step( 1, [ trigger ] ).map( actor => actor.pose.x ), [ 10, 20, 30 ] );
	assert.equal( step( 1.1, [ trigger ] ).length, 3 );
	assert.equal( sounds.length, 3 );
	assert.equal( new Set( sounds.map( sound => sound.id ) ).size, 3 );
	effects.dispose();
});
function effectFixture( answer, others = [] ) {
	let id = 0;
	const jobs = new Map(), requests = [];
	const stage = action => ({
		resource: "hit.efp",
		damageEvent: true,
		startEvent: 1,
		action,
		move: "MOV_NONE",
		bone: null,
		offset: [ 1, 2, 3 ],
		life: 10,
		sound: null,
		count: 1,
		scripts: []
	});
	const records = {
		kind: "effects",
		catalog: { "1": { clips: [ "attack1" ], stages: [ stage( "AT_ONE_FOLLOW" ), stage( "AT_DMG_POS" ) ] } }
	};
	const manifest = { kind: "bytes", buffer: encode( { format: "sro-skill-stage-models", models: {} } ).buffer };
	const effects = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, decode ) {
				requests.push( decode ?? "models" );
				jobs.set( ++id, answer?.( decode, requests ) ?? (decode === "effects" ? records : manifest) );
				return id;
			},
			take( id ) {
				const value = jobs.get( id );
				jobs.delete( id );
				return value;
			},
			cancel( id ) {
				jobs.delete( id );
			}
		},
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	const entity = { gid: 2, refObjId: 1, regionId: 257, x: 10, y: 20, z: 30, heading: 0 },
		gameplay = { casts: [], localGid: 1 };
	return {
		effects,
		entity,
		gameplay,
		requests,
		step: now =>
			effects.step(
				[ ...others, entity ],
				gameplay,
				now,
				() => true,
				() => 10,
				gameplay.casts.map( cast => ({ cast, phase: "SHOT", event: 1, at: now }) )
			)
	};
}
test("a hit effect following its victim keeps the caster's facing, not the victim's", () => {
	// 8DB770 -> 8D5440: the victim's effect copies the caster's model matrix.
	const caster = { gid: 1, refObjId: 1, regionId: 257, x: 0, y: 20, z: 30, heading: 16384 };
	const f = effectFixture( undefined, [ caster ] );
	f.gameplay.localGid = 99;
	f.gameplay.casts = [ { token: 1, caster: 1, target: 2, skill: 1 } ];
	let actors = [];
	for ( let i = 0; i < 12 && !actors.length; i++ ) actors = f.step( i / 10 );
	f.gameplay.casts = [ { token: 2, caster: 1, target: 2, skill: 1 } ];
	actors = f.step( 1.5 );
	const follow = actors.find( actor => actor.attachment?.root && actor.attachment.gid === f.entity.gid );
	assert.ok( follow, "the follow stage attaches to the victim's root" );
	assert.equal( follow.attachment.facing, nativeHeadingYaw( caster.heading ) );
	assert.notEqual( follow.attachment.facing, nativeHeadingYaw( f.entity.heading ) );
});
test("effect catalogs recover with backoff and admit queued callbacks exactly once", () => {
	const f = effectFixture( ( decode, requests ) =>
		requests.length === 1 ?
			{ kind: "error", error: "HTTP 503" } :
			requests.length === 3 ?
			{ kind: "bytes", buffer: encode( { format: "invalid" } ).buffer } :
			undefined
	);
	f.gameplay.casts = [ { token: 1, caster: 1, target: 2, skill: 1 } ];
	f.step( 0 );
	f.step( 0.1 );
	assert.match( f.effects.error(), /503/ );
	for ( let i = 2; i < 6; i++ ) f.step( i / 10 );
	assert.equal( f.requests.length, 1 );
	f.step( 0.6 );
	f.step( 0.7 );
	f.step( 0.8 );
	assert.match( f.effects.error(), /manifest/ );
	f.step( 1 );
	assert.equal( f.requests.length, 3 );
	f.step( 1.3 );
	assert.equal( f.step( 1.4 ).length, 2 );
	assert.equal( f.effects.error(), null );
	assert.deepEqual(
		f.requests,
		[ "effects", "effects", "models", "models" ],
		"accepted records are retained through model failure"
	);
	f.gameplay.casts = [ { token: 2, caster: 1, target: 2, skill: 1 } ];
	assert.equal( f.step( 1.5 ).length, 4 );
	f.effects.dispose();
	assert.deepEqual( f.step( 2 ), [] );
});
/*
================
assertNearPose

A pose whose position went through float rotation: exact region and yaw,
position within a thousandth.
================
*/
function assertNearPose( actual, expected ) {
	assert.equal( actual.regionId, expected.regionId );
	assert.equal( actual.yaw, expected.yaw );
	for ( const axis of [ "x", "y", "z" ] ) {
		assert.ok( Math.abs( actual[axis] - expected[axis] ) < 1e-3, `${axis}: ${actual[axis]} != ${expected[axis]}` );
	}
}
test("follow attachments resolve current region, heading and predicted local pose while world effects stay fixed", () => {
	const f = effectFixture();
	f.step( 0 );
	f.step( 0.1 );
	f.step( 0.2 );
	f.gameplay.casts = [ { token: 1, caster: 1, target: 2, skill: 1 } ];
	const initial = f.step( 1 );
	assert.equal( initial.length, 2 );
	Object.assign( f.entity, { regionId: 258, x: 100, y: 200, z: 300, heading: 16384 } );
	const moved = f.step( 2 );
	// The authored offset (1, 2, 3) is a native model vector turned by the
	// holder's root (8D6880): at this heading the imported root faces +Z, so
	// it lands at (-1, 2, -3).
	assertNearPose( moved[0].pose, {
		regionId: 258,
		x: 99,
		y: 202,
		z: 297,
		yaw: 16384 / 65535 * 2 * Math.PI + Math.PI / 2
	} );
	assert.deepEqual( moved[1].pose, initial[1].pose );
	f.gameplay.localGid = 2;
	f.gameplay.pose = { regionId: 259, x: 400, y: 500, z: 600, angle: 32768 };
	const predicted = f.step( 3 );
	// Half a turn later the same offset turns to (3, 2, -1).
	assertNearPose( predicted[0].pose, {
		regionId: 259,
		x: 403,
		y: 502,
		z: 599,
		yaw: 32768 / 65535 * 2 * Math.PI + Math.PI / 2
	} );
	assert.deepEqual( predicted[1].pose, initial[1].pose );
	assert.equal(
		f.effects.step( [], f.gameplay, 4, () => true, () => 10 ).length,
		1,
		"fixed world one-shot survives disappearance of its anchor"
	);
	f.gameplay.casts = [];
	assert.equal(
		f.effects.step( [], f.gameplay, 4.1, () => true, () => 10 ).length,
		1,
		"a world one-shot owns its lifetime independently of cast retention"
	);
	assert.deepEqual( f.effects.step( [], f.gameplay, 11.2, () => true, () => 10 ), [] );
	f.effects.dispose();
});
test("persistent catalog failures back off and reset cancels pending requests", () => {
	const f = effectFixture( () => ({ kind: "error", error: "offline" }) );
	for ( let i = 0; i < 2000; i++ ) f.step( i / 100 );
	assert.ok( f.requests.length >= 4 && f.requests.length < 10 );
	f.effects.reset();
	f.step( 0 );
	const count = f.requests.length;
	f.effects.reset();
	f.step( 0 );
	assert.equal( f.requests.length, count + 1 );
	f.effects.dispose();
});

test("effect preflight rejects cumulative expansion before allocating any output buffers", t => {
	const primitiveOverflow = catalog();
	primitiveOverflow.effects["hit.efp"].root.resource.meshes = Array.from(
		{ length: 1025 },
		() => ({ path: "mesh.bms", textures: [ "texture.ddj" ] })
	);
	const frameOverflow = catalog(), mesh = frameOverflow.meshes["mesh.bms"];
	mesh.positions = Array( 800 * 3 ).fill( 0 );
	mesh.normals = Array( 800 * 3 ).fill( 0 );
	mesh.uvs = Array( 800 * 2 ).fill( 0 );
	frameOverflow.effects["hit.efp"].root.globalData.totalFrames = 600;
	frameOverflow.effects["hit.efp"].root.resource.meshes = Array.from(
		{ length: 1024 },
		() => ({ path: "mesh.bms", textures: [ "texture.ddj" ] })
	);
	const imageOverflow = catalog();
	imageOverflow.effects["hit.efp"].root.resource.meshes = Array.from( { length: 65 }, ( _, i ) => {
		imageOverflow.textures[i + ".png"] = "/assets/images/" + i + ".png";
		return { path: "mesh.bms", textures: [ i + ".ddj" ] };
	} );
	const fixtures = [ [ encode( primitiveOverflow ), /primitive budget/ ], [
		encode( frameOverflow ),
		/expansion exceeds/
	], [ encode( imageOverflow ), /image budget/ ] ];
	let allocations = 0;
	for ( const name of [ "Float32Array", "Uint32Array" ] ) {
		const original = globalThis[name];
		t.mock.property(
			globalThis,
			name,
			new Proxy( original, {
				construct() {
					allocations++;
					throw new Error( "Output allocated before admission" );
				},
				get( target, key ) {
					if ( key === "from" ) {
						return () => {
							allocations++;
							throw new Error( "Output allocated before admission" );
						};
					}
					return Reflect.get( target, key );
				}
			} )
		);
	}
	for ( const [bytes, error] of fixtures ) {
		assert.throws( () => createEffectPrograms().decode( bytes, "hit.efp" ), error );
	}
	assert.equal( allocations, 0 );
});

test("straight projectile waits for launch callback, drains at arrival, and survives server finalization", () => {
	let id = 0;
	const jobs = new Map(), sounds = [];
	const stage = {
		resource: "bolt.bsr",
		damageEvent: true,
		startEvent: 1,
		phase: "SHOT",
		action: "AT_MOV_1TAR",
		move: "MOV_STRAIGHT",
		movement: { delayMs: 200, startSpeed: 100, endSpeed: 100 },
		bone: null,
		targetBone: null,
		offset: [ 0, 0, 0 ],
		targetOffset: [ 0, 0, 0 ],
		life: 0,
		sound: "/assets/audio/launch.wav",
		soundEnd: "/assets/audio/arrive.wav",
		arrivalResource: "hit.efp",
		count: 1,
		scripts: []
	};
	const effects = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, decode ) {
				jobs.set(
					++id,
					decode === "effects" ?
						{ kind: "effects", catalog: { "1": { clips: [ "attack1" ], stages: [ stage ] } } } :
						{
							kind: "bytes",
							buffer: encode( {
								format: "sro-skill-stage-models",
								models: {
									"bolt.bsr": { glb: "/assets/bolt.glb", clips: [ "stand" ], clipLoop: false }
								}
							} ).buffer
						}
				);
				return id;
			},
			take( id ) {
				const r = jobs.get( id );
				jobs.delete( id );
				return r;
			},
			cancel() {}
		},
		"http://localhost",
		s => sounds.push( s ),
		createPresentationRandom( 1 )
	);
	const entities = [ { gid: 1, regionId: 257, x: 0, y: 0, z: 0, heading: 0 }, {
			gid: 2,
			regionId: 257,
			x: 100,
			y: 0,
			z: 0,
			heading: 0
		} ],
		cast = { token: 1, caster: 1, target: 2, skill: 1 },
		gameplay = { casts: [] };
	const step = ( now, triggers = [], ready = () => true ) =>
		effects.step( entities, gameplay, now, ready, () => 0.25, triggers );
	step( 0 );
	step( 0.1 );
	step( 0.2 );
	gameplay.casts = [ cast ];
	assert.deepEqual( step( 1 ), [], "B245 is not a hit callback" );
	assert.deepEqual( step( 1, [ { cast, phase: "SHOT", event: 1, at: 1 } ] ), [] );
	assert.equal( sounds.length, 0 );
	const flying = step( 1.7 );
	assert.equal( flying.length, 1 );
	assert.ok( Math.abs( flying[0].pose.x - 50 ) < 1e-5 );
	assert.equal( sounds.length, 1 );
	const arrived = step( 2.21 );
	assert.equal( arrived.length, 1 );
	assert.match( arrived[0].model, /hit.efp/ );
	assert.ok( Math.abs( arrived[0].time - 0.01 ) < 1e-5 );
	assert.equal( sounds.length, 2 );
	const transfers = effects.takeImpacts();
	assert.deepEqual( transfers.map( e => e.kind ), [ "launch", "arrival" ] );
	assert.equal( transfers[0].index, 0 );
	assert.equal( transfers[0].soundSkill, 0 );
	assert.equal( transfers[1].at, 2.2 );
	assert.deepEqual( effects.takeImpacts(), [] );
	assert.deepEqual( step( 2.5 ), [] );
	gameplay.casts = [ { ...cast, token: 2 } ];
	step( 3, [ { cast: gameplay.casts[0], phase: "SHOT", event: 1, at: 3 } ] );
	gameplay.casts = [];
	assert.deepEqual( step( 3.1 ), [] );
	assert.equal( sounds.length, 2 );
	assert.equal( step( 3.7 ).length, 1 );
	assert.equal( step( 4.21 ).length, 1 );
	assert.equal( step( 4.3 ).length, 1, "arrival visual outlives source cast" );
	assert.deepEqual( step( 4.5 ), [] );
	effects.dispose();
});

test("authored random-speed arc reaches live effects and reset preserves the shared stream", () => {
	const catalog = createEffectDecoder().decode( encode( {
		"1": {
			authoredShotAnimationNames: [ "ANI_ATTACK1" ],
			authoredStages: [ {
				startKeepRotation: true,
				startAddHeight: false,
				targetKeepRotation: true,
				targetAddHeight: false,
				animationPhase: "SHOT",
				startEvent: 0,
				actionType: "AT_MOV_1TAR",
				objectResourcePath: "bolt.bsr",
				startOffset: [ 0, 0, 0 ],
				move: { kind: "MOV_UPR", delay: 0, startSpeed: 100, endSpeed: 200 },
				param: [ 1000, 0, 0 ],
				createCount: 1
			} ]
		}
	} ) );
	let id = 0;
	const jobs = new Map();
	const effects = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, decode ) {
				jobs.set(
					++id,
					decode === "effects" ?
						{ kind: "effects", catalog } :
						{
							kind: "bytes",
							buffer: encode( {
								format: "sro-skill-stage-models",
								models: {
									"bolt.bsr": { glb: "/assets/bolt.glb", clips: [ "stand" ], clipLoop: true }
								}
							} ).buffer
						}
				);
				return id;
			},
			take( id ) {
				const r = jobs.get( id );
				jobs.delete( id );
				return r;
			},
			cancel( id ) {
				jobs.delete( id );
			}
		},
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	const entities = [ { gid: 1, regionId: 257, x: 100, y: 0, z: 100, heading: 0 }, {
			gid: 2,
			regionId: 257,
			x: 200,
			y: 0,
			z: 100,
			heading: 0
		} ],
		cast = { token: 1, caster: 1, target: 2, skill: 1 },
		gameplay = { casts: [] };
	const step = ( now, triggers = [] ) => effects.step( entities, gameplay, now, () => true, () => 10, triggers );
	step( 0 );
	step( .1 );
	step( .2 );
	gameplay.casts = [ cast ];
	step( 1, [ { cast, phase: "SHOT", event: 0, at: 1 } ] );
	const reference = createPresentationRandom( 1 ),
		speed = reference.range( 100, 200 ),
		nextSpeed = reference.range( 100, 200 );
	const mid = step( 1 + 50 / speed );
	assert.equal( mid.length, 1 );
	assert.ok( Math.abs( mid[0].pose.x - 150 ) < 1e-4 );
	assert.ok( Math.abs( mid[0].pose.y - 100 ) < 1e-4 );
	assert.equal( effects.error(), null );
	assert.deepEqual( step( 1 + 100 / speed + .001 ), [] );
	effects.reset();
	step( 2, [ { cast, phase: "SHOT", event: 0, at: 2 } ] );
	const replay = step( 2 + 50 / nextSpeed );
	assert.deepEqual( replay[0].pose, mid[0].pose );
	effects.dispose();
});

test("socket projectiles capture launch once and keep flight independent of later caster motion", () => {
	const catalog = createEffectDecoder().decode( encode( {
		"1": {
			authoredShotAnimationNames: [ "ANI_ATTACK1" ],
			authoredStages: [ {
				startKeepRotation: true,
				startAddHeight: false,
				targetKeepRotation: true,
				targetAddHeight: false,
				animationPhase: "SHOT",
				startEvent: 0,
				actionType: "AT_MOV_1TAR",
				objectResourcePath: "bolt.bsr",
				startBone: "hand",
				startOffset: [ 1, 2, 3 ],
				move: { kind: "MOV_STRAIGHT", delay: 0, startSpeed: 100, endSpeed: 100 },
				createCount: 1
			} ]
		}
	} ) );
	let id = 0;
	const jobs = new Map();
	const effects = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, decode ) {
				jobs.set(
					++id,
					decode === "effects" ?
						{ kind: "effects", catalog } :
						{
							kind: "bytes",
							buffer: encode( {
								format: "sro-skill-stage-models",
								models: {
									"bolt.bsr": { glb: "/assets/bolt.glb", clips: [ "stand" ], clipLoop: true }
								}
							} ).buffer
						}
				);
				return id;
			},
			take( id ) {
				const r = jobs.get( id );
				jobs.delete( id );
				return r;
			},
			cancel( id ) {
				jobs.delete( id );
			}
		},
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	const entities = [ { gid: 1, regionId: 257, x: 0, y: 0, z: 0, heading: 0 }, {
			gid: 2,
			regionId: 257,
			x: 110,
			y: 0,
			z: 0,
			heading: 0
		} ],
		cast = { token: 1, caster: 1, target: 2, skill: 1 },
		gameplay = { casts: [] };
	let calls = 0;
	const socket = ( gid, bone, offset, trigger ) => {
		calls++;
		assert.equal( gid, 1 );
		assert.equal( bone, "hand" );
		// The authored native vector; the socket owner turns it (8D6880).
		assert.deepEqual( offset, [ 1, 2, 3 ] );
		assert.equal( trigger.at, 1 );
		return { regionId: 257, x: 10, y: 0, z: 0, yaw: 0 };
	};
	const step = ( now, triggers = [] ) =>
		effects.step( entities, gameplay, now, () => true, () => 10, triggers, socket );
	step( 0 );
	step( .1 );
	step( .2 );
	gameplay.casts = [ cast ];
	const start = step( 1, [ { cast, phase: "SHOT", event: 0, at: 1 } ] );
	assert.equal( start[0].pose.x, 10 );
	assert.equal( start[0].attachment, undefined );
	entities[0].x = 999;
	assert.equal( step( 1.5 )[0].pose.x, 60 );
	assert.equal( calls, 1 );
	assert.equal( effects.error(), null );
	gameplay.casts = [];
	assert.equal( step( 1.6 )[0].pose.x, 70 );
	assert.deepEqual( step( 2 ), [] );
	effects.dispose();
});

test("target-bone projectiles follow the evaluated endpoint to their arrival", () => {
	const row = {
		phase: "SHOT",
		startEvent: 0,
		action: "AT_MOV_1TAR",
		move: "MOV_STRAIGHT",
		resource: "bolt.bsr",
		offset: [ 0, 0, 0 ],
		targetOffset: [ 1, 2, 3 ],
		targetBone: "chest",
		movement: { delayMs: 0, startSpeed: 100, endSpeed: 100 },
		count: 1,
		scripts: [],
		life: 0
	};
	let id = 0;
	const jobs = new Map(), catalog = { "1": { clips: [ "attack1" ], stages: [ row ] } };
	const owner = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, decode ) {
				jobs.set(
					++id,
					decode === "effects" ?
						{ kind: "effects", catalog } :
						{
							kind: "bytes",
							buffer: encode( {
								format: "sro-skill-stage-models",
								models: {
									"bolt.bsr": { glb: "/assets/bolt.glb", clips: [ "stand" ], clipLoop: true }
								}
							} ).buffer
						}
				);
				return id;
			},
			take( id ) {
				const value = jobs.get( id );
				jobs.delete( id );
				return value;
			},
			cancel() {}
		},
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	const entities = [ { gid: 1, regionId: 257, x: 0, y: 0, z: 0, heading: 0 }, {
			gid: 2,
			regionId: 257,
			x: 50,
			y: 0,
			z: 0,
			heading: 0
		} ],
		cast = { token: 1, caster: 1, target: 2, skill: 1 },
		gameplay = { casts: [] };
	let calls = 0;
	const socket = ( gid, bone, offset ) => {
		calls++;
		assert.equal( gid, 2 );
		assert.equal( bone, "chest" );
		// The authored native vector; the socket owner turns it (8D6880).
		assert.deepEqual( offset, [ 1, 2, 3 ] );
		return { regionId: 257, x: 200, y: 0, z: 0, yaw: 0 };
	};
	const step = ( at, triggers = [] ) => owner.step( entities, gameplay, at, () => true, () => 10, triggers, socket );
	step( 0 );
	step( .1 );
	step( .2 );
	gameplay.casts = [ cast ];
	assert.equal( step( 1, [ { cast, phase: "SHOT", event: 0, at: 1 } ] ).length, 1 );
	entities[1].x = 999;
	assert.equal( step( 2 )[0].pose.x, 100 );
	// A shot at another actor homes (stepHomingProjectile): its socket is
	// evaluated again on every frame, not captured once at launch.
	assert.ok( calls > 1, "the socket was read once: " + calls );
	assert.deepEqual( step( 3 ), [] );
	assert.equal( owner.error(), null );
	owner.dispose();
});

test("later update/trailing operations reject the whole effect instead of silently disappearing", () => {
	for ( const field of [ "updateProgram", "trailingProgram" ] ) {
		const value = catalog();
		value.effects["hit.efp"].root[field] = [ { name: "SetVelocity" } ];
		assert.throws( () => createEffectPrograms().decode( encode( value ), "hit.efp" ), /lifecycle/ );
	}
	const empty = catalog();
	empty.effects["hit.efp"].root.emitterProgram[0].parameter.value.spawnRate = 0;
	assert.equal( createEffectPrograms().decode( encode( empty ), "hit.efp" ).model.primitives.length, 0 );
});

test("a projectile inherits its callback group result even when the damage flag is on a sibling stage", () => {
	const stages = [ {
		phase: "SHOT",
		startEvent: 1,
		action: "AT_MOV_1TAR",
		move: "MOV_STRAIGHT",
		resource: "bolt.bsr",
		damageEvent: false,
		offset: [ 0, 0, 0 ],
		movement: { delayMs: 0, startSpeed: 100, endSpeed: 100 },
		count: 1,
		scripts: [],
		life: 0
	}, {
		phase: "SHOT",
		startEvent: 1,
		action: "AT_DMG_POS",
		move: "MOV_NONE",
		resource: null,
		damageEvent: true,
		offset: [ 0, 0, 0 ],
		count: 1,
		scripts: [],
		life: 0
	} ];
	let id = 0;
	const jobs = new Map(),
		owner = createCharacterEffects(
			{
				available: () => 4,
				request( url, limit, decode ) {
					jobs.set(
						++id,
						decode === "effects" ?
							{ kind: "effects", catalog: { 1: { clips: [ "attack1" ], stages } } } :
							{
								kind: "bytes",
								buffer: encode( {
									format: "sro-skill-stage-models",
									models: {
										"bolt.bsr": { glb: "/assets/bolt.glb", clips: [ "stand" ], clipLoop: false }
									}
								} ).buffer
							}
					);
					return id;
				},
				take( id ) {
					const r = jobs.get( id );
					jobs.delete( id );
					return r;
				},
				cancel() {}
			},
			"http://localhost",
			() => {},
			createPresentationRandom( 1 )
		);
	const entities = [ { gid: 1, regionId: 257, x: 0, y: 0, z: 0, heading: 0 }, {
			gid: 2,
			regionId: 257,
			x: 100,
			y: 0,
			z: 0,
			heading: 0
		} ],
		cast = { token: 1, caster: 1, target: 2, skill: 1 },
		game = { casts: [] };
	const step = ( at, triggers = [] ) => owner.step( entities, game, at, () => true, () => .2, triggers );
	step( 0 );
	step( .1 );
	step( .2 );
	game.casts = [ cast ];
	step( 1, [ { cast, phase: "SHOT", event: 1, at: 1 } ] );
	assert.equal( owner.takeImpacts()[0].kind, "launch" );
	game.casts = [];
	step( 2.01 );
	const arrival = owner.takeImpacts();
	assert.equal( arrival.length, 1 );
	assert.equal( arrival[0].kind, "arrival" );
	assert.equal( arrival[0].at, 2 );
	owner.dispose();
});

test("attached effects select both activation families, wait for assets, restore loops and retire once", () => {
	let id = 0, admitted = false;
	const jobs = new Map(), sounds = [];
	const phases = [ "ACT_OS", "ACT_OL", "ACT_OE", "ACT_S", "ACT_L", "DEACT" ];
	const stages = phases.map( phase => ({
		phase,
		resource: phase + ".efp",
		damageEvent: false,
		startEvent: 0,
		action: phase.endsWith( "L" ) ? "AT_LOOP" : "AT_ONE_FOLLOW",
		move: "MOV_NONE",
		bone: null,
		offset: [ 0, 0, 0 ],
		life: 1,
		sound: phase + ".wav",
		count: 1,
		scripts: []
	}) );
	const assets = {
		available: () => 4,
		request( url, limit, decode ) {
			jobs.set(
				++id,
				decode === "effects" ?
					{ kind: "effects", catalog: { 7: { clips: [], stages } } } :
					{ kind: "bytes", buffer: encode( { format: "sro-skill-stage-models", models: {} } ).buffer }
			);
			return id;
		},
		take( id ) {
			const r = jobs.get( id );
			jobs.delete( id );
			return r;
		},
		cancel( id ) {
			jobs.delete( id );
		}
	};
	const fx = createCharacterEffects(
		assets,
		"http://localhost",
		s => sounds.push( s ),
		createPresentationRandom( 1 )
	);
	const entity = { gid: 1, regionId: 257, x: 0, y: 0, z: 0, heading: 0 };
	const actor = { gid: 1, model: "body", pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 }, height: 10 };
	const game = { casts: [], attachedEffects: [ { gid: 1, skill: 7, token: 9, phase: 1, receivedAtMs: 0 } ] };
	const step = t => fx.step( [ entity ], game, t, () => admitted, () => 1, [], undefined, [ actor ] );
	step( 0 );
	step( .1 );
	assert.deepEqual( step( .2 ), [] );
	assert.equal( sounds.length, 0 );
	admitted = true;
	assert.equal( step( .3 ).length, 2 );
	assert.deepEqual( sounds.map( s => s.path ), [ "ACT_OS.wav", "ACT_OL.wav" ] );
	assert.equal( step( 2 ).length, 1 );
	game.attachedEffects = [];
	const stopped = step( 3 );
	assert.equal( stopped.length, 2 );
	assert.ok( stopped.every( v => !v.loop ) );
	assert.equal(
		stopped.filter( v => v.emissionEnd !== undefined ).length,
		1,
		"release drops the loop retention, not the fresh stop-vector one-shot"
	);
	assert.equal( sounds.at( -1 ).path, "ACT_OE.wav" );
	assert.equal( step( 5 ).length, 0 );
	assert.equal( sounds.length, 3 );
	game.attachedEffects = [ { gid: 1, skill: 7, token: 0, phase: 2, restored: true } ];
	assert.equal( step( 6 ).length, 1 );
	assert.equal( sounds.at( -1 ).path, "ACT_L.wav" );
	game.attachedEffects = [];
	step( 7 );
	assert.equal( sounds.at( -1 ).path, "DEACT.wav" );
	fx.reset();
	game.attachedEffects = [ { gid: 1, skill: 7, token: 0, phase: 2 } ];
	step( 8 );
	assert.deepEqual( sounds.slice( -3 ).map( s => s.path ), [ "ACT_S.wav", "ACT_L.wav", "DEACT.wav" ] );
	assert.equal( step( 10 ).length, 0 );
	const n = sounds.length;
	step( 11 );
	assert.equal( sounds.length, n );
	fx.dispose();
});

test("an active-phase follower with no authored life stays as long as its attachment", () => {
	// The White Hawk's summon: AT_ONE_FOLLOW in ACT_L, life 0, no SCT_MOVER.
	const stages = [ {
		phase: "ACT_L",
		resource: "follower.efp",
		damageEvent: false,
		startEvent: 0,
		action: "AT_ONE_FOLLOW",
		move: "MOV_NONE",
		bone: null,
		offset: [ 0, 20, 0 ],
		life: 0,
		count: 1,
		scripts: []
	} ];
	let id = 0;
	const jobs = new Map();
	const fx = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, decode ) {
				jobs.set(
					++id,
					decode === "effects" ?
						{ kind: "effects", catalog: { 7: { clips: [], stages } } } :
						{ kind: "bytes", buffer: encode( { format: "sro-skill-stage-models", models: {} } ).buffer }
				);
				return id;
			},
			take( id ) {
				const r = jobs.get( id );
				jobs.delete( id );
				return r;
			},
			cancel( id ) {
				jobs.delete( id );
			}
		},
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	const entity = { gid: 1, regionId: 257, x: 0, y: 0, z: 0, heading: 0 };
	const actor = { gid: 1, model: "body", pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 }, height: 10 };
	const game = { casts: [], attachedEffects: [ { gid: 1, skill: 7, token: 9, phase: 2, receivedAtMs: 0 } ] };
	// The follower's clip lasts one second.
	const step = t => fx.step( [ entity ], game, t, () => true, () => 1, [], undefined, [ actor ] );
	step( 0 );
	step( .1 );
	assert.equal( step( .2 ).length, 1 );
	const later = step( 30 );
	assert.equal( later.length, 1, "the follower vanished one clip after the summon" );
	assert.ok( later[0].loop, "the follower's clip must loop while it stays" );
	game.attachedEffects = [];
	step( 31 );
	assert.equal( step( 40 ).length, 0, "the follower outlived its attachment" );
	fx.dispose();
});

test("native overlap flag changes only the stop vector after initial phase selection", () => {
	const stages = [ "ACT_S", "ACT_L", "DEACT", "ACT_OE" ].map( phase => ({
		phase,
		resource: null,
		startEvent: 0,
		action: "AT_ONE_FOLLOW",
		move: "MOV_NONE",
		scripts: [],
		sound: phase,
		offset: [ 0, 0, 0 ],
		count: 1,
		life: 0
	}) );
	let id = 0;
	const jobs = new Map(), sounds = [];
	const owner = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, decode ) {
				jobs.set(
					++id,
					decode === "effects" ?
						{ kind: "effects", catalog: { 7: { overlap: true, clips: [], stages } } } :
						{ kind: "bytes", buffer: encode( { format: "sro-skill-stage-models", models: {} } ).buffer }
				);
				return id;
			},
			take( id ) {
				const r = jobs.get( id );
				jobs.delete( id );
				return r;
			},
			cancel() {}
		},
		"http://localhost",
		s => sounds.push( s.path ),
		createPresentationRandom( 1 )
	);
	const entity = { gid: 1, regionId: 257, x: 0, y: 0, z: 0, heading: 0 },
		actor = { gid: 1, pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 } },
		game = { casts: [], attachedEffects: [ { gid: 1, skill: 7, token: 1, phase: 2 } ] };
	const step = at => owner.step( [ entity ], game, at, () => true, () => 1, [], undefined, [ actor ] );
	step( 0 );
	step( .1 );
	step( .2 );
	assert.deepEqual( sounds, [ "ACT_S", "ACT_L" ] );
	game.attachedEffects = [];
	step( 1 );
	step( 2 );
	assert.deepEqual( sounds, [ "ACT_S", "ACT_L", "ACT_OE" ] );
	owner.dispose();
	const decoder = createEffectDecoder();
	assert.equal( decoder.decode( encode( { 7: { overlap01: true, authoredStages: [] } } ) )[7].overlap, true );
	assert.throws( () => decoder.decode( encode( { 7: { overlap01: 1, authoredStages: [] } } ) ), /overlap/ );
});

test("proved InitSpawn activation event occurs once after admission, including restored and zero-token starts", () => {
	let id = 0;
	const jobs = new Map(), catalog = { 7: { clips: [], stages: [] } };
	const owner = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, decode ) {
				jobs.set(
					++id,
					decode === "effects" ?
						{ kind: "effects", catalog } :
						{ kind: "bytes", buffer: encode( { format: "sro-skill-stage-models", models: {} } ).buffer }
				);
				return id;
			},
			take( id ) {
				const result = jobs.get( id );
				jobs.delete( id );
				return result;
			},
			cancel() {}
		},
		"http://fixture.invalid",
		() => {},
		createPresentationRandom( 1 )
	);
	const entity = { gid: 1, regionId: 257, x: 0, y: 0, z: 0, heading: 0 },
		body = { gid: 1, pose: { ...entity, yaw: 0 } },
		game = { casts: [], attachedEffects: [] };
	const step = at => owner.step( [ entity ], game, at, () => true, () => 1, [], undefined, [ body ] );
	step( 0 );
	step( .1 );
	step( .2 );
	for ( const [token, restored, at] of [ [ 1, false, 1 ], [ 2, true, 2 ], [ 0, false, 3 ] ] ) {
		game.attachedEffects = [ { gid: 1, skill: 7, token, phase: 2, restored } ];
		step( at );
		assert.deepEqual( owner.takeActivations(), [ { gid: 1, skill: 7, at } ] );
		step( at + .1 );
		assert.deepEqual( owner.takeActivations(), [] );
	}
	owner.system( 1, 7 );
	step( 4 );
	assert.equal( owner.takeActivations().length, 1 );
	step( 4.1 );
	assert.deepEqual( owner.takeActivations(), [] );
	owner.reset();
	assert.deepEqual( owner.takeActivations(), [] );
	owner.dispose();
});

test("Tomb Stone projectiles capture live local target at launch, never its spawn row", () => {
	const record =
		JSON.parse( readFileSync( "../../.generated/client-public/assets/skill/effectRecords.json", "utf8" ) )["173"];
	const catalog = createEffectDecoder().decode( encode( { "173": record } ) );
	let id = 0;
	const jobs = new Map();
	const effects = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, decode ) {
				jobs.set(
					++id,
					decode === "effects" ?
						{ kind: "effects", catalog } :
						{ kind: "bytes", buffer: encode( { format: "sro-skill-stage-models", models: {} } ).buffer }
				);
				return id;
			},
			take( id ) {
				const result = jobs.get( id );
				jobs.delete( id );
				return result;
			},
			cancel() {}
		},
		"http://fixture.invalid",
		() => {},
		createPresentationRandom( 1 )
	);
	const entities = [ { gid: 1, regionId: 24235, x: 850, y: 80, z: 300, heading: 0, kind: "monster" }, {
		gid: 2,
		regionId: 24235,
		x: 755,
		y: 76,
		z: 131,
		heading: 0,
		kind: "player"
	} ];
	const cast = {
		token: 1,
		caster: 1,
		target: 2,
		skill: 173,
		impacts: [ { damage: 156, flags: 1 } ],
		results: [ { target: 2, impacts: [ { damage: 156, flags: 1 } ] } ]
	};
	const gameplay = { localGid: 2, pose: { regionId: 24235, x: 948, y: 73, z: 325, angle: 100 }, casts: [] };
	const step = ( now, triggers = [] ) =>
		effects.step(
			entities,
			gameplay,
			now,
			() => true,
			() => 10,
			triggers,
			() => ({ regionId: 24235, x: 850, y: 90, z: 300, yaw: 0 })
		);
	step( 0 );
	step( .1 );
	step( .2 );
	gameplay.casts = [ cast ];
	const launched = step( 1, [ { cast, phase: "SHOT", event: 1, at: 1 } ] );
	assert.equal( launched.length, 2 );
	const travel = step( 1.1 );
	assert.ok(
		travel.every( a => a.pose.x > 850 ),
		"both bolts travel toward the live player, opposite the stale spawn"
	);
	gameplay.pose = { ...gameplay.pose, x: 1100, z: 450 };
	const arrivals = step( 2 ).filter( a => a.model.includes( "force_hit" ) );
	// The authored target offsets turn with the player's root (8D6880): at
	// this heading their lateral X lands on world Z.
	const round = value => Math.round( value * 100 ) / 100;
	assert.deepEqual(
		arrivals.map( a => [ round( a.pose.x ), round( a.pose.y ), round( a.pose.z ) ] )
			.sort( ( a, b ) => a[0] - b[0] ),
		[ [ 947.98, 83, 327 ], [ 948.02, 81, 323 ] ],
		"native straight flight retains the launch destination after subsequent movement"
	);
	assert.equal( entities[1].x, 755, "effect sampling must not mutate the entity owner" );
	effects.dispose();
});
test("AT_TARGET_F shot stages anchor on the target exactly like AT_TARGET", () => {
	const born = {};
	for ( const action of [ "AT_TARGET", "AT_TARGET_F" ] ) {
		let id = 0;
		const jobs = new Map(),
			effects = createCharacterEffects(
				{
					available: () => 4,
					request( url, limit, decode ) {
						jobs.set(
							++id,
							decode === "effects" ?
								{
									kind: "effects",
									catalog: {
										"1": {
											clips: [ "attack1" ],
											stages: [ {
												resource: "hit.efp",
												damageEvent: false,
												startEvent: 1,
												action,
												move: "MOV_NONE",
												bone: null,
												offset: [ 0, 0, 0 ],
												targetBone: null,
												targetOffset: [ 0, 0, 0 ],
												life: 0,
												sound: null,
												count: 1,
												scripts: []
											} ]
										}
									}
								} :
								{
									kind: "bytes",
									buffer: encode( { format: "sro-skill-stage-models", models: {} } ).buffer
								}
						);
						return id;
					},
					take( id ) {
						const result = jobs.get( id );
						jobs.delete( id );
						return result;
					},
					cancel( id ) {
						jobs.delete( id );
					}
				},
				"http://localhost",
				() => {},
				createPresentationRandom( 1 )
			);
		const caster = { gid: 1, refObjId: 1, regionId: 257, x: 0, y: 0, z: 0, heading: 0 },
			target = { gid: 2, refObjId: 1, regionId: 257, x: 40, y: 5, z: 30, heading: 0 },
			gameplay = { casts: [ { token: 1, caster: 1, target: 2, skill: 1 } ], localGid: 1 };
		const step = time =>
			effects.step( [ caster, target ], gameplay, time, () => true, () => 0.5, [ {
				cast: gameplay.casts[0],
				phase: "SHOT",
				event: 1,
				at: time
			} ] );
		let actors = [];
		for ( const time of [ 0, 0.1, 0.2, 0.3 ] ) actors = step( time );
		assert.equal( effects.error(), null, `${action}: ${effects.error()}` );
		assert.equal( actors.length, 1, `${action} produced no effect` );
		born[action] = actors[0].pose;
		effects.dispose();
	}
	assert.deepEqual( born.AT_TARGET_F, born.AT_TARGET );
	assert.equal( born.AT_TARGET_F.x, 40 );
});
