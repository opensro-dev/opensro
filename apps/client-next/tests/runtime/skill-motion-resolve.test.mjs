/*
===========================================================================

skill-motion-resolve.test.mjs - tests for skill-motion-resolve.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
const { skillMotionResolveAnimation } = await (async () => {
	return import( sourceFileUrl( "src/engine/foundation/animation/skill-motion-resolve.ts" ).href );
})();
const mobAttack1 = {
	stateId: 2,
	durationMs: 1666,
	soundEvents: [],
	trackEvents: [ { cursorMs: 100, eventCode: 1, param0: 0, param1: 0 } ],
	timeWarpCurve: { scale: 0, records: [] }
};
const mobAttack2 = {
	stateId: 5,
	durationMs: 2566,
	soundEvents: [],
	trackEvents: [],
	timeWarpCurve: { scale: 0, records: [] }
};
const mobClips = [ "stand", "walk", "attack1", "attack2", "death" ];
const mobBody = { attack1: mobAttack1, attack2: mobAttack2 };
test("8E7150 DEFAULT retry resolves mob basic attacks from resident GLB branches", () => {
	const sword = skillMotionResolveAnimation( { role: "native:sword:2", clips: mobClips, bodyStates: mobBody } );
	assert.deepEqual( sword, { clip: "attack1", definition: mobAttack1 } );
	const defaultShot = skillMotionResolveAnimation( {
		role: "native:default:5",
		clips: mobClips,
		bodyStates: mobBody
	} );
	assert.deepEqual( defaultShot, { clip: "attack2", definition: mobAttack2 } );
});
test("published BAN admission keeps native clip identity on player bodies", () => {
	const definition = { durationMs: 900, soundEvents: [], trackEvents: [], timeWarpCurve: { scale: 0, records: [] } };
	const role = "native:sword:26", banUrl = "/assets/anim/CHAR/skill_1.ban";
	const resolved = skillMotionResolveAnimation( {
		role,
		clips: [ "stand" ],
		catalogStates: { [role]: definition },
		motionUrls: new Map( [ [ role, banUrl ] ] )
	} );
	assert.deepEqual( resolved, { clip: role, definition, banUrl } );
});
test("authored set miss without DEFAULT state remains an admission failure", () => {
	assert.equal(
		skillMotionResolveAnimation( { role: "native:sword:26", clips: mobClips, bodyStates: mobBody } ),
		undefined
	);
});
test("8EADF0 3-tier lookup tries override set, then weapon fallbackSet, then DEFAULT", () => {
	const spear6 = { durationMs: 1200, soundEvents: [], trackEvents: [], timeWarpCurve: { scale: 0, records: [] } };
	const default6 = { durationMs: 1000, soundEvents: [], trackEvents: [], timeWarpCurve: { scale: 0, records: [] } };
	const default80 = { durationMs: 1500, soundEvents: [], trackEvents: [], timeWarpCurve: { scale: 0, records: [] } };
	const catalogStates = {
		"native:spear:6": spear6,
		"native:default:6": default6,
		"native:default:80": default80
	};
	const motionUrls = new Map( [
		[ "native:spear:6", "/assets/anim/CHAR/spear_stand.ban" ],
		[ "native:default:6", "/assets/anim/CHAR/default_standbattle.ban" ],
		[ "native:default:80", "/assets/anim/CHAR/default_stall.ban" ]
	] );
	// State 6: avatar_wing lacks state 6, so 8EADF0 falls through to spear before DEFAULT.
	assert.deepEqual(
		skillMotionResolveAnimation( {
			role: "native:avatar_wing:6",
			fallbackSet: "spear",
			clips: [ "stand" ],
			catalogStates,
			motionUrls
		} ),
		{ clip: "native:spear:6", definition: spear6, banUrl: "/assets/anim/CHAR/spear_stand.ban" }
	);
	// State 80: both avatar_wing and spear lack state 80, so 8EA820 falls through to DEFAULT.
	assert.deepEqual(
		skillMotionResolveAnimation( {
			role: "native:avatar_wing:80",
			fallbackSet: "spear",
			clips: [ "stand" ],
			catalogStates,
			motionUrls
		} ),
		{ clip: "native:default:80", definition: default80, banUrl: "/assets/anim/CHAR/default_stall.ban" }
	);
});
