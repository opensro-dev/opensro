/*
===========================================================================

character-effects.test.mjs - tests for random.ts, program.ts, effects.ts,
effects.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { createEffectPrograms } = await import( "../../src/engine/runtime/assets/worker/effects/program/program.ts" );
const { createEffectDecoder } = await import( "../../src/engine/runtime/assets/worker/effects/effects.ts" );
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { createCameraScripts } = await import( "../../src/engine/runtime/world/camera/camera.ts" );
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
		spacing: 0,
		end: 0,
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
	const makeOwner = () =>
		createCharacterEffects(
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
	const owner = makeOwner();
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
	// Native default (CProjectileTrace_Process 8D8580): the target socket is
	// captured once at launch and never re-read in flight.
	assert.equal( calls, 1, "native projectile captures the target socket once at launch" );
	assert.deepEqual( step( 3 ), [] );
	assert.equal( owner.error(), null );
	owner.dispose();

	// With the port-only Experimental homingProjectiles toggle enabled, the
	// shot re-evaluates the target socket on every frame (stepHomingProjectile).
	calls = 0;
	entities[1].x = 50;
	gameplay.casts = [];
	gameplay.homingProjectiles = true;
	const homingOwner = makeOwner();
	const stepHoming = ( at, triggers = [] ) =>
		homingOwner.step( entities, gameplay, at, () => true, () => 10, triggers, socket );
	stepHoming( 0 );
	stepHoming( .1 );
	stepHoming( .2 );
	gameplay.casts = [ cast ];
	assert.equal( stepHoming( 1, [ { cast, phase: "SHOT", event: 0, at: 1 } ] ).length, 1 );
	entities[1].x = 999;
	assert.equal( stepHoming( 2 )[0].pose.x, 100 );
	assert.ok( calls > 1, "with homingProjectiles enabled, the socket is re-read in flight: " + calls );
	assert.deepEqual( stepHoming( 3 ), [] );
	assert.equal( homingOwner.error(), null );
	homingOwner.dispose();
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
	const record = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skill/effectRecords.json", "utf8" ) )["173"];
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

/*
================
roarEffects

An effect owner whose catalog and model manifest load empty, so only the
attack-pet roar controller draws. Every model counts as ready.
================
*/
function roarEffects( play = _event => {} ) {
	let id = 0;
	const jobs = new Map();
	return createCharacterEffects(
		{
			available: () => 4,
			progress: () => null,
			health: () => ({ phase: "running" }),
			install() {},
			textureFeatures() {},
			dispose() {},
			request( url, limit, decode ) {
				jobs.set(
					++id,
					decode === "effects" ?
						{ kind: "effects", catalog: {} } :
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
		play,
		createPresentationRandom( 1 )
	);
}
test("challenging roar III glows brighter for one second, then releases a 30 m shockwave", () => {
	const ROAR_AURA = "/assets/effects/challenging-roar-aura";
	const ROAR_SHOCKWAVE = "/assets/effects/challenging-roar-shockwave";
	const effects = roarEffects();
	const bear = {
			gid: 7,
			refObjId: 7,
			name: "Bear",
			kind: "cos",
			regionId: 0x8001,
			x: 0,
			y: 0,
			z: 0,
			heading: 0,
			emote: { action: 18, revision: 1, atMs: 0 }
		},
		near = { gid: 8, refObjId: 8, name: "Near", kind: "monster", regionId: 0x8001, x: 240, y: 0, z: 0, heading: 0 },
		far = { gid: 9, refObjId: 9, name: "Far", kind: "monster", regionId: 0x8001, x: 400, y: 0, z: 0, heading: 0 },
		gameplay = { casts: [] };
	const step = now => effects.step( [ bear, near, far ], gameplay, now, () => true, () => 1, [] );
	const aura = actors => actors.find( actor => actor.model === ROAR_AURA && actor.attachment?.root );
	const waves = actors => actors.filter( actor => actor.model === ROAR_SHOCKWAVE );
	// The roar clock starts on the first frame that sees the emote.
	step( 0 );
	let previousLight = 0, previousOpacity = 0, previousScale = 0;
	for ( const now of [ 0.2, 0.5, 0.8, 0.95 ] ) {
		const actors = step( now );
		const glow = defined( aura( actors ) );
		const light = defined( effects.appearance( bear.gid ).pointLight );
		assert.ok( light.diffuse[0] > previousLight, `light brightens at ${now}s` );
		assert.ok( glow.opacity > previousOpacity, `aura brightens at ${now}s` );
		assert.ok( glow.scale > previousScale, `aura swells at ${now}s` );
		assert.equal( waves( actors ).length, 0, "no shockwave before one second" );
		previousLight = light.diffuse[0];
		previousOpacity = glow.opacity;
		previousScale = glow.scale;
	}
	effects.takeCameraScripts();
	// Release at one second: the camera shakes once and the rings start.
	assert.ok( waves( step( 1.0 ) ).length > 0 );
	const shake = effects.takeCameraScripts();
	assert.equal( shake.length, 1 );
	const camera = createCameraScripts( createPresentationRandom( 1 ) );
	assert.doesNotThrow( () => camera.step( 1000, shake ), "roar shake must satisfy the camera event contract" );
	camera.dispose();
	// Near the end of its expansion the ring approaches 300 units (30 m).
	const late = waves( step( 1.88 ) );
	const widest = Math.max( ...late.map( actor => actor.scale ) );
	assert.ok( widest > 290 && widest <= 300, `primary ring reaches 30 m (${widest})` );
	// Visuals do not guess which monsters the server actually taunted.
	assert.ok( step( 1.9 ).every( actor => actor.model === ROAR_AURA || actor.model === ROAR_SHOCKWAVE ) );
	// The sequence ends and leaves the bear's appearance untouched.
	assert.equal( step( 2.4 ).filter( actor => actor.model === ROAR_SHOCKWAVE ).length, 0 );
	assert.equal( effects.appearance( bear.gid ).pointLight, undefined );
	assert.equal( effects.appearance( bear.gid ).scale, 1 );
	assert.equal( step( 5 ).length, 0, "the persistent emote must not replay the finished roar" );
	assert.equal( effects.takeCameraScripts().length, 0, "finished roars must not shake the camera again" );
	assert.equal( effects.error(), null );
	effects.dispose();
});

test("iron hide draws a 5 s barrier dome on bear and players, then detonates an 8 m burst on tier III expiry", () => {
	const SHIELD_DOME = "/assets/effects/iron-hide-shield";
	const SHOCKWAVE = "/assets/effects/challenging-roar-shockwave";
	const effects = roarEffects();
	const bear = {
			gid: 10,
			refObjId: 10,
			name: "Bear",
			kind: "cos",
			regionId: 0x8001,
			x: 0,
			y: 0,
			z: 0,
			heading: 0,
			shield: { amount: 400, maxHp: 1000, atMs: 0, untilMs: 5000, revision: 1, burst: false }
		},
		owner = {
			gid: 11,
			refObjId: 11,
			name: "Owner",
			kind: "local-player",
			regionId: 0x8001,
			x: 20,
			y: 0,
			z: 0,
			heading: 0,
			shield: { amount: 600, maxHp: 1200, atMs: 0, untilMs: 5000, revision: 1, burst: false }
		},
		gameplay = {
			casts: [],
			localGid: 11,
			pose: {
				...owner,
				angle: 0,
				shield: { amount: 0, maxHp: 1200, atMs: -1000, untilMs: -1000, revision: 99, burst: false }
			}
		};
	const step = now => effects.step( [ bear, owner ], gameplay, now, () => true, () => 1, [] );
	const activeActors = step( 0.2 );
	assert.equal( activeActors.filter( a => a.model === SHIELD_DOME && a.pose.x === bear.x ).length, 2 );
	assert.equal( activeActors.filter( a => a.model === SHIELD_DOME && a.pose.x === owner.x ).length, 2 );
	assert.ok(
		activeActors.every( actor => actor.attachment === undefined ),
		"world-size wards must not inherit character root transforms"
	);
	assert.ok( effects.appearance( owner.gid ).pointLight );
	// Periodic 500 ms server updates increment revision without resetting the barrier animation clock.
	owner.shield = { amount: 500, maxHp: 1200, atMs: 500, untilMs: 5000, revision: 2, burst: false };
	const midActors = step( 0.7 ).filter( a => a.model === SHIELD_DOME && a.pose.x === owner.x );
	assert.equal( midActors.length, 2 );
	assert.ok( Math.abs( midActors[0].time - 0.5 ) < 1e-6, "periodic shield sync must not reset animation age" );
	gameplay.pose.x = 80;
	const moved = step( 1.0 ).filter( a => a.model === SHIELD_DOME );
	assert.equal(
		moved.filter( a => a.pose.x === 80 ).length,
		2,
		"owner ward follows live movement independently of bear and wire pose"
	);
	assert.equal( moved.filter( a => a.pose.x === 0 ).length, 2 );
	gameplay.pose.x = 20;
	// At t = 5.0 s, Tier III shield expires with remaining absorption and bursts (8 m = 80 units).
	bear.shield = { amount: 0, maxHp: 1000, atMs: 5000, untilMs: 5000, revision: 2, burst: true };
	owner.shield = { amount: 0, maxHp: 1200, atMs: 5000, untilMs: 5000, revision: 3, burst: true };
	const burstStart = step( 5.0 );
	assert.ok( burstStart.some( a => a.model === SHOCKWAVE && a.pose.x === owner.x ) );
	assert.ok(
		burstStart.some( a => a.model === SHOCKWAVE && a.pose.x === bear.x ),
		"bear explosion is anchored to its own position"
	);
	assert.ok( effects.appearance( owner.gid ).pointLight );
	const shieldShake = effects.takeCameraScripts();
	assert.equal( shieldShake.length, 2 );
	const shieldCamera = createCameraScripts( createPresentationRandom( 1 ) );
	assert.doesNotThrow( () => shieldCamera.step( 5000, shieldShake ) );
	shieldCamera.dispose();
	assert.ok(
		activeActors.every( actor => (actor.opacity ?? 1) <= 0.20 ),
		"barriers must not obscure their characters"
	);
	const burstLate = step( 5.72 ).filter( a => a.model === SHOCKWAVE && a.pose.x === owner.x );
	const maxScale = Math.max( ...burstLate.map( a => a.scale ) );
	assert.ok( maxScale > 75 && maxScale <= 80, `tier III shield burst reaches 8 m (${maxScale})` );
	assert.equal( step( 6.0 ).length, 0 );
	owner.shield = { amount: 600, maxHp: 1200, atMs: 8000, untilMs: 13000, revision: 4, burst: false };
	assert.equal(
		step( 8.0 ).filter( a => a.model === SHIELD_DOME ).length,
		2,
		"a new cast rearms after a finished burst"
	);
	owner.shield = { amount: 0, maxHp: 1200, atMs: 13000, untilMs: 13000, revision: 5, burst: true };
	assert.ok(
		step( 13.0 ).some( a => a.model === SHOCKWAVE && a.pose.x === owner.x ),
		"subsequent casts burst again"
	);
	assert.equal( step( 14.0 ).length, 0 );
	effects.dispose();
});

test("pet skill audio is positional, once per cast and synchronized to the roar release", () => {
	const played = [], effects = roarEffects( event => played.push( event ) );
	const bear = {
		gid: 7,
		refObjId: 7,
		name: "Bear",
		kind: "cos",
		regionId: 0x8001,
		x: 10,
		y: 5,
		z: 20,
		heading: 0,
		emote: { action: 18, revision: 1, atMs: 0 },
		shield: /** @type {import("../../src/engine/contracts/world.ts").EntityState["shield"]} */ (undefined)
	};
	const step = now => effects.step( [ bear ], { casts: [] }, now, () => true, () => 1, [] );
	step( 0 );
	step( .9 );
	assert.equal( played.length, 0 );
	step( 1 );
	step( 1.1 );
	assert.equal( played.length, 1 );
	assert.match( played[0].path, /cos_bear2_shout1.wav$/ );
	assert.equal( played[0].x, 1930 );
	assert.equal( played[0].y, 5 );
	assert.equal( played[0].z, 245780 );
	bear.shield = { amount: 400, maxHp: 1000, atMs: 2000, untilMs: 7000, revision: 1, burst: false };
	step( 2 );
	step( 2.1 );
	bear.shield = { ...bear.shield, amount: 300, atMs: 2500, revision: 2 };
	step( 2.5 );
	step( 7.1 );
	step( 7.2 );
	assert.equal( played.length, 2, "partial absorption, expiry and stale snapshots must not replay the ward sound" );
	assert.match( played[1].path, /usk_warrior_guard_phyfence_a.wav$/ );
	assert.notEqual( played[0].id, played[1].id );
	bear.shield = { amount: 400, maxHp: 1000, atMs: 8000, untilMs: 13000, revision: 3, burst: false };
	step( 8 );
	assert.equal( played.length, 3, "the next accepted shield cast sounds again" );
	bear.shield = { amount: 0, maxHp: 1000, atMs: 13000, untilMs: 13000, revision: 4, burst: true };
	step( 13 );
	step( 13.1 );
	step( 14 );
	assert.equal( played.length, 4, "expiry plays one explosion sound, never one per frame" );
	assert.match( played[3].path, /usk_warlock_blood_explosion_bomb.wav$/ );
	assert.equal( played[3].x, 1930 );
	assert.ok( played[3].gain < played[1].gain, "simultaneous recipient bursts use a softer mix" );
	effects.dispose();
});

test("retaliation III draws one faint 8m ground ripple without sound or camera shake", () => {
	const played = [], effects = roarEffects( event => played.push( event ) );
	const owner = {
		gid: 11,
		refObjId: 11,
		name: "Owner",
		kind: "local-player",
		regionId: 0x8001,
		x: 20,
		y: 0,
		z: 30,
		heading: 0,
		petCounterPulse: { revision: 1, atMs: 0 }
	};
	const step = now => effects.step( [ owner ], null, now, () => true, () => 1, [] );
	const ring = defined( step( 0 )[0] );
	assert.equal( ring.model, "/assets/effects/pet-counter-ripple" );
	assert.equal( ring.opacity, 0 );
	const peak = defined( step( .275 )[0] );
	assert.ok( defined( peak.opacity ) > 0 && defined( peak.opacity ) <= .22 );
	owner.x = 100;
	const late = defined( step( .52 )[0] );
	assert.equal( late.gid, ring.gid );
	assert.equal( late.pose.x, 20, "pulse stays at its release position" );
	assert.ok( late.scale > 79 && late.scale <= 80 );
	assert.ok( defined( late.opacity ) < defined( peak.opacity ) );
	assert.equal( step( .6 ).length, 0 );
	assert.equal( step( 2 ).length, 0, "finished pulses never replay" );
	assert.equal( played.length, 0 );
	assert.equal( effects.takeCameraScripts().length, 0 );
	owner.petCounterPulse = { revision: 2, atMs: 2100 };
	assert.notEqual( defined( step( 2.1 )[0] ).gid, ring.gid );
	effects.dispose();
});

/*
================
Guardian's Bond visual and audio lifecycle
================
*/
test("Guardian recovery follows a stationary recipient immediately and never replays sound", () => {
	const played = [], effects = roarEffects( event => played.push( event ) );
	const owner = {
		gid: 11,
		refObjId: 11,
		name: "Owner",
		kind: "local-player",
		regionId: 0x8001,
		x: 20,
		y: 0,
		z: 30,
		heading: 0,
		petBondPulse: { revision: 1, atMs: 0, bear: 12, kind: 2 }
	};
	const step = now => effects.step( [ owner ], null, now, () => true, () => 1, [] );
	const first = defined( step( 0 )[0] );
	assert.equal( first.model, "/assets/effects/iron-hide-shield" );
	const peak = defined( step( .75 )[0] );
	assert.ok( defined( peak.opacity ) > 0 );
	assert.equal( peak.pose.x, 20 );
	assert.ok( peak.pose.y > 0 );
	assert.match( played[0].path, /usk_cleric_rebirth_target_a.wav$/ );
	step( 1 );
	step( 1.6 );
	step( 2 );
	assert.equal( played.length, 1 );
	assert.equal( step( 2.1 ).length, 0 );
	owner.x = 25;
	owner.petBondPulse = { revision: 2, atMs: 2200, bear: 12, kind: 1 };
	assert.equal( defined( step( 2.2 )[0] ).pose.x, 25 );
	assert.match( played[1].path, /usk_cleric_heal_target_a.wav$/ );
	owner.petBondPulse = { revision: 3, atMs: 2300, bear: 12, kind: 1 };
	step( 2.3 );
	assert.equal( played.length, 2, "rapid damage ticks do not spam the healing sound" );
	assert.equal( effects.takeCameraScripts().length, 0 );
	effects.dispose();
});

/*
================
Unbroken Spirit presentation
================
*/
test("Resolve ring remains subtle, follows a stationary bear and expires", () => {
	const effects = roarEffects();
	const bear = {
		gid: 77,
		kind: "cos",
		regionId: 257,
		x: 10,
		y: 2,
		z: 30,
		heading: 0,
		petResolve: { stacks: 10, tier: 3, atMs: 0, untilMs: 15000 }
	};
	const entities = [ bear ];
	const ring = effects.step( entities, { casts: [] }, 1, () => true, () => 1, [] ).find( actor =>
		actor.model === "/assets/effects/pet-counter-ripple"
	);
	assert.ok( ring );
	assert.equal( ring.pose.x, 10 );
	assert.ok( ring.opacity > 0 && ring.opacity < .11 );
	assert.equal( ring.pickable, false );
	assert.equal(
		effects.step( entities, { casts: [] }, 15, () => true, () => 1, [] ).filter( actor =>
			actor.model === "/assets/effects/pet-counter-ripple"
		).length,
		0
	);
	effects.dispose();
});

/*
================
Stormbrood wind survives source retirement
================
*/
test("crow wind bursts retain every fixed origin after clone despawn and reset cleanly", () => {
	const effects = roarEffects( () => {} );
	for ( let i = 0; i < 10; i++ ) effects.crowEffect( { regionId: 257, x: i * 10, y: 0, z: 30 } );
	const step = now => effects.step( [], null, now, () => true, () => 1, [] );
	step( 0 );
	const peak = step( .5 ).filter( a => a.model === "/assets/effects/crow-wind-burst" );
	assert.equal( peak.length, 30 );
	assert.equal( new Set( peak.map( a => a.pose.x ) ).size, 10 );
	assert.ok( peak.every( a => defined( a.opacity ) > 0 && a.scale <= 80 && !a.pickable ) );
	assert.equal( step( 1.3 ).length, 0 );
	assert.equal( step( 2 ).length, 0, "no replay" );
	effects.crowEffect( { regionId: 257, x: 0, y: 0, z: 0 } );
	effects.reset();
	assert.equal( step( 3 ).length, 0 );
	effects.dispose();
});

/*
================
Wind ribbon resources
================
*/
test("crow wind uses a rising ribbon with transparent tips and edges", async () => {
	const { createProceduralEffect } = await import(
		"../../src/engine/runtime/characters/resources/procedural-effects.ts"
	);
	const gust = createProceduralEffect( "/assets/effects/crow-wind-burst" );
	assert.ok( gust );
	const texture = gust.images[0];
	assert.equal( texture.width, 64 );
	const pixels = texture.levels[0];
	for ( let y = 0; y < 16; y++ ) {
		assert.equal( pixels[(y * 64) * 4 + 3], 0 );
		assert.equal( pixels[(y * 64 + 63) * 4 + 3], 0 );
	}
	assert.ok( pixels[(8 * 64 + 32) * 4 + 3] > 200 );
	const positions = gust.model.primitives[0].geometry.positions;
	assert.ok( positions[positions.length - 2] > positions[1], "spiral rises above the ground" );
	assert.equal( defined( createProceduralEffect( "/assets/effects/pet-counter-ripple" ) ).images[0].width, 1 );
});

/*
================
Crow skill cosmetics
================
*/
test("every crow cue and book draws bounded distinct feather cosmetics without gameplay", async () => {
	const { createProceduralEffect } = await import(
		"../../src/engine/runtime/characters/resources/procedural-effects.ts"
	);
	assert.ok( createProceduralEffect( "/assets/effects/crow-feather" ) );
	for ( let cue = 1; cue <= 8; cue++ ) {
		for ( let book = 1; book <= 3; book++ ) {
			const effects = roarEffects();
			effects.crowEffect( { regionId: 257, x: 11, y: 0, z: 22 }, cue, book );
			effects.step( [], null, 0, () => true, () => 1, [] );
			const actors = effects.step( [], null, .6, () => true, () => 1, [] );
			assert.equal( actors.length, (3 + book) * (cue === 1 || cue === 4 ? 2 : 1) );
			assert.ok(
				actors.every( a =>
					a.model ===
						(cue === 1 ?
							"/assets/effects/crow-heal-wisp" :
							cue === 4 ?
							"/assets/effects/crow-mana-wisp" :
							"/assets/effects/crow-feather") && defined( a.opacity ) > 0 && !a.pickable
				)
			);
			assert.equal( effects.step( [], null, 1.3, () => true, () => 1, [] ).length, 0 );
			effects.dispose();
		}
	}
});

/*
================
Crow recovery effects
================
*/
test("crow recovery uses red rising glow and blue inward wisps, with soft transparent edges", async () => {
	const { createProceduralEffect } = await import(
		"../../src/engine/runtime/characters/resources/procedural-effects.ts"
	);
	const resource = defined( createProceduralEffect( "/assets/effects/crow-feather" ) );
	const image = resource.images[0];
	assert.equal( image.width, 64 );
	const pixels = image.levels[0];
	for ( let y = 0; y < 64; y++ ) {
		assert.equal( pixels[y * 64 * 4 + 3], 0 );
		assert.equal( pixels[(y * 64 + 63) * 4 + 3], 0 );
	}
	const paths = [];
	for ( const cue of [ 1, 4 ] ) {
		const effects = roarEffects();
		effects.crowEffect( { regionId: 257, x: 0, y: 0, z: 0 }, cue, 3 );
		effects.step( [], null, 0, () => true, () => 1, [] );
		const actors = effects.step( [], null, .6, () => true, () => 1, [] );
		assert.equal( actors.length, 12 );
		const colored = defined( createProceduralEffect( actors[0].model ) ).images[0].levels[0];
		const at = (32 * 64 + 32) * 4;
		assert.ok(
			cue === 1 ? colored[at + 2] > colored[at] * 2 : colored[at] > colored[at + 2] * 2,
			"unlit BGRA texture carries the recovery color"
		);
		assert.ok( actors[1].scale < actors[0].scale / 2 );
		assert.equal(
			defined( createProceduralEffect( actors[0].model ) ).model.primitives[0].geometry.material.blendPair
				.destination,
			6
		);
		assert.ok( actors.every( a => a.scale < 2 ), "recovery no longer draws large flat feathers" );
		assert.equal(
			defined( createProceduralEffect( actors[0].model ) ).model.primitives[0].geometry.material.fadeAlphaOnly,
			false,
			"soft texture alpha survives instance fading"
		);
		assert.ok( actors[1].opacity < actors[0].opacity );
		paths.push( actors[0].pose );
		assert.equal( effects.step( [], null, 1.3, () => true, () => 1, [] ).length, 0 );
		effects.dispose();
	}
	assert.notDeepEqual( paths[0], paths[1], "healing rises beside the body; mana draws inward at torso height" );
});
