/*
===========================================================================

resources.ts - priority admission and lifetime of character asset requests

===========================================================================
*/
import {
	characterBytes,
	CHARACTER_MODELS,
	CHARACTER_RESIDENT_BYTES,
	CHARACTER_SOURCE_BYTES
} from "@/engine/foundation/animation/character-budget";
import { assetRequestBudget } from "@/engine/foundation/assets/asset-budget";
import { decodeNativeClip } from "@/engine/foundation/animation/native-clip";
import { withBearRoar, withWolfHowl, withWolfRend } from "@/engine/foundation/animation/bear-roar";
import type { AssetOwner } from "@/engine/contracts/assets";
import type { CharacterModel } from "@/engine/contracts/character";
import type { Renderer } from "@/engine/contracts/runtime";
import type { NativeTexture } from "@/engine/contracts/texture";
// A failed resource has its own retry deadline. It never stops actor reconciliation.

/*
================
createIdentityMatrix

Return a fresh 4x4 column-major identity matrix for procedural effect bindings.
================
*/
function createIdentityMatrix(): Float32Array {
	const out = new Float32Array( 16 );
	out[0] =
		out[5] =
		out[10] =
		out[15] =
			1;
	return out;
}

/*
================
buildProceduralEffectModel

Wrap vertex streams and a procedural BGRA native texture into an admitted CharacterModel.
================
*/
function buildProceduralEffectModel(
	positions: number[],
	normals: number[],
	uvs: number[],
	indices: number[],
	texture: NativeTexture
): { model: CharacterModel; images: NativeTexture[]; } {
	const vertexCount = positions.length / 3;
	const weights = new Float32Array( vertexCount * 4 );
	for ( let i = 0; i < vertexCount; i++ ) {
		weights[i * 4] = 1;
	}
	return {
		model: {
			nodes: [
				{
					name: "root",
					parent: -1,
					translation: [ 0, 0, 0 ],
					rotation: [ 0, 0, 0, 1 ],
					scale: [ 1, 1, 1 ]
				}
			],
			clips: [ { name: "effect", duration: 1, channels: [] } ],
			images: [ { width: texture.width, height: texture.height } ],
			primitives: [
				{
					name: "effect",
					node: 0,
					joints: [ 0 ],
					inverseBind: createIdentityMatrix(),
					image: 0,
					geometry: {
						positions: Float32Array.from( positions ),
						normals: Float32Array.from( normals ),
						uvs: Float32Array.from( uvs ),
						joints: new Uint32Array( vertexCount * 4 ),
						weights,
						indices: Uint32Array.from( indices ),
						transform: createIdentityMatrix(),
						material: {
							color: [ 1, 1, 1, 1 ],
							alphaCutoff: 0,
							blend: true,
							blendPair: { source: 5, destination: 2 },
							depthWrite: false,
							textureAlpha: true,
							fadeAlphaOnly: true,
							doubleSided: true,
							unlit: true,
							fogDisabled: true
						}
					}
				}
			]
		},
		images: [ texture ]
	};
}

