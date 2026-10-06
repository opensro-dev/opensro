/*
===========================================================================

program.ts - native effect programs (EFP) as character models

Decodes the published effect catalog and compiles one effect's emitter tree
into a CharacterModel: nodes, animated channels, mesh and particle
primitives with their native material state (the resource's D3D blend pair
and stage-0 ops, CEFEffect_Render B153A0), within the character budgets.

===========================================================================
*/
import { validTextureStage } from "@/engine/foundation/rendering/texture-stage";
import { validBlend } from "@/engine/foundation/rendering/blend-state";
import { particleCommandFrames } from "@/engine/foundation/animation/particle-command-frames";
import type { ParticleEmitter } from "@/engine/foundation/animation/particle-graph";
import { particleProgram } from "@/engine/foundation/animation/particle-program";
import { particleRotation } from "@/engine/foundation/animation/particle-rotation";
import { particleBirthFrames } from "@/engine/foundation/animation/particle-emission";
import {
	CHARACTER_PARTICLE_BIRTHS,
	CHARACTER_MODEL_BYTES,
	CHARACTER_PRIMITIVES,
	CHARACTER_IMAGES
} from "@/engine/foundation/animation/character-budget";
import type { CharacterModel, CharacterNode, CharacterPrimitive, CharacterChannel } from "@/engine/contracts/character";
import { identity } from "@/engine/foundation/rendering/world-math";
import { multiply } from "@/engine/foundation/math/pose-math";
type Operation = {
	name: string;
	flags?: number;
	byte1?: number;
	start?: number;
	end?: number;
	step?: number;
	parameter?: {
		kind: string;
		value: unknown;
		left?: unknown;
		right?: unknown;
	};
};
type Node = {
	byte0?: number;
	byte1?: number;
	byte2?: number;
	byte3?: number;
	int0?: number;
	int1?: number;
	int2?: number;
	int3?: number;
	name: string;
	children: Node[];
	globalData: {
		totalFrames: number;
		parameters?: { kind: string; value: unknown; }[];
	};
	updateProgram?: Operation[];
	trailingProgram?: Operation[];
	preProgram: Operation[];
	postEmitterProgram: Operation[];
	emitterProgram: Operation[];
	renderProgram: Operation[];
	viewCommand: Operation;
	renderCommand: Operation;
	lifeCommand: Operation;
	resource: {
		srcBlend: number;
		dstBlend: number;
		backFaceType: number;
		// B153A0 stage 0: src* is COLORARG1/2/COLOROP, dst* is ALPHAARG1/2/ALPHAOP.
		srcTextureArg1: number;
		srcTextureArg2: number;
		srcTextureOp: number;
		dstTextureArg1: number;
		dstTextureArg2: number;
		dstTextureOp: number;
		meshes: {
			path: string;
			textures: string[];
		}[];
	};
};
type Catalog = {
	nativeUnavailable?: { archiveSha256: string; effects: string[]; };
	framesPerSecond: number;
	effects: Record<string, {
		scale: number;
		root: Node;
	}>;
	meshes: Record<string, {
		positions: number[];
		normals: number[];
		uvs: number[];
		indices: number[];
	}>;
	textures: Record<string, string>;
};
/*
================
normalize

A catalog key: forward slashes, lower case.
================
*/
const normalize = ( value: string ) => value.replaceAll( "\\", "/" ).toLowerCase();
// Retail af7210 reads these four XYZ/UV vertices at cdd308 and indices at
// cdd2f8. RenderPlate owns a unit quad; it does not reference a BMS mesh.
const plate = {
	positions: [ -0.5, 0.5, 0, 0.5, 0.5, 0, 0.5, -0.5, 0, -0.5, -0.5, 0 ],
	normals: [ 0, 0, -1, 0, 0, -1, 0, 0, -1, 0, 0, -1 ],
	uvs: [ 0, 0, 1, 0, 1, 1, 0, 1 ],
	indices: [ 0, 1, 2, 0, 2, 3 ]
};
/*
================
matToQuat

The unit quaternion of a column-major rotation matrix.
================
*/
function matToQuat( m: readonly number[] ): number[] {
	const m00 = m[0]!, m01 = m[4]!, m02 = m[8]!;
	const m10 = m[1]!, m11 = m[5]!, m12 = m[9]!;
	const m20 = m[2]!, m21 = m[6]!, m22 = m[10]!;
	const tr = m00 + m11 + m22;
	let x = 0, y = 0, z = 0, w = 1;
	if ( tr > 0 ) {
		const s = 0.5 / Math.sqrt( tr + 1.0 );
		w = 0.25 / s;
		x = (m21 - m12) * s;
		y = (m02 - m20) * s;
		z = (m10 - m01) * s;
	} else if ( m00 > m11 && m00 > m22 ) {
		const s = 2.0 * Math.sqrt( Math.max( 1e-12, 1.0 + m00 - m11 - m22 ) );
		w = (m21 - m12) / s;
		x = 0.25 * s;
		y = (m01 + m10) / s;
		z = (m02 + m20) / s;
	} else if ( m11 > m22 ) {
		const s = 2.0 * Math.sqrt( Math.max( 1e-12, 1.0 + m11 - m00 - m22 ) );
		w = (m02 - m20) / s;
		x = (m01 + m10) / s;
		y = 0.25 * s;
		z = (m12 + m21) / s;
	} else {
		const s = 2.0 * Math.sqrt( Math.max( 1e-12, 1.0 + m22 - m00 - m11 ) );
		w = (m10 - m01) / s;
		x = (m02 + m20) / s;
		y = (m12 + m21) / s;
		z = 0.25 * s;
	}
	const len = Math.hypot( x, y, z, w );
	return [ x / len, y / len, z / len, w / len ];
}
/*
================
createEffectPrograms

The decoder, holding the parsed catalog between decodes.
================
*/
export function createEffectPrograms() {
	let catalog: Catalog | null = null;
	return {
		/*
		================
		decode

		The model of the effect at path, and the images it draws.
		================
		*/
		decode( bytes: Uint8Array, path: string ): {
			model: CharacterModel;
			imagePaths: string[];
		} {
			catalog ??= JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( bytes ) ) as Catalog;
			if ( catalog.framesPerSecond !== 20 ) {
				throw new Error( "Unsupported effect time base" );
			}
			const effect = catalog.effects[normalize( path )];
			const unavailable = catalog.nativeUnavailable;
			if (
				unavailable &&
				(!/^[a-f0-9]{64}$/.test( unavailable.archiveSha256 ) || !Array.isArray( unavailable.effects ) ||
					unavailable.effects.some( p =>
						typeof p !== "string" || !p.endsWith( ".efp" ) || p.includes( ".." ) || !!catalog!.effects[p]
					))
			) throw Error( "Invalid native unavailable EFP evidence" );
			// B1F270 resets before loading; AFF730 retains that empty stored
			// object on failure. A known archive miss has no visual children.
			if ( !effect && unavailable?.effects.includes( normalize( path ) ) ) {
				return { model: { nodes: [], clips: [], images: [], primitives: [] }, imagePaths: [] };
			}
			if ( !effect ) {
				throw new Error( `Missing effect program ${path}` );
			}
			const effectScale = effect.scale;
			if ( !Number.isFinite( effectScale ) || effectScale <= 0 ) throw new Error( "Invalid effect scale" );
			const nodes: CharacterNode[] = [],
				primitives: CharacterPrimitive[] = [],
				channels: CharacterChannel[] = [],
				imagePaths: string[] = [];
			const particleGraph: ParticleEmitter[] = [];
			let duration = 0, expanded = 0, primitiveCount = 0;
			/*
			================
			reserve

			Charges expanded bytes against the model budget.
			================
			*/
			function reserve( bytes: number ) {
				expanded += bytes;
				if ( !Number.isSafeInteger( bytes ) || bytes < 0 || expanded > CHARACTER_MODEL_BYTES ) {
					throw new Error( "Effect expansion exceeds budget" );
				}
			}
			/*
			================
			walk

			Compiles node and its children under parent; compile false only
			counts what the node would expand to.
			================
			*/
			function walk(
				node: Node,
				parent: number,
				compile: boolean,
				parentBirths: readonly number[] = [ 0 ],
				parentFrames = node.globalData.totalFrames,
				parentEmitter = -1
			) {
				if ( nodes.length >= 128 ) {
					throw new Error( "Effect node budget exceeded" );
				}
				const isLeafAnchor = node.emitterProgram.length === 0 && node.lifeCommand.name === "NeverExtinct" &&
					node.renderCommand.name === "RenderNone" && node.children.length === 0;
				if (
					node.updateProgram?.some( op => op.name !== "ProgramUpdate" ) || node.trailingProgram?.length ||
					node.preProgram.length || node.postEmitterProgram.length ||
					(!isLeafAnchor && ![ "NormalTimeExtinct", "NormalTimeLoop" ].includes( node.lifeCommand.name )) ||
					![ "ViewNone", "ViewBillboard", "ViewYBillboard", "ViewVBillboard" ].includes(
						node.viewCommand.name
					)
				) {
					throw new Error( `Unsupported effect lifecycle/view ${node.name}` );
				}
				if (
					!isLeafAnchor && (node.emitterProgram.length !== 1 || node.emitterProgram[0]!.name !== "StaticEmit")
				) {
					throw new Error( `Unsupported effect emitter ${node.name}` );
				}
				let births: number[] = [], birthParents: number[] = [], general = false;
				let emission: import("@/engine/foundation/animation/particle-emission").ParticleEmitter | undefined;
				if ( !isLeafAnchor ) {
					const emit = node.emitterProgram[0]!.parameter?.value as {
						min: number;
						max: number;
						minParticles: number;
						burstRate: number;
						spawnRate: number;
					} | undefined;
					if ( !emit ) throw new Error( "Missing particle emitter" );
					emission = {
						start: emit.min,
						duration: emit.max,
						period: emit.burstRate,
						limit: emit.minParticles,
						rate: emit.spawnRate
					};
					const schedule = particleBirthFrames( emission, parentFrames );
					if ( parentBirths.length * schedule.length > CHARACTER_PARTICLE_BIRTHS ) {
						throw new Error( "Effect population exceeds budget" );
					}
					births = parentBirths.flatMap( ( parent, b ) =>
						schedule.map( frame => {
							birthParents.push( b );
							return parent + frame;
						} )
					);
					general = births.length !== 1 || births[0] !== 0;
				}
				const render = node.renderCommand.name;
				if (
					![ "RenderMesh", "RenderPlate", "RenderNone", "RenderLinkPipe", "RenderLinkDPipe", "RenderLinkObj" ]
						.includes( render )
				) {
					throw new Error( `Unsupported effect render command ${render}` );
				}
				const allowed = new Set( [
					"SetGraphDiffuse",
					"SetGraphScale",
					"TextureSlide",
					"SetPosition",
					"SetSpherePos",
					"SetConeVel",
					"SetGraphRandomScale",
					"SetShapeRot",
					"SetShapeRotVel",
					"SetRotation",
					"SetRotationMat",
					"Force",
					"SetVelocity",
					"SetBANRot",
					"SetBANPos",
					"Attraction",
					"ConeForce",
					"SetRVelocity",
					"SetConePos"
				] );
				for ( const op of node.renderProgram ) {
					if ( !allowed.has( op.name ) ) {
						throw new Error( `Unsupported effect operation ${op.name}` );
					}
				}
				const program = particleProgram(
					node.renderProgram,
					node.globalData.parameters,
					node.globalData.totalFrames
				);
				const position = node.renderProgram.find( op => op.name === "SetPosition" )?.parameter?.value ??
					[ 0, 0, 0 ];
				if (
					!Array.isArray( position ) || position.length !== 3 ||
					position.some( n => typeof n !== "number" || !Number.isFinite( n ) )
				) {
					throw new Error( "Invalid effect position" );
				}
				const rotations = node.renderProgram.filter( op =>
					op.name === "SetRotation" || op.name === "SetRotationMat"
				);
				let combinedMatrix: number[] | null = null;
				if ( rotations.length ) {
					if ( nodes.length >= 127 ) throw new Error( "Effect node budget exceeded" );
					for ( const op of rotations ) {
						if ( op.start === 0 && op.step === 0 && op.end === 0 ) {
							// Native sub_b0c290 returns count=0 (period < 1e-6); sub_b143c0 skips scheduling.
							continue;
						}
						const matrix = particleRotation( op.parameter );
						// AFE1E0 emits the Matrix command, using RotVector's converted
						// matrix (AFD030). Keep this static transform above animated
						// scale channels; a matrix on their node would suppress them.
						const validSchedule = op.start === 0 && (
							((op.flags === 0 || op.flags === 1) && op.step === 0 && op.end === 1) ||
							(op.flags === 2 && op.step === 0 && op.end === 1) ||
							(op.flags === 3 && (op.step === 0 || op.step === 1) && op.end === 1)
						);
						if (
							!validSchedule || !Array.isArray( matrix ) || matrix.length !== 16 || matrix.some( v =>
								typeof v !== "number" || !Number.isFinite( v )
							) || matrix[3] !== 0 || matrix[7] !== 0 || matrix[11] !== 0 || matrix[15] !== 1
						) {
							throw new Error( "Unsupported particle rotation assignment" );
						}
						// AF4410: Opcode execution uses last-wins overwrite semantics
						combinedMatrix = [ ...matrix ];
					}
					if ( combinedMatrix ) {
						const transform = [ ...combinedMatrix ];
						transform[12] = position[0]!;
						transform[13] = position[1]!;
						transform[14] = position[2]!;
						const rotationParent = nodes.length;
						nodes.push( {
							name: node.name + ":rotation",
							parent,
							matrix: transform,
							translation: [ 0, 0, 0 ],
							rotation: [ 0, 0, 0, 1 ],
							scale: [ 1, 1, 1 ]
						} );
						parent = rotationParent;
					}
				}
				const index = nodes.length;
				nodes.push( {
					name: node.name,
					parent,
					translation: combinedMatrix !== null ? [ 0, 0, 0 ] : position,
					rotation: [ 0, 0, 0, 1 ],
					scale: [ 1, 1, 1 ]
				} );
				const life = node.globalData.totalFrames / catalog!.framesPerSecond;
				if (
					!isLeafAnchor &&
					(!Number.isInteger( node.globalData.totalFrames ) || !Number.isFinite( life ) || life <= 0 ||
						life > 60)
				) {
					throw new Error( "Effect lifetime exceeds budget" );
				}
				if ( !isLeafAnchor ) {
					duration = Math.max( duration, life + (births.at( -1 ) ?? 0) / catalog!.framesPerSecond );
				}
				const table = ( name: string, width: number, selected?: Operation ): number[][] => {
					const value = (selected ?? node.renderProgram.find( op => op.name === name ))?.parameter?.value;
					if ( value === undefined ) {
						return [];
					}
					if (
						!Array.isArray( value ) || value.length > 4096 ||
						value.some( row =>
							!Array.isArray( row ) || row.length !== width ||
							row.some( n => typeof n !== "number" || !Number.isFinite( n ) )
						)
					) {
						throw new Error( "Invalid effect frame table" );
					}
					return value;
				};
				const scales = table( "SetGraphScale", 3 ),
					colors = table( "SetGraphDiffuse", 4 ),
					windows = table( "TextureSlide", 4 ),
					banPositions = table( "SetBANPos", 3 ),
					banRotations = table( "SetBANRot", 16 );
				const emitterIndex = particleGraph.length;
				const commands = node.renderProgram.flatMap<NonNullable<ParticleEmitter["commands"]>[number]>( op => {
					if ( isLeafAnchor ) return [];
					const flags = op.flags ?? 0;
					if ( op.name === "SetBANRot" || op.name === "SetRotation" || op.name === "SetRotationMat" ) {
						const frames = particleCommandFrames( op, node.globalData.totalFrames );
						if ( !frames.length ) return [];
						if ( flags < 0 || flags > 3 ) throw Error( "Unsupported effect orientation mode" );
						const rotations = op.name === "SetBANRot" ?
							table( op.name, 16, op ) :
							[ Array.from( particleRotation( op.parameter! ) ) ];
						return [ { name: op.name, frames, flags, rotations, program: {} } ];
					}
					if ( op.name === "SetBANPos" ) {
						const frames = particleCommandFrames( op, node.globalData.totalFrames );
						if ( flags < 0 || flags > 11 ) throw Error( "Unsupported effect position mode" );
						return [ { name: op.name, frames, flags, positions: table( op.name, 3, op ), program: {} } ];
					}
					const command = particleProgram( [ op ], node.globalData.parameters, node.globalData.totalFrames );
					return command ?
						[ {
							name: op.name,
							frames: particleCommandFrames( op, node.globalData.totalFrames ),
							program: command
						} ] :
						[];
				} );
				particleGraph.push( {
					emission,
					loop: node.lifeCommand.name === "NormalTimeLoop",
					commands,
					parent: parentEmitter,
					parents: birthParents,
					births,
					frames: node.globalData.totalFrames,
					program,
					matrix: combinedMatrix ?? undefined,
					localMotion: !!node.byte2,
					shapeMotion: !!node.byte3,
					keepMatrix: !!node.byte0,
					keepOrigin: !!node.byte1,
					positionDepth: node.int0 ?? 0,
					matrixDepth: node.int1 ?? 0,
					velocityDepth: node.int2 ?? 0,
					followDepth: node.int3 ?? 0,
					scales,
					positions: banPositions,
					rotations: banRotations
				} );
				if ( !compile ) reserve( (scales.length + banPositions.length + banRotations.length) * 16 );
				if ( compile && scales.length ) {
					const times = Float32Array.from( scales.map( ( _, i ) => i / catalog!.framesPerSecond ) );
					channels.push( {
						node: index,
						path: "scale",
						interpolation: "STEP",
						times,
						values: Float32Array.from( scales.flat() )
					} );
				}
				if ( compile && banPositions.length ) {
					const times = Float32Array.from( banPositions.map( ( _, i ) => i / catalog!.framesPerSecond ) );
					channels.push( {
						node: index,
						path: "translation",
						interpolation: "LINEAR",
						times,
						values: Float32Array.from( banPositions.flat() )
					} );
				}
				if ( compile && banRotations.length ) {
					const times = Float32Array.from( banRotations.map( ( _, i ) => i / catalog!.framesPerSecond ) );
					channels.push( {
						node: index,
						path: "rotation",
						interpolation: "LINEAR",
						times,
						values: Float32Array.from( banRotations.flatMap( matToQuat ) )
					} );
				}
				if (
					births.length &&
					[ "RenderMesh", "RenderPlate", "RenderLinkPipe", "RenderLinkDPipe", "RenderLinkObj" ].includes(
						render
					)
				) {
					const resource = node.resource;
					// B153A0 sets SRCBLEND/DESTBLEND from the resource as authored.
					const blendPair = { source: resource.srcBlend, destination: resource.dstBlend };
					if ( !validBlend( blendPair ) ) {
						throw new Error( "Undefined native effect blend" );
					}
					if (
						!validTextureStage( {
							colorOp: resource.srcTextureOp,
							colorArg1: resource.srcTextureArg1,
							colorArg2: resource.srcTextureArg2,
							alphaOp: resource.dstTextureOp,
							alphaArg1: resource.dstTextureArg1,
							alphaArg2: resource.dstTextureArg2
						} )
					) {
						throw new Error( "Undefined native effect texture stage" );
					}
					for ( const mesh of resource.meshes ) {
						const data =
							[ "RenderPlate", "RenderLinkPipe", "RenderLinkDPipe", "RenderLinkObj" ].includes( render ) ?
								plate :
								catalog!.meshes[normalize( mesh.path )];
						if ( !data || mesh.textures.length !== 1 ) {
							throw new Error( "Missing effect mesh/texture" );
						}
						const texture = catalog!.textures[normalize( mesh.textures[0]! ).replace( /\.ddj$/, ".png" )];
						if ( !texture ) {
							throw new Error( "Missing published effect image" );
						}
						let image = imagePaths.indexOf( texture );
						if ( image < 0 ) {
							image = imagePaths.length;
							imagePaths.push( texture );
							if ( imagePaths.length > CHARACTER_IMAGES ) {
								throw new Error( "Effect image budget exceeded" );
							}
						}
						if ( !compile && ++primitiveCount > CHARACTER_PRIMITIVES ) {
							throw new Error( "Effect primitive budget exceeded" );
						}
						const count = data.positions.length / 3;
						if ( count > 65536 || data.indices.length > 393216 ) {
							throw new Error( "Effect geometry exceeds budget" );
						}
						if (
							!Number.isInteger( count ) || !count || data.normals.length !== count * 3 ||
							data.uvs.length !== count * 2 || [ data.positions, data.normals, data.uvs ].some( values =>
								values.some( n =>
									!Number.isFinite( n )
								)
							) || data.indices.some( n => !Number.isInteger( n ) || n < 0 || n >= count )
						) {
							throw new Error( "Invalid effect geometry" );
						}
						if ( count > 65536 || data.indices.length > 393216 ) {
							throw new Error( "Effect geometry exceeds budget" );
						}
						const frameCount = Math.max( 2, node.globalData.totalFrames + 1 );
						if ( !compile ) {
							// Every reference produces its own geometry and material frames.
							reserve( count * 64 + data.indices.length * 4 + 128 + frameCount * 32 );
							continue;
						}
						const weights = new Float32Array( count * 4 );
						for ( let i = 0; i < count; i++ ) {
							weights[i * 4] = 1;
						}
						const frames = Math.max( 2, node.globalData.totalFrames + 1 ),
							frameColors = new Float32Array( frames * 4 ),
							frameWindows = new Float32Array( frames * 4 );
						for ( let i = 0; i < frames; i++ ) {
							const age = i / (frames - 1),
								color = colors[Math.min( i, colors.length - 1 )] ?? [ 255, 255, 255, 255 ];
							for ( let c = 0; c < 4; c++ ) frameColors[i * 4 + c] = color[c]! / 255;
							if ( i === frames - 1 ) {
								frameColors[i * 4 + 3] = 0;
							}
							const uv = windows[Math.min( windows.length - 1, Math.floor( age * windows.length ) )] ??
								[ 0, 0, 1, 1 ];
							frameWindows.set( [ uv[2]!, uv[3]!, uv[0]!, uv[1]! ], i * 4 );
						}
						primitives.push( {
							particleEmitter: emitterIndex,
							ribbon: [ "RenderLinkPipe", "RenderLinkDPipe", "RenderLinkObj" ].includes( render ) ?
								{
									widths: Float32Array.from( scales.map( row => row[0]! ) ),
									fps: 20,
									spline: render === "RenderLinkPipe"
								} :
								undefined,
							particleProgram: program,
							emission: {
								loop: node.lifeCommand.name === "NormalTimeLoop",
								births: births.map( frame => frame / catalog!.framesPerSecond ),
								lifetime: life,
								follow: !general
							},
							billboard: node.viewCommand.name === "ViewBillboard" ?
								"camera" :
								node.viewCommand.name === "ViewYBillboard" ?
								"y" :
								node.viewCommand.name === "ViewVBillboard" ?
								"v" :
								undefined,
							name: node.name,
							node: index,
							joints: [ index ],
							inverseBind: identity(),
							image,
							materialFrames: {
								sampling: "step",
								fps: (frames - 1) / life,
								colors: frameColors,
								windows: frameWindows
							},
							geometry: {
								positions: Float32Array.from( data.positions ),
								indices: Uint32Array.from( data.indices ),
								normals: Float32Array.from( data.normals ),
								uvs: Float32Array.from( data.uvs ),
								joints: new Uint32Array( count * 4 ),
								weights,
								transform: identity(),
								material: {
									color: [ 1, 1, 1, 1 ],
									alphaCutoff: 0,
									blend: true,
									blendPair,
									doubleSided: resource.backFaceType === 1,
									unlit: true,
									textureStage: {
										colorOp: resource.srcTextureOp,
										colorArg1: resource.srcTextureArg1,
										colorArg2: resource.srcTextureArg2,
										alphaOp: resource.dstTextureOp,
										alphaArg1: resource.dstTextureArg1,
										alphaArg2: resource.dstTextureArg2
									}
								}
							}
						} );
					}
				}
				for ( const child of node.children ) {
					walk( child, index, compile, births, node.globalData.totalFrames, emitterIndex );
				}
			}
			// Validate the entire graph and its cumulative expansion before the
			// first typed-array allocation, including later nodes and references.
			const root: CharacterNode = {
				name: "effect-scale",
				parent: -1,
				translation: [ 0, 0, 0 ],
				rotation: [ 0, 0, 0, 1 ],
				scale: [ effectScale, effectScale, effectScale ]
			};
			nodes.push( root );
			walk( effect.root, 0, false );
			particleGraph.length = 0;
			nodes.length = 0;
			imagePaths.length = 0;
			duration = 0;
			nodes.push( root );
			walk( effect.root, 0, true );
			// Completion includes the last possible emission and its descendant
			// tail. An emitter limit is concurrent occupancy, not a total quota.
			const latestBirth: number[] = [];
			for ( let n = 0; n < particleGraph.length; n++ ) {
				const def = particleGraph[n]!, emit = def.emission;
				const parentEnd = def.parent < 0 ? Infinity : particleGraph[def.parent]!.frames;
				const last = emit && emit.period > 0 && emit.rate > 0 && emit.limit > 0 ?
					Math.min( parentEnd, emit.start + emit.duration ) - 1 :
					-1;
				latestBirth[n] =
					last >= 0 && emit && last >= emit.start && (def.parent < 0 || latestBirth[def.parent]! >= 0) ?
						(def.parent < 0 ? 0 : latestBirth[def.parent]!) + emit.start +
						Math.floor( (last - emit.start) / emit.period ) * emit.period :
						-1;
				if ( latestBirth[n]! >= 0 ) duration = Math.max( duration, (latestBirth[n]! + def.frames) / 20 );
			}
			const tails = particleGraph.map( () => 0 );
			for ( let n = particleGraph.length - 1; n >= 0; n-- ) {
				const def = particleGraph[n]!;
				if ( def.parent >= 0 ) tails[def.parent] = Math.max( tails[def.parent]!, def.frames + tails[n]! );
			}
			for ( let n = 0; n < particleGraph.length; n++ ) {
				const def = particleGraph[n]!,
					parentCapacity = def.parent < 0 ? 1 : particleGraph[def.parent]!.capacity ?? 1;
				if ( !def.emission ) {
					particleGraph[n] = { ...def, capacity: def.births.length };
					continue;
				}
				const emit = def.emission, parent = def.parent < 0 ? undefined : particleGraph[def.parent];
				const repeats = parent && (parent.loop || parent.parent < 0);
				const window = Math.max( 0, Math.min( emit.duration, (parent?.frames ?? Infinity) - emit.start ) );
				const lifetimeBirths = repeats ?
					Infinity :
					emit.period > 0 ?
					Math.ceil( window / emit.period ) * Math.ceil( emit.rate ) :
					0;
				const retained = emit.limit * (1 + Math.ceil( tails[n]! / Math.max( 1, def.frames ) ));
				const capacity = Math.min(
					CHARACTER_PARTICLE_BIRTHS,
					Math.max( def.births.length, parentCapacity * Math.min( retained, lifetimeBirths ) )
				);
				particleGraph[n] = { ...def, capacity };
			}
			for ( let p = 0; p < primitives.length; p++ ) {
				const primitive = primitives[p]!;
				if ( primitive.emission && primitive.particleEmitter !== undefined ) {
					primitives[p] = {
						...primitive,
						emission: {
							...primitive.emission,
							capacity: particleGraph[primitive.particleEmitter]!.capacity
						}
					};
				}
			}
			return {
				model: {
					particleGraph,
					nodes,
					primitives,
					clips: [ { name: "effect", duration, channels } ],
					images: []
				},
				imagePaths
			};
		},
		/*
		================
		clear
		================
		*/
		clear() {
			catalog = null;
		}
	};
}
