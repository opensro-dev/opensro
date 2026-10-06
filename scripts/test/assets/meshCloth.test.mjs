/*
===========================================================================

meshCloth.test.mjs - native dynamic streams and signed water bump publication

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { readMeshCloth } from "../../build/shared/meshCloth.mjs";
import { runPython } from "../../build/shared/pythonRun.mjs";
import { rebuildRoot } from "../../build/world/paths.mjs";

test("BMS cloth preserves mobility, pin flags, ordered edges and force parameters", () => {
	const bytes = Buffer.alloc( 256 ), offsets = [ 0, 0, 0, 64, 84, 140 ];
	bytes.writeUInt32LE( 2, 64 );
	bytes.writeFloatLE( 0, 68 );
	bytes.writeUInt32LE( 1, 72 );
	bytes.writeFloatLE( .5, 76 );
	bytes.writeUInt32LE( 0, 80 );
	bytes.writeUInt32LE( 1, 84 );
	bytes.writeUInt32LE( 0, 88 );
	bytes.writeUInt32LE( 1, 92 );
	bytes.writeFloatLE( 3.5, 96 );
	bytes.writeUInt32LE( 0, 100 );
	bytes.writeUInt32LE( 1, 104 );
	for ( const [i, n] of [ 1, 2, 3, 4, 5, 6, .5 ].entries() ) bytes.writeFloatLE( n, 108 + i * 4 );
	bytes.writeUInt32LE( 7, 136 );
	const actual = readMeshCloth( bytes, offsets, 2, false );
	assert.deepEqual( actual, {
		mobility: [ 0, .5 ],
		pins: [ 1, 0 ],
		constraints: [ [ 0, 1, 3.5 ] ],
		order: [ 0 ],
		force: [ 1, 2, 3 ],
		gravity: 4,
		gravityMobility: 5,
		windMobility: 6,
		damping: .5,
		windPeriod: 7
	} );
	assert.deepEqual( readMeshCloth( bytes, offsets, 2 ).force, [ 1, 2, -3 ] );
	assert.throws( () => readMeshCloth( bytes, offsets, 3 ), /Invalid BMS cloth/ );
	assert.equal( readMeshCloth( bytes, [ 0, 0, 0, 0, 0 ], 2 ), undefined );
	assert.throws( () => readMeshCloth( bytes.subarray( 0, 130 ), offsets, 2 ), /Invalid BMS cloth/ );
});

test("water bump publisher uses blue, wrapped neighbors and signed truncation", async () => {
	const temp = await mkdtemp( path.join( tmpdir(), "sro-water-" ) );
	try {
		const code = `import importlib.util,sys
from PIL import Image
from pathlib import Path
spec=importlib.util.spec_from_file_location('water',sys.argv[1])
module=importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
root=Path(sys.argv[2])
image=Image.new('RGBA',(3,3))
image.putdata([(200,150,b,255) for b in [0,20,40,3,99,9,50,60,80]])
image.save(root/'source.png')
module.convert(root/'source.png',root/'bump.png')
out=Image.open(root/'bump.png')
assert out.getpixel((0,0)) == (138,151,0,255),out.getpixel((0,0))
assert out.getpixel((1,1)) == (125,108,0,255),out.getpixel((1,1))
assert out.getpixel((2,0)) == (138,163,0,255),out.getpixel((2,0))
`;
		await runPython( [ "-c", code, path.join( rebuildRoot, "scripts/build/world/assets/water_bump.py" ), temp ], {
			task: "water bump fixture"
		} );
	} finally {
		await rm( temp, { recursive: true, force: true } );
	}
});
