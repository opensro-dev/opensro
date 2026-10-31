/*
===========================================================================

world-journal-shared.test.mjs - world journal byte accounting and catalogue
retention across fresh login, portal travel, and transport resume

The 2026-10-14 backlog failure: every HP or LIFE state of a walking monster
carried the same walking path, and the budget charged it to each event
(a 62 KB state, 33 MB with 616 queued) although the batch's structured
clone sends it once.

The 2026-10-19 Donwhang Cave (region 32769) failure (F11-DC1): the static
bootstrap catalogues (27,835 skill rows ~27.1 MB in UTF-16 plus 8,439 GM
items and 12,456 GM monsters ~5.9 MB) rode on the coalesced gameplay
snapshot and were charged against the 32 MiB dynamic world-event backlog,
overflowing when 705 cave floor spawns (537,427 B) arrived in the same
batch, and omitting skillCatalog across a world reset left presentation's
skillCatalog and skillIndex undefined.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { root } from "../../tools/project.mjs";

/*
================
load
================
*/
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { createEntities } = await load( "src/engine/runtime/simulation/worker/session/world/entities/entities.ts" );
const { createWorldCore } = await load( "src/engine/runtime/simulation/worker/session/world/core.ts" );
const { createPresentation } = await load( "src/engine/runtime/presentation/presentation.ts" );
const { journalCost } = await load( "src/engine/foundation/gameplay/journal-cost.ts" );

// About 400 terrain samples: a long chase, as the failing monster held.
const SAMPLES = 400;
// Enough states that full costing passes the 32 MiB budget (~ 64 MB).
const STATES = 1000;

/*
================
walkingPath
================
*/
function walkingPath() {
	return Array.from( { length: SAMPLES }, ( _, i ) => ({
		regionId: 24230,
		x: 1000.123456789 + i,
		y: 12.123456789,
		z: 900.123456789,
		angle: 1234
	}) );
}

/*
================
walker

A walking monster's state, as entities.ts spreads it: the paths stay the
same references from one state to the next.
================
*/
function walker( shared, hp ) {
	const movementPath = { from: shared[0], to: shared[SAMPLES - 1], walkingPath: shared };
	return {
		gid: 471430,
		kind: "monster",
		refObjId: 1933,
		regionId: 24230,
		x: 1000,
		y: 12,
		z: 900,
		heading: 0,
		name: "",
		hp,
		movementPath,
		movementTransition: { relocation: 0, reason: "receipt", eligible: true, walkingPath: shared }
	};
}

test("a walking path shared by many queued states counts once per batch", () => {
	const shared = walkingPath();
	assert.ok(
		journalCost( { kind: "state", entity: walker( shared, 1 ) } ) > 60000,
		"the fixture is the failing size"
	);
	const owner = createEntities();
	for ( let i = 0; i < STATES; i++ ) owner.publish( { kind: "state", entity: walker( shared, i ) } );
	const batch = owner.take();
	assert.equal( batch.events.length, STATES );
	owner.ack( batch.sequence );
	// The next batch is another clone: the path counts again, once.
	for ( let i = 0; i < STATES; i++ ) owner.publish( { kind: "state", entity: walker( shared, i ) } );
	assert.equal( owner.take().events.length, STATES );
});

test("a batch stamps how long its oldest event waited unpublished", () => {
	const owner = createEntities(), shared = walkingPath();
	owner.publish( { kind: "state", entity: walker( shared, 1 ) } );
	// The consumer stalls for a minute of simulation time (a hidden or
	// frozen tab): the batch must say so, so the presentation can drop
	// one-shot work that outlived its natural lifetime in the queue.
	owner.step( 60000 );
	const batch = owner.take();
	assert.equal( batch.spanMs, 60000 );
	owner.ack( batch.sequence );
	// A promptly published batch spans nothing.
	owner.publish( { kind: "state", entity: walker( shared, 2 ) } );
	assert.equal( owner.take().spanMs, 0 );
});

