/*
===========================================================================

session-builder.mjs - how a synthetic presentation session is written

The pieces every scenario (scenarios.mjs) is built from: the seeded
stream, the published catalogs, the session under construction (frames,
world batches, rows written whole as the worker writes them), the rows of
players and NPCs, gear for a body, the crowd grid, the local player's
start and a remote walk. A session is a pure function of its seed.

===========================================================================
*/
import { readFileSync } from "node:fs";
import path from "node:path";
import { CLIENT_PUBLIC_ROOT } from "../../../../../scripts/lib/generatedRoot.mjs";

export const FRAME_HZ = 60;
const FRAME_MS = 1000 / FRAME_HZ;
// Main-clock milliseconds of simulation time zero (simulationOrigin).
const ORIGIN_MS = 1000;
// A region in the middle of the world map; poses are region-relative.
export const REGION = 0x61a8;
export const CENTER = 960;
export const LOCAL_GID = 1;
export const FIRST_PEER_GID = 0x1000;
// contracts/world.ts MOVEMENT_MODE_SEATED; 3 is the worker's default gait (run).
export const MOVEMENT_SEATED = 4;
export const MOVEMENT_RUN = 3;
export const MOVEMENT_WALK = 2;
// appearanceState[0]: 1 alive, 2 dead; [6] 4 is the stall title mode.
export const ALIVE = 1;
export const DEAD = 2;
export const STALL_TITLE = 4;
// The #3600 crowd's bodies: the three player bodies the design names.
export const CROWD_BODIES = [ "CHAR_CH_MAN_ADVENTURER", "CHAR_CH_WOMAN_ADVENTURER", "CHAR_EU_MAN_NOBLE" ];
const ARMOR_FAMILIES = [ "HEAVY", "LIGHT", "CLOTHES" ];

/*
================
seeded

mulberry32: every scenario choice is drawn from it.
================
*/
export function seeded( seed ) {
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
export function loadCatalogs() {
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
export function wear( catalogs, codename, key, plus = 0 ) {
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
export function gearFor( catalogs, rng, codename ) {
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
sunGearFor

A degree 9 Sun set (the _C_RARE rows) for a body, enchanted as a bench
crowd wears it: armor +9, the weapon and shield +12, so every part carries
its rare glow programs (equipment-particles.ts).
================
*/
export function sunGearFor( catalogs, rng, codename ) {
	const { race, sex, key } = bodyKey( codename );
	const family = rng.pick( ARMOR_FAMILIES );
	const parts = [ rng.pick( [ "HA", "CA" ] ), "BA", "SA", "AA", "LA", "FA" ];
	const worn = parts.map( part => wear( catalogs, `ITEM_${race}_${sex}_${family}_09_${part}_C_RARE`, key, 9 ) );
	const weapons = race === "CH" ?
		[ "SWORD", "BLADE", "SPEAR", "TBLADE", "BOW" ] :
		[ "SWORD", "STAFF", "CROSSBOW", "DAGGER", "AXE" ];
	const weapon = rng.pick( weapons );
	worn.push( wear( catalogs, `ITEM_${race}_${weapon}_09_C_RARE`, key, 12 ) );
	if ( weapon === "SWORD" ) worn.push( wear( catalogs, `ITEM_${race}_SHIELD_09_C_RARE`, key, 12 ) );
	return worn.filter( item => item );
}

/*
================
heading

The native angle word for a step from one point to another (0..65535).
================
*/
export function heading( fromX, fromZ, toX, toZ ) {
	const turn = Math.atan2( toX - fromX, toZ - fromZ ) / (Math.PI * 2);
	return Math.round( (turn + 1) * 65536 ) % 65536;
}

/*
================
pose
================
*/
export function pose( x, z, angle = 0, y = 0 ) {
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
export function createSession( name, seed, settings = {} ) {
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
export function playerRow( catalogs, gid, codename, at, extra = {} ) {
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
export function npcRow( catalogs, gid, codename, at, extra = {} ) {
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
export function peer( session, catalogs, rng, index, gid, at, extra = {} ) {
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
export function grid( rng, i, spacing = 12 ) {
	return pose( CENTER - 84 + (i % 15) * spacing, CENTER + 12 + Math.floor( i / 15 ) * spacing, rng.int( 65536 ) );
}

/*
================
localStart

The world reset that opens the session, the local player and its first
gameplay snapshot (frame 0), the camera behind it.
================
*/
export function localStart( session, catalogs, rng, vitals = [] ) {
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
export function walk( session, gid, start, to, mode = MOVEMENT_RUN ) {
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
