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

===========================================================================
*/
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { gzipSync } from "node:zlib";
import { pathToFileURL } from "node:url";
import { CLIENT_PUBLIC_ROOT } from "../../../../../scripts/lib/generatedRoot.mjs";

const FRAME_HZ = 60;
const FRAME_MS = 1000 / FRAME_HZ;
// Main-clock milliseconds of simulation time zero (simulationOrigin).
const ORIGIN_MS = 1000;
// A region in the middle of the world map; poses are region-relative.
const REGION = 0x61a8;
const CENTER = 960;
const LOCAL_GID = 1;
const FIRST_PEER_GID = 0x1000;
// contracts/world.ts MOVEMENT_MODE_SEATED; 3 is the worker's default gait (run).
const MOVEMENT_SEATED = 4;
const MOVEMENT_RUN = 3;
const MOVEMENT_WALK = 2;
// appearanceState[0]: 1 alive, 2 dead; [6] 4 is the stall title mode.
const ALIVE = 1;
const DEAD = 2;
const STALL_TITLE = 4;
// The #3600 crowd's bodies: the three player bodies the design names.
const CROWD_BODIES = [ "CHAR_CH_MAN_ADVENTURER", "CHAR_CH_WOMAN_ADVENTURER", "CHAR_EU_MAN_NOBLE" ];
const ARMOR_FAMILIES = [ "HEAVY", "LIGHT", "CLOTHES" ];

/*
================
seeded

mulberry32: every scenario choice is drawn from it.
================
*/
function seeded( seed ) {
	let state = seed >>> 0;
	const next = () => {
		state = (state + 0x6d2b79f5) >>> 0;
		let t = state;
		t = Math.imul( t ^ (t >>> 15), t | 1 );
		t ^= t + Math.imul( t ^ (t >>> 7), t | 61 );
		return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
	};
	return { next, int: n => Math.floor( next() * n ), pick: list => list[Math.floor( next() * list.length )] };
}

/*
================
loadCatalogs

The published catalogs the scenarios draw bodies, gear and NPCs from.
================
*/
function loadCatalogs() {
	const json = relative => JSON.parse( readFileSync( path.join( CLIENT_PUBLIC_ROOT, relative ), "utf8" ) );
	const roster = json( "assets/char/roster.json" );
	const items = json( "assets/data/missionPresentation.json" ).itemsByRefObjId;
	const npcs = json( "assets/npc/manifest.json" ).models;
	const byCode = new Map( Object.entries( items ).map( ( [id, row] ) => [ row.codename, Number( id ) ] ) );
	const bodies = new Map( roster.models.map( model => [ model.codename, model ] ) );
	return { roster, items, npcs, byCode, bodies };
}

/*
================
bodyKey

The dress key of a player body: CHAR_CH_MAN_* wears CH_M.
================
*/
function bodyKey( codename ) {
	const [, race, sex] = /^CHAR_(CH|EU)_(MAN|WOMAN)_/.exec( codename ) ?? [];
	if ( !race ) throw Error( `Not a player body: ${codename}` );
	return { race, sex: sex === "MAN" ? "M" : "W", key: `${race}_${sex === "MAN" ? "M" : "W"}` };
}

/*
================
wear

One worn item as the worker publishes it (EntityEquipment), or null when
the catalog has no dress entry for this body.
================
*/
function wear( catalogs, codename, key, plus = 0 ) {
	const id = catalogs.byCode.get( codename ), dress = id && catalogs.roster.dress.equipment[id];
	if ( !dress?.bodies?.[key] || typeof dress.slot !== "number" ) return null;
	return { slot: dress.slot, refObjId: id, typeFlags: catalogs.items[id].typeFlags ?? 0, plus };
}

