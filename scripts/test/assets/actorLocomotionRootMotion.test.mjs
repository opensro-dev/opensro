/*
===========================================================================

actorLocomotionRootMotion.test.mjs - published actors move in place

Every actor model the client drives by its holder (NPCs, monsters, COS and
avatars) must export walk and run without horizontal root motion, since
the simulation owns position. Also pins the resource censuses so a new or
removed locomotion resource is reviewed, and the Baroi VAT contract.

Needs the full asset build (.generated/client-public).

===========================================================================
*/
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { avatarToGlb } from "../../build/char/exportGlb.mjs";

const rebuildRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", "..", ".." );
const publicRoot = path.join( rebuildRoot, ".generated", "client-public" );

/*
================
publicAssetPath
================
*/
function publicAssetPath( publicPath ) {
	assert.match( publicPath, /^\/assets\//, `invalid public asset path ${publicPath}` );
	return path.join( publicRoot, ...publicPath.slice( 1 ).split( "/" ) );
}

/*
================
readGlb
================
*/
function readGlb( glb ) {
	assert.equal( glb.readUInt32LE( 0 ), 0x46546c67, "fixture output must be a GLB" );
	const jsonLength = glb.readUInt32LE( 12 );
	const json = JSON.parse( glb.subarray( 20, 20 + jsonLength ).toString( "utf8" ).trim() );
	const binaryHeaderOffset = 20 + jsonLength;
	const binaryLength = glb.readUInt32LE( binaryHeaderOffset );
	const binary = glb.subarray( binaryHeaderOffset + 8, binaryHeaderOffset + 8 + binaryLength );
	return { json, binary };
}

/*
================
readFloatAccessor
================
*/
function readFloatAccessor( document, binary, accessorIndex ) {
	const accessor = document.accessors[accessorIndex];
	assert.equal( accessor.componentType, 5126, "translation accessor must use float32" );
	const componentCount = { SCALAR: 1, VEC2: 2, VEC3: 3, VEC4: 4, MAT4: 16 }[accessor.type];
	assert.ok( componentCount, `unsupported accessor type ${accessor.type}` );
	const view = document.bufferViews[accessor.bufferView];
	const byteOffset = (view.byteOffset ?? 0) + (accessor.byteOffset ?? 0);
	return Array.from(
		new Float32Array(
			binary.buffer,
			binary.byteOffset + byteOffset,
			accessor.count * componentCount
		)
	);
}

/*
================
readFloat16
================
*/
function readFloat16( buffer, floatIndex ) {
	const bits = buffer.readUInt16LE( floatIndex * 2 );
	const sign = (bits & 0x8000) === 0 ? 1 : -1;
	const exponent = (bits >>> 10) & 0x1f;
	const fraction = bits & 0x03ff;
	if ( exponent === 0 ) return sign * (2 ** -14) * (fraction / 1024);
	if ( exponent === 0x1f ) return fraction === 0 ? sign * Infinity : Number.NaN;
	return sign * (2 ** (exponent - 15)) * (1 + fraction / 1024);
}

/*
================
translationKeysForRole
================
*/
function translationKeysForRole( glb, role ) {
	const { json, binary } = readGlb( glb );
	const animation = json.animations.find( ( candidate ) => candidate.name === role );
	assert.ok( animation, `${role} animation is absent` );
	const channel = animation.channels.find(
		( candidate ) => candidate.target.node === 0 && candidate.target.path === "translation"
	);
	assert.ok( channel, `${role} root translation channel is absent` );
	return readFloatAccessor( json, binary, animation.samplers[channel.sampler].output );
}

/*
================
assertHorizontalLocomotionInPlace
================
*/
function assertHorizontalLocomotionInPlace( glb, label ) {
	const { json, binary } = readGlb( glb );
	const locomotionAnimations = (json.animations ?? []).filter(
		( candidate ) => candidate.name === "walk" || candidate.name === "run"
	);
	if ( locomotionAnimations.length === 0 ) return 0;
	const rootNode = json.skins?.[0]?.skeleton;
	assert.equal( typeof rootNode, "number", `${label} has no skeleton root` );
	let checkedRoles = 0;
	for ( const role of [ "walk", "run" ] ) {
		const animation = locomotionAnimations.find( ( candidate ) => candidate.name === role );
		if ( !animation ) continue;
		const channel = animation.channels.find(
			( candidate ) => candidate.target.node === rootNode && candidate.target.path === "translation"
		);
		// A clip without a root translation channel stays at bind pose and is
		// already in-place by construction.
		if ( !channel ) {
			checkedRoles += 1;
			continue;
		}
		const translations = readFloatAccessor( json, binary, animation.samplers[channel.sampler].output );
		const horizontalX = [];
		const horizontalZ = [];
		for ( let offset = 0; offset < translations.length; offset += 3 ) {
			horizontalX.push( translations[offset] );
			horizontalZ.push( translations[offset + 2] );
		}
		assert.ok(
			Math.max( ...horizontalX ) - Math.min( ...horizontalX ) <= 1e-6,
			`${label} ${role} root X contains duplicate holder travel`
		);
		assert.ok(
			Math.max( ...horizontalZ ) - Math.min( ...horizontalZ ) <= 1e-6,
			`${label} ${role} root Z contains duplicate holder travel`
		);
		checkedRoles += 1;
	}
	return checkedRoles;
}

/*
================
makeClip
================
*/
function makeClip( role ) {
	return {
		role,
		clip: {
			frameCount: 2,
			frameTimesMs: [ 0, 1000 ],
			bones: [ {
				name: "Root",
				keyCount: 2,
				keys: [
					{ q: [ 0, 0, 0, 1 ], t: [ 1, 11, 2 ] },
					{ q: [ 0, 0, 0, 1 ], t: [ 3, 12, 4 ] }
				]
			} ]
		}
	};
}

test("Mission-owned locomotion keeps pose height but removes duplicate root travel", () => {
	const glb = avatarToGlb( {
		name: "root-motion-owner-fixture",
		skeleton: {
			boneCount: 1,
			bones: [ {
				name: "Root",
				parentIndex: -1,
				local: { q: [ 0, 0, 0, 1 ], t: [ 5, 10, 7 ] }
			} ],
			byName: new Map( [ [ "Root", 0 ] ] )
		},
		parts: [],
		materials: new Map(),
		clips: [ makeClip( "walk" ), makeClip( "attack1" ) ],
		inPlaceHorizontalRootMotionRoles: [ "walk" ]
	} );

	assert.deepEqual(
		translationKeysForRole( glb, "walk" ),
		[ 5, 11, -7, 5, 12, -7 ],
		"the holder owns X/Z while the authored vertical pose remains intact"
	);
	assert.deepEqual(
		translationKeysForRole( glb, "attack1" ),
		[ 1, 11, -2, 3, 12, -4 ],
		"non-locomotion root motion must remain authored"
	);
});

test("root-motion role policy rejects malformed ownership declarations", () => {
	const base = {
		name: "invalid-root-motion-policy",
		skeleton: {
			boneCount: 1,
			bones: [ { name: "Root", parentIndex: -1, local: { q: [ 0, 0, 0, 1 ], t: [ 0, 0, 0 ] } } ],
			byName: new Map( [ [ "Root", 0 ] ] )
		},
		parts: [],
		materials: new Map(),
		clips: []
	};

	assert.throws(
		() => avatarToGlb( { ...base, inPlaceHorizontalRootMotionRoles: [ "walk", "walk" ] } ),
		/contains a duplicate role/
	);
	assert.throws(
		() => avatarToGlb( { ...base, inPlaceHorizontalRootMotionRoles: "walk" } ),
		/must be an array of role names/
	);
});

test("published Baroi locomotion is horizontally in-place", () => {
	const glb = fs.readFileSync(
		path.join( rebuildRoot, ".generated", "client-public", "assets", "npc", "mob", "europe", "baroi.glb" )
	);
	const { json, binary } = readGlb( glb );
	const rootNode = json.skins[0].skeleton;
	const rootBindTranslation = json.nodes[rootNode].translation;

	for ( const role of [ "walk", "run" ] ) {
		const animation = json.animations.find( ( candidate ) => candidate.name === role );
		assert.ok( animation, `Baroi ${role} animation is absent` );
		const channel = animation.channels.find(
			( candidate ) => candidate.target.node === rootNode && candidate.target.path === "translation"
		);
		assert.ok( channel, `Baroi ${role} root translation channel is absent` );
		const translations = readFloatAccessor( json, binary, animation.samplers[channel.sampler].output );
		const verticalValues = [];
		for ( let offset = 0; offset < translations.length; offset += 3 ) {
			assert.ok(
				Math.abs( translations[offset] - rootBindTranslation[0] ) <= 1e-6,
				`Baroi ${role} root X escaped holder ownership at key ${offset / 3}`
			);
			assert.ok(
				Math.abs( translations[offset + 2] - rootBindTranslation[2] ) <= 1e-6,
				`Baroi ${role} root Z escaped holder ownership at key ${offset / 3}`
			);
			verticalValues.push( translations[offset + 1] );
		}
		assert.ok(
			Math.max( ...verticalValues ) - Math.min( ...verticalValues ) > 0.01,
			`Baroi ${role} lost its authored vertical pose motion`
		);
	}
});

// Reviewed 2026-09-28. COS: the 22 pet models published since every enabled
// COS reference is baked (npcModelRoster.mjs enabledCosReferences), each with
// walk and run. Actors: 148, including MOB_DH_SOLDIEREARTHGHOST, which the
// v1.150 client places and a quest needs; the server keeps it spawnable
// despite the v1.188 shard's all-zero caps (monster laterDisabledCodenames).
const COS_LOCOMOTION_RESOURCES = 22;
const COS_LOCOMOTION_ROLES = 44;

test("every holder-driven actor resource exports in-place locomotion", () => {
	const npcManifest = JSON.parse(
		fs.readFileSync( path.join( publicRoot, "assets", "npc", "manifest.json" ), "utf8" )
	);
	const avatarRoster = JSON.parse(
		fs.readFileSync( path.join( publicRoot, "assets", "char", "roster.json" ), "utf8" )
	);
	// COS (growth pets, transports) are counted apart from NPCs, monsters and
	// avatars, so each census still flags a new or removed resource.
	const resources = new Map();
	for ( const entry of Object.values( npcManifest.models ) ) {
		if ( !entry.glb || resources.has( entry.glb ) ) continue;
		resources.set( entry.glb, { label: `NPC resource ${entry.glb}`, cos: entry.kind === "cos" } );
	}
	for ( const entry of avatarRoster.models ) {
		if ( entry.glb ) resources.set( entry.glb, { label: `avatar resource ${entry.glb}`, cos: false } );
	}

	const census = { actor: { resources: 0, roles: 0 }, cos: { resources: 0, roles: 0 } };
	for ( const [publicPath, { label, cos }] of resources ) {
		const roleCount = assertHorizontalLocomotionInPlace(
			fs.readFileSync( publicAssetPath( publicPath ) ),
			label
		);
		const bucket = cos ? census.cos : census.actor;
		if ( roleCount > 0 ) bucket.resources += 1;
		bucket.roles += roleCount;
	}
	assert.deepEqual(
		census.actor,
		{ resources: 148, roles: 296 },
		"the holder-driven actor census changed; review every new or removed locomotion resource"
	);
	assert.deepEqual(
		census.cos,
		{ resources: COS_LOCOMOTION_RESOURCES, roles: COS_LOCOMOTION_ROLES },
		"the COS locomotion census changed; review every new or removed pet or transport resource"
	);
});

test("published Baroi VAT preserves the in-place locomotion contract", () => {
	const vatDirectory = path.join(
		rebuildRoot,
		".generated",
		"client-public",
		"assets",
		"npc",
		"vat",
		"mob",
		"europe"
	);
	const manifest = JSON.parse( fs.readFileSync( path.join( vatDirectory, "baroi.vat.json" ), "utf8" ) );
	const binary = fs.readFileSync( path.join( vatDirectory, "baroi.vat.bin" ) );
	assert.equal(
		binary.byteLength,
		manifest.texture.frameCount * manifest.texture.floatsPerFrame * 2,
		"Baroi VAT must use the declared float16 matrix layout"
	);

	for ( const role of [ "walk", "run" ] ) {
		const clip = manifest.clips[role];
		assert.ok( clip, `Baroi VAT ${role} range is absent` );
		const horizontalX = [];
		const horizontalZ = [];
		const verticalY = [];
		for ( let frame = clip.startFrame; frame <= clip.endFrame; frame += 1 ) {
			const rootMatrixOffset = frame * manifest.texture.floatsPerFrame;
			horizontalX.push( readFloat16( binary, rootMatrixOffset + 12 ) );
			verticalY.push( readFloat16( binary, rootMatrixOffset + 13 ) );
			horizontalZ.push( readFloat16( binary, rootMatrixOffset + 14 ) );
		}
		assert.ok(
			Math.max( ...horizontalX ) - Math.min( ...horizontalX ) <= 1e-6,
			`Baroi VAT ${role} root X contains duplicate path travel`
		);
		assert.ok(
			Math.max( ...horizontalZ ) - Math.min( ...horizontalZ ) <= 1e-6,
			`Baroi VAT ${role} root Z contains duplicate path travel`
		);
		assert.ok(
			Math.max( ...verticalY ) - Math.min( ...verticalY ) > 0.01,
			`Baroi VAT ${role} lost its authored vertical pose motion`
		);
	}
});