/*
================
createChallengingRoarAuraResource

Build the 3D golden-crimson primal aura shell and interlocking vortex rings that
brighten over the 1-second Challenging Roar buildup around the bear.
================
*/
function createChallengingRoarAuraResource(): { model: CharacterModel; images: NativeTexture[]; } {
	const size = 128;
	const pixels = new Uint8Array( size * size * 4 );
	for ( let y = 0; y < size; y++ ) {
		const v = (y + 0.5) / size;
		const envV = Math.pow( Math.sin( v * Math.PI ), 1.35 );
		const coreV = Math.pow( Math.sin( v * Math.PI ), 2.6 );
		for ( let x = 0; x < size; x++ ) {
			const u = (x + 0.5) / size;
			const angle = u * Math.PI * 2;
			const f1 = 0.5 + 0.5 * Math.sin( angle * 8 + v * 10 );
			const f2 = 0.5 + 0.5 * Math.cos( angle * 14 - v * 16 );
			const f3 = 0.5 + 0.5 * Math.sin( angle * 22 + v * 6 );
			const energy = envV * (0.38 + 0.34 * (f1 * f1) + 0.18 * (f2 * f2 * f2) + 0.10 * (f3 * f3));
			const r = Math.min( 255, Math.round( 255 * Math.min( 1, energy * 1.50 + coreV * 0.42 ) ) );
			const g = Math.min(
				255,
				Math.round( 255 * Math.min( 1, Math.pow( energy, 1.32 ) * 1.08 + Math.pow( coreV, 1.8 ) * 0.58 ) )
			);
			const b = Math.min(
				255,
				Math.round( 255 * Math.min( 1, Math.pow( energy, 2.35 ) * 0.65 + Math.pow( coreV, 2.9 ) * 0.72 ) )
			);
			const a = Math.min( 255, Math.round( 255 * Math.min( 1, energy * 1.18 ) ) );
			const idx = (y * size + x) * 4;
			pixels[idx] = b;
			pixels[idx + 1] = g;
			pixels[idx + 2] = r;
			pixels[idx + 3] = a;
		}
	}
	const positions: number[] = [], normals: number[] = [], uvs: number[] = [], indices: number[] = [];
	const segs = 32, rings = 8;
	// 1. Ovoid primal energy cocoon around the bear's torso/head.
	for ( let r = 0; r <= rings; r++ ) {
		const v = r / rings;
		const phi = (v - 0.5) * Math.PI * 0.88;
		const cp = Math.cos( phi ), sp = Math.sin( phi );
		for ( let s = 0; s <= segs; s++ ) {
			const u = s / segs, theta = u * Math.PI * 2;
			const ct = Math.cos( theta ), st = Math.sin( theta );
			positions.push( ct * cp * 10.5, 10.5 + sp * 11.5, st * cp * 10.5 );
			normals.push( ct * cp, sp, st * cp );
			uvs.push( u, v );
		}
	}
	for ( let r = 0; r < rings; r++ ) {
		for ( let s = 0; s < segs; s++ ) {
			const a = r * (segs + 1) + s, b = a + segs + 1;
			indices.push( a, b, a + 1, a + 1, b, b + 1 );
		}
	}
	// 2. Rising 8-pointed flame pillar crown around the bear.
	const crownBase = positions.length / 3;
	for ( let r = 0; r <= 4; r++ ) {
		const v = r / 4;
		const y = 0.8 + v * 22.0;
		const baseR = 11.5 + v * 3.8;
		for ( let s = 0; s <= segs; s++ ) {
			const u = s / segs, theta = u * Math.PI * 2;
			const star = 1 + 0.14 * Math.cos( theta * 8 + v * 1.8 );
			const rad = baseR * star;
			const ct = Math.cos( theta ), st = Math.sin( theta );
			positions.push( ct * rad, y, st * rad );
			normals.push( ct, 0.15, st );
			uvs.push( u, v );
		}
	}
	for ( let r = 0; r < 4; r++ ) {
		for ( let s = 0; s < segs; s++ ) {
			const a = crownBase + r * (segs + 1) + s, b = a + segs + 1;
			indices.push( a, b, a + 1, a + 1, b, b + 1 );
		}
	}
	// 3. Two interlocking tilted vortex bands precessing around the bear.
	for ( const tilt of [ 0.48, -0.48 ] ) {
		const bandBase = positions.length / 3;
		const cs = Math.cos( tilt ), sn = Math.sin( tilt );
		for ( let r = 0; r <= 2; r++ ) {
			const v = r / 2;
			const localY = (v - 0.5) * 4.6;
			const rad = 14.2 - Math.abs( v - 0.5 ) * 1.4;
			for ( let s = 0; s <= segs; s++ ) {
				const u = s / segs, theta = u * Math.PI * 2;
				const lx = Math.cos( theta ) * rad, lz = Math.sin( theta ) * rad;
				const wy = 10.0 + localY * cs - lz * sn;
				const wz = localY * sn + lz * cs;
				positions.push( lx, wy, wz );
				normals.push( Math.cos( theta ), cs, Math.sin( theta ) * cs );
				uvs.push( u, v );
			}
		}
		for ( let r = 0; r < 2; r++ ) {
			for ( let s = 0; s < segs; s++ ) {
				const a = bandBase + r * (segs + 1) + s, b = a + segs + 1;
				indices.push( a, b, a + 1, a + 1, b, b + 1 );
			}
		}
	}
	return buildProceduralEffectModel( positions, normals, uvs, indices, {
		kind: "native-texture",
		width: size,
		height: size,
		format: "bgra8unorm",
		levels: [ pixels ]
	} );
}

