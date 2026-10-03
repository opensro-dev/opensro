/*
===========================================================================

exportGlb.mjs - character model container and authored texture publication

===========================================================================
*/
// Emit an assembled SRO avatar (skinned mesh + skeleton + walk clip) as a binary
// glTF (.glb) with a skin and one animation. Build-time only.
//
// SRO is left-handed (Y-up); glTF is right-handed. We convert by negating Z on
// all positions/normals/translations, reversing triangle winding, and mapping
// rotation quaternions (x,y,z,w) -> (-x,-y,z,w). inverseBindMatrices are computed
// by forward kinematics from the converted local joint transforms so they stay
// consistent regardless of how the BSK world matrices were authored.
//
// Runtime note: exported skinned-character GLBs share one model-space basis
// adapter in the client. Keep native yaw state raw; do not add per-screen
// 180-degree fixes.

import fs from "node:fs";
import { readCharacterTexture } from "../shared/nativeCharacterTextures.mjs";
import { applyCharacterMaterialState } from "../shared/characterMaterialState.mjs";
import { embedCharacterEnvironment } from "../shared/characterEnvironment.mjs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { assembleAvatar } from "./buildAvatar.mjs";
import { compileBanTimeline } from "./compileBanTimeline.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";

// ---- small math (column-major mat4 as Float64Array(16)) ----

/*
================
quatToMat4

Convert an authored quaternion and translation into a column-major bind matrix.
================
*/
function quatToMat4( q, t ) {
	const [x, y, z, w] = q;
	const [tx, ty, tz] = t;
	const x2 = x + x, y2 = y + y, z2 = z + z;
	const xx = x * x2, xy = x * y2, xz = x * z2;
	const yy = y * y2, yz = y * z2, zz = z * z2;
	const wx = w * x2, wy = w * y2, wz = w * z2;
	// column-major
	return new Float64Array( [
		1 - (yy + zz),
		xy + wz,
		xz - wy,
		0,
		xy - wz,
		1 - (xx + zz),
		yz + wx,
		0,
		xz + wy,
		yz - wx,
		1 - (xx + yy),
		0,
		tx,
		ty,
		tz,
		1
	] );
}

/*
================
mul

Compose parent and child bind transforms in column-major order.
================
*/
function mul( a, b ) {
	const o = new Float64Array( 16 );
	for ( let c = 0; c < 4; c += 1 ) {
		for ( let r = 0; r < 4; r += 1 ) {
			o[c * 4 + r] = a[r] * b[c * 4] + a[4 + r] * b[c * 4 + 1] + a[8 + r] * b[c * 4 + 2] +
				a[12 + r] * b[c * 4 + 3];
		}
	}
	return o;
}
// general 4x4 inverse (good enough; rigid transforms here)

