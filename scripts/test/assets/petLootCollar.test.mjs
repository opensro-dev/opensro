/*
===========================================================================

petLootCollar.test.mjs - the owned pet drop prop remains grounded and modest

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { createPetLootCollar, petLootDropModifiers } from "../../build/char/petLootCollar.mjs";

test("caravan collar exports grounded geometry at 180% of its original dimensions", () => {
	const bytes = createPetLootCollar();
	assert.equal( bytes.readUInt32LE( 0 ), 0x46546c67 );
	assert.equal( bytes.readUInt32LE( 8 ), bytes.length );
	const jsonLength = bytes.readUInt32LE( 12 );
	const model = JSON.parse( bytes.subarray( 20, 20 + jsonLength ).toString() );
	const binary = bytes.subarray( 28 + jsonLength );
	let triangles = 0;
	for ( const mesh of model.meshes ) {
		for ( const primitive of mesh.primitives ) {
			const accessor = model.accessors[primitive.attributes.POSITION];
			assert.ok( accessor.min.every( Number.isFinite ) && accessor.max.every( Number.isFinite ) );
			assert.ok( accessor.min[1] >= 0 && accessor.max[1] < 2.1, "enlarged but still grounded" );
			assert.ok( accessor.max[0] - accessor.min[0] < 7.8, "bounded enlarged footprint" );
			triangles += model.accessors[primitive.indices].count / 3;
			const texcoords = model.accessors[primitive.attributes.TEXCOORD_0];
			const texview = model.bufferViews[texcoords.bufferView];
			for ( let i = 0; i < texcoords.count * 2; i++ ) {
				const value = binary.readFloatLE( texview.byteOffset + i * 4 );
				assert.ok( value > 0 && value < 1, "atlas UVs stay finite and inset from image edges" );
			}
			if ( model.materials[primitive.material].name === "stone" ) {
				const centers = Array.from(
					{ length: texcoords.count },
					( _, i ) =>
						Math.abs( binary.readFloatLE( texview.byteOffset + i * 8 ) - 5 / 6 ) < 0.001 &&
						Math.abs( binary.readFloatLE( texview.byteOffset + i * 8 + 4 ) - 0.25 ) < 0.001
				).filter( Boolean );
				assert.equal( centers.length, 16, "both sides of the eight-facet gem use radial tile-center UVs" );
			}
			if ( model.materials[primitive.material].name === "leather" ) {
				assert.ok( Math.abs( accessor.max[0] - accessor.min[0] - 4.04 * 1.8 ) < 0.001, "exact 80% increase" );
				const normal = model.accessors[primitive.attributes.NORMAL];
				const view = model.bufferViews[normal.bufferView];
				assert.ok( binary.readFloatLE( view.byteOffset ) > 0.9, "outer band normal faces outward" );
			}
		}
	}
	assert.ok( triangles < 1500, "small low-poly loot prop" );
	assert.equal( model.materials.length, 6 );
	assert.equal( model.images.length, 1, "one detailed atlas shared by all six materials" );
	const image = model.bufferViews[model.images[0].bufferView];
	assert.ok( binary.readUInt32BE( image.byteOffset + 16 ) >= 768, "atlas retains authored texture detail" );
	assert.ok( binary.readUInt32BE( image.byteOffset + 20 ) >= 512 );
	assert.ok( model.materials.every( material => material.pbrMetallicRoughness.baseColorTexture ) );
	assert.ok( model.materials.every( material => !material.emissiveFactor && material.alphaMode !== "BLEND" ) );
});

test("pet loot borrows native chest glow and rejects a missing donor", () => {
	const chest = {
		modifierSets: [],
		particleModifiers: [ { entries: [ { effectPath: "system\\item_drop_equip.efp" } ] } ]
	};
	assert.deepEqual( petLootDropModifiers( chest ), chest );
	assert.throws( () => petLootDropModifiers( undefined ), /native armor chest/ );
	assert.throws( () => petLootDropModifiers( { particleModifiers: [] } ), /native armor chest/ );
});