/*
================
createChallengingRoarShockwaveResource

Build the unit-radius (1.0 world unit) 3D shockwave ring, curved supersonic
blast wall and secondary wake ring. Scaling an actor of this model to 250
produces an exact 25.0 m (250 world units) radial shockwave.
================
*/
function createChallengingRoarShockwaveResource(): { model: CharacterModel; images: NativeTexture[]; } {
	const size = 256, half = size / 2;
	const pixels = new Uint8Array( size * size * 4 );
	for ( let y = 0; y < size; y++ ) {
		const dy = (y + 0.5) / half - 1;
		for ( let x = 0; x < size; x++ ) {
			const dx = (x + 0.5) / half - 1;
			const r = Math.hypot( dx, dy );
			if ( r <= 0.24 || r >= 0.995 ) continue;
			const theta = Math.atan2( dy, dx );
			const outerFeather = r > 0.93 ? Math.max( 0, (0.995 - r) / 0.065 ) : 1;
			const crest = Math.exp( -Math.pow( (r - 0.865) / 0.042, 2 ) );
			const corona = Math.exp( -Math.pow( (r - 0.835) / 0.098, 2 ) );
			const ripple1 = Math.exp( -Math.pow( (r - 0.725) / 0.028, 2 ) ) * 0.74;
			const ripple2 = Math.exp( -Math.pow( (r - 0.610) / 0.024, 2 ) ) * 0.52;
			const ripple3 = Math.exp( -Math.pow( (r - 0.495) / 0.020, 2 ) ) * 0.34;
			const spokeEnv = r > 0.34 && r < 0.92 ?
				Math.sin( ((r - 0.34) / 0.58) * Math.PI ) :
				0;
			const spokes = (Math.pow( 0.5 + 0.5 * Math.cos( theta * 36 ), 6 ) * 0.68 +
				Math.pow( 0.5 + 0.5 * Math.sin( theta * 64 + r * 10 ), 8 ) * 0.56) *
				spokeEnv;
			const total = Math.min(
				1,
				(crest * 1.18 + corona * 0.66 + ripple1 + ripple2 + ripple3 + spokes * 0.62) * outerFeather
			);
			const whiteHot = Math.min( 1, (Math.pow( crest, 1.45 ) + spokes * crest * 0.65) * outerFeather );
			const red = Math.min( 255, Math.round( 255 * Math.min( 1, total * 1.55 ) ) );
			const green = Math.min(
				255,
				Math.round( 255 * Math.min( 1, Math.pow( total, 1.32 ) * 1.06 + whiteHot * 0.48 ) )
			);
			const blue = Math.min(
				255,
				Math.round( 255 * Math.min( 1, Math.pow( total, 2.55 ) * 0.55 + whiteHot * 0.72 ) )
			);
			const alpha = Math.min( 255, Math.round( 255 * total ) );
			const idx = (y * size + x) * 4;
			pixels[idx] = blue;
			pixels[idx + 1] = green;
			pixels[idx + 2] = red;
			pixels[idx + 3] = alpha;
		}
	}
	const positions: number[] = [], normals: number[] = [], uvs: number[] = [], indices: number[] = [];
	const segs = 64;
	// 1. Ground-hugging radial shockwave annulus (unit outer radius = 1.0).
	const discRadii = [ 0.26, 0.46, 0.64, 0.78, 0.88, 0.95, 1.0 ];
	const discHeights = [ 0.008, 0.014, 0.020, 0.026, 0.032, 0.022, 0.010 ];
	for ( let r = 0; r < discRadii.length; r++ ) {
		const rad = discRadii[r]!, y = discHeights[r]!;
		for ( let s = 0; s <= segs; s++ ) {
			const theta = (s / segs) * Math.PI * 2;
			const ct = Math.cos( theta ), st = Math.sin( theta );
			positions.push( ct * rad, y, st * rad );
			normals.push( 0, 1, 0 );
			uvs.push( 0.5 + 0.5 * rad * ct, 0.5 + 0.5 * rad * st );
		}
	}
	for ( let r = 0; r < discRadii.length - 1; r++ ) {
		for ( let s = 0; s < segs; s++ ) {
			const a = r * (segs + 1) + s, b = a + segs + 1;
			indices.push( a, b, a + 1, a + 1, b, b + 1 );
		}
	}
	// 2. 3D curved supersonic blast-wave crest wall near the leading edge.
	const wallBase = positions.length / 3;
	const wallRadii = [ 0.78, 0.865, 0.935, 0.90, 0.82 ];
	const wallHeights = [ 0.012, 0.045, 0.095, 0.145, 0.175 ];
	const wallUvRadii = [ 0.72, 0.83, 0.865, 0.90, 0.96 ];
	for ( let r = 0; r < wallRadii.length; r++ ) {
		const rad = wallRadii[r]!, y = wallHeights[r]!, uvRad = wallUvRadii[r]!;
		for ( let s = 0; s <= segs; s++ ) {
			const theta = (s / segs) * Math.PI * 2;
			const ct = Math.cos( theta ), st = Math.sin( theta );
			positions.push( ct * rad, y, st * rad );
			normals.push( ct, 0.35, st );
			uvs.push( 0.5 + 0.5 * uvRad * ct, 0.5 + 0.5 * uvRad * st );
		}
	}
	for ( let r = 0; r < wallRadii.length - 1; r++ ) {
		for ( let s = 0; s < segs; s++ ) {
			const a = wallBase + r * (segs + 1) + s, b = a + segs + 1;
			indices.push( a, b, a + 1, a + 1, b, b + 1 );
		}
	}
	// 3. Inner angled sonic compression wake ring.
	const wakeBase = positions.length / 3;
	const wakeRadii = [ 0.56, 0.65, 0.72, 0.68 ];
	const wakeHeights = [ 0.010, 0.038, 0.075, 0.105 ];
	const wakeUvRadii = [ 0.52, 0.62, 0.725, 0.95 ];
	for ( let r = 0; r < wakeRadii.length; r++ ) {
		const rad = wakeRadii[r]!, y = wakeHeights[r]!, uvRad = wakeUvRadii[r]!;
		for ( let s = 0; s <= segs; s++ ) {
			const theta = (s / segs) * Math.PI * 2;
			const ct = Math.cos( theta ), st = Math.sin( theta );
			positions.push( ct * rad, y, st * rad );
			normals.push( ct, 0.45, st );
			uvs.push( 0.5 + 0.5 * uvRad * ct, 0.5 + 0.5 * uvRad * st );
		}
	}
	for ( let r = 0; r < wakeRadii.length - 1; r++ ) {
		for ( let s = 0; s < segs; s++ ) {
			const a = wakeBase + r * (segs + 1) + s, b = a + segs + 1;
			indices.push( a, b, a + 1, a + 1, b, b + 1 );
		}
	}
	return buildProceduralEffectModel( positions, normals, uvs, indices, {
		kind: "native-texture",
		width: size,
		height: size,
		format: "bgra8unorm",
		levels: [ pixels ]
	} );
}