/*
================
invert

Compute inverse bind matrices without changing the authored skeleton hierarchy.
================
*/
function invert( m ) {
	const inv = new Float64Array( 16 );
	const a = m;
	inv[0] = a[5] * a[10] * a[15] - a[5] * a[11] * a[14] - a[9] * a[6] * a[15] + a[9] * a[7] * a[14] +
		a[13] * a[6] * a[11] - a[13] * a[7] * a[10];
	inv[4] = -a[4] * a[10] * a[15] + a[4] * a[11] * a[14] + a[8] * a[6] * a[15] - a[8] * a[7] * a[14] -
		a[12] * a[6] * a[11] + a[12] * a[7] * a[10];
	inv[8] = a[4] * a[9] * a[15] - a[4] * a[11] * a[13] - a[8] * a[5] * a[15] + a[8] * a[7] * a[13] +
		a[12] * a[5] * a[11] - a[12] * a[7] * a[9];
	inv[12] = -a[4] * a[9] * a[14] + a[4] * a[10] * a[13] + a[8] * a[5] * a[14] - a[8] * a[6] * a[13] -
		a[12] * a[5] * a[10] + a[12] * a[6] * a[9];
	inv[1] = -a[1] * a[10] * a[15] + a[1] * a[11] * a[14] + a[9] * a[2] * a[15] - a[9] * a[3] * a[14] -
		a[13] * a[2] * a[11] + a[13] * a[3] * a[10];
	inv[5] = a[0] * a[10] * a[15] - a[0] * a[11] * a[14] - a[8] * a[2] * a[15] + a[8] * a[3] * a[14] +
		a[12] * a[2] * a[11] - a[12] * a[3] * a[10];
	inv[9] = -a[0] * a[9] * a[15] + a[0] * a[11] * a[13] + a[8] * a[1] * a[15] - a[8] * a[3] * a[13] -
		a[12] * a[1] * a[11] + a[12] * a[3] * a[9];
	inv[13] = a[0] * a[9] * a[14] - a[0] * a[10] * a[13] - a[8] * a[1] * a[14] + a[8] * a[2] * a[13] +
		a[12] * a[1] * a[10] - a[12] * a[2] * a[9];
	inv[2] = a[1] * a[6] * a[15] - a[1] * a[7] * a[14] - a[5] * a[2] * a[15] + a[5] * a[3] * a[14] +
		a[13] * a[2] * a[7] - a[13] * a[3] * a[6];
	inv[6] = -a[0] * a[6] * a[15] + a[0] * a[7] * a[14] + a[4] * a[2] * a[15] - a[4] * a[3] * a[14] -
		a[12] * a[2] * a[7] + a[12] * a[3] * a[6];
	inv[10] = a[0] * a[5] * a[15] - a[0] * a[7] * a[13] - a[4] * a[1] * a[15] + a[4] * a[3] * a[13] +
		a[12] * a[1] * a[7] - a[12] * a[3] * a[5];
	inv[14] = -a[0] * a[5] * a[14] + a[0] * a[6] * a[13] + a[4] * a[1] * a[14] - a[4] * a[2] * a[13] -
		a[12] * a[1] * a[6] + a[12] * a[2] * a[5];
	inv[3] = -a[1] * a[6] * a[11] + a[1] * a[7] * a[10] + a[5] * a[2] * a[11] - a[5] * a[3] * a[10] -
		a[9] * a[2] * a[7] + a[9] * a[3] * a[6];
	inv[7] = a[0] * a[6] * a[11] - a[0] * a[7] * a[10] - a[4] * a[2] * a[11] + a[4] * a[3] * a[10] +
		a[8] * a[2] * a[7] - a[8] * a[3] * a[6];
	inv[11] = -a[0] * a[5] * a[11] + a[0] * a[7] * a[9] + a[4] * a[1] * a[11] - a[4] * a[3] * a[9] -
		a[8] * a[1] * a[7] + a[8] * a[3] * a[5];
	inv[15] = a[0] * a[5] * a[10] - a[0] * a[6] * a[9] - a[4] * a[1] * a[10] + a[4] * a[2] * a[9] + a[8] * a[1] * a[6] -
		a[8] * a[2] * a[5];
	let det = a[0] * inv[0] + a[1] * inv[4] + a[2] * inv[8] + a[3] * inv[12];
	if ( !det ) return new Float64Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
	det = 1 / det;
	for ( let i = 0; i < 16; i += 1 ) inv[i] *= det;
	return inv;
}
// SRO(LH) -> glTF(RH): flip Z
const convQuat = ( q ) => [ -q[0], -q[1], q[2], q[3] ];
const convPos = ( t ) => [ t[0], t[1], -t[2] ];

// ---- glTF buffer builder ----