/*
================
gearFor

A full set for a body: armor of one family and degree, a weapon the
catalog dresses on the body, and a shield when the race has one.
================
*/
function gearFor( catalogs, rng, codename ) {
	const { race, sex, key } = bodyKey( codename );
	const family = rng.pick( ARMOR_FAMILIES ), degree = String( 1 + rng.int( 8 ) ).padStart( 2, "0" );
	const parts = [ rng.pick( [ "HA", "CA" ] ), "BA", "SA", "AA", "LA", "FA" ];
	const worn = parts.map( part => wear( catalogs, `ITEM_${race}_${sex}_${family}_${degree}_${part}_A`, key ) );
	const weapons = race === "CH" ?
		[ "SWORD", "BLADE", "SPEAR", "TBLADE", "BOW" ] :
		[ "SWORD", "STAFF", "CROSSBOW", "DAGGER", "AXE" ];
	const weapon = rng.pick( weapons );
	worn.push( wear( catalogs, `ITEM_${race}_${weapon}_${degree}_A`, key, rng.int( 8 ) ) );
	if ( weapon === "SWORD" ) worn.push( wear( catalogs, `ITEM_${race}_SHIELD_${degree}_A`, key ) );
	return worn.filter( item => item );
}

/*
================
heading

The native angle word for a step from one point to another (0..65535).
================
*/
function heading( fromX, fromZ, toX, toZ ) {
	const turn = Math.atan2( toX - fromX, toZ - fromZ ) / (Math.PI * 2);
	return Math.round( (turn + 1) * 65536 ) % 65536;
}

/*
================
pose
================
*/
function pose( x, z, angle = 0, y = 0 ) {
	return { regionId: REGION, x, y, z, angle };
}

/*
================
createSession

A session under construction. It keeps the rows it published, so state
and gameplay changes are written as the worker writes them: the whole new
row, or a gameplay delta. Frames are created on demand; each holds the
clocks and, when anything happened, the batch's ordered events.
================
*/
function createSession( name, seed, settings = {} ) {
	const frames = [], rows = new Map();
	/*
	================
	frame
	================
	*/
	function frame( index ) {
		while ( frames.length <= index ) {
			const i = frames.length;
			frames.push( { nowMs: ORIGIN_MS + i * FRAME_MS, simulationMs: i * FRAME_MS } );
		}
		return frames[index];
	}
	/*
	================
	event
	================
	*/
	function event( index, value ) {
		(frame( index ).batch ??= { events: [] }).events.push( value );
	}
	frame( 0 ).originMs = ORIGIN_MS;
	return {
		event,
		row: gid => rows.get( gid ),
		simulationMs: index => frame( index ).simulationMs,
		/*
		================
		spawn
		================
		*/
		spawn( index, row ) {
			rows.set( row.gid, row );
			event( index, { kind: "spawn", entity: row } );
		},
		/*
		================
		state

		The row again with changes; undefined removes a field.
		================
		*/
		state( index, gid, changes ) {
			const next = { ...rows.get( gid ), ...changes };
			for ( const key of Object.keys( next ) ) if ( next[key] === undefined ) delete next[key];
			rows.set( gid, next );
			event( index, { kind: "state", entity: next } );
		},
		/*
		================
		despawn
		================
		*/
		despawn( index, gid ) {
			rows.delete( gid );
			event( index, { kind: "despawn", gid } );
		},
		/*
		================
		gameplay
		================
		*/
		gameplay( index, changes ) {
			event( index, { kind: "gameplay", state: changes, delta: true } );
		},
		/*
		================
		view

		The camera from this frame on: its eye, looking at the local
		player, and its pitch.
		================
		*/
		view( index, camera, pitch ) {
			const target = frame( index );
			target.camera = { regionId: REGION, ...camera, target: [ CENTER, 10, CENTER ] };
			target.pitch = pitch;
		},
		/*
		================
		finish
		================
		*/
		finish( count ) {
			frame( count - 1 );
			return { format: "sro-presentation-session", version: 1, name, seed, settings, frames };
		}
	};
}

/*
================
playerRow

A remote player row: alive, standing, at its region pose.
================
*/
function playerRow( catalogs, gid, codename, at, extra = {} ) {
	const body = catalogs.bodies.get( codename );
	if ( !body ) throw Error( `Unknown body ${codename}` );
	return {
		gid,
		refObjId: body.refObjId,
		kind: "player",
		name: `peer${gid}`,
		regionId: at.regionId,
		x: at.x,
		y: at.y,
		z: at.z,
		heading: at.angle,
		appearanceState: [ ALIVE, 0, 0, 0, 0, 0, 0 ],
		countryByte9c: bodyKey( codename ).race === "CH" ? 0 : 1,
		level: 80,
		movementMode: MOVEMENT_RUN,
		walkSpeed: body.walkSpeed,
		runSpeed: body.runSpeed,
		moving: false,
		movementRevision: 0,
		equipment: [],
		avatars: [],
		...extra
	};
}

