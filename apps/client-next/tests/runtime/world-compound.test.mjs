/*
===========================================================================

world-compound.test.mjs - tests for world.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { parseCompound } from "../../../../scripts/build/world/objects/compound.mjs";
import { dataExtractedRoot } from "../../../../scripts/build/world/paths.mjs";

const { createWorldDecoder } = await import(
	sourceFileUrl( path.join( root, "src/engine/runtime/assets/worker/world/world.ts" ) ).href
);

test("installed waterfall CPD retains all five ordered BSR children and rejects damaged offsets", () => {
	const bytes = fs.readFileSync(
		path.join( dataExtractedRoot, "compound/particle/cj_waterfall01.cpd" )
	);
	const parsed = parseCompound( bytes );
	assert.equal( parsed.branches.length, 5 );
	assert.equal( parsed.branches[0], "res/nature/particle/oa_ho_waterfall01_01-1.bsr" );
	assert.equal( parsed.branches[4], "res/nature/particle/oa_ho_waterfall01_05.bsr" );
	for ( const length of [ 0, 11, 39, 74, bytes.length - 1 ] ) {
		assert.throws( () => parseCompound( bytes.subarray( 0, length ) ) );
	}
	const huge = Buffer.from( bytes );
	huge.writeUInt32LE( 513, 74 );
	assert.throws( () => parseCompound( huge ), /budget/ );
	const traversal = Buffer.from( bytes );
	traversal.write( "../", 82 );
	assert.throws( () => parseCompound( traversal ), /path/ );
});

test("compound children keep independent materials, share placement/fade, and fail as a unit", () => {
	const mesh = ( name ) => ({
		sourcePath: name,
		positions: [ 0, 0, 0, 1, 0, 0, 0, 1, 0 ],
		normals: [ 0, 1, 0, 0, 1, 0, 0, 1, 0 ],
		uvs: [ 0, 0, 1, 0, 0, 1 ],
		indices: [ 0, 1, 2 ],
		bounds: { min: [ 0, 0, 0 ], max: [ 1, 1, 0 ] },
		metadata: { materialName: "shared name" }
	});
	const branch = ( n ) => ({ sourcePath: `${n}.bsr`, meshPaths: [ `${n}.bms` ], materialPaths: [ `${n}.bmt` ] });
	const branches = [ branch( "a" ), branch( "b" ) ];
	const b = {
		source: { sectorX: 1, sectorY: 1 },
		terrain: { blocks: [] },
		terrainTextures: { tileCatalog: { referencedTiles: [] } },
		objects: {
			placements: [ { objectId: 1, uid: 1, regionId: "257", position: { x: 30, y: 40, z: 50 }, yaw: 0 } ],
			resources: {
				bsr: [ {
					objectId: 1,
					sourcePath: "compound.cpd",
					branches,
					meshPaths: [ "a.bms", "b.bms" ],
					materialPaths: [ "a.bmt", "b.bmt" ]
				} ],
				meshes: [ mesh( "a.bms" ), mesh( "b.bms" ) ],
				materialSets: [ "a", "b" ].map( n => ({
					sourcePath: `${n}.bmt`,
					materials: [ {
						name: "shared name",
						flags: 0,
						texturePublicPath: `${n}.png`,
						colors: { diffuse: [ 1, 1, 1, 1 ] }
					} ]
				}) )
			}
		}
	};
	branches[0].modifiers = {
		materialModifiers: [],
		textureModifiers: [],
		particleModifiers: [ {
			kind: 2,
			stateId: -1,
			animationSetName: "ambient",
			entries: [ {
				field00: 1,
				field4c: 0,
				flags50: [ 0, 0, 0 ],
				flag53: 0,
				vector3c: [ 0, 2, 0 ],
				effectPath: "map/frame2.efp"
			} ]
		} ]
	};
	const scene = createWorldDecoder().decode( b );
	assert.equal( scene.scenery.length, 1, "one branch emitter, independent of material count" );
	assert.equal( scene.scenery[0].pose.y, 42 );

	assert.equal( scene.groups.length, 2 );
	assert.deepEqual( scene.groups.map( g => g.material.texture ).sort(), [ "a.png", "b.png" ] );
	assert.deepEqual( [ ...scene.groups[0].geometry.instances.slice( 12, 15 ) ], [ 30, 40, 50 ] );
	assert.deepEqual( scene.groups[0].visibility, scene.groups[1].visibility );
	b.objects.resources.meshes.pop();
	const missing = createWorldDecoder().decode( b );
	assert.equal( missing.groups.length, 0 );
	assert.equal( missing.scenery.length, 0 );
	assert.match( missing.warnings[0], /Compound dependencies absent/ );
});