/*
================
createGltf

Own container JSON and aligned binary chunks in one explicit builder lifetime.
================
*/
function createGltf() {
	// images/samplers/textures are optional: avatarToGlb attaches them lazily, only
	// when a material actually embeds a texture.
	/**
	 * @type {{
	 *   asset: { version: string, generator: string },
	 *   scenes: { nodes: number[] }[],
	 *   scene: number,
	 *   nodes: any[],
	 *   meshes: any[],
	 *   skins: any[],
	 *   materials: any[],
	 *   accessors: any[],
	 *   bufferViews: { buffer: number, byteOffset: number, byteLength: number, target?: number }[],
	 *   buffers: { byteLength: number }[],
	 *   animations: any[],
	 *   images?: { bufferView: number, mimeType: string }[],
	 *   samplers?: { magFilter: number, minFilter: number, wrapS: number, wrapT: number }[],
	 *   textures?: { source: number, sampler: number }[],
	 *   extras?: { sroAggregateBox?: number[] }
	 * }}
	 */
	const json = {
		asset: { version: "2.0", generator: "sro-char-exporter" },
		scenes: [ { nodes: [] } ],
		scene: 0,
		nodes: [],
		meshes: [],
		skins: [],
		materials: [],
		accessors: [],
		bufferViews: [],
		buffers: [],
		animations: []
	};
	const bin = [];
	let binLen = 0;

	/*
	================
	pad

	Align the next binary view without changing preceding payload bytes.
	================
	*/
	function pad( n ) {
		while ( binLen % n !== 0 ) {
			bin.push( Buffer.from( [ 0 ] ) );
			binLen += 1;
		}
	}

	/*
	================
	addView

	Append one binary view and retain its unpadded payload length.
	================
	*/
	function addView( buf, target ) {
		pad( 4 );
		const byteOffset = binLen;
		bin.push( buf );
		binLen += buf.length;
		const view = { buffer: 0, byteOffset, byteLength: buf.length };
		if ( target ) view.target = target;
		json.bufferViews.push( view );
		return json.bufferViews.length - 1;
	}

	/*
	================
	addAccessor

	Describe typed geometry or animation data already owned by a binary view.
	================
	*/
	function addAccessor( view, componentType, count, type, opts = {} ) {
		const acc = { bufferView: view, componentType, count, type, ...opts };
		json.accessors.push( acc );
		return json.accessors.length - 1;
	}

	/*
	================
	build

	Serialize aligned JSON and binary chunks into the final model container.
	================
	*/
	function build() {
		// glTF forbids empty top-level arrays (minItems 1) - drop any we never filled.
		for ( const key of [ "animations", "meshes", "skins", "materials" ] ) {
			if ( Array.isArray( json[key] ) && json[key].length === 0 ) delete json[key];
		}
		const binary = Buffer.concat( bin );
		json.buffers = [ { byteLength: binary.length } ];
		const jsonStr = Buffer.from( JSON.stringify( json ), "utf8" );
		const jsonPad = (4 - (jsonStr.length % 4)) % 4;
		const jsonChunk = Buffer.concat( [ jsonStr, Buffer.alloc( jsonPad, 0x20 ) ] );
		const binPad = (4 - (binary.length % 4)) % 4;
		const binChunk = Buffer.concat( [ binary, Buffer.alloc( binPad, 0 ) ] );
		const header = Buffer.alloc( 12 );
		header.writeUInt32LE( 0x46546c67, 0 ); // glTF
		header.writeUInt32LE( 2, 4 );
		header.writeUInt32LE( 12 + 8 + jsonChunk.length + 8 + binChunk.length, 8 );
		const jh = Buffer.alloc( 8 );
		jh.writeUInt32LE( jsonChunk.length, 0 );
		jh.writeUInt32LE( 0x4e4f534a, 4 ); // JSON
		const bh = Buffer.alloc( 8 );
		bh.writeUInt32LE( binChunk.length, 0 );
		bh.writeUInt32LE( 0x004e4942, 4 ); // BIN
		return Buffer.concat( [ header, jh, jsonChunk, bh, binChunk ] );
	}
	return { json, addView, addAccessor, build };
}

const F32 = 5126, U16 = 5123, U32 = 5125;
const ARRAY_BUFFER = 34962, ELEMENT_ARRAY_BUFFER = 34963;

/**
 * @param {any} value
 * @param {string} context
 * @returns {number[]}
 */

