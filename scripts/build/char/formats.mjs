/*
===========================================================================

formats.mjs - native asset compilation

===========================================================================
*/
import { readMeshCloth } from "../shared/meshCloth.mjs";
import { parseModDataSection } from "../shared/bsrModifiers.mjs";
// SRO skinned-character file parsers (build-time).
// Formats reverse-engineered and validated against the native client.
//
//   JMXVRES 0109  character resource (.bsr)  -> mesh/skeleton/animation refs
//   JMXVBMS 0110  skinned mesh (.bms)        -> pos/normal/uv + 2-bone skin
//   JMXVBSK 0101  skeleton (.bsk)            -> 43 bones, local + inverse-bind
//   JMXVBAN 0102  animation (.ban)           -> per-bone quat+pos keyframes

import {
	readCountedString as readStr,
	readJmxSignature as readSig,
	readUInt32Array as readU32Array
} from "../shared/jmxBinaryReader.mjs";

const BSR_SIG = "JMXVRES 0109";
const BMS_SIG = "JMXVBMS 0110";
const BSK_SIG = "JMXVBSK 0101";
const BAN_SIG = "JMXVBAN 0102";
const BMS_VERTEX_STRIDE = 44;

// ---------------------------------------------------------------- BSR (character)
/*
================
parseCharacterBsr
================
*/
export function parseCharacterBsr( buf, where = "<bsr>" ) {
	readSig( buf, BSR_SIG, where );
	const ptr = readU32Array( buf, 0x0c, 13 );
	const name = readStr( buf, 0x44 ).value;
	const modData = ptr[6] ?
		parseModDataSection( buf, ptr[6] ) :
		{
			next: 0,
			modifierSets: [],
			soundModifiers: [],
			particleModifiers: [],
			materialModifiers: [],
			textureModifiers: [],
			environmentModifiers: []
		};

	const meshPaths = readCountedStringList( buf, ptr[1] );

	// Skeleton section @ptr[2]: u32 count, then count x {string bskPath, string attachBone}.
	// attachBone is the WEARER bone the item's whole bone branch is parented to (RE:
	// CRTBranch_LinkToParentBranch sub_abc680 reads the string the loader stashed at
	// CResSkeleton+0x58 and links the branch root socket under the wearer socket of that
	// name). Ornament items (shoulder orbs...) carry "Bip01 Neck1"/"Bip01 Neck"; plain
	// characters and most items leave it empty.
	const skeletons = [];
	if ( ptr[2] && ptr[2] + 4 <= buf.length ) {
		let p = ptr[2];
		const count = buf.readUInt32LE( p );
		p += 4;
		for ( let i = 0; i < count && p + 4 <= buf.length; i += 1 ) {
			const sPath = readStr( buf, p );
			p = sPath.nextOffset;
			const sAttach = readStr( buf, p );
			p = sAttach.nextOffset;
			skeletons.push( { path: sPath.value, attachBone: sAttach.value } );
		}
	}

	// Animation list @ptr[3]: u32 flagA, u32 flagB, u32 count, then `count` paths.
	const animationPaths = [];
	if ( ptr[3] && ptr[3] + 12 <= buf.length ) {
		let p = ptr[3] + 8;
		const count = buf.readUInt32LE( p );
		p += 4;
		for ( let i = 0; i < count && p + 4 <= buf.length; i += 1 ) {
			const s = readStr( buf, p );
			animationPaths.push( s.value );
			p = s.nextOffset;
		}
	}
	return {
		name,
		meshPaths,
		skeletonPath: skeletons[0]?.path ?? null,
		// Wearer bone the item's private bone branch attaches to ("" = none).
		skeletonAttachBone: skeletons[0]?.attachBone ?? "",
		animationPaths,
		animationSets: parseAnimationSets( buf, ptr, animationPaths ),
		// CModDataSet kind/state records carrying the authored animation sound
		// callbacks. Track keys are native cursor units (the same millisecond-like
		// integer timeline consumed by CRTAnimation_DispatchEventsInFrameRange).
		modifierSets: modData.modifierSets ?? [],
		soundModifiers: modData.soundModifiers,
		// Preserve the complete serialized ModDataParticle payload. Field-offset
		// names deliberately avoid assigning unproved transform/attachment meaning.
		particleModifiers: modData.particleModifiers,
		materialModifiers: modData.materialModifiers,
		textureModifiers: modData.textureModifiers,
		// ModDataEnvMap is separate from texture animation and particle payloads.
		environmentModifiers: modData.environmentModifiers ?? [],
		// {coverKey -> prim index}: which naked-skin prim each cover key hides.
		partLink: parseAttachPartLink( buf )
	};
}

