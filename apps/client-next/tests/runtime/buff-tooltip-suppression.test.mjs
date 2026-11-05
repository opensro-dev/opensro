/*
===========================================================================

buff-tooltip-suppression.test.mjs - local buff slot suppression reaches its help

Exercise the board's real help source so the tooltip follows the displayed
slot flag, including duplicate skills and a later refresh that clears it.
Viewer help remains separate from the local board's presentation state.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";

const { buffBoard } = await import( "../../src/engine/foundation/ui/buff-board.ts" );
const { buffTooltip } = await import( "../../src/engine/foundation/ui/buff-tooltip.ts" );

const LOCAL_GID = 7;
const SKILL_ID = 30;
const SLOT_SERIAL = 1;
const EFFECT_TOKEN = 101;
const STOP_COLOR = 0xffff4a4a;
const WHITE = 0xffffffff;

/*
================
text
================
*/
function text( key ) {
	return {
		SKILL_NAME: "Moving March",
		SKILL_DESCRIPTION: "Move faster.",
		UIIT_TOOLTIP_OPERATION_STOP: "Not affected",
		UIIT_STT_REMAIN_TIME: "Remaining time",
		PARAM_SECOND: "s"
	}[key] ?? "";
}

/*
================
fixture

Use supplied skill data and text, without a retail extraction or a browser.
================
*/
function fixture() {
	/** @type {import('../../src/engine/foundation/ui/skill-tooltip-data.ts').SkillTooltipRowView} */
	const row = {
		id: SKILL_ID,
		groupId: 1,
		basicLevel: 1,
		basicActivity: 1,
		masteryId: 0,
		reqMasteryLevel: 0,
		reqStr: 0,
		reqInt: 0,
		reqGroups: [],
		reqLearnSp: 0,
		nameSymbol: "SKILL_NAME",
		chainNextSkillId: 0,
		requiredWeaponKinds: [ 0, 0 ],
		requiredHp: 0,
		requiredMp: 0,
		requiredHpRatio: 0,
		requiredMpRatio: 0,
		tooltipDescriptionSymbol: "SKILL_DESCRIPTION",
		studySymbol: "",
		directTooltipParams: {
			nativeParamBlocks: [],
			durationMs: null,
			multiCount: null,
			downAttackRatio: null,
			criticalFlat: 0,
			criticalRatio: 0,
			tauntFlat: 0,
			tauntRatio: 0,
			knockout: null,
			rangeIncreaseDeci: null,
			knockback: null,
			defense: null,
			recovery: null,
			setValues: [],
			area: null,
			areas: []
		},
		attack: { present: false, flags: 0, percent: 0, minimum: 0, maximum: 0, value5: 0 }
	};
	const effect = {
		gid: LOCAL_GID,
		skill: SKILL_ID,
		token: EFFECT_TOKEN,
		phase: 2,
		remainingMs: 5000,
		receivedAtMs: 0
	};
	/** @type {import('../../src/engine/contracts/gameplay.ts').GameplayState} */
	const game = {
		revision: 1,
		localGid: LOCAL_GID,
		pose: null,
		authoritativePose: null,
		pendingMoves: 0,
		acknowledgedMove: 0,
		target: 0,
		targetPending: 0,
		inventory: [],
		inventoryPending: false,
		vitals: [],
		casts: [],
		error: null,
		attachedEffects: [ effect ],
		buffSlots: [ { state: "active", serial: SLOT_SERIAL, secondary: false, effect } ],
		skillCatalog: [ {
			id: SKILL_ID,
			group: 1,
			level: 1,
			name: "Moving March",
			icon: "skill/europe/bard_speed.ddj",
			spCost: 0,
			trainable: false,
			targetRequired: false,
			cooldownMs: 0,
			masteries: [],
			prerequisites: []
		} ]
	};
	const catalog = Object.assign( new Map( [ [ SKILL_ID, row ] ] ), { groups: new Map() } );
	return { game, catalog };
}

/*
================
localTooltip
================
*/
function localTooltip( f, suppressed ) {
	const icon = defined( buffBoard( f.game, 0, suppressed )[0] );
	return buffTooltip( defined( icon.helpSource ), f.game, 0, f.catalog, text );
}

test("a suppressed local buff shows its localized red warning directly beneath its name", () => {
	const f = fixture();
	const rows = localTooltip( f, new Set( [ SLOT_SERIAL ] ) );
	assert.deepEqual( rows.slice( 0, 4 ), [
		{ value: "Moving March Lv 1", color: WHITE },
		{ value: "(Not affected)", color: STOP_COLOR },
		{ value: " ", color: WHITE },
		{ value: "Move faster.", color: WHITE }
	] );
	assert.equal( rows.at( -1 )?.value, "\nRemaining time 5s" );
});

test("active buffs and a refreshed slot whose suppression cleared have no warning", () => {
	const f = fixture();
	const active = localTooltip( f, new Set() );
	assert.equal( active.some( row => row.value.includes( "Not affected" ) ), false );
	assert.equal( active[0].value, "Moving March Lv 1\n " );
	assert.ok( localTooltip( f, new Set( [ SLOT_SERIAL ] ) ).some( row => row.color === STOP_COLOR ) );
	assert.deepEqual( localTooltip( f, new Set() ), active );
});

test("suppression belongs to one slot instance when two buffs share a skill", () => {
	const f = fixture();
	const otherEffect = { ...defined( f.game.attachedEffects?.[0] ), token: EFFECT_TOKEN + 1 };
	const game = {
		...f.game,
		attachedEffects: [ ...defined( f.game.attachedEffects ), otherEffect ],
		buffSlots: [
			...defined( f.game.buffSlots ),
			{ state: /** @type {const} */ ("active"), serial: SLOT_SERIAL + 1, secondary: false, effect: otherEffect }
		]
	};
	const rows = buffBoard( game, 0, new Set( [ SLOT_SERIAL ] ) ).map( icon =>
		buffTooltip( defined( icon.helpSource ), game, 0, f.catalog, text )
	);
	assert.equal( rows[0].filter( row => row.color === STOP_COLOR ).length, 1 );
	assert.equal( rows[1].filter( row => row.color === STOP_COLOR ).length, 0 );
});

test("viewer help never inherits the local board's suppression warning", () => {
	const f = fixture();
	const source = defined( buffBoard( f.game, 0, new Set( [ SLOT_SERIAL ] ) )[0].helpSource );
	const rows = buffTooltip( { ...source, viewer: true }, f.game, 0, f.catalog, text );
	assert.equal( rows.some( row => row.value.includes( "Not affected" ) ), false );
	assert.equal( rows.some( row => row.value.includes( "Remaining time" ) ), false );
});

test("the warning uses the text catalogue and a removed effect produces no help", () => {
	const f = fixture();
	const source = defined( buffBoard( f.game, 0, new Set( [ SLOT_SERIAL ] ) )[0].helpSource );
	const localized = key => key === "UIIT_TOOLTIP_OPERATION_STOP" ? "Inactif" : text( key );
	const rows = buffTooltip( source, f.game, 0, f.catalog, localized );
	assert.ok( rows.some( row => row.value === "(Inactif)" ) );
	assert.deepEqual( buffTooltip( source, { ...f.game, attachedEffects: [] }, 0, f.catalog, text ), [] );
});