test("distinct paths still count in full and compact past the budget", () => {
	const owner = createEntities();
	for ( let i = 0; i < STATES; i++ ) owner.publish( { kind: "state", entity: walker( walkingPath(), i ) } );
	assert.ok( owner.resyncs() >= 1, "no sharing discount: the budget still bites, as compactions" );
	const batch = owner.take();
	assert.equal( batch.events[0].kind, "reset" );
	owner.ack( batch.sequence );
	assert.equal( owner.synchronized(), true );
});

test("journalCost charges a repeated array once only when given the batch's set", () => {
	const shared = walkingPath(), event = { kind: "state", entity: walker( shared, 1 ) };
	const full = journalCost( event ), set = new WeakSet();
	const first = journalCost( event, set ), second = journalCost( event, set );
	assert.ok( first < full, "the event's own second reference is already shared" );
	assert.ok( second < first / 10, `a repeat costs references only: ${second} vs ${first}` );
	assert.equal( journalCost( event ), full, "without a set nothing is memoised" );
});

test("journalCost charges a repeated object once when given a set", () => {
	const row = skillMetadataRow( 1 );
	const pair = { first: row, second: row, third: [ row, row ] };
	const shared = journalCost( pair, new WeakSet() );
	assert.ok( shared < journalCost( pair ) / 2, `a repeated object costs references only: ${shared}` );
	assert.equal( journalCost( pair ), journalCost( { first: row, second: row, third: [ row, row ] } ) );
});

/*
================
skillMetadataRow

One distinct SkillMetadata row matching foundation/gameplay/skill-catalog.ts
(~487 JSON chars = ~974 UTF-16 bytes per row; 27,835 rows = ~27.1 MB).
================
*/
function skillMetadataRow( id ) {
	return {
		id,
		group: (id % 300) + 1,
		level: (id % 12) + 1,
		name: `SKILL_CH_SWORD_LIGHTNING_SERIES_A_${id}`,
		nameSymbol: `SN_SKILL_CH_SWORD_LIGHTNING_SERIES_A_${id}`,
		icon: "icon\\skillgroup\\china\\skill_ch_sword_lightning_a.ddj",
		buffSecondary: false,
		detectRange: 0,
		buffCancel: "direct",
		spCost: 45,
		reqStr: 0,
		reqInt: 0,
		trainable: true,
		targetRequired: true,
		targetSelf: false,
		groundTarget: false,
		cooldownMs: 4000,
		actionMs: 1000,
		haltsWalk: false,
		range: 3,
		mp: 120,
		cooldownGroup: 0,
		masteries: [ { ID: 257, Level: 60 }, { ID: 0, Level: 0 } ],
		prerequisites: [ { ID: 1, Level: 5 }, { ID: 2, Level: 3 }, { ID: 0, Level: 0 } ]
	};
}

/*
================
realGameplaySnapshot

Real GameplayState shape (contracts/gameplay.ts): skills is readonly number[],
quickSlots is readonly QuickSlot[] with numeric skillId payloads, and
skillCatalog (27,835 rows) + gmCatalog (8,439 items + 12,456 monsters) hold
distinct objects with zero shared references (~33.06 MB in journalCost).
================
*/
function realGameplaySnapshot( { gm = true } = {} ) {
	const skillCatalog = Array.from( { length: 27835 }, ( _, i ) => skillMetadataRow( i + 1 ) );
	const gmCatalog = gm ?
		{
			items: Array.from( { length: 8439 }, ( _, i ) => ({
				refObjId: i + 1,
				codename: `ITEM_CH_SWORD_0${(i % 9) + 1}_C_RARE_${i}`,
				name: `Rare Blade of the Dragon ${i}`,
				kind: "equipment",
				level: 60
			}) ),
			monsters: Array.from( { length: 12456 }, ( _, i ) => ({
				refObjId: 20000 + i,
				codename: `MOB_DH_BUG_GHOST_CLON_${i}`,
				name: `Earth Ghost Warrior ${i}`,
				level: 62,
				monsterType: 0
			}) )
		} :
		undefined;
	return {
		revision: 1,
		localGid: 100001,
		pose: { regionId: 32769, x: 120, y: 0, z: 140, angle: 0 },
		authoritativePose: { regionId: 32769, x: 120, y: 0, z: 140, angle: 0 },
		pendingMoves: 0,
		acknowledgedMove: 0,
		target: 0,
		targetPending: 0,
		inventory: [],
		inventoryPending: false,
		vitals: [ { gid: 100001, hp: 5000, mp: 5000, maxHp: 5000, maxMp: 5000 } ],
		casts: [],
		error: null,
		eligibility: { gm, pcRoomEvent: false },
		skills: Array.from( { length: 600 }, ( _, i ) => i + 1 ),
		quickSlots: Array.from( { length: 50 }, ( _, i ) => ({ slot: i, kind: "skill", skillId: i + 1 }) ),
		skillCatalog,
		...(gmCatalog ? { gmCatalog } : {})
	};
}

