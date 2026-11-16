/*
===========================================================================

scenarios.mjs - synthetic presentation sessions as world-batch streams

Usage: node scenarios.mjs OUTDIR [--seed N] [--only a,b]

Writes one session file per scenario (OUTDIR/<name>.json.gz) for
replay.mjs. A session is what the simulation worker hands the main thread
frame by frame: world batches (spawn, state, despawn, gameplay and
feedback events), the frame and simulation clocks, the camera and its
pitch. Every scenario is a pure function of the seed, built from the
published roster, item and NPC catalogs the presentation itself loads:
the same seed writes the same bytes.

The rows are synthetic, not the worker's: they carry the fields the
presentation reads, in the shapes the worker publishes them (contracts/
world.ts), without the packet history that produced them.
session-builder.mjs writes them; this file says which sessions exist.

===========================================================================
*/
import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { gzipSync } from "node:zlib";
import { pathToFileURL } from "node:url";

import {
	FRAME_HZ,
	REGION,
	CENTER,
	LOCAL_GID,
	FIRST_PEER_GID,
	MOVEMENT_SEATED,
	MOVEMENT_RUN,
	MOVEMENT_WALK,
	ALIVE,
	DEAD,
	STALL_TITLE,
	CROWD_BODIES,
	seeded,
	loadCatalogs,
	wear,
	gearFor,
	sunGearFor,
	heading,
	pose,
	createSession,
	playerRow,
	npcRow,
	peer,
	grid,
	localStart,
	walk
} from "./session-builder.mjs";

/*
================
standing

200 geared players standing for 60 s around the local player: long
enough for every idle fidget timer to fire.
================
*/
function standing( seed ) {
	const catalogs = loadCatalogs(), rng = seeded( seed ), session = createSession( "standing", seed );
	localStart( session, catalogs, rng );
	for ( let i = 0; i < 200; i++ ) peer( session, catalogs, rng, i, FIRST_PEER_GID + i, grid( rng, i ) );
	return session.finish( 60 * FRAME_HZ );
}

/*
================
walkers

30 players walking and running legs between random points on sand,
with pauses: movement entry, the walk cursor, foot contacts, footprints
and footstep cues.
================
*/
function walkers( seed ) {
	const catalogs = loadCatalogs(), rng = seeded( seed );
	const session = createSession( "walkers", seed, { surface: "SAND" } ), end = 20 * FRAME_HZ;
	localStart( session, catalogs, rng );
	for ( let i = 0; i < 30; i++ ) {
		const gid = FIRST_PEER_GID + i;
		peer( session, catalogs, rng, i, gid, grid( rng, i ) );
		let at = 30 + rng.int( 60 );
		while ( at < end - 4 * FRAME_HZ ) {
			const to = { x: CENTER - 90 + rng.int( 180 ), z: CENTER + rng.int( 180 ) };
			at = walk( session, gid, at, to, rng.next() < .3 ? MOVEMENT_WALK : MOVEMENT_RUN ) + 20 + rng.int( 90 );
		}
	}
	return session.finish( end );
}

