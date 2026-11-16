/*
===========================================================================

sceneryBones.mjs - bind-pose matrices for bone-attached scenery emitters

A world object's ambient ModDataParticle entry with field00 == 0 names a
bone of the object's own skeleton (the character path reads the same
field: model-particles.ts, a non-zero field00 is the root). The client
needs that bone's matrix to place the emitter, and the .bsk is a build
input, so the publisher resolves it here once and writes it on the entry
as boneMatrix: the bone's world bind transform (BSK T1), column-major in
native model space.

Outdoor world objects draw their skinned BMS parts static, at the bind
pose their vertices are stored in, so the bind matrix puts the emitter on
the bone the player sees. A bone the skeleton lacks gets no matrix: the
native socket update keeps the parent matrix when a socket is missing
(CRTSocket_UpdateOrdinaryMatrices AB68C0), and the client applies that
fallback.

===========================================================================
*/
import { parseBsk, parseCharacterBsr } from "../../char/formats.mjs";

/*
================
nativeBoneMatrix

A BSK quaternion (x, y, z, w) and translation as a column-major matrix.
The quaternion is native and applies to column vectors as stored, the
same reading exportGlb's quatToMat4 makes before its Z mirror.
================
*/
export function nativeBoneMatrix( q, t ) {
	const [x, y, z, w] = q;
	const x2 = x + x, y2 = y + y, z2 = z + z;
	const xx = x * x2, xy = x * y2, xz = x * z2;
	const yy = y * y2, yz = y * z2, zz = z * z2;
	const wx = w * x2, wy = w * y2, wz = w * z2;
	return [
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
		t[0],
		t[1],
		t[2],
		1
	].map( Math.fround );
}

/*
================
hasSceneryBoneEntries
================
*/
export function hasSceneryBoneEntries( modifiers ) {
	return (modifiers?.particleModifiers ?? []).some( m => m.entries.some( e => e.field00 === 0 && e.boneName ) );
}

/*
================
loadScenerySkeleton

The parsed .bsk a BSR names, or null when it names none. loadAsset( path )
reads one game asset.
================
*/
export async function loadScenerySkeleton( buffer, sourcePath, loadAsset ) {
	const { skeletonPath } = parseCharacterBsr( buffer, sourcePath );
	return skeletonPath ? parseBsk( await loadAsset( skeletonPath ), skeletonPath ) : null;
}

/*
================
bindSceneryParticleBones

Returns the modifiers with boneMatrix written on every bone-attached
ambient particle entry whose bone the skeleton holds. Root entries and
bones the skeleton lacks are left as authored.
================
*/
export function bindSceneryParticleBones( modifiers, skeleton ) {
	if ( !skeleton || !hasSceneryBoneEntries( modifiers ) ) return modifiers;
	/*
	================
	bind
	================
	*/
	const bind = entry => {
		if ( entry.field00 !== 0 || !entry.boneName ) return entry;
		const index = skeleton.byName.get( entry.boneName );
		if ( index === undefined ) return entry;
		const { q, t } = skeleton.bones[index].world;
		return { ...entry, boneMatrix: nativeBoneMatrix( q, t ) };
	};
	return {
		...modifiers,
		particleModifiers: modifiers.particleModifiers.map( m => ({ ...m, entries: m.entries.map( bind ) }) )
	};
}