/*
================
createIronHideShieldResource

Port-only crystalline-cyan ward: a translucent barrier dome with a hexagonal
energy lattice, glowing meridian/latitude ribs and interlocking equatorial rings.
================
*/
function createIronHideShieldResource(): { model: CharacterModel; images: NativeTexture[]; } {
	const positions: number[] = [], normals: number[] = [], uvs: number[] = [], indices: number[] = [];
	const columns = 32, rows = 18;
	for ( let y = 0; y <= rows; y++ ) {
		const v = y / rows;
		const theta = v * Math.PI * 0.88;
		const sinT = Math.sin( theta ), cosT = Math.cos( theta );
		for ( let x = 0; x <= columns; x++ ) {
			const u = x / columns;
			const phi = u * Math.PI * 2;
			const nx = sinT * Math.cos( phi ), ny = cosT, nz = sinT * Math.sin( phi );
			positions.push( nx, ny * 0.95 + 0.95, nz );
			normals.push( nx, ny, nz );
			uvs.push( u, v );
			if ( x < columns && y < rows ) {
				const i = y * (columns + 1) + x;
				indices.push( i, i + 1, i + columns + 1, i + 1, i + columns + 2, i + columns + 1 );
			}
		}
	}
	const ringConfigs = [
		{ tiltX: 0.24, tiltZ: -0.18, radius: 1.08, halfH: 0.12, centerY: 0.95 },
		{ tiltX: -0.22, tiltZ: 0.24, radius: 1.05, halfH: 0.10, centerY: 0.95 },
		{ tiltX: 0, tiltZ: 0, radius: 1.04, halfH: 0.08, centerY: 0.10 }
	];
	for ( const cfg of ringConfigs ) {
		const baseVertex = positions.length / 3;
		const cX = Math.cos( cfg.tiltX ), sX = Math.sin( cfg.tiltX );
		const cZ = Math.cos( cfg.tiltZ ), sZ = Math.sin( cfg.tiltZ );
		for ( let r = 0; r <= 2; r++ ) {
			const v = 0.35 + (r / 2) * 0.30;
			const ly = (r / 2 - 0.5) * 2 * cfg.halfH;
			for ( let s = 0; s <= columns; s++ ) {
				const u = s / columns;
				const phi = u * Math.PI * 2;
				const lx = Math.cos( phi ) * cfg.radius, lz = Math.sin( phi ) * cfg.radius;
				const y1 = ly * cX - lz * sX, z1 = ly * sX + lz * cX;
				const x2 = lx * cZ - y1 * sZ, y2 = lx * sZ + y1 * cZ + cfg.centerY;
				positions.push( x2, y2, z1 );
				normals.push( Math.cos( phi ), 0, Math.sin( phi ) );
				uvs.push( u, v );
			}
		}
		for ( let r = 0; r < 2; r++ ) {
			for ( let s = 0; s < columns; s++ ) {
				const i0 = baseVertex + r * (columns + 1) + s, i1 = i0 + 1;
				const i2 = i0 + (columns + 1), i3 = i2 + 1;
				indices.push( i0, i2, i1, i1, i2, i3 );
			}
		}
	}
	const size = 128, pixels = new Uint8Array( size * size * 4 );
	for ( let y = 0; y < size; y++ ) {
		const v = (y + 0.5) / size;
		const eqBand = Math.exp( -Math.pow( (v - 0.50) / 0.09, 2 ) );
		const baseBand = Math.exp( -Math.pow( (v - 0.92) / 0.045, 2 ) );
		const crownBand = Math.exp( -Math.pow( (v - 0.14) / 0.06, 2 ) );
		const latRib = Math.pow( Math.max( 0, Math.cos( v * Math.PI * 10 ) ), 16 );
		const shellFade = Math.sin( v * Math.PI );
		for ( let x = 0; x < size; x++ ) {
			const u = (x + 0.5) / size;
			const lonRib = Math.pow( Math.max( 0, Math.cos( u * Math.PI * 14 ) ), 18 );
			const hexA = Math.pow( Math.max( 0, Math.cos( (u * 10 + v * 6) * Math.PI * 2 ) ), 16 );
			const hexB = Math.pow( Math.max( 0, Math.cos( (u * 10 - v * 6) * Math.PI * 2 ) ), 16 );
			const lattice = Math.min( 1, Math.max( latRib, lonRib * 0.85, (hexA + hexB) * 0.65 ) );
			const energy = Math.min(
				1,
				0.04 * shellFade + 0.58 * lattice * shellFade + 0.34 * eqBand + 0.52 * baseBand + 0.24 * crownBand
			);
			const idx = (y * size + x) * 4;
			pixels[idx] = Math.min( 255, Math.round( 255 * Math.min( 1, energy * 1.35 ) ) );
			pixels[idx + 1] = Math.min( 255, Math.round( 245 * Math.min( 1, Math.pow( energy, 1.15 ) * 1.12 ) ) );
			pixels[idx + 2] = Math.min( 255, Math.round( 180 * Math.min( 1, Math.pow( energy, 1.75 ) * 0.85 ) ) );
			pixels[idx + 3] = Math.min( 255, Math.round( 230 * Math.pow( energy, 1.12 ) ) );
		}
	}
	return buildProceduralEffectModel( positions, normals, uvs, indices, {
		kind: "native-texture",
		width: size,
		height: size,
		format: "bgra8unorm",
		levels: [ pixels ]
	} );
}