/*
================
combat

A player casts its sword attack (skill 2) on two monsters: two hits,
a knockdown (impact type 4) that wakes on its own, a fatal hit and the
corpse's despawn. Each cast turns the caster to its target; a step
aside between casts releases the held yaw.
================
*/
function combat( seed ) {
	const catalogs = loadCatalogs(), rng = seeded( seed ), session = createSession( "combat", seed );
	const caster = FIRST_PEER_GID, prey = FIRST_PEER_GID + 1, brute = FIRST_PEER_GID + 2;
	const vitals = [
		{ gid: prey, hp: 400, maxHp: 400, abnormal: 0 },
		{ gid: brute, hp: 400, maxHp: 400, abnormal: 0 }
	];
	localStart( session, catalogs, rng, vitals );
	session.spawn(
		0,
		playerRow( catalogs, caster, "CHAR_CH_MAN_ADVENTURER", pose( CENTER, CENTER + 40, 32768 ), {
			equipment: [
				wear( catalogs, "ITEM_CH_SWORD_05_A", "CH_M" ),
				wear( catalogs, "ITEM_CH_SHIELD_05_A", "CH_M" )
			]
		} )
	);
	session.spawn( 0, npcRow( catalogs, prey, "MOB_CH_MANGNYANG", pose( CENTER + 20, CENTER + 70, 32768 ) ) );
	session.spawn( 0, npcRow( catalogs, brute, "MOB_CH_TIGER", pose( CENTER - 25, CENTER + 60, 32768 ) ) );
	for ( const row of vitals ) session.event( 0, { kind: "hp-seed", gid: row.gid, hp: row.hp } );
	let token = 0;
	/*
	================
	cast

	One cast's life: received, shot with its impacts 30 frames later
	(each impact's result reference first, as combat.ts publishes it),
	finished 50 frames after that.
	================
	*/
	const cast = ( at, target, impact ) => {
		const base = { token: ++token, caster, target, skill: 2, damage: impact.damage, fatal: impact.fatal };
		const received = { ...base, receivedAtMs: session.simulationMs( at ) };
		session.gameplay( at, { casts: [ received ] } );
		session.event( at + 30, { kind: "hp-result", gid: target, key: `${token}:${target}:0`, fatal: impact.fatal } );
		session.gameplay( at + 30, {
			casts: [ {
				...received,
				shotAtMs: session.simulationMs( at + 30 ),
				results: [ { target, impacts: [ { flags: 0, secondaryAmount: 0, ...impact } ] } ]
			} ]
		} );
		session.gameplay( at + 80, { casts: [] } );
	};
	cast( 120, prey, { type: 0, damage: 60, fatal: false } );
	cast( 300, prey, { type: 0, damage: 80, fatal: false } );
	// The facing holds until the caster moves (cast-facing.ts): it steps
	// aside, and the body turns back from the held yaw.
	walk( session, caster, 420, { x: CENTER - 12, z: CENTER + 30 } );
	cast( 480, brute, { type: 4, damage: 30, fatal: false } );
	cast( 900, prey, { type: 0, damage: 260, fatal: true } );
	session.state( 932, prey, { appearanceState: [ DEAD, 0, 0 ] } );
	session.gameplay( 932, {
		vitals: [ { gid: LOCAL_GID, hp: 1000, maxHp: 1000, abnormal: 0 }, { ...vitals[0], hp: 0 }, vitals[1] ]
	} );
	session.despawn( 1200, prey );
	return session.finish( 22 * FRAME_HZ );
}

/*
================
sitStand

Players sit (movement mode 4) and stand again: the posture one-shots
and the height ramp.
================
*/
function sitStand( seed ) {
	const catalogs = loadCatalogs(), rng = seeded( seed ), session = createSession( "sit-stand", seed );
	localStart( session, catalogs, rng );
	for ( let i = 0; i < 6; i++ ) {
		const gid = FIRST_PEER_GID + i;
		peer( session, catalogs, rng, i, gid, grid( rng, i ) );
		session.state( 60 + i * 20, gid, { movementMode: MOVEMENT_SEATED } );
		session.state( 360 + i * 20, gid, { movementMode: MOVEMENT_RUN } );
	}
	return session.finish( 10 * FRAME_HZ );
}

/*
================
stall

A player opens a stall (title mode 4: motion 80 and the booth) and
closes it.
================
*/
function stall( seed ) {
	const catalogs = loadCatalogs(), rng = seeded( seed ), session = createSession( "stall", seed );
	localStart( session, catalogs, rng );
	const gid = FIRST_PEER_GID;
	peer( session, catalogs, rng, 0, gid, grid( rng, 7 ) );
	session.state( 60, gid, { appearanceState: [ ALIVE, 0, 0, 0, 0, 0, STALL_TITLE ] } );
	session.state( 420, gid, { appearanceState: [ ALIVE, 0, 0, 0, 0, 0, 0 ] } );
	return session.finish( 10 * FRAME_HZ );
}

