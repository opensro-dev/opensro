/*
===========================================================================

skill-object-assets.test.mjs - persistent native skill resource admission

Exercises the asset compiler's public entry points with independent authored
rows. A cast effect must not be required to publish a stationary skill model.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { buildEffectRecordTable, parseSkillAniSet } from "../../../../scripts/build/char/parseSkillEffect.mjs";
import {
	collectSkillStageBsrPaths,
	collectSkillObjectResources
} from "../../../../scripts/build/char/buildSkillStageModelAssets.mjs";

/*
================
animationRow

The service column is outside the native animation-set record's field order.
================
*/
function animationRow( name, resource, enabled = 1 ) {
	const columns = Array( 26 ).fill( "none" );
	columns[0] = name;
	columns[1] = name;
	columns[2] = "0";
	columns[3] = "FALSE";
	columns[4] = "0";
	columns[5] = "DEFAULT";
	columns[22] = resource;
	return [ enabled, ...columns ].join( "\t" );
}

test("stationary skill models survive name joins without a cast-stage dependency", () => {
	const dir = fs.mkdtempSync( path.join( os.tmpdir(), "skill-object-assets-" ) );
	try {
		const source = path.join( dir, "skilleffect.txt" );
		fs.writeFileSync(
			source,
			[
				"#section skillaniset2",
				animationRow( "TRAP", "res\\etc\\capture_trap.bsr" ),
				animationRow( "FIELD", "skill\\field.efp" ),
				animationRow( "DISABLED", "res\\etc\\disabled.bsr", 0 )
			].join( "\n" ),
			"utf16le"
		);
		fs.writeFileSync(
			path.join( dir, "SkillData_5000.txt" ),
			"1\t77\t0\tTRAP_01\t0\tTRAP\n1\t78\t0\tFIELD\t0\txxx\n",
			"utf16le"
		);
		const sets = parseSkillAniSet( source );
		assert.equal( sets.has( "DISABLED" ), false );
		assert.deepEqual( collectSkillStageBsrPaths( source ), [ "res/etc/capture_trap.bsr" ] );
		const { table, namedTable } = buildEffectRecordTable( dir, source );
		assert.deepEqual( table[77].objectResource, { kind: "model", path: "res/etc/capture_trap.bsr" } );
		assert.deepEqual( table[78].objectResource, { kind: "effect", path: "skill/field.efp" } );
		assert.deepEqual( namedTable.TRAP.objectResource, table[77].objectResource );
		assert.deepEqual( table[77].authoredStages, [] );
		assert.deepEqual( collectSkillObjectResources( dir, source ), {
			77: { kind: "model", path: "res/etc/capture_trap.bsr" },
			78: { kind: "effect", path: "skill/field.efp" }
		} );
	} finally {
		fs.rmSync( dir, { recursive: true, force: true } );
	}
});