/*
================
createPetCounterRippleResource

A soft, unit-radius ground annulus without the roar's walls or bright spokes.
================
*/
function createPetCounterRippleResource(): { model: CharacterModel; images: NativeTexture[]; } {
	const positions: number[] = [], normals: number[] = [], uvs: number[] = [], indices: number[] = [];
	for ( let i = 0; i <= 64; i++ ) {
		const angle = i / 64 * Math.PI * 2;
		for ( const radius of [ .96, 1 ] ) {
			positions.push( Math.cos( angle ) * radius, .025, Math.sin( angle ) * radius );
			normals.push( 0, 1, 0 );
			uvs.push( .5, radius === 1 ? 1 : 0 );
		}
		if ( i < 64 ) {
			const a = i * 2;
			indices.push( a, a + 2, a + 1, a + 1, a + 2, a + 3 );
		}
	}
	const pixels = new Uint8Array( 16 * 4 );
	for ( let i = 0; i < 16; i++ ) {
		pixels.set( [ 220, 220, 220, Math.round( Math.sin( i / 15 * Math.PI ) * 255 ) ], i * 4 );
	}
	return buildProceduralEffectModel( positions, normals, uvs, indices, {
		kind: "native-texture",
		width: 1,
		height: 16,
		format: "bgra8unorm",
		levels: [ pixels ]
	} );
}