/*
================
ride

A horse spawns, a player mounts it, the horse runs a leg with its rider,
the rider dismounts and the horse despawns.
================
*/
function ride( seed ) {
	const catalogs = loadCatalogs(), rng = seeded( seed ), session = createSession( "ride", seed );
	localStart( session, catalogs, rng );
	const rider = FIRST_PEER_GID, horse = FIRST_PEER_GID + 1;
	peer( session, catalogs, rng, 0, rider, pose( CENTER - 30, CENTER + 40 ) );
	session.spawn(
		30,
		npcRow( catalogs, horse, "COS_C_HORSE1", pose( CENTER - 30, CENTER + 40 ), { ownerGid: rider } )
	);
	session.state( 90, rider, { mountedOn: horse } );
	const arrival = walk( session, horse, 150, { x: CENTER + 60, z: CENTER + 90 } );
	const row = session.row( horse );
	session.state( arrival, rider, { x: row.x, z: row.z, heading: row.heading } );
	session.state( arrival + 60, rider, { mountedOn: undefined } );
	session.despawn( arrival + 120, horse );
	return session.finish( arrival + 240 );
}

/*
================
equipment

Players change their whole set every two seconds, then strip to
nothing: assembly, residency and the weapon's motion set.
================
*/
function equipment( seed ) {
	const catalogs = loadCatalogs(), rng = seeded( seed ), session = createSession( "equipment", seed );
	localStart( session, catalogs, rng );
	for ( let i = 0; i < 6; i++ ) {
		const gid = FIRST_PEER_GID + i, codename = CROWD_BODIES[i % CROWD_BODIES.length];
		peer( session, catalogs, rng, i, gid, grid( rng, i ) );
		for ( let at = 120 + i * 10; at < 600; at += 120 ) {
			session.state( at, gid, { equipment: gearFor( catalogs, rng, codename ) } );
		}
		session.state( 660 + i * 10, gid, { equipment: [] } );
	}
	return session.finish( 14 * FRAME_HZ );
}

/*
================
statuses

Freeze (abnormal bit 0x1: the pose lock) and slow (0x2: half rate) on
standing and walking players, set and cleared through their vitals.
================
*/
function statuses( seed ) {
	const catalogs = loadCatalogs(), rng = seeded( seed ), session = createSession( "statuses", seed );
	const gids = [ 0, 1, 2, 3 ].map( i => FIRST_PEER_GID + i );
	const vitals = abnormal => gids.map( ( gid, i ) => ({ gid, hp: 500, maxHp: 500, abnormal: abnormal[i] }) );
	localStart( session, catalogs, rng, vitals( [ 0, 0, 0, 0 ] ) );
	gids.forEach( ( gid, i ) => peer( session, catalogs, rng, i, gid, grid( rng, i * 3 ) ) );
	walk( session, gids[2], 60, { x: CENTER - 60, z: CENTER + 150 }, MOVEMENT_WALK );
	walk( session, gids[3], 60, { x: CENTER + 60, z: CENTER + 150 }, MOVEMENT_WALK );
	const local = { gid: LOCAL_GID, hp: 1000, maxHp: 1000, abnormal: 0 };
	session.gameplay( 120, { vitals: [ local, ...vitals( [ 0x1, 0x2, 0x1, 0x2 ] ) ] } );
	session.gameplay( 420, { vitals: [ local, ...vitals( [ 0, 0, 0, 0 ] ) ] } );
	return session.finish( 10 * FRAME_HZ );
}

/*
================
emote

Players play each emote action in turn; one walks off during its emote.
================
*/
function emote( seed ) {
	const catalogs = loadCatalogs(), rng = seeded( seed ), session = createSession( "emote", seed );
	localStart( session, catalogs, rng );
	// Each gid's rows are written in frame order: a row carries every
	// change before it.
	for ( let i = 0; i < 4; i++ ) {
		const gid = FIRST_PEER_GID + i;
		peer( session, catalogs, rng, i, gid, grid( rng, i * 2 ) );
		for ( let n = 0; n < (i === 0 ? 1 : 7); n++ ) {
			const at = 60 + n * 150 + i * 7;
			session.state( at, gid, { emote: { action: n, revision: n + 1, atMs: session.simulationMs( at ) } } );
		}
	}
	walk( session, FIRST_PEER_GID, 100, { x: CENTER + 40, z: CENTER + 120 } );
	return session.finish( 20 * FRAME_HZ );
}

