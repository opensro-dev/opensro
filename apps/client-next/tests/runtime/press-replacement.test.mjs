/*
===========================================================================

press-replacement.test.mjs - the press predicts only what 59D870 admits

The native press animates only on the server's answer (6FCD50). The port
predicts a press's cast and cooldown, so a press the server's buff
replacement check (59D870, 0x300C) refuses must not be predicted: it showed
a cooldown and a start animation, then cancelled (Concentration 3rd over an
active 2nd). The replay reads the same truth table as the server's
DecideReplacement test (tests/fixtures/native/buff-replacement-cases.json).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { root } from "../../tools/project.mjs";
const { decideReplacement, pressAdmission, parsePressAdmit, parsePressReplace } = await import(
	"../../src/engine/foundation/gameplay/press-admission.ts"
);

const table = JSON.parse(
	fs.readFileSync( path.join( root, "tests/fixtures/native/buff-replacement-cases.json" ), "utf8" )
);
// SKILL_CH_LIGHTNING_JIPJUNG_B (Concentration 2nd) and _C (3rd), as published.
const CONCENTRATION_2ND = { category: 3, group: 267, rank: 6, states: 13 };
const CONCENTRATION_3RD = { category: 3, group: 336, rank: 1, states: 13 };

/*
================
descriptor

A table row as the published, parsed descriptor; null stays undefined
(an effect whose row publishes none).
================
*/
function descriptor( row ) {
	return row === null ? undefined : parsePressReplace( row );
}

/*
================
caster

A caster every other 58D8F0 gate admits.
================
*/
/**
 * @param {Record<string, unknown>} overrides
 * @returns {any}
 */
function caster( overrides = {} ) {
	return {
		abnormal: 0,
		hp: 1000,
		maxHp: 1000,
		mp: 1000,
		maxMp: 1000,
		body: 0,
		mounted: false,
		seated: false,
		equipped: [],
		effects: [],
		...overrides
	};
}

test("the replay decides every shared case as the server does", () => {
	assert.ok( table.cases.length >= 16 );
	for ( const c of table.cases ) {
		const got = decideReplacement( parsePressReplace( c.press ), c.effects.map( descriptor ) );
		assert.equal( got.kind, c.expect, c.name );
		if ( got.kind === "refuse" ) assert.equal( got.code, 0x300c, c.name );
	}
});

test("Concentration 3rd over an active 2nd is not predicted, and 3rd over itself is", () => {
	const admit = parsePressAdmit( { weaponKinds: [ 255, 255 ], replace: CONCENTRATION_3RD } );
	const skill = { admit, mpCost: 10 };
	const refused = pressAdmission( skill, caster( { effects: [ parsePressReplace( CONCENTRATION_2ND ) ] } ) );
	assert.deepEqual( refused, { kind: "refuse", code: 0x300c } );
	const higher = parsePressAdmit( { weaponKinds: [ 255, 255 ], replace: { ...CONCENTRATION_3RD, rank: 2 } } );
	assert.deepEqual(
		pressAdmission(
			{ admit: higher, mpCost: 10 },
			caster( { effects: [ parsePressReplace( CONCENTRATION_3RD ) ] } )
		),
		{
			kind: "admit"
		}
	);
	assert.deepEqual( pressAdmission( skill, caster() ), { kind: "admit" } );
});

test("what the replay cannot see is never predicted, and never refused", () => {
	const admit = parsePressAdmit( { weaponKinds: [ 255, 255 ], replace: CONCENTRATION_3RD } );
	const skill = { admit, mpCost: 10 };
	/** @type {{ name: string; over: Record<string, unknown>; }[]} */
	const cases = [
		{ name: "unknown effects", over: { effects: undefined } },
		{ name: "own cast running", over: { casting: true } },
		{ name: "an effect without a descriptor", over: { effects: [ undefined ] } }
	];
	for ( const { name, over } of cases ) {
		assert.equal( pressAdmission( skill, caster( over ) ).kind, "unknown", name );
	}
	// A row without a published descriptor keeps the earlier admission.
	const plain = parsePressAdmit( { weaponKinds: [ 255, 255 ] } );
	assert.deepEqual( pressAdmission( { admit: plain, mpCost: 10 }, caster( { effects: [ undefined ] } ) ), {
		kind: "admit"
	} );
});

test("a malformed descriptor is refused at the parse", () => {
	for (
		const bad of [
			null,
			{ group: 1, rank: 1 },
			{ category: 3, group: -1, rank: 1 },
			{ category: 3, group: 1, rank: 256 },
			{ category: 3, group: 1, rank: 1, cbuf: 1 }
		]
	) {
		assert.throws( () => parsePressReplace( bad ), /replacement/, JSON.stringify( bad ) );
	}
});
