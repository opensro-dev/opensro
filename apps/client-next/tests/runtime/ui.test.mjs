/*
===========================================================================

ui.test.mjs - tests for ui.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { copyUi, prepareUi, createUiPreparation } = await import(
	sourceFileUrl( "src/engine/foundation/ui/ui.ts" ).href
);
test("UI admission isolates retained commands and rejects malformed replacements", () => {
	const scene = {
		revision: 1,
		width: 100,
		height: 100,
		quads: [ {
			rect: [ 0, 0, 10, 10 ],
			uv: [ 0, 0, 1, 1 ],
			clip: [ 0, 0, 100, 100 ],
			color: [ 1, 1, 1, 1 ],
			texture: ""
		} ]
	};
	const owned = copyUi( scene );
	scene.quads[0].rect[0] = 90;
	assert.equal( owned.quads[0].rect[0], 0 );
	assert.throws( () => copyUi( { ...scene, width: 0 } ), /Invalid/ );
	assert.throws( () => copyUi( { ...scene, quads: Array( 8193 ).fill( scene.quads[0] ) } ), /Invalid/ );
	scene.quads[0].color[0] = NaN;
	assert.throws( () => copyUi( scene ), /Invalid/ );
});

test("UI masks are admitted by value and reject degenerate sampling rectangles", () => {
	const quad = {
		rect: [ 0, 0, 10, 10 ],
		uv: [ 0, 0, 1, 1 ],
		clip: [ 0, 0, 100, 100 ],
		color: [ 1, 1, 1, 1 ],
		texture: "",
		mask: { texture: "mask", rect: [ 1, 2, 10, 10 ] }
	};
	const scene = { revision: 1, width: 100, height: 100, quads: [ quad ] }, owned = copyUi( scene );
	quad.mask.rect[0] = 90;
	assert.equal( owned.quads[0].mask.rect[0], 1 );
	quad.mask.rect[2] = 0;
	assert.throws( () => copyUi( scene ), /Invalid UI mask/ );
	quad.mask.rect[2] = 10;
	quad.mask.texture = "";
	assert.throws( () => copyUi( scene ), /Invalid UI mask/ );
});

test("admitted UI retains unique label membership and first portrait/doll without retaining caller aliases", () => {
	const q = {
		rect: [ 0, 0, 10, 10 ],
		uv: [ 0, 0, 1, 1 ],
		clip: [ 0, 0, 100, 100 ],
		color: [ 1, 1, 1, 1 ],
		texture: ""
	};
	const source = {
		revision: 1,
		width: 100,
		height: 100,
		quads: [ { ...q, characterAnchor: 7, portraitGid: 9, doll: { gid: 7, yaw: 1 } }, {
			...q,
			characterAnchor: 7,
			portraitGid: 10
		}, { ...q, characterAnchor: 8, worldAnchor: { regionId: 1, x: 2, y: 3, z: 4 } } ]
	};
	const product = prepareUi( source );
	assert.deepEqual( [ ...product.anchors ], [ 7, 8 ] );
	assert.equal( product.portraitGid, 9 );
	assert.equal( product.worldAnchors, true );
	assert.deepEqual( product.doll, { gid: 7, yaw: 1 } );
	source.quads[0].characterAnchor = 99;
	source.quads[0].doll.yaw = 2;
	source.quads[2].worldAnchor.x = 99;
	assert.deepEqual( [ ...product.anchors ], [ 7, 8 ] );
	assert.equal( product.doll.yaw, 1 );
	assert.equal( product.scene.quads[2].worldAnchor.x, 2 );
	const empty = prepareUi( { ...source, revision: 2, quads: [] } );
	assert.equal( empty.anchors.size, 0 );
	assert.equal( empty.worldAnchors, false );
	assert.equal( empty.portraitGid, undefined );
	assert.equal( empty.doll, undefined );
	const mounted = prepareUi( {
		...source,
		revision: 3,
		quads: [ { ...q, doll: { gid: 7, yaw: 0, mount: true } } ]
	} );
	assert.deepEqual( mounted.doll, { gid: 7, yaw: 0, mount: true } );
	assert.throws( () => prepareUi( { ...source, width: 0 } ), /Invalid UI/ );
	assert.equal( product.portraitGid, 9 );
});

test("incremental UI admission retains static commands without trusting caller identity", () => {
	const q = {
		rect: [ 0, 0, 10, 10 ],
		uv: [ 0, 0, 1, 1 ],
		clip: [ 0, 0, 100, 100 ],
		color: [ 1, 1, 1, 1 ],
		texture: ""
	};
	const owner = createUiPreparation(),
		scene = { revision: 1, width: 100, height: 100, quads: [ structuredClone( q ), structuredClone( q ) ] };
	const first = owner.prepare( scene );
	scene.quads[1].rect[0] = 5;
	scene.revision++;
	const second = owner.prepare( scene );
	assert.deepEqual( second, prepareUi( scene ) );
	assert.equal( first.scene.quads[0], second.scene.quads[0] );
	assert.notEqual( first.scene.quads[1], second.scene.quads[1] );
	assert.equal( first.scene.quads[1].rect[0], 0 );
	scene.quads[1].color[0] = NaN;
	assert.throws( () => owner.prepare( scene ), /Invalid/ );
	scene.quads[1].color[0] = 1;
	assert.equal( owner.prepare( scene ).scene.quads[0], second.scene.quads[0] );
	scene.width = 200;
	const resized = owner.prepare( scene );
	assert.notEqual( resized.scene.quads[0], second.scene.quads[0] );
	assert.deepEqual( resized, prepareUi( scene ) );
	scene.quads[0].texture = "loaded";
	assert.deepEqual( owner.prepare( scene ), prepareUi( scene ) );
	owner.prepare( { ...scene, quads: [] } );
	assert.deepEqual( owner.prepare( scene ), prepareUi( scene ) );
	owner.prepare( null );
	const reset = owner.prepare( scene );
	assert.notEqual( reset.scene.quads[0], resized.scene.quads[0] );
	const other = createUiPreparation().prepare( scene );
	assert.notEqual( other.scene.quads[0], reset.scene.quads[0] );
	scene.quads[0].rect[0] = -0;
	const signed = owner.prepare( scene );
	assert.deepEqual( signed, prepareUi( scene ) );
	assert.ok( Object.is( signed.scene.quads[0].rect[0], -0 ) );
});

test("incremental portrait remapping and invalid membership replacement are transactional", () => {
	const q = {
		rect: [ 0, 0, 10, 10 ],
		uv: [ 0, 0, 1, 1 ],
		clip: [ 0, 0, 100, 100 ],
		color: [ 1, 1, 1, 1 ],
		texture: ""
	};
	const scene = {
			revision: 1,
			width: 100,
			height: 100,
			quads: [ { ...q, portraitGid: 1 }, { ...q, portraitGid: 2 } ]
		},
		owner = createUiPreparation();
	owner.prepare( scene );
	const reordered = { ...scene, quads: [ scene.quads[1], scene.quads[0] ] };
	assert.deepEqual( owner.prepare( reordered ), prepareUi( reordered ) );
	assert.throws(
		() =>
			owner.prepare( {
				...scene,
				quads: Array.from( { length: 9 }, ( _, i ) => ({ ...q, portraitGid: i + 1 }) )
			} ),
		/capacity/
	);
	assert.deepEqual( owner.prepare( reordered ), prepareUi( reordered ) );
	owner.reset();
	assert.deepEqual( owner.prepare( scene ), prepareUi( scene ) );
});

test("gradient colors cross admission by value and invalidate only their retained command", () => {
	const q = {
		rect: [ 0, 0, 600, 112 ],
		uv: [ 0, 0, 1, 1 ],
		clip: [ 0, 0, 1000, 800 ],
		color: [ 0, 0, 0, 200 / 255 ],
		rightColor: [ 0, 0, 0, 0 ],
		texture: ""
	};
	const scene = { revision: 1, width: 1000, height: 800, quads: [ q ] }, owner = createUiPreparation();
	const first = owner.prepare( scene );
	assert.deepEqual( first.scene.quads[0].rightColor, [ 0, 0, 0, 0 ] );
	q.rightColor[3] = .5;
	const next = owner.prepare( { ...scene, revision: 2 } );
	assert.equal( first.scene.quads[0].rightColor[3], 0 );
	assert.equal( next.scene.quads[0].rightColor[3], .5 );
	assert.notEqual( first.scene.quads[0], next.scene.quads[0] );
	for ( const invalid of [ [ 0, 0, 0, NaN ], [ 0, 0, 0, 2 ], [ 0, 0, 0 ] ] ) {
		assert.throws( () => owner.prepare( { ...scene, quads: [ { ...q, rightColor: invalid } ] } ), /Invalid UI/ );
	}
	assert.equal( owner.prepare( scene ).scene.quads[0], next.scene.quads[0] );
});
