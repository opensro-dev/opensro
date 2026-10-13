/*
===========================================================================

buff-viewer.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
await mkdir( "temp/artifacts/buff-viewer-tests", { recursive: true } );
async function load( path, name ) {
	return import( sourceFileUrl( "src/engine/" + path ).href );
}
const viewer = await load( "foundation/ui/buff-viewer.ts", "viewer" );
const { buffTooltip } = await load( "foundation/ui/buff-tooltip.ts", "tooltip" );
const { buffBoard } = await load( "foundation/ui/buff-board.ts", "board" );
const {
	targetBuffViewer,
	collectActiveBuffs,
	rebuildBuffViewer,
	tickBuffViewer,
	buffViewerCells,
	buffViewerIcons,
	createBuffViewer,
	boardSuppression,
	skillLookup
} = viewer;
const icon = "skill/china/sword_smash_a.ddj";
const catalog = [
	{ id: 10, icon },
	{ id: 11, icon },
	{ id: 12, icon },
	{ id: 20, icon, buffSecondary: true },
	{ id: 30, icon, speedBuff: { active: true } },
	{ id: 31, icon, speedBuff: { active: true } },
	{ id: 32, icon, speedBuff: { active: false } },
	{ id: 40, icon, hide: { mask: 1, level: 5 } },
	{ id: 41, icon, detect: { mask: 5, level: 6 } },
	{ id: 42, icon, detect: { mask: 1, level: 3 } },
	{ id: 50, icon, buffCancelInstance: true }
];
const skill = skillLookup( catalog ), target = targetBuffViewer();
const effect = ( skill, token, extra = {} ) => ({ gid: 7, skill, token, phase: 2, ...extra });
const ids = state => [ state.primary.map( s => s.kind[0] + s.id ), state.secondary.map( s => s.kind[0] + s.id ) ];

test("581410 rebuild: primary unless bbuf, abnormal bits ascending, target layout has no minimum row", () => {
	const state = rebuildBuffViewer(
		target,
		collectActiveBuffs( [ effect( 20, 1 ), effect( 10, 2 ), effect( 11, 3, { gid: 8 } ) ], 7, skill ),
		0x9,
		skill
	);
	assert.deepEqual( ids( state ), [ [ "b10" ], [ "b20", "a0", "a3" ] ] );
	assert.deepEqual( buffViewerCells( target, state ).map( c => [ c.x, c.y ] ), [ [ 0, 0 ], [ 0, 25 ], [ 23, 25 ], [
		46,
		25
	] ] );
	const secondaryOnly = rebuildBuffViewer( target, [], 0x2, skill );
	assert.deepEqual( buffViewerCells( target, secondaryOnly ).map( c => [ c.x, c.y ] ), [ [ 0, 2 ] ] );
	const nine = rebuildBuffViewer(
		target,
		collectActiveBuffs( Array.from( { length: 9 }, ( _, i ) => effect( 10, i + 1 ) ), 7, skill ),
		0,
		skill
	);
	assert.deepEqual( buffViewerCells( target, nine ).at( -1 ), { slot: nine.primary[8], x: 0, y: 23 } );
});

test("6DFBA0 tick keeps surviving slots in place, appends newcomers and interleaves abnormal bits", () => {
	let state = rebuildBuffViewer(
		target,
		collectActiveBuffs( [ effect( 10, 1 ), effect( 11, 2 ), effect( 20, 3 ) ], 7, skill ),
		0x1,
		skill
	);
	// 11 ends, 12 arrives, a new debuff arrives after the existing freeze cell.
	state = tickBuffViewer(
		target,
		state,
		collectActiveBuffs( [ effect( 12, 4 ), effect( 20, 3 ), effect( 10, 1 ), effect( 20, 5 ) ], 7, skill ),
		0x5,
		skill
	);
	assert.deepEqual( ids( state ), [ [ "b10", "b12" ], [ "b20", "a0", "b20", "a2" ] ] );
	state = tickBuffViewer( target, state, collectActiveBuffs( [ effect( 12, 4 ) ], 7, skill ), 0x4, skill );
	assert.deepEqual( ids( state ), [ [ "b12" ], [ "a2" ] ] );
});

test("6DE630 speed buffs: only the last active one applies, and only from the first tick", () => {
	const entries = collectActiveBuffs(
		[ effect( 30, 1 ), effect( 31, 2 ), effect( 32, 3 ), effect( 10, 4 ) ],
		7,
		skill
	);
	const rebuilt = rebuildBuffViewer( target, entries, 0, skill );
	assert.deepEqual( rebuilt.primary.map( s => s.suppressed ), [ false, false, false, false ] );
	const ticked = tickBuffViewer( target, rebuilt, entries, 0, skill );
	assert.deepEqual( ticked.primary.map( s => s.suppressed ), [ true, false, true, false ] );
	const icons = buffViewerIcons( target, ticked, 7, skill );
	assert.match( icons[0].overlay[0], /s_admittance_back/ );
	assert.match( icons[0].overlay[1], /s_admittance_icon/ );
	assert.deepEqual( icons[1].overlay, [] );
});

test("8608A0 hide buffs are suppressed once detection levels reach them", () => {
	const flag = effects => collectActiveBuffs( effects, 7, skill ).find( e => e.skill === 40 ).suppressed;
	assert.equal( flag( [ effect( 40, 1 ) ] ), false );
	assert.equal( flag( [ effect( 40, 1 ), effect( 42, 2 ) ] ), false );
	assert.equal( flag( [ effect( 40, 1 ), effect( 41, 2 ) ] ), true );
	assert.equal( flag( [ effect( 40, 1 ), effect( 41, 2, { gid: 8 } ) ] ), false );
});

test("named detection entries are primary with a two-bar gauge; lnks skills get one bar", () => {
	const entries = collectActiveBuffs(
		[ effect( 20, 1, { subject: { gid: 9, name: "Hunter" } } ), effect( 50, 2 ), effect( 10, 3 ) ],
		7,
		skill
	);
	const state = rebuildBuffViewer( target, entries, 0, skill ), icons = buffViewerIcons( target, state, 7, skill );
	assert.deepEqual( ids( state ), [ [ "b20", "b50", "b10" ], [] ] );
	assert.deepEqual( icons.map( i => i.gauge.map( p => p.split( "/" ).at( -1 ) ) ), [
		[ "s_stateodd_time.png", "s_stateodd_time02_gauge.png", "s_stateodd_time_gauge.png" ],
		[ "s_stateodd_time.png", "s_stateodd_time_gauge.png" ],
		[]
	] );
	assert.deepEqual( icons[0].helpSource, { kind: "effect", gid: 7, token: 1, skill: 20, viewer: true } );
});

test("6DE6F0 viewer tooltips omit the remaining time and name the detection source", () => {
	const text = key =>
		({ UIIT_STT_TARGETTING: "Targetting", UIIT_STT_REMAIN_TIME: "Remain", PARAM_SECOND: "s", SN_X: "Stealth" })[
			key
		] ?? "";
	// Parameter lines are covered by the skill tooltip tests; this row has none.
	const directTooltipParams = new Proxy( {}, {
		get: ( _, key ) => [ "nativeParamBlocks", "setValues" ].includes( key ) ? [] : null
	} );
	const row = { nameSymbol: "SN_X", basicLevel: 2, tooltipDescriptionSymbol: "", directTooltipParams };
	const game = {
		vitals: [],
		attachedEffects: [
			effect( 20, 1, { subject: { gid: 9, name: "Hunter" }, remainingMs: 5000, receivedAtMs: 0 } )
		]
	};
	const tooltip = source => buffTooltip( source, game, 0, new Map( [ [ 20, row ] ] ), text ).map( r => r.value );
	const viewerRows = tooltip( { kind: "effect", gid: 7, token: 1, skill: 20, viewer: true } );
	assert.equal( viewerRows.some( v => v.includes( "Remain" ) ), false );
	assert.equal( viewerRows.at( -1 ), "Targetting : Hunter" );
	assert.equal(
		tooltip( { kind: "effect", gid: 7, token: 1, skill: 20 } ).some( v => v.includes( "Remain 5s" ) ),
		true
	);
});

test("abnormal tooltip reads the grade stored under its mask bit", () => {
	const text = key => ({ PARAM_BLOOD: "Bleeding", UIIT_STT_GRADE: " grade" })[key] ?? "";
	const game = { vitals: [ { gid: 7, abnormal: 0x800, abnormalLevels: [ { bit: 0x800, level: 3 } ] } ] };
	assert.equal(
		buffTooltip( { kind: "abnormal", gid: 7, bit: 11 }, game, 0, new Map(), text )[0].value,
		"Bleeding 3 grade"
	);
	assert.equal(
		buffTooltip( { kind: "abnormal", gid: 7, bit: 11, unlevelled: true }, game, 0, new Map(), text )[0].value,
		"Bleeding"
	);
});

test("viewer owner rebuilds on a new subject and diffs once a second", () => {
	const owner = createBuffViewer( target );
	const first = owner.step( 7, 0, [ effect( 10, 1 ) ], 0, skill );
	assert.equal( owner.step( 7, 999, [ effect( 10, 1 ), effect( 11, 2 ) ], 0, skill ), first );
	assert.deepEqual( ids( owner.step( 7, 1000, [ effect( 10, 1 ), effect( 11, 2 ) ], 0, skill ) ), [
		[ "b10", "b11" ],
		[]
	] );
	assert.deepEqual( ids( owner.step( 8, 1001, [ effect( 12, 3, { gid: 8 } ) ], 0, skill ) ), [ [ "b12" ], [] ] );
	assert.deepEqual( ids( owner.step( 0, 1002, [], 0, skill ) ), [ [], [] ] );
});

test("6E64A0 local board takes per-skill flags then the speed rule over board order", () => {
	const slots = [ { kind: "buff", id: 30, serial: 1, suppressed: false }, {
		kind: "buff",
		id: 40,
		serial: 2,
		suppressed: false
	}, { kind: "buff", id: 31, serial: 3, suppressed: false } ];
	const entries = collectActiveBuffs(
		[ effect( 30, 1 ), effect( 40, 2 ), effect( 31, 3 ), effect( 41, 4 ) ],
		7,
		skill
	);
	assert.deepEqual( boardSuppression( slots, entries, skill ).map( s => s.suppressed ), [ true, true, false ] );
	const game = {
		localGid: 7,
		skillCatalog: catalog,
		vitals: [],
		buffSlots: [ { state: "active", serial: 3, secondary: false, effect: effect( 31, 3 ) } ]
	};
	assert.equal( buffBoard( game, 0, new Set( [ 3 ] ) )[0].suppressed, true );
});

test("a token-0 heal landing on the target lists no viewer slot", () => {
	const rows = collectActiveBuffs(
		[ effect( 10, 0 ), effect( 11, 4 ), { ...effect( 20, 0 ), restored: true } ],
		7,
		skill
	);
	assert.deepEqual( rows.map( row => [ row.skill, row.effect.token ] ), [ [ 11, 4 ], [ 20, 0 ] ] );
});