// Native animation-set section @ptr[5]. RE:
//   CRes_LoadAnimationSetGroups -> CResAnimationSet_LoadStateRecords.
// Each named set owns state records. The first two fields are the state id and
// animation slot. CAnimationState_ReadTransitionMap (sub_a6b360) loads the next
// 16-byte records into CResAnimationStateTable+0x04; sub_ae0280 walks that map
// and raises the authored animation events. The final vector/scalar is the
// normalized-time warp curve consumed by sub_adf080, not another event list.
/*
================
parseAnimationSets
================
*/
function parseAnimationSets( buf, ptr, animationPaths ) {
	const off = ptr[5];
	const end = ptr[6] || buf.length;
	if ( !off || off + 4 > end || end > buf.length ) return [];
	let p = off;
	const setCount = buf.readUInt32LE( p );
	p += 4;
	if ( setCount > 256 ) return [];

	const sets = [];
	for ( let setIndex = 0; setIndex < setCount && p + 4 <= end; setIndex += 1 ) {
		const name = readStr( buf, p );
		p = name.nextOffset;
		if ( p + 4 > end ) break;
		const stateCount = buf.readUInt32LE( p );
		p += 4;
		if ( stateCount > 512 ) break;
		const states = [];
		for ( let stateIndex = 0; stateIndex < stateCount && p + 12 <= end; stateIndex += 1 ) {
			const stateId = buf.readUInt32LE( p );
			p += 4;
			const animationIndex = buf.readInt32LE( p );
			p += 4;
			const animationPath = animationIndex >= 0 && animationIndex < animationPaths.length ?
				animationPaths[animationIndex] :
				null;

			const transitionCount = buf.readUInt32LE( p );
			p += 4;
			const trackEvents = [];
			for ( let eventIndex = 0; eventIndex < transitionCount; eventIndex += 1 ) {
				if ( p + 16 > end ) {
					throw new Error( `truncated animation track event in ${stateId}` );
				}
				trackEvents.push( {
					cursorMs: buf.readInt32LE( p ),
					eventCode: buf.readInt32LE( p + 4 ),
					param0: buf.readInt32LE( p + 8 ),
					param1: buf.readInt32LE( p + 12 )
				} );
				p += 16;
			}
			if ( p + 4 > end ) break;

			const timeWarpPointCount = buf.readUInt32LE( p );
			p += 4;
			let timeWarpScale = 0;
			const timeWarpPoints = [];
			if ( timeWarpPointCount > 0 ) {
				if ( p + 4 + timeWarpPointCount * 8 > end ) {
					throw new Error( `truncated animation time-warp curve in ${stateId}` );
				}
				// sub_a6ad40 uses FLD/FSTP for the scalar and memcpy-loads `count`
				// pairs into the float curve evaluated by sub_a66a40.
				timeWarpScale = buf.readFloatLE( p );
				p += 4;
				for ( let pointIndex = 0; pointIndex < timeWarpPointCount; pointIndex += 1 ) {
					timeWarpPoints.push( {
						input: buf.readFloatLE( p ),
						output: buf.readFloatLE( p + 4 )
					} );
					p += 8;
				}
			}
			if ( p > end ) break;

			states.push( {
				stateId,
				animationIndex,
				animationPath,
				trackEvents,
				timeWarpScale,
				timeWarpPoints
			} );
		}
		sets.push( { name: name.value, states } );
	}
	return sets;
}