/*
================
requireRgbaFactor

Reject missing native material factors instead of inventing a lighting tint.
================
*/
function requireRgbaFactor( value, context ) {
	if ( !Array.isArray( value ) || value.length < 4 || value.some( ( component ) => !Number.isFinite( component ) ) ) {
		throw new Error( `${context}: missing or invalid native RGBA material factor` );
	}
	if ( value.some( ( component ) => component < 0 || component > 1 ) ) {
		throw new Error( `${context}: native RGBA material factor is outside [0,1]` );
	}
	return value.slice( 0, 4 );
}

/*
================
readInPlaceHorizontalRootMotionRoles

An exported avatar normally retains authored root translation verbatim. Some
runtime owners, however, advance the character holder independently from the
pose clip. Those owners must opt their locomotion roles into an in-place
contract or the holder and the skeleton both apply the same travel.

Only horizontal root translation is removed. Vertical root motion remains
part of the pose (footfall/body-height animation), and every non-root bone and
non-opted clip remains byte-for-byte governed by the authored BAN keys.
================
*/
function readInPlaceHorizontalRootMotionRoles( avatar ) {
	const value = avatar.inPlaceHorizontalRootMotionRoles ?? [];
	if ( !Array.isArray( value ) || value.some( ( role ) => typeof role !== "string" || role.length === 0 ) ) {
		throw new Error( `${avatar.name}: inPlaceHorizontalRootMotionRoles must be an array of role names` );
	}
	if ( new Set( value ).size !== value.length ) {
		throw new Error( `${avatar.name}: inPlaceHorizontalRootMotionRoles contains a duplicate role` );
	}
	return new Set( value );
}

/*
================
avatarToGlb

Publish native material, skin and animation contracts with shared texture admission.
================
*/