/*
================
pickup

Players pick up every two seconds (pickupRevision); one is mounted, and
mounted riders ignore the pickup.
================
*/
function pickup( seed ) {
	const catalogs = loadCatalogs(), rng = seeded( seed ), session = createSession( "pickup", seed );
	localStart( session, catalogs, rng );
	const horse = FIRST_PEER_GID + 10;
	session.spawn( 0, npcRow( catalogs, horse, "COS_C_HORSE1", grid( rng, 3 ) ) );
	for ( let i = 0; i < 3; i++ ) {
		const gid = FIRST_PEER_GID + i;
		peer( session, catalogs, rng, i, gid, grid( rng, i ), i === 2 ? { mountedOn: horse } : {} );
		for ( let n = 1; n <= 4; n++ ) session.state( n * 120 + i * 5, gid, { pickupRevision: n } );
	}
	return session.finish( 11 * FRAME_HZ );
}

/*
================
churn

Spawn and despawn churn at the edge of view: every half second five
players leave and five new ones arrive; one gid respawns as a new body.
================
*/
function churn( seed ) {
	const catalogs = loadCatalogs(), rng = seeded( seed ), session = createSession( "churn", seed );
	localStart( session, catalogs, rng );
	let next = FIRST_PEER_GID;
	const live = [];
	for ( let i = 0; i < 40; i++ ) live.push( peer( session, catalogs, rng, i, next++, grid( rng, i ) ).gid );
	for ( let at = 60; at < 20 * FRAME_HZ; at += 30 ) {
		for ( let n = 0; n < 5; n++ ) {
			session.despawn( at, live.splice( rng.int( live.length ), 1 )[0] );
			const codename = rng.pick( CROWD_BODIES ), gid = next++;
			const row = playerRow( catalogs, gid, codename, grid( rng, rng.int( 60 ) ), {
				equipment: gearFor( catalogs, rng, codename )
			} );
			session.spawn( at, row );
			live.push( gid );
		}
	}
	// One of the first crowd, alive throughout: its gid is reused.
	const reused = live.find( gid => gid < FIRST_PEER_GID + 40 );
	session.despawn( 900, reused );
	session.spawn( 900, playerRow( catalogs, reused, "CHAR_EU_MAN_NOBLE", grid( rng, 5 ) ) );
	return session.finish( 22 * FRAME_HZ );
}

/*
================
camera

60 standing players while the camera pulls back from 20 to 450 units
and returns: every entity LOD rescan on the way sees new distances.
================
*/
function camera( seed ) {
	const catalogs = loadCatalogs(), rng = seeded( seed ), session = createSession( "camera", seed );
	localStart( session, catalogs, rng );
	for ( let i = 0; i < 60; i++ ) peer( session, catalogs, rng, i, FIRST_PEER_GID + i, grid( rng, i, 20 ) );
	const end = 24 * FRAME_HZ;
	for ( let at = 0; at < end; at++ ) {
		const t = 1 - Math.abs( 1 - 2 * at / end ), back = 20 + 430 * t;
		session.view( at, { x: CENTER, y: 20 + back * .4, z: CENTER - back }, -0.35 );
	}
	return session.finish( end );
}