// ------------------------------------------------- attach part-link tail (native dress)
//
// RE (SRO_Client v1.150): CResAttachable::Load (sub_a508a0) reads, after all other
// sections, `u32 a, u32 b, u32 c, u32 count, count x {u32 key, u32 value}` into a
// per-resource vector (header lands at this[0xc3..0xc5], entries are 12-byte runtime
// triplets {key, value, &this[0xc3]}). CCompChar::OnAttachItem (sub_a89bd0, via attach
// vfunc sub_a89ed0) then walks the worn item's pairs and HIDES (sub_aa0d40) every char
// prim whose pair key matches; detach (sub_a89cd0) re-shows everything and re-applies
// the remaining items. So:
//   - char .bsr tail pairs  = { coverKey -> prim index } (its hideable skin prims;
//     face carries no key and can never be hidden),
//   - item .bsr tail pairs  = { coverKey -> 0 } (the body regions this item covers),
//   - `b` on items is the equip slot id (HA=0 BA=1 LA=2 FA=3 SA=4 AA=5),
//   - `c` is the GATE sub_a89bd0 actually checks (result_2[2] == this[0xc5]): the
//     cover MODE. c=1 = replacement armor -> hide matching skin prims; c=2 = layered
//     accessory (bracers AA, shoulder pads SA) -> drawn OVER the skin, hides NOTHING.
//   - `a` is always 1 on items and is NOT the gate (an earlier read of sub_a89bd0
//     mistook it for one; it only "worked" because most parts also have c=1).
// Char resources (CResChar::Load sub_a89410) read one extra trailing u32 (nComboNum,
// asserted 0) after the table; items end exactly at the table.
//
// The native loader reads the tail SEQUENTIALLY: CRes_LoadSections (sub_a4ff00) ends
// by seeking ptr[6] and reading the ModDataSet section (CModDataSetList_Load
// sub_a75c60), leaving the stream cursor right where the part-link tail starts. There
// is no header pointer to the tail, so the only correct way to locate it is to walk
// section 6 byte-exactly the way the client does. Every IModData subclass Load was
// RE'd for this (IModData_LoadBase sub_ac55b0 + 12 typed payload readers); the walk
// below lands exactly at EOF(-trailing) on 3929/3930 BSRs in the corpus (lone
// exception: npc/npc/tt.bsr, a leftover authored "JMXVRES 0107" test file).
/*
================
parseAttachPartLink
================
*/
export function parseAttachPartLink( buf ) {
	const ptr6 = buf.readUInt32LE( 0x0c + 6 * 4 );
	if ( !ptr6 || ptr6 >= buf.length ) return null;
	let p = parseModDataSection( buf, ptr6 ).next;
	if ( p === buf.length ) return null; // no part-link tail
	const a = buf.readUInt32LE( p );
	const b = buf.readUInt32LE( p + 4 );
	const c = buf.readUInt32LE( p + 8 );
	const count = buf.readUInt32LE( p + 12 );
	p += 16;
	const pairs = [];
	for ( let i = 0; i < count; i += 1 ) {
		pairs.push( { key: buf.readUInt32LE( p ), value: buf.readUInt32LE( p + 4 ) } );
		p += 8;
	}
	// Items end exactly at the table; characters carry one trailing u32 (nComboNum).
	const remaining = buf.length - p;
	if ( remaining !== 0 && remaining !== 4 ) {
		throw new Error(
			`parseAttachPartLink: tail mismatch (cursor ${p}, ${remaining}B left, EOF ${buf.length})`
		);
	}
	return { a, b, c, pairs };
}

