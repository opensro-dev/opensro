/*
===========================================================================

native-loading-states.test.mjs - loading coverage across session handoffs

Exercise production UI with deferred assets so missing frontend snapshots and
retained operation errors cannot expose replacement controls.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createUi } = await import( "../../src/engine/runtime/ui/ui.ts" );

/*
================
fixture
================
*/
function fixture() {
	let scene;
	const commands = [];
	const ui = createUi(
		{ available: () => 0, take: () => null, request: () => 0, cancel: () => {} },
		command => commands.push( command ),
		value => {
			scene = value;
		},
		() => {},
		"https://fixture.invalid",
		"https://fixture.invalid"
	);
	return { ui, commands, scene: () => scene };
}

const missionPhases = /** @type {const} */ ([ "connecting", "entering-world", "reconnecting", "world" ]);
for ( const phase of missionPhases ) {
	for ( const error of [ undefined, "Previous operation failed" ] ) {
		test(`native mission loading covers ${phase} with retained error ${!!error}`, () => {
			const f = fixture();
			try {
				/** @type {import("../../src/engine/contracts/ui.ts").UiView} */
				const state = {
					session: { phase, revision: 1, character: "Fixture", error },
					gameplay: null,
					entities: [],
					width: 1024,
					height: 768,
					worldReady: false,
					loadingProgress: .4
				};
				const initial = f.ui.step( state, 0 );
				const semantics = f.ui.step( state, 10 ) ?? initial;
				assert.ok( semantics );
				assert.equal( semantics.loadingVisible, true );
				assert.ok( f.scene().quads.some( q => q.texture.endsWith( "loading_europe_1.png" ) ) );
				assert.equal( f.scene().quads.find( q => q.texture.endsWith( "gauge_loading.png" ) )?.uv[2], .4 );
				assert.ok( semantics );
				assert.equal( semantics.controls.length, 0 );
				assert.equal( f.ui.blocks( 512, 384 ), true );
			} finally {
				f.ui.dispose();
			}
		});
	}
}

const rosterPhases = /** @type {const} */ ([ "loading-roster", "character-select" ]);
for ( const phase of rosterPhases ) {
	test(`missing frontend during ${phase} keeps native transition art`, () => {
		const f = fixture();
		try {
			/** @type {import("../../src/engine/contracts/ui.ts").UiView} */
			const state = {
				session: { phase, revision: 1, characters: [] },
				gameplay: null,
				entities: [],
				width: 1024,
				height: 768,
				worldReady: false
			};
			const semantics = f.ui.step( state, 0 );
			assert.ok( f.scene().quads.some( q => q.texture.endsWith( "loading_charactercustom_europe.png" ) ) );
			assert.ok( semantics );
			assert.equal( semantics.controls.length, 0 );
			assert.equal( f.ui.blocks( 512, 384 ), true );
		} finally {
			f.ui.dispose();
		}
	});
}

test("resource invalidation repaints before the retained UI poll deadline", () => {
	const f = fixture();
	try {
		const session = { phase: /** @type {const} */ ("world"), revision: 1 };
		const state = { session, gameplay: null, entities: [], width: 1024, height: 768, worldReady: true };
		f.ui.step( state, 0 );
		const semantics = f.ui.step( { ...state, worldReady: false }, 1 );
		assert.ok( semantics?.loadingVisible );
		assert.ok( f.scene().quads.some( q => q.texture.endsWith( "gauge_loading.png" ) ) );
	} finally {
		f.ui.dispose();
	}
});

test("frontend handoff retains loading despite operation errors and disconnect retires it", () => {
	const f = fixture();
	try {
		const frontend = {
			phase: /** @type {const} */ ("world"),
			generation: 1,
			elapsed: 0,
			alpha: 1,
			logoAlpha: 0,
			error: null
		};
		const state = {
			frontend,
			session: { phase: /** @type {const} */ ("world"), revision: 1, error: "Previous operation failed" },
			gameplay: null,
			entities: [],
			width: 1024,
			height: 768,
			worldReady: false
		};
		assert.ok( f.ui.step( state, 0 )?.loadingVisible );
		assert.equal(
			f.ui.step( { ...state, session: { phase: "disconnected", revision: 2 } }, 10 )?.loadingVisible,
			false
		);
		assert.ok( f.scene().quads.every( q => !q.texture.endsWith( "gauge_loading.png" ) ) );
	} finally {
		f.ui.dispose();
	}
});