/*
================
floorSpawn

One cave-floor monster spawn at the live region 32769 size (~762 B in
journalCost; 705 spawns = ~537,427 B).
================
*/
function floorSpawn( i ) {
	const path = [
		{ regionId: 32769, x: 100.25 + i, y: 12.5, z: 900.75, angle: 120 },
		{ regionId: 32769, x: 108.5 + i, y: 12.5, z: 912.25, angle: 120 }
	];
	return {
		gid: 200000 + i,
		kind: "monster",
		refObjId: 1933,
		regionId: 32769,
		x: 100.25 + i,
		y: 12.5,
		z: 900.75,
		heading: 120,
		name: `Earth Ghost Soldier ${i}`,
		level: 61,
		hp: 14200,
		maxHp: 14200,
		movementPath: { from: path[0], to: path[1], durationMs: 2500, walkingPath: path }
	};
}

test("real GameplayState (33 MB distinct skillCatalog+gmCatalog, plain skills/quickSlots) + 705 cave spawns fits 32 MiB cap for GM and non-GM", () => {
	const gmState = realGameplaySnapshot( { gm: true } );
	const gmEvent = { kind: "gameplay", state: gmState };
	const rawCatalogBytes = journalCost( gmEvent, new WeakSet() );
	assert.ok(
		rawCatalogBytes >= 33_000_000,
		`real distinct skillCatalog+gmCatalog exceeds 33 MB even with WeakSet: ${rawCatalogBytes}`
	);
	let spawnBytes = 0;
	const spawns = Array.from( { length: 705 }, ( _, i ) => {
		const ev = { kind: "spawn", entity: floorSpawn( i ) };
		spawnBytes += journalCost( ev );
		return ev;
	} );
	assert.ok( spawnBytes >= 530_000, `705 cave spawns match the ~537 KB incident burst: ${spawnBytes}` );
	assert.ok(
		rawCatalogBytes + spawnBytes > (32 << 20),
		`combined raw bytes (${rawCatalogBytes + spawnBytes}) exceed the 32 MiB journalByteLimit`
	);

	for ( const gm of [ true, false ] ) {
		const owner = createEntities();
		const presentation = createPresentation();
		const state = gm ? gmState : realGameplaySnapshot( { gm: false } );

		// 1. Fresh login / reload directly inside dense region 32769.
		owner.bootstrap( worldBootstrapFixture( { gm, regionId: 32769 } ), false );
		for ( const ev of spawns ) owner.publish( ev );
		owner.publish( { kind: "gameplay", state } );
		const loginBatch = owner.take();
		assert.equal( loginBatch.events.length, 708 );
		presentation.apply( loginBatch );
		owner.ack( loginBatch.sequence );
		const afterLogin = presentation.gameplay();
		assert.equal( afterLogin?.skillCatalog?.length, 27835 );
		assert.equal( afterLogin?.skillIndex?.get( 1 )?.id, 1 );
		assert.equal( Boolean( afterLogin?.gmCatalog ), gm );

		// 2. Intra-session portal travel (0x3369) into dense region 32769.
		owner.receive( { opcode: 0x3369, payload: new Uint8Array( [ 0x01, 0x80 ] ) }, 10 );
		owner.bootstrap( worldBootstrapFixture( { gm, regionId: 32769 } ), true );
		for ( const ev of spawns ) owner.publish( ev );
		owner.publish( { kind: "gameplay", state } );
		const portalBatch = owner.take();
		assert.equal( portalBatch.events.length, 708 );
		presentation.apply( portalBatch );
		owner.ack( portalBatch.sequence );
		const afterPortal = presentation.gameplay();
		assert.equal( afterPortal?.skillCatalog?.length, 27835 );
		assert.equal( afterPortal?.skillIndex?.get( 27835 )?.id, 27835 );
		assert.equal( Boolean( afterPortal?.gmCatalog ), gm );
	}
});