/*
================
readCountedStringList
================
*/
function readCountedStringList( buf, off ) {
	if ( !off || off + 4 > buf.length ) return [];
	const count = buf.readUInt32LE( off );
	if ( count > 4096 ) return [];
	let p = off + 4;
	const out = [];
	for ( let i = 0; i < count && p + 4 <= buf.length; i += 1 ) {
		const s = readStr( buf, p );
		out.push( s.value );
		p = s.nextOffset;
	}
	return out;
}

// ---------------------------------------------------------------- skinned BMS
/*
================
parseSkinnedBms
================
*/
export function parseSkinnedBms( buf, where = "<bms>" ) {
	readSig( buf, BMS_SIG, where );
	const ptr = readU32Array( buf, 0x0c, 12 );

	// metadata @0x3c: u32,u32,u32, meshName, materialName, then u32 vertexFlags
	let o = 0x3c + 12;
	const meshName = readStr( buf, o );
	o = meshName.nextOffset;
	const materialName = readStr( buf, o );
	o = materialName.nextOffset;

	// vertices
	const vOff = ptr[0];
	const vertexCount = buf.readUInt32LE( vOff );
	const positions = new Float32Array( vertexCount * 3 );
	const normals = new Float32Array( vertexCount * 3 );
	const uvs = new Float32Array( vertexCount * 2 );
	for ( let i = 0; i < vertexCount; i += 1 ) {
		const b = vOff + 4 + i * BMS_VERTEX_STRIDE;
		positions[i * 3] = buf.readFloatLE( b );
		positions[i * 3 + 1] = buf.readFloatLE( b + 4 );
		positions[i * 3 + 2] = buf.readFloatLE( b + 8 );
		normals[i * 3] = buf.readFloatLE( b + 12 );
		normals[i * 3 + 1] = buf.readFloatLE( b + 16 );
		normals[i * 3 + 2] = buf.readFloatLE( b + 20 );
		uvs[i * 2] = buf.readFloatLE( b + 24 );
		uvs[i * 2 + 1] = buf.readFloatLE( b + 28 );
	}

	// skin @ptr[1]: u32 boneCount, counted bone names, then 6 bytes/vertex.
	// Native MeshBatch_BuildVertexRunsType1Skinned (sub_a4ecb0) treats an
	// absent/empty weight table as rigid geometry. Several shipped NPC props
	// use exactly ptr[1] == ptr[2] - 4: the zero count is the entire section.
	const boneNames = [];
	let sp = ptr[1];
	const boneCount = buf.readUInt32LE( sp );
	sp += 4;
	for ( let i = 0; i < boneCount; i += 1 ) {
		const s = readStr( buf, sp );
		boneNames.push( s.value );
		sp = s.nextOffset;
	}
	// per-vertex skin: {u8 bone0, u16 w0, u8 bone1, u16 w1}
	const boneIndices = new Uint16Array( vertexCount * 2 );
	const boneWeights = new Float32Array( vertexCount * 2 );
	const rigid = boneCount === 0;
	if ( !rigid ) {
		for ( let i = 0; i < vertexCount; i += 1 ) {
			const b = sp + i * 6;
			const b0 = buf.readUInt8( b );
			const w0 = buf.readUInt16LE( b + 1 );
			const b1 = buf.readUInt8( b + 3 );
			const w1 = buf.readUInt16LE( b + 4 );
			const has1 = b1 !== 0xff;
			const wf0 = w0;
			const wf1 = has1 ? w1 : 0;
			const sum = wf0 + wf1 || 1;
			boneIndices[i * 2] = b0;
			boneIndices[i * 2 + 1] = has1 ? b1 : b0;
			boneWeights[i * 2] = wf0 / sum;
			boneWeights[i * 2 + 1] = wf1 / sum;
		}
	}

	// indices @ptr[2]: u32 triangleCount, then triangleCount*3 u16
	const iOff = ptr[2];
	const triangleCount = buf.readUInt32LE( iOff );
	const indices = new Uint16Array( triangleCount * 3 );
	for ( let i = 0; i < indices.length; i += 1 ) indices[i] = buf.readUInt16LE( iOff + 4 + i * 2 );

	return {
		meshName: meshName.value,
		materialName: materialName.value,
		vertexCount,
		triangleCount,
		positions,
		normals,
		uvs,
		boneNames,
		boneIndices,
		boneWeights,
		indices,
		rigid,
		cloth: readMeshCloth( buf, ptr, vertexCount )
	};
}