/*
================
groundItems

Gold heaps and equipment drops land, one is claimed, and all despawn.
================
*/
function groundItems( seed ) {
	const catalogs = loadCatalogs(), rng = seeded( seed ), session = createSession( "ground-items", seed );
	localStart( session, catalogs, rng );
	const codes = [
		"ITEM_ETC_GOLD_01",
		"ITEM_ETC_GOLD_02",
		"ITEM_ETC_GOLD_03",
		"ITEM_CH_SWORD_05_A",
		"ITEM_EU_STAFF_03_A"
	];
	codes.forEach( ( code, i ) => {
		const refObjId = catalogs.byCode.get( code ), gid = FIRST_PEER_GID + i, at = 30 + i * 40;
		const { regionId, x, y, z, angle } = grid( rng, i * 2 );
		session.spawn( at, {
			gid,
			refObjId,
			kind: "ground-item",
			name: code,
			regionId,
			x,
			y,
			z,
			heading: angle,
			groundItem: {
				typeFlags: catalogs.items[refObjId].typeFlags,
				goldAmount: i < 3 ? 100 * (i + 1) : 0,
				tint: 0
			}
		} );
		if ( i === 3 ) {
			session.state( 400, gid, { groundItem: { ...session.row( gid ).groundItem, claimantGid: LOCAL_GID } } );
		}
		session.despawn( 500 + i * 20, gid );
	} );
	return session.finish( 12 * FRAME_HZ );
}