/*
================
worldBootstrapFixture

Valid world bootstrap payload for createWorldCore covering initial login,
0x3369 / 0x366a portal travel, and resumed=true transport re-entry.
================
*/
function worldBootstrapFixture( { gm = true, regionId = 32769 } = {} ) {
	return {
		protocolVersion: 2,
		nativeResult: 1,
		character: {
			name: gm ? "[GM]Test2" : "asd2",
			gmPrivilege: gm,
			pcRoomEvent: false,
			hp: 5000,
			mp: 5000,
			maxHp: 5000,
			maxMp: 5000,
			gold: 1000000,
			skillPoints: 500,
			statPoints: 0,
			masteries: [ { id: 257, level: 60 } ],
			learnedSkills: [ 1, 2, 3 ]
		},
		localPlayerEntry: {
			gid: 100001,
			refObjId: 1907,
			level: 60,
			countryByte9c: 0,
			startProfile: { regionId, x: 120, y: 0, z: 140, angle: 0 }
		},
		refObjSnapshot: [
			{ refObjId: 1907, kind: "player", countryByte9c: 0, level: 60, maxHp: 5000 },
			{
				refObjId: 1933,
				kind: "monster",
				codename: "MOB_DH_BUG_GHOST",
				name: "Earth Ghost",
				nameStrId: "SN_MOB_DH_BUG_GHOST",
				level: 61,
				maxHp: 14200,
				monsterType: 0
			}
		],
		refItemSnapshot: [
			{ refObjId: 62, typeFlags: 0x100, codename: "ITEM_CH_SWORD_01_A", name: "Crude Sword" }
		],
		refSkillSnapshot: [
			{
				id: 1,
				group: 1,
				level: 1,
				status: false,
				token: false,
				effectRider: false,
				ui: {
					name: "Strike Smash",
					spCost: 10,
					cooldownMs: 2000,
					trainable: true,
					targetRequired: true,
					masteries: [ { ID: 257, Level: 1 }, { ID: 0, Level: 0 } ],
					prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
				}
			},
			{
				id: 2,
				group: 1,
				level: 2,
				status: false,
				token: false,
				effectRider: false,
				ui: {
					name: "Strike Smash II",
					spCost: 20,
					cooldownMs: 2000,
					trainable: true,
					targetRequired: true,
					masteries: [ { ID: 257, Level: 5 }, { ID: 0, Level: 0 } ],
					prerequisites: [ { ID: 1, Level: 1 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
				}
			}
		]
	};
}

test("createWorldCore -> createPresentation retains skillCatalog and skillIndex across fresh login, 0x3369/0x366a portal travel, and resumed bootstrap", () => {
	for ( const gm of [ true, false ] ) {
		const core = createWorldCore( () => {} );
		const presentation = createPresentation();
		const blob = worldBootstrapFixture( { gm, regionId: 25000 } );

		// 1. Fresh login bootstrap.
		core.bootstrap( blob, false, 0 );
		core.step( 0, false );
		const batch1 = core.take();
		assert.ok( batch1 );
		presentation.apply( batch1 );
		core.ack( batch1.sequence );
		const afterLogin = presentation.gameplay();
		assert.equal( afterLogin?.skillCatalog?.length, 2 );
		assert.equal( afterLogin?.skillIndex?.get( 1 )?.name, "Strike Smash" );
		const initialCatalogRef = afterLogin?.skillCatalog;
		const initialGmCatalogRef = afterLogin?.gmCatalog;
		const initialIndexRef = afterLogin?.skillIndex;
		assert.ok( initialCatalogRef );
		if ( gm ) assert.ok( initialGmCatalogRef );

		// Instrument the retained references so any deep-compare walk in
		// sameSkillCatalog(next, retained) increments the counter.
		let skillCompares = 0;
		const firstSkillName = initialCatalogRef[0].name;
		Object.defineProperty( initialCatalogRef[0], "name", {
			enumerable: true,
			configurable: true,
			get() {
				skillCompares++;
				return firstSkillName;
			}
		} );
		let gmCompares = 0;
		if ( gm && initialGmCatalogRef ) {
			const firstMonsterCode = initialGmCatalogRef.monsters[0].codename;
			Object.defineProperty( initialGmCatalogRef.monsters[0], "codename", {
				enumerable: true,
				configurable: true,
				get() {
					gmCompares++;
					return firstMonsterCode;
				}
			} );
		}

		// 2. Intra-session portal travel (0x3369) into dense region 32769.
		core.receive( { opcode: 0x3369, payload: new Uint8Array( [ 0x01, 0x80 ] ) }, 10 );
		core.bootstrap( worldBootstrapFixture( { gm, regionId: 32769 } ), false, 20 );
		core.step( 20, false );
		const batch2 = core.take();
		assert.ok( batch2 );
		presentation.apply( batch2 );
		core.ack( batch2.sequence );
		const afterPortal = presentation.gameplay();
		assert.equal(
			afterPortal?.skillCatalog?.length,
			2,
			"skillCatalog must not become undefined after 0x3369 reset"
		);
		assert.equal( afterPortal?.skillIndex?.get( 2 )?.name, "Strike Smash II" );
		assert.equal( afterPortal?.skillCatalog, initialCatalogRef, "unchanged catalog array identity is reused" );
		assert.equal( afterPortal?.skillIndex, initialIndexRef, "presentation skillIndex Map is reused" );
		if ( gm ) assert.equal( afterPortal?.gmCatalog, initialGmCatalogRef, "unchanged gmCatalog identity is reused" );
		assert.equal( skillCompares, 1, "0x3369 bootstrap compares skillCatalog exactly once" );
		if ( gm ) assert.equal( gmCompares, 1, "0x3369 bootstrap compares gmCatalog exactly once" );

		// Subsequent dirty gameplay steps after portal travel (while walking/acting)
		// must short-circuit on seenCatalog / seenGmCatalog without re-comparing or resending.
		for ( let tick = 1; tick <= 5; tick++ ) {
			core.notice( `post-portal-step-${tick}` );
			core.step( 20 + tick, true );
			const stepBatch = core.take();
			assert.ok( stepBatch );
			for ( const event of stepBatch.events ) {
				if ( event.kind === "gameplay" ) {
					assert.equal(
						"skillCatalog" in event.state,
						false,
						"post-portal step must not resend skillCatalog"
					);
					assert.equal( "gmCatalog" in event.state, false, "post-portal step must not resend gmCatalog" );
				}
			}
			presentation.apply( stepBatch );
			core.ack( stepBatch.sequence );
		}
		assert.equal( skillCompares, 1, "repeated post-portal takes must not re-compare skillCatalog" );
		if ( gm ) assert.equal( gmCompares, 1, "repeated post-portal takes must not re-compare gmCatalog" );

		// 3. Second portal travel (0x366a).
		core.receive( { opcode: 0x366a, payload: new Uint8Array( [ 0x01, 0x80 ] ) }, 30 );
		core.bootstrap( worldBootstrapFixture( { gm, regionId: 32769 } ), false, 40 );
		core.step( 40, false );
		const batch3 = core.take();
		assert.ok( batch3 );
		presentation.apply( batch3 );
		core.ack( batch3.sequence );
		assert.equal( presentation.gameplay()?.skillCatalog, initialCatalogRef );
		assert.equal( presentation.gameplay()?.skillIndex, initialIndexRef );
		if ( gm ) assert.equal( presentation.gameplay()?.gmCatalog, initialGmCatalogRef );
		assert.equal( skillCompares, 2, "0x366a bootstrap compares skillCatalog once" );
		if ( gm ) assert.equal( gmCompares, 2, "0x366a bootstrap compares gmCatalog once" );
		for ( let tick = 1; tick <= 3; tick++ ) {
			core.notice( `post-366a-step-${tick}` );
			core.step( 40 + tick, true );
			const stepBatch = core.take();
			assert.ok( stepBatch );
			presentation.apply( stepBatch );
			core.ack( stepBatch.sequence );
		}
		assert.equal( skillCompares, 2, "repeated post-366a takes must not re-compare skillCatalog" );
		if ( gm ) assert.equal( gmCompares, 2, "repeated post-366a takes must not re-compare gmCatalog" );

		// 4. Transport resume (resumed = true).
		core.bootstrap( worldBootstrapFixture( { gm, regionId: 32769 } ), true, 50 );
		core.step( 50, false );
		const batch4 = core.take();
		assert.ok( batch4 );
		presentation.apply( batch4 );
		core.ack( batch4.sequence );
		const afterResume = presentation.gameplay();
		assert.equal( afterResume?.skillCatalog, initialCatalogRef, "skillCatalog survives resumed=true reset" );
		assert.equal( afterResume?.skillIndex, initialIndexRef, "skillIndex survives resumed=true reset" );
		if ( gm ) assert.equal( afterResume?.gmCatalog, initialGmCatalogRef, "gmCatalog survives resumed=true reset" );
		assert.equal( skillCompares, 3, "resumed=true bootstrap compares skillCatalog once" );
		if ( gm ) assert.equal( gmCompares, 3, "resumed=true bootstrap compares gmCatalog once" );
		for ( let tick = 1; tick <= 3; tick++ ) {
			core.notice( `post-resume-step-${tick}` );
			core.step( 50 + tick, true );
			const stepBatch = core.take();
			assert.ok( stepBatch );
			presentation.apply( stepBatch );
			core.ack( stepBatch.sequence );
		}
		assert.equal( skillCompares, 3, "repeated post-resume takes must not re-compare skillCatalog" );
		if ( gm ) assert.equal( gmCompares, 3, "repeated post-resume takes must not re-compare gmCatalog" );
	}
});

test("core republishes the gameplay snapshot in full behind a journal resync", () => {
	const fixture = JSON.parse(
		fs.readFileSync(
			path.resolve( root, "../server/internal/game/enterworld/testdata/monster_spawn_fixture.json" ),
			"utf8"
		)
	);
	const rows = fixture.packets.map( p => ({ opcode: p.opcode, payload: Buffer.from( p.payloadHex, "hex" ) }) );
	const core = createWorldCore( () => {} );
	const presentation = createPresentation();
	// The core fixture's character over the packet fixture's world rows.
	const blob = { ...worldBootstrapFixture( { gm: false, regionId: 25000 } ), refObjSnapshot: fixture.refObjSnapshot };
	core.bootstrap( blob, false, 0 );
	core.step( 0, false );
	const entry = core.take();
	presentation.apply( entry );
	core.ack( entry.sequence );
	assert.equal( presentation.gameplay()?.skillCatalog?.length, 2 );
	for ( const row of rows ) core.receive( row, 10 );
	core.step( 10, false );
	// The consumer stalls on this batch while the wire keeps producing
	// state behind it, past the journal's byte budget.
	const inflight = core.take();
	assert.ok( inflight );
	const packet = value => {
		const payload = Buffer.alloc( 6 );
		payload.writeUInt32LE( fixture.expect.gid );
		payload[4] = 4;
		payload[5] = value;
		return { opcode: 0x3122, payload };
	};
	let compacted = [];
	for ( let i = 0; i < 2000000 && !compacted.length; i++ ) {
		core.receive( packet( i % 2 ? 4 : 0 ), 10 );
		compacted = core.takeDegradations().filter( d => d.code === "world-journal-resync" );
	}
	assert.equal( compacted.length, 1, "the flood compacts once instead of throwing" );
	presentation.apply( inflight );
	core.ack( inflight.sequence );
	const resync = core.take();
	assert.ok( resync );
	assert.equal( resync.events[0].kind, "reset" );
	// The full snapshot rides the resync batch itself: the reset that
	// opens it nulls the presentation's gameplay, so a snapshot in the
	// NEXT batch would leave one batch with none (or a delta on null).
	const snapshot = resync.events.find( event => event.kind === "gameplay" );
	assert.ok( snapshot, "the full gameplay snapshot rides inside the resync batch" );
	assert.equal( snapshot.state.skillCatalog?.length, 2 );
	presentation.apply( resync );
	core.ack( resync.sequence );
	assert.equal( presentation.gameplay()?.skillCatalog?.length, 2 );
	// Nothing is owed afterwards: the snapshot was consumed by the batch.
	core.step( 20, false );
	const follow = core.take();
	assert.equal( follow?.events.some( event => event.kind === "gameplay" ) ?? false, false );
	if ( follow ) {
		presentation.apply( follow );
		core.ack( follow.sequence );
	}
	assert.equal( presentation.gameplay()?.skillCatalog?.length, 2 );
});

test("core republishes the pending travel inside a journal resync", () => {
	const fixture = JSON.parse(
		fs.readFileSync(
			path.resolve( root, "../server/internal/game/enterworld/testdata/monster_spawn_fixture.json" ),
			"utf8"
		)
	);
	const rows = fixture.packets.map( p => ({ opcode: p.opcode, payload: Buffer.from( p.payloadHex, "hex" ) }) );
	const core = createWorldCore( () => {} );
	const presentation = createPresentation();
	const blob = { ...worldBootstrapFixture( { gm: false, regionId: 25000 } ), refObjSnapshot: fixture.refObjSnapshot };
	core.bootstrap( blob, false, 0 );
	core.step( 0, false );
	const entry = core.take();
	presentation.apply( entry );
	core.ack( entry.sequence );
	for ( const row of rows ) core.receive( row, 10 );
	core.step( 10, false );
	const entered = core.take();
	presentation.apply( entered );
	core.ack( entered.sequence );
	// Portal travel begins; its event waits in the journal queue.
	core.receive( { opcode: 0x3369, payload: new Uint8Array( [ 0x01, 0x80 ] ) }, 20 );
	assert.equal( core.readyRevision(), 1 );
	// State floods past the journal budget before the queue is taken:
	// the queued travel event is dropped with everything else.
	const packet = value => {
		const payload = Buffer.alloc( 6 );
		payload.writeUInt32LE( fixture.expect.gid );
		payload[4] = 4;
		payload[5] = value;
		return { opcode: 0x3122, payload };
	};
	let compacted = [];
	for ( let i = 0; i < 2000000 && !compacted.length; i++ ) {
		core.receive( packet( i % 2 ? 4 : 0 ), 20 );
		compacted = core.takeDegradations().filter( d => d.code === "world-journal-resync" );
	}
	assert.equal( compacted.length, 1, "the flood compacts once instead of throwing" );
	const resync = core.take();
	assert.ok( resync );
	// Without the republish the reset nulls the presentation's travel and
	// world-ready carries revision 0 against the core's 1: never ready.
	const travel = resync.events.find( event => event.kind === "travel" );
	assert.ok( travel, "the resync batch republishes the pending travel" );
	assert.equal( travel.travel.revision, core.readyRevision() );
	assert.equal( travel.travel.region, 0x8001 );
	presentation.apply( resync );
	core.ack( resync.sequence );
	assert.equal( presentation.travel()?.revision, core.readyRevision() );
});

test("a snapshot with stall network bigint price and serial publishes and costs without throwing", () => {
	const owner = createEntities();
	const event = {
		kind: "gameplay",
		state: {
			stall: {
				phase: "none",
				network: {
					open: true,
					rows: [ {
						item: {},
						owner: 1,
						slot: 0,
						quantity: 1,
						price: 12345678901234567890n,
						serial: 987654321n
					} ]
				}
			}
		}
	};
	assert.ok( journalCost( event ) > 0 );
	owner.publish( event );
	const batch = owner.take();
	assert.equal( batch.events.length, 1 );
	assert.equal( batch.events[0].state.stall.network.rows[0].price, 12345678901234567890n );
	assert.equal( batch.events[0].state.stall.network.rows[0].serial, 987654321n );
});
