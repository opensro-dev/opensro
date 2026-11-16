/*
===========================================================================

ui-scene-goldens.test.mjs - every window's published scene, pinned

Wave 0 of the ui.ts split (#2514, umbrella #2516): the split moves code
and must not move one pixel. Each script in helpers/scene-scripts drives
the production HUD with retail assets and a fixed clock; the scene it
settles into - quads, textures, uv, clip, the control list and the
semantics - must equal its recorded golden byte for byte, and a gesture
tape must match after every beat, not only where it settles.

The suite needs the generated asset tree, so without it every scene
skips (CI runs this way; the PR for #2514 says so). Regenerating a
golden is deliberate: SRO_SCENE_GOLDENS_UPDATE=1, in its own commit,
with no production change beside it.

===========================================================================
*/
import assert from "node:assert/strict";
import { existsSync } from "node:fs";
import test from "node:test";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import {
	checkSceneGolden,
	canonicalSceneText,
	canonicalStreamText,
	SCENE_GOLDENS_UPDATE
} from "../helpers/scene-golden.mjs";
import { SCENE_SCRIPTS } from "../helpers/scene-scripts.mjs";
import { EXPANDED_BUILD } from "../helpers/expanded-build.mjs";

const ASSETS_HERE = existsSync( CLIENT_PUBLIC_ROOT + "/assets/fonts/native-ui-font-atlas.json" );

// The goldens record the native build. An expanded build (Alexandria and
// cap 110) draws more in these scenes: the world map shows Alexandria's
// teleports. They skip there, with the reason, and every other scene
// still has to match the native recording.
const NATIVE_SCENES = new Set( [ "panel-map" ] );

/*
================
sceneSkip
================
*/
function sceneSkip( name ) {
	if ( !ASSETS_HERE ) return "needs the generated asset tree";
	if ( EXPANDED_BUILD && NATIVE_SCENES.has( name ) ) {
		return "native scene: this build adds the Alexandria and cap-110 content";
	}
	return false;
}

/*
================
settle

Step a script's world the way the panel sweep does: sixteen fixed ticks
of 100 ms, keeping the last semantics the HUD published. The tick
cadence is load-bearing, not repaint waste: the rebirth prompt's
3000 ms timer arms off it (dialog-rebirth and dialog-resurrection
goldens change if the ticks collapse into one step, tried 2026-11-08).
================
*/
function settle( ctx, ticks = 16 ) {
	for ( let i = 0; i < ticks; i++ ) {
		const semantics = ctx.fx.ui.step( ctx.state, ctx.now += 100 );
		if ( semantics ) ctx.semantics = semantics;
	}
	ctx.scene = ctx.fx.scenes.at( -1 );
}

/*
================
runScript

Play one script against a fresh HUD and return its canonical text.
================
*/
async function runScript( uiFixture, script ) {
	const fx = uiFixture();
	/** @type {any} */
	const ctx = {
		fx,
		state: fx.state,
		// The clock starts where the behavioural suites start theirs:
		// native timers (the rebirth prompt's 3000 ms) arm off it, and a
		// large origin silently never fires them.
		now: 0,
		scene: undefined,
		semantics: undefined,
		event: e => fx.ui.event( e )
	};
	try {
		settle( ctx );
		const hudControls = JSON.stringify( ctx.semantics?.controls ?? [] );
		if ( script.stream ) {
			const beats = [];
			for ( const scriptBeat of script.beats ) {
				scriptBeat.run( ctx );
				settle( ctx, scriptBeat.ticks );
				beats.push( { label: scriptBeat.label, scene: ctx.scene, semantics: ctx.semantics } );
			}
			return canonicalStreamText( beats );
		}
		for ( const scriptBeat of script.beats ) {
			scriptBeat.run( ctx );
			settle( ctx, scriptBeat.ticks );
		}
		if ( script.guard ) {
			assert.notEqual(
				JSON.stringify( ctx.semantics?.controls ?? [] ),
				hudControls,
				`scene ${script.name}: the script never opened anything over the world HUD - the recipe is stale`
			);
		}
		assert.ok( ctx.scene, `scene ${script.name}: the HUD published no scene` );
		assert.ok( ctx.semantics, `scene ${script.name}: the HUD published no semantics` );
		return canonicalSceneText( ctx.scene, ctx.semantics );
	} finally {
		fx.dispose?.();
	}
}

for ( const script of SCENE_SCRIPTS ) {
	test( `scene golden: ${script.name}`, { skip: sceneSkip( script.name ) }, async () => {
		const { uiFixture } = await import( "../helpers/ui-fixture.mjs" );
		const text = await runScript( uiFixture, script );
		const failure = checkSceneGolden( script.name, text );
		if ( SCENE_GOLDENS_UPDATE ) return;
		if ( failure !== null ) assert.fail( failure );
	} );
}