// ---------------------------------------------------------------- BSK skeleton
/*
================
parseBsk
================
*/
export function parseBsk( buf, where = "<bsk>" ) {
	readSig( buf, BSK_SIG, where );
	let p = 0x0c;
	const boneCount = buf.readUInt32LE( p );
	p += 4;
	/** @type {Array<{ index: number, type: number, name: string, parent: string, local: { q: number[], t: number[] }, world: { q: number[], t: number[] }, invWorld: { q: number[], t: number[] }, parentIndex?: number }>} */
	const bones = [];
	for ( let b = 0; b < boneCount; b += 1 ) {
		const type = buf.readUInt8( p );
		p += 1;
		const name = readStr( buf, p );
		p = name.nextOffset;
		const parent = readStr( buf, p );
		p = parent.nextOffset;
		const local = readTransform( buf, p );
		p += 28; // T0 parent-local
		const world = readTransform( buf, p );
		p += 28; // T1 world bind
		const invWorld = readTransform( buf, p );
		p += 28; // T2 inverse world bind
		const childCount = buf.readUInt32LE( p );
		p += 4;
		for ( let c = 0; c < childCount; c += 1 ) {
			const cn = readStr( buf, p );
			p = cn.nextOffset;
		}
		bones.push( { index: b, type, name: name.value, parent: parent.value, local, world, invWorld } );
	}
	// resolve parent indices by name
	const byName = new Map( bones.map( ( bn ) => [ bn.name, bn.index ] ) );
	for ( const bn of bones ) bn.parentIndex = bn.parent ? (byName.get( bn.parent ) ?? -1) : -1;
	return { boneCount, bones, byName };
}

/*
================
readTransform
================
*/
function readTransform( buf, off ) {
	return {
		q: [
			buf.readFloatLE( off ),
			buf.readFloatLE( off + 4 ),
			buf.readFloatLE( off + 8 ),
			buf.readFloatLE( off + 12 )
		],
		t: [ buf.readFloatLE( off + 16 ), buf.readFloatLE( off + 20 ), buf.readFloatLE( off + 24 ) ]
	};
}

// ---------------------------------------------------------------- BAN animation
/*
================
parseBan
================
*/
export function parseBan( buf, where = "<ban>" ) {
	readSig( buf, BAN_SIG, where );
	let p = 0x0c;
	p += 4; // flagA
	p += 4; // flagB
	const name = readStr( buf, p );
	p = name.nextOffset;
	const durationMs = buf.readUInt32LE( p );
	p += 4;
	const field1 = buf.readUInt32LE( p );
	p += 4;
	const field2 = buf.readUInt32LE( p );
	p += 4;
	const frameCount = buf.readUInt32LE( p );
	p += 4;
	const frameTimesMs = readU32Array( buf, p, frameCount );
	p += frameCount * 4;
	const animBoneCount = buf.readUInt32LE( p );
	p += 4;
	const bones = [];
	for ( let b = 0; b < animBoneCount; b += 1 ) {
		const bn = readStr( buf, p );
		p = bn.nextOffset;
		const keyCount = buf.readUInt32LE( p );
		p += 4;
		const keys = [];
		for ( let k = 0; k < keyCount; k += 1 ) {
			keys.push( readTransform( buf, p ) );
			p += 28;
		}
		bones.push( { name: bn.value, keyCount, keys } );
	}
	return { name: name.value, durationMs, field1, field2, frameCount, frameTimesMs, animBoneCount, bones };
}