export function avatarToGlb( avatar ) {
	const g = createGltf();
	const bones = avatar.skeleton.bones;
	const N = bones.length;
	const inPlaceHorizontalRootMotionRoles = readInPlaceHorizontalRootMotionRoles( avatar );
	const skeletonRootIndices = new Set(
		bones.flatMap( ( bone, index ) => bone.parentIndex < 0 ? [ index ] : [] )
	);

	// joint nodes with converted local TRS
	const jointNodeIndex = [];
	for ( let i = 0; i < N; i += 1 ) {
		const b = bones[i];
		const node = {
			name: b.name,
			rotation: convQuat( b.local.q ),
			translation: convPos( b.local.t ),
			children: []
		};
		g.json.nodes.push( node );
		jointNodeIndex.push( g.json.nodes.length - 1 );
	}
	for ( let i = 0; i < N; i += 1 ) {
		const pi = bones[i].parentIndex;
		if ( pi >= 0 ) g.json.nodes[jointNodeIndex[pi]].children.push( jointNodeIndex[i] );
	}

	// FK -> world bind -> inverse bind
	const worldMats = new Array( N );
	for ( let i = 0; i < N; i += 1 ) {
		const local = quatToMat4( convQuat( bones[i].local.q ), convPos( bones[i].local.t ) );
		const pi = bones[i].parentIndex;
		worldMats[i] = pi >= 0 ? mul( worldMats[pi], local ) : local;
	}
	const ibm = new Float32Array( N * 16 );
	for ( let i = 0; i < N; i += 1 ) {
		const inv = invert( worldMats[i] );
		for ( let k = 0; k < 16; k += 1 ) ibm[i * 16 + k] = inv[k];
	}

	// Merge geometry into groups. Default grouping is by material name (one draw per
	// material); parts carrying a slot label (crowd dress: "slot:torso", "part:BA", ...)
	// group by slot+material instead and the group name is exposed as the glTF mesh/node
	// name, so the runtime can address each slot mesh individually (per-instance hiding).
	const groups = new Map(); // key -> { name, rigid, positions:[], normals:[], uvs:[], joints:[], weights:[], indices:[], material }
	for ( const part of avatar.parts ) {
		const m = part.mesh;
		const mat = m.materialName || "default";
		const rigid = m.rigid === true;
		const environmentModifiers = part.environmentModifiers ?? [], bsrModifiers = part.bsrModifiers;
		const key = `${part.slot ? `${part.slot}|` : ""}${mat}|${rigid ? "rigid" : "skinned"}|${
			JSON.stringify( environmentModifiers )
		}|${JSON.stringify( bsrModifiers )}`;
		if ( !groups.has( key ) ) {
			groups.set( key, {
				name: part.slot ?? mat,
				material: mat,
				rigid,
				environmentModifiers,
				bsrModifiers,
				positions: [],
				normals: [],
				uvs: [],
				joints: [],
				weights: [],
				indices: []
			} );
		}
		const grp = groups.get( key );
		const base = grp.positions.length / 3;
		for ( let i = 0; i < m.vertexCount; i += 1 ) {
			grp.positions.push( m.positions[i * 3], m.positions[i * 3 + 1], -m.positions[i * 3 + 2] );
			grp.normals.push( m.normals[i * 3], m.normals[i * 3 + 1], -m.normals[i * 3 + 2] );
			// Retail boat meshes carry undefined UV components. Use the same
			// portable zero-coordinate policy as runtime textureCoordinate;
			// GLB must not publish NaN even though native vertex packing copies it.
			grp.uvs.push(
				Number.isFinite( m.uvs[i * 2] ) ? m.uvs[i * 2] : 0,
				Number.isFinite( m.uvs[i * 2 + 1] ) ? m.uvs[i * 2 + 1] : 0
			);
			if ( !rigid ) {
				grp.joints.push(
					part.localToGlobal[m.boneIndices[i * 2]],
					part.localToGlobal[m.boneIndices[i * 2 + 1]],
					0,
					0
				);
				grp.weights.push( m.boneWeights[i * 2], m.boneWeights[i * 2 + 1], 0, 0 );
			}
		}
		for ( let t = 0; t < m.triangleCount; t += 1 ) {
			// reverse winding for the Z flip
			grp.indices.push( base + m.indices[t * 3], base + m.indices[t * 3 + 2], base + m.indices[t * 3 + 1] );
		}
	}

	const meshGroups = [];
	// One embedded image per authored texture and one texture per image: material groups almost
	// always share the model's atlas, and embedding a copy PER GROUP used to bloat every
	// avatar GLB ~4x and cost the runtime 8 duplicate decodes/uploads per model.
	const imageIndexBySourcePath = new Map();
	const textureIndexByImage = new Map();
	// BMS meshes reference their .bmt material by name with INCONSISTENT CASE in
	// shipped data (bandit.bms says "Bandit", bandit.bmt says "bandit"; same for
	// gyo/chakji/waterghost) - the native D3D loader matched case-insensitively.
	// Exact-case first so a real case-only collision (none known) stays stable,
	// then the folded index. MONSTER-LIVE BUG-10: the exact-only get left those
	// four species' body materials textureless -> flat white in the canvas.
	const materialsByFoldedName = new Map();
	for ( const [name, info] of avatar.materials ?? [] ) {
		const folded = name.toLowerCase();
		if ( !materialsByFoldedName.has( folded ) ) materialsByFoldedName.set( folded, info );
	}
	for ( const grp of groups.values() ) {
		const matName = grp.material;
		const pos = new Float32Array( grp.positions );
		const nrm = new Float32Array( grp.normals );
		const uv = new Float32Array( grp.uvs );
		const jnt = grp.rigid ? null : new Uint16Array( grp.joints );
		const wgt = grp.rigid ? null : new Float32Array( grp.weights );
		const idx = new Uint32Array( grp.indices );
		const vcount = pos.length / 3;
		// bounds for POSITION accessor (required)
		const min = [ Infinity, Infinity, Infinity ], max = [ -Infinity, -Infinity, -Infinity ];
		for ( let i = 0; i < vcount; i += 1 ) {
			for ( let a = 0; a < 3; a += 1 ) {
				const v = pos[i * 3 + a];
				if ( v < min[a] ) min[a] = v;
				if ( v > max[a] ) max[a] = v;
			}
		}
		const aPos = g.addAccessor(
			g.addView( Buffer.from( pos.buffer, pos.byteOffset, pos.byteLength ), ARRAY_BUFFER ),
			F32,
			vcount,
			"VEC3",
			{ min, max }
		);
		const aNrm = g.addAccessor(
			g.addView( Buffer.from( nrm.buffer, nrm.byteOffset, nrm.byteLength ), ARRAY_BUFFER ),
			F32,
			vcount,
			"VEC3"
		);
		const aUv = g.addAccessor(
			g.addView( Buffer.from( uv.buffer, uv.byteOffset, uv.byteLength ), ARRAY_BUFFER ),
			F32,
			vcount,
			"VEC2"
		);
		const aJnt = jnt ?
			g.addAccessor(
				g.addView( Buffer.from( jnt.buffer, jnt.byteOffset, jnt.byteLength ), ARRAY_BUFFER ),
				U16,
				vcount,
				"VEC4"
			) :
			null;
		const aWgt = wgt ?
			g.addAccessor(
				g.addView( Buffer.from( wgt.buffer, wgt.byteOffset, wgt.byteLength ), ARRAY_BUFFER ),
				F32,
				vcount,
				"VEC4"
			) :
			null;
		const aIdx = g.addAccessor(
			g.addView( Buffer.from( idx.buffer, idx.byteOffset, idx.byteLength ), ELEMENT_ARRAY_BUFFER ),
			U32,
			idx.length,
			"SCALAR"
		);
		const texInfo = avatar.materials?.get( matName ) ?? materialsByFoldedName.get( matName.toLowerCase() );
		if ( !texInfo ) throw new Error( `${avatar.name}:${matName}: material is absent from the native BMT set` );
		const diffuseFactor = requireRgbaFactor( texInfo.colors?.diffuse, `${avatar.name}:${matName}:diffuse` );
		const ambientFactor = requireRgbaFactor( texInfo.colors?.ambient, `${avatar.name}:${matName}:ambient` );
		const matIndex = g.json.materials.length;
		const pbr = {
			// CRITICAL BRIGHTNESS PARITY: preserve native CPrimMtrl diffuse
			// losslessly for BOTH primitive and CRT/skinned consumers. Native
			// CRTModMtrl_SetDiffuseAndAmbient (sub_a8fc50) seeds +0x1f4/+0x204
			// from the current material RGB; sub_a91970 later uploads those tints.
			// Do not replace this factor with white or delete the sro* extras below.
			// This is also a carrier for gamma-space diffuse bytes, not permission
			// to use glTF/PBR color management: native-preview loaders must keep
			// useSRGBBuffers=false.
			baseColorFactor: diffuseFactor,
			metallicFactor: 0,
			roughnessFactor: 1
		};
		if ( texInfo.pngPath && !fs.existsSync( texInfo.pngPath ) ) {
			throw new Error( `${avatar.name}:${matName}: converted texture is missing at ${texInfo.pngPath}` );
		}
		if ( texInfo.pngPath ) {
			let imageIndex = imageIndexBySourcePath.get( texInfo.pngPath );
			if ( imageIndex === undefined ) {
				const texture = readCharacterTexture( texInfo.texturePath, texInfo.pngPath );
				const imgView = g.addView( texture.bytes );
				g.json.images = g.json.images ?? [];
				g.json.images.push( { bufferView: imgView, mimeType: texture.mime } );
				imageIndex = g.json.images.length - 1;
				imageIndexBySourcePath.set( texInfo.pngPath, imageIndex );
			}
			g.json.samplers = g.json.samplers ?? [ { magFilter: 9729, minFilter: 9987, wrapS: 10497, wrapT: 10497 } ];
			g.json.textures = g.json.textures ?? [];
			let textureIndex = textureIndexByImage.get( imageIndex );
			if ( textureIndex === undefined ) {
				g.json.textures.push( { source: imageIndex, sampler: 0 } );
				textureIndex = g.json.textures.length - 1;
				textureIndexByImage.set( imageIndex, textureIndex );
			}
			pbr.baseColorTexture = { index: textureIndex };
		}
		const material = {
			name: matName,
			pbrMetallicRoughness: pbr,
			extras: {
				sroAmbientFactor: ambientFactor,
				sroDiffuseFactor: diffuseFactor,
				...(texInfo.materialIndex === undefined ?
					{} :
					{
						sroMaterialIndex: texInfo.materialIndex,
						sroMaterialSet: texInfo.materialSetPath,
						...(grp.bsrModifiers ? { sroBsrModifiers: grp.bsrModifiers } : {})
					})
			}
		};
		// CPrimMtrl flag bit 0x200 = material uses the texture alpha channel (hair and
		// other cutouts) -> glTF MASK. Bit 0x1 is NOT transparency: garments with 0x141
		// carry specular/sheen data in their DXT3 alpha channel and must stay opaque
		// (verified against prim/mtrl data: hwan_hair=0x341, light_01_la=0x141).
		// Serialization of the BMT float does not prove its draw-time meaning.
		// A5C510/A5C930 -> AAE3E0 (AAE459) explicitly sets ALPHAREF to 128.
		// Using the frequently-zero serialized field admitted entire leaf cards.
		// AEE6D0 applies the owning BSR's environment modifier before material setup:
		// it replaces that baseline with reference 1 or disables alpha testing.
		const flags = texInfo.flags ?? 0;
		applyCharacterMaterialState( material, flags, grp.environmentModifiers );
		embedCharacterEnvironment( g.json, material, bytes => g.addView( bytes ), new Map() );
		g.json.materials.push( material );
		meshGroups.push( {
			name: grp.name,
			rigid: grp.rigid,
			primitive: {
				attributes: {
					POSITION: aPos,
					NORMAL: aNrm,
					TEXCOORD_0: aUv,
					...(aJnt !== null && aWgt !== null ? { JOINTS_0: aJnt, WEIGHTS_0: aWgt } : {})
				},
				indices: aIdx,
				material: matIndex
			}
		} );
	}

	// inverse bind accessor
	const aIbm = g.addAccessor(
		g.addView( Buffer.from( ibm.buffer, ibm.byteOffset, ibm.byteLength ) ),
		F32,
		N,
		"MAT4"
	);

	// One skinned mesh+node per group; the node/mesh NAME carries the slot label so the
	// runtime can address slot meshes by name after glTF import.
	g.json.skins.push( { inverseBindMatrices: aIbm, joints: jointNodeIndex, skeleton: jointNodeIndex[0] } );
	g.json.scenes[0].nodes.push( jointNodeIndex[0] );
	for ( const mg of meshGroups ) {
		const meshIndex = g.json.meshes.length;
		g.json.meshes.push( { name: mg.name, primitives: [ mg.primitive ] } );
		const meshNodeIndex = g.json.nodes.length;
		g.json.nodes.push( { name: mg.name, mesh: meshIndex, ...(mg.rigid ? {} : { skin: 0 }) } );
		g.json.scenes[0].nodes.push( meshNodeIndex );
	}

	// animations: one glTF animation per clip role ("walk", "ride", ...). The runtime VAT
	// loader keys off these names to bake a pedestrian walk loop and a seated ride pose
	// into a single concatenated vertex-animation texture per model.
	if ( !Array.isArray( avatar.clips ) ) throw new Error( `${avatar.name}: clips must be an explicit array` );
	const clips = avatar.clips;
	for ( const { role, clip: c } of clips ) {
		if ( !c ) continue;
		const { times, sourceIndices } = compileBanTimeline( c, `${avatar.name}:${role}` );
		const aTime = g.addAccessor(
			g.addView( Buffer.from( times.buffer, times.byteOffset, times.byteLength ) ),
			F32,
			times.length,
			"SCALAR",
			{ min: [ times[0] ], max: [ times[times.length - 1] ] }
		);
		const channels = [], samplers = [];
		for ( const ab of c.bones ) {
			const ji = avatar.skeleton.byName.get( ab.name );
			if ( ji === undefined ) continue;
			const node = jointNodeIndex[ji];
			const kc = sourceIndices.length;
			const rot = new Float32Array( kc * 4 );
			const tr = new Float32Array( kc * 3 );
			const bindTranslation = convPos( bones[ji].local.t );
			// Some retail BANs (penguin finger tracks) contain non-finite positions.
			// Reject the unusable translation channel as a unit; retain bind position
			// and the independent authored rotation instead of poisoning skin matrices.
			const validTranslation = sourceIndices.every( index => ab.keys[index].t.every( Number.isFinite ) );
			if ( !validTranslation ) {
				console.warn(
					`[glb] ${avatar.name}:${role}:${ab.name}: invalid BAN translation; retaining bind translation`
				);
			}
			const holderOwnsHorizontalRootMotion = skeletonRootIndices.has( ji ) &&
				inPlaceHorizontalRootMotionRoles.has( role );
			for ( let k = 0; k < kc; k += 1 ) {
				const key = ab.keys[sourceIndices[k]];
				const q = convQuat( key.q ), t = validTranslation ? convPos( key.t ) : bindTranslation;
				rot[k * 4] = q[0];
				rot[k * 4 + 1] = q[1];
				rot[k * 4 + 2] = q[2];
				rot[k * 4 + 3] = q[3];
				tr[k * 3] = holderOwnsHorizontalRootMotion ? bindTranslation[0] : t[0];
				tr[k * 3 + 1] = t[1];
				tr[k * 3 + 2] = holderOwnsHorizontalRootMotion ? bindTranslation[2] : t[2];
			}
			const aRot = g.addAccessor(
				g.addView( Buffer.from( rot.buffer, rot.byteOffset, rot.byteLength ) ),
				F32,
				kc,
				"VEC4"
			);
			const aTr = g.addAccessor(
				g.addView( Buffer.from( tr.buffer, tr.byteOffset, tr.byteLength ) ),
				F32,
				kc,
				"VEC3"
			);
			samplers.push( { input: aTime, output: aRot, interpolation: "LINEAR" } );
			channels.push( { sampler: samplers.length - 1, target: { node, path: "rotation" } } );
			samplers.push( { input: aTime, output: aTr, interpolation: "LINEAR" } );
			channels.push( { sampler: samplers.length - 1, target: { node, path: "translation" } } );
		}
		g.json.animations.push( { name: role, channels, samplers } );
	}
	// The base resource's authored pick box (CResObject +0x280); see
	// assembleAvatar. Mirrored on Z like every exported position.
	if ( avatar.aggregateBox ) {
		const [minX, minY, minZ, maxX, maxY, maxZ] = avatar.aggregateBox;
		g.json.extras ??= {};
		g.json.extras.sroAggregateBox = [ minX, minY, -maxZ, maxX, maxY, -minZ ];
	}

	return g.build();
}

// ---- CLI ----
if ( isMainScript( import.meta.url ) ) {
	const target = process.argv[2] ?? "res/char/europe/europeman_adventurer.bsr";
	const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
	// Output mirrors the native data layout: res/char/europe/<name>.bsr -> assets/char/europe/<name>.glb
	const baseName = path.basename( target ).replace( /\.bsr$/i, "" );
	const region = target.replaceAll( "\\", "/" ).match( /char\/([^/]+)\// )?.[1] ?? "europe";
	const publicOut = path.resolve(
		scriptDir,
		"..",
		"..",
		"..",
		".generated",
		"client-public",
		"assets",
		"char",
		region,
		`${baseName}.glb`
	);
	const out = process.argv[3] ?? publicOut;
	fs.mkdirSync( path.dirname( out ), { recursive: true } );
	const avatar = await assembleAvatar( target );
	const glb = avatarToGlb( avatar );
	fs.writeFileSync( out, glb );
	console.log(
		`wrote ${out} (${glb.length} bytes)  nodes=${avatar.skeleton.boneCount}+mesh prims grouped by material; anim=${
			avatar.clips.map( ( entry ) => entry.role ).join( "," ) || "none"
		}`
	);
}