/*
================
settle

Fifteen players and four NPCs stand until their motion is settled (a
steady decision, presentation-due.ts); then each meets one kind of input,
one every 40 frames, that must unsettle it on the frame it lands: a hit
and the caster's cast (action layers, the WAIT state, the combat stance),
a death, an emote, sitting and standing, an equipment change, a mount and
a dismount, a stall, a teleport to another region and back, a pickup, an
attached effect's host motion, a disguise (an attached msch 3 reference
appearance: the body changes with no new row), a slow; a monster is knocked down, one
with no appearance state dies by its vitals alone, and a guard tower's
damage changes its stage model on its own timer, with no new row. Then
the dress inputs no row of the wearer carries: a cast whose skill hides
the weapon, the server's job-suit and Moonlit Ash rules over a thief suit,
and the local player joining an arena team, which dresses a peer of the
other team in fortress clothes. The target changes, to a near player and
to one beyond the protected animation distance, a pet resolve's stack
window ends, and the camera pulls back across a LOD rescan: inputs only
the published actor reads. 20 s of standing lets the idle fidget timers
fire.
================
*/
function settle( seed ) {
	const catalogs = loadCatalogs(), rng = seeded( seed ), session = createSession( "settle", seed );
	const players = Array.from( { length: 13 }, ( _, i ) => FIRST_PEER_GID + i );
	const brute = FIRST_PEER_GID + 20, bare = FIRST_PEER_GID + 21, horse = FIRST_PEER_GID + 22;
	const tower = FIRST_PEER_GID + 23, towerHp = 10000, thief = FIRST_PEER_GID + 24;
	const far = FIRST_PEER_GID + 25, resolute = FIRST_PEER_GID + 26;
	// The gameplay vitals list is replaced whole: keep every row's current one.
	const vitals = new Map( [ LOCAL_GID, ...players, brute, bare, tower ].map( gid => {
		const hp = gid === LOCAL_GID ? 1000 : gid === tower ? towerHp : 500;
		return [ gid, { gid, hp, maxHp: hp, abnormal: 0 } ];
	} ) );
	const setVital = ( at, gid, changes ) => {
		vitals.set( gid, { ...vitals.get( gid ), ...changes } );
		session.gameplay( at, { vitals: [ ...vitals.values() ] } );
	};
	localStart( session, catalogs, rng, [ ...vitals.values() ].filter( row => row.gid !== LOCAL_GID ) );
	players.forEach( ( gid, i ) => peer( session, catalogs, rng, i, gid, grid( rng, i ) ) );
	// A player beyond the protected animation distance (300) from the local
	// player, whose animation becoming the target protects (animationLod),
	// and one under a pet resolve whose stack window ends with no new row.
	session.spawn(
		0,
		playerRow( catalogs, far, "CHAR_EU_MAN_NOBLE", pose( CENTER, CENTER + 420 ), {
			equipment: gearFor( catalogs, rng, "CHAR_EU_MAN_NOBLE" )
		} )
	);
	session.spawn(
		0,
		playerRow( catalogs, resolute, "CHAR_CH_WOMAN_ADVENTURER", grid( rng, 16 ), {
			petResolve: { untilMs: session.simulationMs( 700 ), stacks: 3 }
		} )
	);
	// A Chinese man in a thief suit (job slot), of arena team 1.
	session.spawn(
		0,
		playerRow( catalogs, thief, "CHAR_CH_MAN_ADVENTURER", grid( rng, 14 ), {
			equipment: [
				...gearFor( catalogs, rng, "CHAR_CH_MAN_ADVENTURER" ),
				wear( catalogs, "ITEM_CH_M_TRADE_THIEF_02", "CH_M" )
			],
			arenaTeam: 1
		} )
	);
	session.spawn( 0, npcRow( catalogs, brute, "MOB_CH_TIGER", grid( rng, 20 ) ) );
	const { appearanceState: _state, ...bareRow } = npcRow( catalogs, bare, "MOB_CH_MANGNYANG", grid( rng, 22 ) );
	session.spawn( 0, bareRow );
	session.spawn( 0, npcRow( catalogs, horse, "COS_C_HORSE1", grid( rng, 24 ), { ownerGid: players[6] } ) );
	session.spawn(
		0,
		npcRow( catalogs, tower, "STRUCTURE_GUARD_TOWER_01", grid( rng, 27 ), { maxHp: towerHp, structureState: 0 } )
	);
	// Only the brute's hp is seeded: a seeded hp stands over the vitals'
	// (presentation.ts), and the bare monster dies by its vitals.
	session.event( 0, { kind: "hp-seed", gid: brute, hp: 500 } );
	let at = 300, token = 0;
	const next = () => at += 40;
	/*
	================
	cast

	One cast's life as combat() writes it: received, shot with its impact
	30 frames later, finished 50 frames after that.
	================
	*/
	const cast = ( when, caster, target, impact, skill = 2 ) => {
		const base = { token: ++token, caster, target, skill, damage: impact.damage, fatal: impact.fatal };
		const received = { ...base, receivedAtMs: session.simulationMs( when ) };
		session.gameplay( when, { casts: [ received ] } );
		session.event( when + 30, {
			kind: "hp-result",
			gid: target,
			key: `${token}:${target}:0`,
			fatal: impact.fatal
		} );
		session.gameplay( when + 30, {
			casts: [ {
				...received,
				shotAtMs: session.simulationMs( when + 30 ),
				results: [ { target, impacts: [ { flags: 0, secondaryAmount: 0, ...impact } ] } ]
			} ]
		} );
		session.gameplay( when + 80, { casts: [] } );
	};
	cast( next(), players[1], players[0], { type: 0, damage: 50, fatal: false } );
	const death = next();
	session.state( death, players[2], { appearanceState: [ DEAD, 0, 0, 0, 0, 0, 0 ] } );
	setVital( death, players[2], { hp: 0 } );
	const emoted = next();
	session.state( emoted, players[3], { emote: { action: 1, revision: 1, atMs: session.simulationMs( emoted ) } } );
	const sat = next();
	session.state( sat, players[4], { movementMode: MOVEMENT_SEATED } );
	session.state( sat + 240, players[4], { movementMode: MOVEMENT_RUN } );
	session.state( next(), players[5], { equipment: gearFor( catalogs, rng, CROWD_BODIES[5 % CROWD_BODIES.length] ) } );
	const mounted = next();
	session.state( mounted, players[6], { mountedOn: horse } );
	session.state( mounted + 120, players[6], { mountedOn: undefined } );
	const stalled = next();
	session.state( stalled, players[7], { appearanceState: [ ALIVE, 0, 0, 0, 0, 0, STALL_TITLE ] } );
	session.state( stalled + 240, players[7], { appearanceState: [ ALIVE, 0, 0, 0, 0, 0, 0 ] } );
	const home = session.row( players[8] ), away = next();
	session.state( away, players[8], { regionId: REGION + 1, x: 400, z: 400 } );
	session.state( away + 120, players[8], { regionId: home.regionId, x: home.x, z: home.z } );
	session.state( next(), players[9], { pickupRevision: 1 } );
	// Skill 20507 attaches motion default 188 to its host (the adventurer
	// bodies hold attached-default-188).
	// The attached effects list is replaced whole too.
	const effects = new Map();
	const attach = ( when, key, effect ) => {
		if ( effect ) effects.set( key, effect );
		else effects.delete( key );
		session.gameplay( when, { attachedEffects: [ ...effects.values() ] } );
	};
	const attached = next();
	attach( attached, "motion", { gid: players[10], skill: 20507, token: 900, phase: 1 } );
	// A second row on the same host, with a motion of its own.
	attach( attached + 60, "second motion", { gid: players[10], skill: 20507, token: 903, phase: 1 } );
	attach( attached + 180, "motion" );
	attach( attached + 240, "second motion" );
	// Skill 9452 is a type 3 reference appearance (characterActionData.json).
	const disguised = next();
	attach( disguised, "disguise", { gid: players[12], skill: 9452, token: 901, phase: 1 } );
	attach( disguised + 240, "disguise" );
	// A slow changes nothing a single timed layer shows: the pickup while it
	// holds is the multi-layer frame that reads the slowed clock.
	const slowed = next();
	setVital( slowed, players[11], { abnormal: 0x2 } );
	session.state( slowed + 60, players[11], { pickupRevision: 1 } );
	setVital( slowed + 180, players[11], { abnormal: 0 } );
	cast( next(), players[1], brute, { type: 4, damage: 30, fatal: false } );
	setVital( next(), bare, { hp: 0 } );
	setVital( next(), tower, { hp: towerHp / 5 } );
	// Berserk (body state 1): the effects owner holds the body's scale at 1.1
	// and eases it back when berserk ends, while the body stands (no cast,
	// so no combat stance, holds the decision).
	const berserk = next();
	session.state( berserk, players[5], { appearanceState: [ ALIVE, 0, 1, 0, 0, 0, 0 ] } );
	session.state( berserk + 120, players[5], { appearanceState: [ ALIVE, 0, 0, 0, 0, 0, 0 ] } );
	at = berserk + 160;
	// Skill 93 hides the caster's weapon while it casts (effectRecords hideWeapon).
	cast( next(), players[5], brute, { type: 0, damage: 10, fatal: false }, 93 );
	const rules = next();
	session.gameplay( rules, { jobSuitHidesArmor: true } );
	session.gameplay( rules + 40, { moonlitThiefSuit: true } );
	session.gameplay( rules + 160, { jobSuitHidesArmor: false, moonlitThiefSuit: false } );
	const arena = next() + 160;
	session.state( arena, LOCAL_GID, { arenaTeam: 0 } );
	session.state( arena + 120, LOCAL_GID, { arenaTeam: 255 } );
	session.gameplay( next(), { target: players[3] } );
	session.gameplay( next(), { target: far } );
	const pulled = next();
	for ( let i = 0; i < 240; i++ ) {
		const back = 120 + 300 * Math.min( 1, i / 120 );
		session.view( pulled + i, { x: CENTER, y: 60 + back * .3, z: CENTER - back }, -0.35 );
	}
	return session.finish( 20 * FRAME_HZ );
}

