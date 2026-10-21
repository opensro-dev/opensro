/*
===========================================================================

wolf-effects.test.mjs - every wolf skill signal draws its own retail effects

Each signal names effect programs that exist in the published catalog and
compile; none borrows the bear's roar or shield models.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { publicRoot } from "../../../../scripts/build/world/paths.mjs";

const wolf = await import( "../../src/engine/foundation/animation/wolf-effects.ts" );
const { createEffectPrograms } = await import( "../../src/engine/runtime/assets/worker/effects/program/program.ts" );

test("every wolf signal plays published, compiling, wolf-only effect programs", () => {
	const bytes = readPublishedAssetBytesSync( "assets/effects/programs.json", publicRoot );
	const catalog = JSON.parse( new TextDecoder().decode( bytes ) );
	const compiler = createEffectPrograms();
	const signals = [
		...wolf.WOLF_HOWL,
		...wolf.WOLF_REND,
		wolf.WOLF_EXECUTED,
		wolf.WOLF_SWEEP,
		wolf.WOLF_HUNT,
		wolf.WOLF_HUNT_BURST,
		wolf.WOLF_BLOOD_FULL
	];
	for ( const action of signals ) {
		const parts = wolf.wolfEffectParts( action );
		assert.ok( parts.length > 0, `signal ${action} draws nothing` );
		assert.ok( parts.length <= wolf.WOLF_EFFECT_MAX_PARTS, `signal ${action} has more parts than actors` );
		for ( const part of parts ) {
			assert.ok( catalog.effects[part.path], `${part.path} is not published` );
			assert.ok( compiler.decode( bytes, part.path ).model, `${part.path} does not compile` );
			assert.doesNotMatch( part.path, /roar|iron-hide|shield/ );
			assert.ok( part.at >= 0 && part.at < wolf.WOLF_EFFECT_SECONDS );
		}
	}
	// Higher books add to the lower ones: the howl rouses the owner, Rend cleaves.
	assert.ok( wolf.wolfEffectParts( 2 ).some( part => part.anchor === "owner" ) );
	assert.ok( !wolf.wolfEffectParts( 1 ).some( part => part.anchor === "owner" ) );
	assert.ok( wolf.wolfEffectParts( 6 ).length > wolf.wolfEffectParts( 4 ).length );
	// The sweep's eruptions ring out to its 10 m radius (100 units).
	const reach = Math.max(
		...wolf.wolfEffectParts( wolf.WOLF_SWEEP ).map( p => Math.hypot( ...(p.offset ?? [ 0, 0 ]) ) )
	);
	assert.ok( Math.abs( reach - 100 ) < 1e-9 );
	assert.deepEqual( wolf.wolfEffectParts( 7 ), [] );
	assert.deepEqual( wolf.wolfEffectParts( 16 ), [], "the bear's roar is not a wolf signal" );
	assert.equal( wolf.wolfEffectKind( wolf.WOLF_EXECUTED ), "victim" );
	assert.equal( wolf.wolfEffectKind( wolf.WOLF_BLOOD_FULL ), "strike" );
	for (
		const action of [ ...wolf.WOLF_HOWL, ...wolf.WOLF_REND, wolf.WOLF_EXECUTED, wolf.WOLF_SWEEP, wolf.WOLF_HUNT ]
	) {
		assert.ok( wolf.wolfEffectSound( action ), `signal ${action} is silent` );
	}
	// The third howl and the last rend shake the camera; the last rend is louder.
	assert.deepEqual(
		[ 1, 3, 5, 6 ].map( action => [ wolf.wolfEffectSound( action )?.shake, wolf.wolfEffectSound( action )?.gain ] ),
		[ [ false, .8 ], [ true, .8 ], [ false, .55 ], [ true, .6 ] ]
	);
	assert.equal( wolf.wolfEffectSound( wolf.WOLF_BLOOD_FULL ), undefined );
});