/*
================
npcRow

A monster, NPC or COS row from the published NPC manifest.
================
*/
function npcRow( catalogs, gid, codename, at, extra = {} ) {
	const model = catalogs.npcs[codename];
	if ( !model ) throw Error( `Unknown NPC ${codename}` );
	return {
		gid,
		refObjId: model.refObjId,
		kind: model.kind,
		name: codename,
		regionId: at.regionId,
		x: at.x,
		y: at.y,
		z: at.z,
		heading: at.angle,
		appearanceState: [ ALIVE, 0, 0 ],
		level: 20,
		movementMode: MOVEMENT_RUN,
		walkSpeed: 16,
		runSpeed: 50,
		moving: false,
		movementRevision: 0,
		...extra
	};
}

/*
================
peer

One geared crowd player of a body, spawned at a pose.
================
*/
function peer( session, catalogs, rng, index, gid, at, extra = {} ) {
	const codename = CROWD_BODIES[index % CROWD_BODIES.length];
	const row = playerRow( catalogs, gid, codename, at, { equipment: gearFor( catalogs, rng, codename ), ...extra } );
	session.spawn( 0, row );
	return row;
}

/*
================
grid

The pose of crowd slot i: rows of 15 in front of the local player,
12 units apart.
================
*/
function grid( rng, i, spacing = 12 ) {
	return pose( CENTER - 84 + (i % 15) * spacing, CENTER + 12 + Math.floor( i / 15 ) * spacing, rng.int( 65536 ) );
}

/*
================
localStart

The world reset that opens the session, the local player and its first
gameplay snapshot (frame 0), the camera behind it.
================
*/
function localStart( session, catalogs, rng, vitals = [] ) {
	const at = pose( CENTER, CENTER );
	const codename = "CHAR_CH_MAN_ADVENTURER", equipment = gearFor( catalogs, rng, codename );
	session.event( 0, { kind: "reset", epoch: 1 } );
	session.spawn(
		0,
		playerRow( catalogs, LOCAL_GID, codename, at, { kind: "local-player", name: "local", equipment } )
	);
	session.event( 0, {
		kind: "gameplay",
		state: {
			localGid: LOCAL_GID,
			pose: at,
			moving: false,
			movementRevision: 0,
			inventory: equipment,
			vitals: [ { gid: LOCAL_GID, hp: 1000, maxHp: 1000, abnormal: 0 }, ...vitals ],
			casts: [],
			attachedEffects: []
		}
	} );
	session.view( 0, { x: CENTER, y: 60, z: CENTER - 120 }, -0.35 );
}

/*
================
walk

One leg of a remote walk: the worker's row on every tick from start to
arrival (a new movement revision and path, the sampled pose, moving),
then the stop. Returns the arrival frame.
================
*/
function walk( session, gid, start, to, mode = MOVEMENT_RUN ) {
	const row = session.row( gid ), speed = mode === MOVEMENT_WALK ? row.walkSpeed : row.runSpeed;
	const angle = heading( row.x, row.z, to.x, to.z );
	const from = pose( row.x, row.z, angle, row.y ), target = pose( to.x, to.z, angle, row.y );
	const distance = Math.hypot( to.x - row.x, to.z - row.z );
	const frames = Math.max( 1, Math.ceil( distance / speed * FRAME_HZ ) );
	const revision = row.movementRevision + 1;
	for ( let i = 0; i <= frames; i++ ) {
		const t = i / frames, at = start + i;
		session.state( at, gid, {
			x: from.x + (target.x - from.x) * t,
			z: from.z + (target.z - from.z) * t,
			heading: angle,
			movementMode: mode,
			moving: i < frames,
			movementRevision: revision,
			movementPath: i < frames ? { from, to: target } : undefined,
			poseAtMs: session.simulationMs( at )
		} );
	}
	return start + frames;
}

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
	"ground-items": groundItems
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