/*
================
evict

Eight geared players stand the whole run while waves of monsters, one
distinct body each, and of freshly geared players arrive and leave twice a
second: the resident sources outgrow the residency budget and the cache
retires the oldest inactive ones (resources.ts retainWanted) while the
standing players' resolves are skipped. Their sources must keep the same
place in the cache order a resolve gives them.
================
*/
function evict( seed ) {
	const catalogs = loadCatalogs(), rng = seeded( seed ), session = createSession( "evict", seed );
	localStart( session, catalogs, rng );
	for ( let i = 0; i < 8; i++ ) peer( session, catalogs, rng, i, FIRST_PEER_GID + i, grid( rng, i ) );
	const bodies = new Map();
	for ( const [codename, model] of Object.entries( catalogs.npcs ) ) {
		if ( model.kind === "monster" && !bodies.has( model.bsr ?? codename ) ) {
			bodies.set( model.bsr ?? codename, codename );
		}
	}
	const monsters = [ ...bodies.values() ];
	let next = FIRST_PEER_GID + 100, wave = [];
	for ( let at = 60, n = 0; at < 24 * FRAME_HZ; at += 30, n++ ) {
		for ( const gid of wave ) session.despawn( at, gid );
		wave = [];
		for ( let i = 0; i < 5; i++ ) {
			const gid = next++;
			session.spawn(
				at,
				npcRow( catalogs, gid, monsters[(n * 5 + i) % monsters.length], grid( rng, 30 + i * 2 ) )
			);
			wave.push( gid );
		}
		for ( let i = 0; i < 5; i++ ) {
			const codename = rng.pick( CROWD_BODIES ), gid = next++;
			session.spawn(
				at,
				playerRow( catalogs, gid, codename, grid( rng, 31 + i * 2 ), {
					equipment: gearFor( catalogs, rng, codename )
				} )
			);
			wave.push( gid );
		}
	}
	return session.finish( 24 * FRAME_HZ );
}