/*
================
createProceduralEffect

Explicit calls keep effect execution visible to the capability contract.
================
*/
function createProceduralEffect( path: string ) {
	switch ( path ) {
		case "/assets/effects/pet-counter-ripple":
			return createPetCounterRippleResource();
		case "/assets/effects/iron-hide-shield":
			return createIronHideShieldResource();
		case "/assets/effects/challenging-roar-aura":
			return createChallengingRoarAuraResource();
		case "/assets/effects/challenging-roar-shockwave":
			return createChallengingRoarShockwaveResource();
		default:
			return null;
	}
}

/*
================
createCharacterResources

Own one decode reservation and a bounded cache of admitted character sources.
================
*/
export function createCharacterResources( assets: AssetOwner, renderer: Renderer, origin: string ) {
	const durations = new Map<string, ReadonlyMap<string, number>>();
	const loaded = new Set<string>(), wanted = new Set<string>(), planned = new Set<string>();
	const costs = new Map<string, number>();
	const proceduralCache = new Map<string, { model: CharacterModel; images: NativeTexture[]; }>();
	let plannedBytes = 0, reserved: string | null = null;
	// Ordered requests are priority admission, not a list of everything nearby.
	// A decoded source is charged what it measured. A source that has never been
	// decoded reserves one source class, and only the single load slot may hold
	// that reservation: an unstarted load must never be charged for residency a
	// resident actor still needs. Reservations are headroom, never a charge, so
	// speculation cannot displace an actor that is already decoded.

	/*
	================
	reservable

	A cold source can reserve room only when the single load slot can start it.
	================
	*/
	function reservable( path: string ): boolean {
		if ( job ) return job.path === path;
		return reserved === null && !disposed && assets.available() > 0 &&
			(failures.get( path )?.retryAt ?? -Infinity) <= now;
	}

	/*
	================
	plan

	Charge shared sources once and reserve at most one undecoded source.
	================
	*/
	function plan( paths: readonly string[] ): boolean {
		// A path admitted this frame is already charged and wanted. Shared
		// crowd models must not allocate a Set/filter for each additional actor.
		if ( paths.every( path => planned.has( path ) ) ) return true;
		const extra = [ ...new Set( paths ) ].filter( path => !planned.has( path ) );
		// Equipped actors admit their parts one source at a time. A group that
		// needs two undecoded sources cannot start both and reserves neither.
		const cold = extra.filter( path => !costs.has( path ) );
		if ( cold.length > 1 || (cold.length === 1 && !reservable( cold[0]! )) ) return false;
		const bytes = extra.reduce( ( sum, path ) => sum + (costs.get( path ) ?? 0), 0 );
		if (
			planned.size + extra.length > CHARACTER_MODELS ||
			plannedBytes + bytes + (cold.length ? CHARACTER_SOURCE_BYTES : 0) > CHARACTER_RESIDENT_BYTES
		) return false;
		for ( const path of paths ) {
			planned.add( path );
			wanted.add( path );
		}
		plannedBytes += bytes;
		if ( cold.length ) reserved = cold[0]!;
		return true;
	}
	const failures = new Map<string, {
		retryAt: number;
		message: string;
	}>();
	let job: {
		id: number;
		path: string;
		model: boolean;
		animation?: { body: string; name: string; };
	} | null = null;
	let now = 0, disposed = false;

	/*
	================
	request

	Submit only published resources whose independent retry deadline has elapsed.
	================
	*/
	function request( path: string, model: boolean ) {
		if ( model && !plan( [ path ] ) ) return;
		wanted.add( path );
		if (
			assets.available() === 0 || disposed || job || loaded.has( path ) ||
			(failures.get( path )?.retryAt ?? -Infinity) > now
		) {
			return;
		}
		try {
			if ( !path.startsWith( "/assets/" ) || path.includes( ".." ) || path.includes( "\\" ) ) {
				throw new Error( "Unpublished character resource" );
			}
			const decode = model ? (path.includes( "/effects/programs.json#" ) ? "effect" : "character") : undefined;
			job = {
				id: assets.request( new URL( path, origin ).href, assetRequestBudget( decode ), decode ),
				path,
				model
			};
		} catch ( error ) {
			failures.set( path, { retryAt: now + 2, message: String( error ) } );
		}
	}
	return {
		/*
		================
		duration

		Read a clip duration from the admitted model or native animation metadata.
		================
		*/
		duration( path: string, clip: string ) {
			return durations.get( path )?.get( clip ) ?? 0;
		},

		/*
		================
		begin

		Reset frame admission intent while preserving the inactive bounded cache.
		================
		*/
		begin( seconds: number ) {
			now = seconds;
			wanted.clear();
			planned.clear();
			plannedBytes = 0;
			reserved = null;
		},

		/*
		================
		poll

		Transfer a completed source to the renderer, releasing rejected image resources.
		================
		*/
		poll(): {
			path: string;
			buffer: ArrayBuffer;
		} | null {
			if ( !job || disposed ) {
				return null;
			}
			const result = assets.take( job.id );
			if ( !result ) {
				return null;
			}
			const completed = job;
			job = null;
			try {
				if ( result.kind === "error" ) {
					throw new Error( result.error );
				}
				if ( completed.model && result.kind === "character" ) {
					const model = /\/npc\/cos\/p_bear0[12]\.glb$/i.test( completed.path ) ?
						withBearRoar( result.model ) :
						/\/npc\/cos\/p_wolf(_white)?_0[12]\.glb$/i.test( completed.path ) ?
						withWolfRend( withWolfHowl( result.model ) ) :
						result.model;
					try {
						costs.set( completed.path, characterBytes( model, result.images ) );
					} catch ( error ) {
						for ( const image of result.images ) if ( !("kind" in image) ) image.close();
						throw error;
					}
					renderer.setCharacterModel( completed.path, model, result.images );
					durations.set(
						completed.path,
						new Map( model.clips.map( clip => [ clip.name, clip.duration ] ) )
					);
					loaded.add( completed.path );
					failures.delete( completed.path );
				} else if ( completed.animation && result.kind === "bytes" ) {
					const { body, name } = completed.animation,
						source = decodeNativeClip( new Uint8Array( result.buffer ) );
					costs.set( body, renderer.setCharacterAnimation( body, name, source ) );
					durations.set( body, new Map( [ ...(durations.get( body ) ?? []), [ name, source.duration ] ] ) );
					failures.delete( completed.path );
				} else if ( !completed.model && result.kind === "bytes" ) {
					return { path: completed.path, buffer: result.buffer };
				} else {
					if ( result.kind === "image" ) {
						result.image.close();
					}
					if ( result.kind === "character" ) {
						for ( const image of result.images ) {
							if ( !("kind" in image) ) image.close();
						}
					}
					throw new Error( "Character resource result mismatch" );
				}
			} catch ( error ) {
				failures.set( completed.path, { retryAt: now + 2, message: String( error ) } );
			}
			return null;
		},

		/*
		================
		manifest

		Use the same request slot for metadata without a model residency reservation.
		================
		*/
		manifest( path: string ) {
			request( path, false );
		},

		/*
		================
		animation

		Load an authored clip only after its owning body has been admitted.
		================
		*/
		animation( body: string, name: string, path: string ) {
			if ( !loaded.has( body ) ) return false;
			if ( durations.get( body )?.has( name ) ) return true;
			if ( !path.startsWith( "/assets/anim/" ) || !path.endsWith( ".ban" ) ) {
				throw Error( "Unpublished native motion" );
			}
			const pending = job;
			request( path, false );
			if ( !pending && job && job.path === path ) job.animation = { body, name };
			return false;
		},

		/*
		================
		accepted

		Clear the retry record after the consumer accepts the resource.
		================
		*/
		accepted( path: string ) {
			failures.delete( path );
		},

		/*
		================
		rejected

		Keep the failing source isolated behind its own retry deadline.
		================
		*/
		rejected( path: string, error: unknown ) {
			failures.set( path, { retryAt: now + 2, message: String( error ) } );
		},
		plan,

		/*
		================
		ready

		Prioritize wanted sources and move admitted entries to the recent end of the cache.
		================
		*/
		ready( path: string ) {
			if ( disposed ) return false;
			let built = proceduralCache.get( path );
			if ( !built ) {
				built = createProceduralEffect( path ) ?? undefined;
				if ( built ) proceduralCache.set( path, built );
			}
			if ( built && !costs.has( path ) ) costs.set( path, characterBytes( built.model, built.images ) );
			// A failing source keeps its own retry deadline and stays wanted so
			// retainWanted does not drop the deadline and refetch every frame.
			if ( !loaded.has( path ) && (failures.get( path )?.retryAt ?? -Infinity) > now ) {
				wanted.add( path );
				return false;
			}
			if ( !plan( [ path ] ) ) return false;
			if ( built ) {
				if ( !loaded.has( path ) ) {
					renderer.setCharacterModel( path, built.model, built.images );
					durations.set(
						path,
						new Map( built.model.clips.map( clip => [ clip.name, clip.duration ] ) )
					);
					loaded.add( path );
				}
			} else {
				request( path, true );
			}
			if ( !loaded.has( path ) ) return false;
			// Most recently used last: the cache retires the oldest inactive
			// source first when the frame takes its room back.
			loaded.delete( path );
			loaded.add( path );
			return true;
		},

		/*
		================
		retainWanted

		Preserve wanted sources first, then retain inactive sources within the remaining budget.
		================
		*/
		retainWanted( assemblies: readonly string[] ) {
			if ( job && !wanted.has( job.path ) ) {
				assets.cancel( job.id );
				job = null;
			}
			// A finished attack is not a resource invalidation. Keep decoded
			// models in the remaining bounded budget, most recently used first.
			// The in-flight source class is held back, so inactive cache entries
			// retire before the next poll can admit its decode.
			let bytes = plannedBytes +
					(reserved !== null || (job !== null && !costs.has( job.path )) ? CHARACTER_SOURCE_BYTES : 0),
				count = planned.size;
			const keep = new Set( planned );
			for ( const path of [ ...loaded ].reverse() ) {
				if ( !keep.has( path ) ) {
					const cost = costs.get( path )!;
					if ( count < CHARACTER_MODELS && bytes + cost <= CHARACTER_RESIDENT_BYTES ) {
						keep.add( path );
						count++;
						bytes += cost;
					}
				}
			}
			for ( const path of loaded ) {
				if ( !keep.has( path ) ) {
					loaded.delete( path );
					durations.delete( path );
					costs.delete( path );
				}
			}
			for ( const path of failures.keys() ) {
				if ( !wanted.has( path ) ) {
					failures.delete( path );
				}
			}
			renderer.retainCharacterModels( [ ...wanted, ...loaded, ...assemblies ] );
		},

		/*
		================
		failed

		Expose the per-source failure state without disturbing unrelated admission.
		================
		*/
		failed( path: string ) {
			return failures.has( path );
		},

		/*
		================
		error

		Report one retained failure for runtime diagnostics.
		================
		*/
		error() {
			return failures.values().next().value?.message ?? null;
		},

		/*
		================
		reset

		Cancel pending work and release all renderer residency for a new world.
		================
		*/
		reset() {
			if ( job ) {
				assets.cancel( job.id );
			}
			job = null;
			failures.clear();
			wanted.clear();
			loaded.clear();
			durations.clear();
			costs.clear();
			planned.clear();
			plannedBytes = 0;
			reserved = null;
			renderer.retainCharacterModels( [] );
		},

		/*
		================
		dispose

		Cancel work and release local records after the renderer lifecycle has ended.
		================
		*/
		dispose() {
			disposed = true;
			if ( job ) {
				assets.cancel( job.id );
			}
			job = null;
			failures.clear();
			wanted.clear();
			loaded.clear();
			durations.clear();
			costs.clear();
			planned.clear();
			plannedBytes = 0;
			reserved = null;
		}
	};
}