/*
================
glow

120 players in degree 9 Sun sets standing for 20 s (armor +9, weapon and
shield +12): the crowd the bench draws, where every body carries its
equipment glow programs, three and more particle actors a body. Over the
last 10 s the camera pulls back past the distance that hides a glow
particle (entity-lod.ts): the far bodies' particles hide while the
bodies stand.
================
*/
function glow( seed ) {
	const catalogs = loadCatalogs(), rng = seeded( seed ), session = createSession( "glow", seed );
	localStart( session, catalogs, rng );
	for ( let i = 0; i < 120; i++ ) {
		const codename = CROWD_BODIES[i % CROWD_BODIES.length];
		session.spawn(
			0,
			playerRow( catalogs, FIRST_PEER_GID + i, codename, grid( rng, i ), {
				equipment: sunGearFor( catalogs, rng, codename )
			} )
		);
	}
	const end = 20 * FRAME_HZ;
	for ( let at = end / 2; at < end; at++ ) {
		const back = 20 + 1000 * (2 * at / end - 1);
		session.view( at, { x: CENTER, y: 20 + back * .4, z: CENTER - back }, -0.35 );
	}
	return session.finish( end );
}

export const SCENARIOS = {
	standing,
	walkers,
	combat,
	"sit-stand": sitStand,
	stall,
	ride,
	equipment,
	statuses,
	emote,
	pickup,
	churn,
	camera,
	"ground-items": groundItems,
	settle,
	evict,
	glow
};

/*
================
main
================
*/
function main( argv ) {
	const outDir = argv[0];
	if ( !outDir ) {
		console.error( "usage: node scenarios.mjs OUTDIR [--seed N] [--only a,b]" );
		console.error( `scenarios: ${Object.keys( SCENARIOS ).join( ", " )}` );
		process.exit( 2 );
	}
	let seed = 1, only = Object.keys( SCENARIOS );
	for ( let i = 1; i < argv.length; i += 2 ) {
		if ( argv[i] === "--seed" ) seed = Number( argv[i + 1] );
		else if ( argv[i] === "--only" ) only = argv[i + 1].split( "," );
		else throw Error( `Unknown option ${argv[i]}` );
	}
	if ( !Number.isInteger( seed ) || seed < 0 ) throw Error( "--seed takes a non-negative integer" );
	mkdirSync( outDir, { recursive: true } );
	for ( const name of only ) {
		const build = SCENARIOS[name];
		if ( !build ) throw Error( `Unknown scenario ${name}` );
		const session = build( seed );
		const file = path.join( outDir, `${name}.json.gz` );
		writeFileSync( file, gzipSync( JSON.stringify( session ) ) );
		console.log( `${name}: ${session.frames.length} frames -> ${file}` );
	}
}

if ( import.meta.url === pathToFileURL( process.argv[1] ).href ) main( process.argv.slice( 2 ) );
