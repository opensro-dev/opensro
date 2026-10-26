// 2026-09-12 correction: native wire bearing subtracts pi/2 from model yaw
// (853550); decoder 852F80 calls 8535A0 to restore it. Historical model-yaw
// words in movement/patrol vectors were incorrect, not a retail oracle.
// Parity-vector generator for the Go internal/game/world/simulation package.
//
// Historical Node behavior with explicit native heading corrections. Constants
// and clock inputs are made deterministic. Movement and patrol now encode wire
// bearings, not model yaw; do not treat the retired Node output as a native oracle.
//
// Run:  node gen_parity_vectors.mjs
// Output: movement_parity.json (consumed by parity_test.go)

import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

// --- constants (server.mjs) -------------------------------------------------
const missionCharacterWalkSpeed = 20;
const missionCharacterRunSpeed = 50;
const missionCharacterWalkMode = 2;
const missionCharacterRunMode = 3;
const missionNativeRegionSize = 1920;
const missionTwoPi = Math.PI * 2;
const missionNpcPatrolSpanZ = 20;
const missionNpcPatrolStepPerTick = 5;
const missionStartProfilesByRace = {
	europe: { regionId: 0x6b4f, x: 1205, y: 80, z: 396, angle: 0 },
	china: { regionId: 0x62a8, x: 960.418884, y: 20, z: 458.259766, angle: 0 }
};
const MISSION_NPC_SHOP_SPAWN = Object.freeze( {
	regionId: 25511,
	x: 941.5,
	y: 7.2,
	z: 1417.2
} );
const MISSION_NPC_ROSTER = [
	{
		objectId: 200001,
		refObjId: 7495,
		tidWord: 0x0146,
		codename: "NPC_EU_SMITH",
		kind: "npc",
		name: "Weapon Trader Balbardo"
	}
];

// --- coercion helpers (server.mjs, verbatim) --------------------------------
function coerceInteger( value, min, max, fallback ) {
	const integer = Number( value );
	if ( !Number.isInteger( integer ) ) {
		return fallback;
	}

	return Math.min( max, Math.max( min, integer ) );
}

function coerceFiniteNumber( value ) {
	const number = Number( value );
	return Number.isFinite( number ) ? number : undefined;
}

function clampFiniteNumber( value, min, max ) {
	const number = Number( value );
	if ( !Number.isFinite( number ) ) {
		return min;
	}

	return Math.min( max, Math.max( min, number ) );
}

// --- sector grid (server.mjs, verbatim) -------------------------------------
function missionRegionId( sectorX, sectorY ) {
	return (((sectorY & 0xff) << 8) | (sectorX & 0xff)) >>> 0;
}

function missionRegionSectorX( id ) {
	return id & 0xff;
}

function missionRegionSectorY( id ) {
	return (id >> 8) & 0xff;
}

function missionRegionIdFromSeedLocal( seedRegionId, localX, localZ, regionSize ) {
	const seedX = missionRegionSectorX( seedRegionId );
	const seedY = missionRegionSectorY( seedRegionId );
	return missionRegionId( seedX + Math.floor( localX / regionSize ), seedY + Math.floor( localZ / regionSize ) );
}

function missionRegionLocalPositionToSeedLocal( seedRegionId, sourceRegionId, position, regionSize ) {
	const seedX = missionRegionSectorX( seedRegionId );
	const seedY = missionRegionSectorY( seedRegionId );
	const sourceX = missionRegionSectorX( sourceRegionId );
	const sourceY = missionRegionSectorY( sourceRegionId );

	return {
		x: (sourceX - seedX) * regionSize + position.x,
		y: position.y,
		z: (sourceY - seedY) * regionSize + position.z
	};
}

function missionSeedLocalPositionToRegionLocal( seedRegionId, regionId, position, regionSize ) {
	const seedX = missionRegionSectorX( seedRegionId );
	const seedY = missionRegionSectorY( seedRegionId );
	const regionX = missionRegionSectorX( regionId );
	const regionY = missionRegionSectorY( regionId );

	return {
		x: position.x - (regionX - seedX) * regionSize,
		y: position.y,
		z: position.z - (regionY - seedY) * regionSize
	};
}

function missionWorldDistance2D( a, b ) {
	const ax = ((a.regionId ?? 0) & 0xff) * missionNativeRegionSize + Number( a.x ?? 0 );
	const az = (((a.regionId ?? 0) >> 8) & 0x7f) * missionNativeRegionSize + Number( a.z ?? 0 );
	const bx = ((b.regionId ?? 0) & 0xff) * missionNativeRegionSize + Number( b.x ?? 0 );
	const bz = (((b.regionId ?? 0) >> 8) & 0x7f) * missionNativeRegionSize + Number( b.z ?? 0 );
	return Math.hypot( bx - ax, bz - az );
}

// --- movement model (server.mjs, verbatim) ----------------------------------
function missionSpawnFromObject( source, fallback = missionStartProfilesByRace.europe ) {
	return {
		regionId: coerceInteger( source?.regionId, 0, 0xffff, fallback.regionId ),
		x: coerceFiniteNumber( source?.x ) ?? fallback.x,
		y: coerceFiniteNumber( source?.y ) ?? fallback.y,
		z: coerceFiniteNumber( source?.z ) ?? fallback.z,
		angle: coerceInteger( source?.angle, 0, 0xffff, fallback.angle ?? 0 )
	};
}

function missionSpawnFromMovement( movement, previousSpawn ) {
	return {
		regionId: movement.regionId,
		x: movement.x,
		y: movement.y,
		z: movement.z,
		angle: missionHeadingFromMovement( previousSpawn, movement ) ?? previousSpawn?.angle ?? 0
	};
}

function missionLiveSpawnForWorld( world, nowMs = Date.now() ) {
	const spawn = world?.spawn;
	const segment = world?.moveSegment;
	if ( !spawn || !segment || typeof segment !== "object" ) {
		return spawn;
	}
	const startedAtMs = Number( segment.startedAtMs );
	const arrivesAtMs = Number( segment.arrivesAtMs );
	if ( !Number.isFinite( startedAtMs ) || !Number.isFinite( arrivesAtMs ) || arrivesAtMs <= startedAtMs ) {
		return spawn;
	}
	if ( nowMs >= arrivesAtMs ) {
		return spawn;
	}
	const t = Math.max( 0, (nowMs - startedAtMs) / (arrivesAtMs - startedAtMs) );
	const from = missionSpawnFromObject( segment.from, spawn );
	const goalInFromFrame = missionRegionLocalPositionToSeedLocal(
		from.regionId,
		spawn.regionId,
		spawn,
		missionNativeRegionSize
	);
	const seedLocal = {
		x: from.x + (goalInFromFrame.x - from.x) * t,
		y: from.y + (goalInFromFrame.y - from.y) * t,
		z: from.z + (goalInFromFrame.z - from.z) * t
	};
	const regionId = missionRegionIdFromSeedLocal(
		from.regionId,
		seedLocal.x,
		seedLocal.z,
		missionNativeRegionSize
	);
	const local = missionSeedLocalPositionToRegionLocal(
		from.regionId,
		regionId,
		seedLocal,
		missionNativeRegionSize
	);
	return { regionId, x: local.x, y: local.y, z: local.z, angle: spawn.angle };
}

function missionMoveSegmentForTravel( liveFrom, nextSpawn, movementMode, startedAtMs = Date.now() ) {
	const speed = movementMode === missionCharacterWalkMode ? missionCharacterWalkSpeed : missionCharacterRunSpeed;
	const distance = missionWorldDistance2D( liveFrom, nextSpawn );
	const travelMs = speed > 0 ? Math.ceil( (distance / speed) * 1000 ) : 0;
	if ( !(travelMs > 0) ) {
		return undefined;
	}
	return {
		from: {
			regionId: liveFrom.regionId,
			x: liveFrom.x,
			y: liveFrom.y,
			z: liveFrom.z,
			angle: liveFrom.angle
		},
		startedAtMs,
		arrivesAtMs: startedAtMs + travelMs
	};
}

function movementSourceFromMissionSpawn( spawn ) {
	return {
		regionId: spawn.regionId,
		x: spawn.x,
		y: spawn.y,
		z: spawn.z
	};
}

function missionHeadingFromMovement( from, to ) {
	if ( !from || !to ) {
		return undefined;
	}

	const dx = (missionRegionSectorX( to.regionId ) - missionRegionSectorX( from.regionId )) * missionNativeRegionSize +
		to.x -
		from.x;
	const dz = (missionRegionSectorY( to.regionId ) - missionRegionSectorY( from.regionId )) * missionNativeRegionSize +
		to.z -
		from.z;

	if ( Math.hypot( dx, dz ) < 0.000001 ) {
		return undefined;
	}

	const yaw = ((Math.atan2( dz, dx ) % missionTwoPi) + missionTwoPi) % missionTwoPi;
	return Math.round( (yaw / missionTwoPi) * 0xffff ) & 0xffff;
}

// --- wire builders (server.mjs, verbatim) -----------------------------------
class NativePacketWriter {
	bytes = [];

	u8( value ) {
		this.bytes.push( Number( value ) & 0xff );
		return this;
	}

	u16( value ) {
		const number = Number( value ) & 0xffff;
		this.bytes.push( number & 0xff, (number >>> 8) & 0xff );
		return this;
	}

	u32( value ) {
		const number = Number( value ) >>> 0;
		this.bytes.push( number & 0xff, (number >>> 8) & 0xff, (number >>> 16) & 0xff, (number >>> 24) & 0xff );
		return this;
	}

	u64( value ) {
		let bigint = BigInt( value );
		for ( let index = 0; index < 8; index += 1 ) {
			this.bytes.push( Number( bigint & 0xffn ) );
			bigint >>= 8n;
		}
		return this;
	}

	f32( value ) {
		const buffer = new ArrayBuffer( 4 );
		new DataView( buffer ).setFloat32( 0, Number( value ), true );
		this.bytes.push( ...new Uint8Array( buffer ) );
		return this;
	}

	string( value ) {
		const bytes = new TextEncoder().encode( String( value ?? "" ) );
		this.u16( bytes.length );
		this.bytes.push( ...bytes );
		return this;
	}

	toArray() {
		return this.bytes;
	}
}

// The movement wire's coordinate words are signed i16 region-local
// values (the Go encoder's roundI16Word): clamp into [-0x8000, 0x7fff]
// and round; u16() writes the two's-complement bytes. The unsigned form
// this replaces clamped cave-negative coordinates to the origin edge
// (#1679).
function i16Word( value ) {
	return Math.round( clampFiniteNumber( Number( value ), -0x8000, 0x7fff ) );
}

function buildV150MovementAckPayload( objectId, movement, source ) {
	const writer = new NativePacketWriter();

	writer
		.u32( objectId )
		.u8( movement.mode )
		.u16( movement.regionId )
		.u16( i16Word( movement.x ) )
		.u16( i16Word( movement.y ) )
		.u16( i16Word( movement.z ) );

	if ( source ) {
		writer
			.u8( 1 )
			.u16( source.regionId )
			.u16( i16Word( source.x * 10 ) )
			.f32( source.y )
			.u16( i16Word( source.z * 10 ) );
	} else {
		writer.u8( 0 );
	}

	return writer.toArray();
}

function computeNpcPatrolState( spawnProfile, npcIndex, tick ) {
	const ax = clampFiniteNumber( Number( spawnProfile.x ) + 8, 0, 0xffff );
	const ay = Number( spawnProfile.y );
	const az = clampFiniteNumber( Number( spawnProfile.z ) + 5, 0, 0xffff );
	const bz = clampFiniteNumber( az + missionNpcPatrolSpanZ, 0, 0xffff );

	const legTicks = Math.max( 1, Math.ceil( Math.abs( bz - az ) / missionNpcPatrolStepPerTick ) );
	const phase = ((Number( tick ) % (2 * legTicks)) + 2 * legTicks) % (2 * legTicks);
	const t = phase <= legTicks ? phase / legTicks : (2 * legTicks - phase) / legTicks;
	const z = az + (bz - az) * t;
	const movingToB = phase <= legTicks;
	// Encode the actual leg bearing; +Z is wire pi/2 (model yaw pi).
	const legDz = movingToB ? bz - az : az - bz;
	const yaw = ((Math.atan2( legDz, 0 ) % missionTwoPi) + missionTwoPi) % missionTwoPi;
	const headingWord = Math.round( (yaw / missionTwoPi) * 65535 ) & 0xffff;

	return {
		regionId: Number( spawnProfile.regionId ) & 0xffff,
		x: ax,
		y: ay,
		z,
		headingWord,
		npcIndex
	};
}

function buildV150NpcObjectMovePayload( state, gid ) {
	return new NativePacketWriter()
		.u16( state.regionId )
		.f32( state.x )
		.f32( state.y )
		.f32( state.z )
		.u16( state.headingWord )
		.u32( gid )
		.toArray();
}

function buildV150NpcSourceCorrectionPayload( state, gid ) {
	return new NativePacketWriter()
		.u32( gid )
		.u16( state.regionId )
		.f32( state.x )
		.f32( state.y )
		.f32( state.z )
		.u16( state.headingWord )
		.toArray();
}

function buildV150NpcRefreshStatePayload( gid, stateType, value ) {
	return new NativePacketWriter().u32( gid ).u8( stateType ).u8( value ).toArray();
}

function buildV150NpcDespawnPayload( gid ) {
	return new NativePacketWriter().u32( gid ).toArray();
}

// buildV150NpcCreateRow with resolveMissionNpcSpawnAnchor replaced by the
// explicit `spawn` anchor argument (body otherwise verbatim).
function buildV150NpcCreateRow( npc, spawn ) {
	const x = clampFiniteNumber( Number( spawn.x ) + 8, 0, 0xffff );
	const z = clampFiniteNumber( Number( spawn.z ) + 5, 0, 0xffff );

	return new NativePacketWriter()
		.u32( npc.refObjId )
		.u32( npc.objectId )
		.u16( spawn.regionId )
		.f32( x )
		.f32( spawn.y )
		.f32( z )
		.u16( 0 )
		.u8( 0 )
		.u8( 1 )
		.u8( 0 )
		.u16( 0 )
		.u8( 0 )
		.u8( 0 )
		.u8( 0 )
		.f32( 20 )
		.f32( 50 )
		.f32( 100 )
		.u8( 0 )
		.u8( 1 )
		.string( npc.name )
		.toArray();
}

function buildV150GameTimePayload() {
	return new NativePacketWriter().u16( 1 ).u8( 12 ).u8( 0 ).toArray();
}

function buildV150VitalsRefreshPayload( objectId, currentHp, currentMp ) {
	return new NativePacketWriter()
		.u32( objectId )
		.u16( 0 )
		.u8( 0x03 )
		.u32( currentHp )
		.u32( currentMp )
		.toArray();
}

// --- moveMissionCharacter's accepted-move core (server.mjs 2472-2494) -------
function applyMoveCore( world, objectId, movement, characterMovementMode, nowMs ) {
	const source = world.movementSourceSeeded ? null : movementSourceFromMissionSpawn( world.spawn );
	const payload = buildV150MovementAckPayload( objectId, movement, source );
	const liveSpawnBefore = missionLiveSpawnForWorld( world, nowMs );
	const nextSpawn = missionSpawnFromMovement( movement, liveSpawnBefore );
	const nextWorld = {
		...world,
		spawn: nextSpawn,
		moveSegment: missionMoveSegmentForTravel( liveSpawnBefore, nextSpawn, characterMovementMode, nowMs ),
		movementMode: characterMovementMode,
		spawnSet: true,
		movementSourceSeeded: true
	};
	return { world: nextWorld, ackBytes: payload, liveBefore: liveSpawnBefore, sourceIncluded: Boolean( source ) };
}

// --- vector generation -------------------------------------------------------
const europe = missionStartProfilesByRace.europe;
const china = missionStartProfilesByRace.china;
const T0 = 1_784_000_000_000; // deterministic epoch anchor

function spawnAt( regionId, x, y, z, angle = 0 ) {
	return { regionId, x, y, z, angle };
}

const vectors = {};

// live-spawn interpolation cases.
{
	const cases = [];

	const push = ( name, world, nowMs ) => {
		cases.push( { name, world, nowMs, expected: missionLiveSpawnForWorld( world, nowMs ) } );
	};

	const settled = { spawn: spawnAt( europe.regionId, europe.x, europe.y, europe.z ), moveSegment: undefined };
	push( "settledNoSegment", settled, T0 );

	const sameRegionGoal = spawnAt( europe.regionId, 1305, 80, 496, 8192 );
	const sameRegionSegment = missionMoveSegmentForTravel(
		spawnAt( europe.regionId, 1205, 80, 396 ),
		sameRegionGoal,
		missionCharacterRunMode,
		T0
	);
	const sameRegionWorld = { spawn: sameRegionGoal, moveSegment: sameRegionSegment };
	push( "sameRegionStart", sameRegionWorld, T0 );
	push( "sameRegionQuarter", sameRegionWorld, T0 + Math.floor( (sameRegionSegment.arrivesAtMs - T0) / 4 ) );
	push( "sameRegionMid", sameRegionWorld, T0 + Math.floor( (sameRegionSegment.arrivesAtMs - T0) / 2 ) );
	push( "sameRegionNearArrive", sameRegionWorld, sameRegionSegment.arrivesAtMs - 1 );
	push( "sameRegionArrived", sameRegionWorld, sameRegionSegment.arrivesAtMs );
	push( "sameRegionPast", sameRegionWorld, sameRegionSegment.arrivesAtMs + 5000 );
	push( "sameRegionBeforeStart", sameRegionWorld, T0 - 1000 );

	const crossFrom = spawnAt( europe.regionId, 1900, 80, 1900 );
	const crossGoalRegion = missionRegionId(
		missionRegionSectorX( europe.regionId ) + 1,
		missionRegionSectorY( europe.regionId ) + 1
	);
	const crossGoal = spawnAt( crossGoalRegion, 100, 82, 50, 0 );
	const crossSegment = missionMoveSegmentForTravel( crossFrom, crossGoal, missionCharacterRunMode, T0 );
	const crossWorld = { spawn: crossGoal, moveSegment: crossSegment };
	push( "crossRegionEarly", crossWorld, T0 + 200 );
	push( "crossRegionMid", crossWorld, T0 + Math.floor( (crossSegment.arrivesAtMs - T0) / 2 ) );
	push( "crossRegionLate", crossWorld, crossSegment.arrivesAtMs - 10 );

	const walkSegment = missionMoveSegmentForTravel(
		spawnAt( china.regionId, china.x, china.y, china.z ),
		spawnAt( china.regionId, china.x + 50, china.y, china.z - 30, 4000 ),
		missionCharacterWalkMode,
		T0
	);
	const walkWorld = {
		spawn: spawnAt( china.regionId, china.x + 50, china.y, china.z - 30, 4000 ),
		moveSegment: walkSegment
	};
	push( "walkModeMid", walkWorld, T0 + Math.floor( (walkSegment.arrivesAtMs - T0) / 2 ) );

	const malformed = {
		spawn: spawnAt( europe.regionId, 1305, 80, 496, 100 ),
		moveSegment: { from: spawnAt( europe.regionId, 1205, 80, 396 ), startedAtMs: T0, arrivesAtMs: T0 }
	};
	push( "malformedSegmentArrivesEqualsStarted", malformed, T0 - 500 );

	// Dungeon plane (regionId & 0x8000): the v1.150 client serializes dungeon
	// clicks through the SAME i16 positional form (sub_877cc0 has no
	// region-conditional arm), so the live plane must interpolate with the
	// dungeon bit intact - including across a sector crossing, where the high
	// byte carries the bit through missionRegionId's & 0xff.
	const dungeonRegion = 0x8000 | europe.regionId; // 0xEB4F
	const dungeonGoal = spawnAt( dungeonRegion, 700, 0, 900, 12000 );
	const dungeonSegment = missionMoveSegmentForTravel(
		spawnAt( dungeonRegion, 500, 0, 500 ),
		dungeonGoal,
		missionCharacterRunMode,
		T0
	);
	const dungeonWorld = { spawn: dungeonGoal, moveSegment: dungeonSegment };
	push( "dungeonSameRegionMid", dungeonWorld, T0 + Math.floor( (dungeonSegment.arrivesAtMs - T0) / 2 ) );
	push( "dungeonSameRegionArrived", dungeonWorld, dungeonSegment.arrivesAtMs );

	const dungeonCrossFrom = spawnAt( dungeonRegion, 1900, 40, 1900 );
	const dungeonCrossGoalRegion = missionRegionId(
		missionRegionSectorX( dungeonRegion ) + 1,
		missionRegionSectorY( dungeonRegion ) + 1
	);
	const dungeonCrossGoal = spawnAt( dungeonCrossGoalRegion, 100, 40, 50, 0 );
	const dungeonCrossSegment = missionMoveSegmentForTravel(
		dungeonCrossFrom,
		dungeonCrossGoal,
		missionCharacterRunMode,
		T0
	);
	const dungeonCrossWorld = { spawn: dungeonCrossGoal, moveSegment: dungeonCrossSegment };
	push( "dungeonCrossRegionEarly", dungeonCrossWorld, T0 + 200 );
	push( "dungeonCrossRegionMid", dungeonCrossWorld, T0 + Math.floor( (dungeonCrossSegment.arrivesAtMs - T0) / 2 ) );
	push( "dungeonCrossRegionLate", dungeonCrossWorld, dungeonCrossSegment.arrivesAtMs - 10 );

	vectors.liveSpawn = cases;
}

// move-segment construction cases.
{
	const cases = [];
	const push = ( name, liveFrom, nextSpawn, movementMode ) => {
		const segment = missionMoveSegmentForTravel( liveFrom, nextSpawn, movementMode, T0 );
		cases.push( {
			name,
			liveFrom,
			nextSpawn,
			movementMode,
			startedAtMs: T0,
			expected: segment ?? null,
			distance: missionWorldDistance2D( liveFrom, nextSpawn )
		} );
	};

	push(
		"runSameRegion",
		spawnAt( europe.regionId, 1205, 80, 396 ),
		spawnAt( europe.regionId, 1305, 80, 496, 8192 ),
		missionCharacterRunMode
	);
	push(
		"walkSameRegion",
		spawnAt( europe.regionId, 1205, 80, 396 ),
		spawnAt( europe.regionId, 1305, 80, 496, 8192 ),
		missionCharacterWalkMode
	);
	push(
		"zeroLengthHop",
		spawnAt( europe.regionId, 1205, 80, 396 ),
		spawnAt( europe.regionId, 1205, 80, 396 ),
		missionCharacterRunMode
	);
	push(
		"crossRegion",
		spawnAt( europe.regionId, 1900, 80, 1900 ),
		spawnAt(
			missionRegionId( missionRegionSectorX( europe.regionId ) + 1, missionRegionSectorY( europe.regionId ) + 1 ),
			100,
			82,
			50
		),
		missionCharacterRunMode
	);
	push(
		"dungeonBitPair",
		spawnAt( 0x8000 | europe.regionId, 500, 0, 500 ),
		spawnAt( 0x8000 | europe.regionId, 700, 0, 900 ),
		missionCharacterRunMode
	);
	push(
		"yOnlyHopIs2DZero",
		spawnAt( europe.regionId, 1205, 80, 396 ),
		spawnAt( europe.regionId, 1205, 200, 396 ),
		missionCharacterRunMode
	);

	vectors.moveSegmentForTravel = cases;
}

// heading cases.
{
	const cases = [];
	const push = ( name, from, to ) => {
		const heading = missionHeadingFromMovement( from, to );
		cases.push( { name, from, to, expected: heading ?? null } );
	};
	const base = spawnAt( europe.regionId, 1000, 80, 1000 );
	push( "north(-z)", base, spawnAt( europe.regionId, 1000, 80, 900 ) );
	push( "south(+z)", base, spawnAt( europe.regionId, 1000, 80, 1100 ) );
	push( "east(+x)", base, spawnAt( europe.regionId, 1100, 80, 1000 ) );
	push( "west(-x)", base, spawnAt( europe.regionId, 900, 80, 1000 ) );
	push( "diagonalNE", base, spawnAt( europe.regionId, 1100, 80, 900 ) );
	push( "shallowAngle", base, spawnAt( europe.regionId, 1103.7, 80, 998.2 ) );
	push(
		"crossRegionEast",
		spawnAt( europe.regionId, 1900, 80, 1000 ),
		spawnAt(
			missionRegionId( missionRegionSectorX( europe.regionId ) + 1, missionRegionSectorY( europe.regionId ) ),
			20,
			80,
			1000
		)
	);
	push( "degenerateSamePoint", base, spawnAt( europe.regionId, 1000, 80, 1000 ) );
	push( "degenerateSubEpsilon", base, spawnAt( europe.regionId, 1000 + 4e-7, 80, 1000 ) );
	push(
		"dungeonDiagonal",
		spawnAt( 0x8000 | europe.regionId, 800, 0, 800 ),
		spawnAt( 0x8000 | europe.regionId, 950, 0, 650 )
	);
	vectors.heading = cases;
}

// world-distance cases.
{
	const cases = [];
	const push = ( name, a, b ) => cases.push( { name, a, b, expected: missionWorldDistance2D( a, b ) } );
	push( "sameRegion", spawnAt( europe.regionId, 1205, 80, 396 ), spawnAt( europe.regionId, 1305, 80, 496 ) );
	push(
		"crossRegionX",
		spawnAt( europe.regionId, 1900, 80, 1000 ),
		spawnAt(
			missionRegionId( missionRegionSectorX( europe.regionId ) + 1, missionRegionSectorY( europe.regionId ) ),
			20,
			80,
			1000
		)
	);
	push(
		"dungeonBitBothSides",
		spawnAt( 0x8000 | europe.regionId, 100, 0, 100 ),
		spawnAt( 0x8000 | europe.regionId, 300, 0, 500 )
	);
	push(
		"chinaToOffsetRegion",
		spawnAt( china.regionId, 960.418884, 20, 458.259766 ),
		spawnAt(
			missionRegionId( missionRegionSectorX( china.regionId ) - 1, missionRegionSectorY( china.regionId ) ),
			1800,
			20,
			400
		)
	);
	vectors.worldDistance = cases;
}

// movement-ack payload cases.
{
	const cases = [];
	const push = ( name, objectId, movement, source ) => {
		cases.push( {
			name,
			objectId,
			movement,
			source: source ?? null,
			expectedBytes: buildV150MovementAckPayload( objectId, movement, source )
		} );
	};
	push( "destinationOnly", 100001, { mode: 1, regionId: europe.regionId, x: 1305, y: 80, z: 496 }, null );
	push( "destinationFractional", 100001, { mode: 1, regionId: europe.regionId, x: 1305.6, y: 80.4, z: 495.5 }, null );
	push(
		"withSourceBlockTimesTen",
		100001,
		{ mode: 1, regionId: europe.regionId, x: 1305, y: 80, z: 496 },
		{ regionId: europe.regionId, x: 1205, y: 80, z: 396 }
	);
	push(
		"withSourceFractional",
		100042,
		{ mode: 1, regionId: china.regionId, x: 1010.25, y: 20, z: 400.75 },
		{ regionId: china.regionId, x: 960.418884, y: 20, z: 458.259766 }
	);
	// A cave leg north of the region origin: destination and source z are
	// negative i16 words, y a negative height (#1679).
	push(
		"caveDestinationNegativeZ",
		100001,
		{ mode: 1, regionId: 0x8001, x: 777, y: -5.01, z: -40 },
		{ regionId: 0x8001, x: 671, y: 0.07, z: -35 }
	);
	// The same contract on the x words: destination x and source x
	// (x10 = -20000) are negative i16 words inside the range (#1679).
	push(
		"caveDestinationNegativeX",
		100001,
		{ mode: 1, regionId: 0x8001, x: -2000, y: -5.01, z: -40 },
		{ regionId: 0x8001, x: -2000, y: 0.07, z: -35 }
	);
	vectors.movementAck = cases;
}

// applyMove end-to-end: the bug-D resteer chain.
{
	const objectId = 100001;
	const steps = [];
	let world = {
		spawn: spawnAt( europe.regionId, europe.x, europe.y, europe.z, europe.angle ),
		moveSegment: undefined,
		movementMode: missionCharacterRunMode,
		spawnSet: false,
		movementSourceSeeded: false
	};

	const recordMove = ( name, movement, movementMode, atMs ) => {
		const result = applyMoveCore( world, objectId, movement, movementMode, atMs );
		world = result.world;
		steps.push( {
			action: "move",
			name,
			atMs,
			movement,
			movementMode,
			expect: {
				ackBytes: result.ackBytes,
				sourceIncluded: result.sourceIncluded,
				liveBefore: result.liveBefore,
				spawn: world.spawn,
				segment: world.moveSegment ?? null
			}
		} );
	};

	const recordProbe = ( name, atMs ) => {
		steps.push( {
			action: "probeLive",
			name,
			atMs,
			expect: { live: missionLiveSpawnForWorld( world, atMs ) }
		} );
	};

	// First click: source block rides the ack exactly once.
	recordMove(
		"firstMove",
		{ mode: 1, regionId: europe.regionId, x: 1405, y: 80, z: 396 },
		missionCharacterRunMode,
		T0
	);
	// Bug-D oracle probe: mid-flight the live plane must NOT be the goal.
	recordProbe( "bugDMidFlightDropPosition", T0 + 1000 );
	// Resteer mid-flight: departure = interpolated point of the PREVIOUS segment.
	recordMove(
		"resteerMidFlight",
		{ mode: 1, regionId: europe.regionId, x: 1205, y: 80, z: 596 },
		missionCharacterRunMode,
		T0 + 1000
	);
	recordProbe( "secondSegmentMid", T0 + 2500 );
	// Walk-mode resteer: slower wire speed retimes the segment.
	recordMove(
		"walkResteer",
		{ mode: 1, regionId: europe.regionId, x: 1260, y: 80, z: 500 },
		missionCharacterWalkMode,
		T0 + 2500
	);
	recordProbe( "walkMid", T0 + 4000 );
	// Let it mature, then a zero-length hop clears the stale segment.
	recordProbe( "afterArrival", T0 + 1_000_000 );
	recordMove(
		"zeroLengthHopClearsSegment",
		{ mode: 1, regionId: europe.regionId, x: 1260, y: 80, z: 500 },
		missionCharacterWalkMode,
		T0 + 1_000_000
	);
	recordProbe( "settledAfterZeroHop", T0 + 1_000_001 );

	vectors.applyMove = {
		objectId,
		initialWorld: {
			spawn: spawnAt( europe.regionId, europe.x, europe.y, europe.z, europe.angle ),
			movementMode: missionCharacterRunMode,
			spawnSet: false,
			movementSourceSeeded: false
		},
		steps
	};
}

// patrol cases (anchor variants x ticks 0..9; npcIndex fixed 0 - unused for pose).
{
	const anchors = {
		shop: MISSION_NPC_SHOP_SPAWN,
		europeStart: { regionId: europe.regionId, x: europe.x, y: europe.y, z: europe.z }
	};
	const out = [];
	for ( const [anchorName, anchor] of Object.entries( anchors ) ) {
		const ticks = [];
		for ( let tick = 0; tick <= 9; tick += 1 ) {
			const state = computeNpcPatrolState( anchor, 0, tick );
			ticks.push( {
				tick,
				expected: {
					regionId: state.regionId,
					x: state.x,
					y: state.y,
					z: state.z,
					headingWord: state.headingWord
				}
			} );
		}
		out.push( { anchorName, anchor, ticks } );
	}
	vectors.patrol = out;
}

// npc wire payloads.
{
	const npc = MISSION_NPC_ROSTER[0];
	const anchor = MISSION_NPC_SHOP_SPAWN;
	const gid0 = npc.objectId;

	const movePayloads = [];
	const correctionPayloads = [];
	for ( const tick of [ 0, 1, 3, 4, 5, 8 ] ) {
		const state = computeNpcPatrolState( anchor, 0, tick );
		movePayloads.push( { tick, anchor, expectedBytes: buildV150NpcObjectMovePayload( state, gid0 ) } );
		correctionPayloads.push( { tick, anchor, expectedBytes: buildV150NpcSourceCorrectionPayload( state, gid0 ) } );
	}
	const thirdNpc = { ...npc, objectId: 200003 };

	vectors.npcWire = {
		createRow: [
			{ npc, anchor, expectedBytes: buildV150NpcCreateRow( npc, anchor ) },
			{
				npc: thirdNpc,
				anchor: { regionId: europe.regionId, x: europe.x, y: europe.y, z: europe.z },
				expectedBytes: buildV150NpcCreateRow( thirdNpc, {
					regionId: europe.regionId,
					x: europe.x,
					y: europe.y,
					z: europe.z
				} )
			}
		],
		movePayloads,
		correctionPayloads,
		statePayloads: [
			{ tick: 0, expectedBytes: buildV150NpcRefreshStatePayload( gid0, 1, 2 ) },
			{ tick: 1, expectedBytes: buildV150NpcRefreshStatePayload( gid0, 1, 3 ) }
		],
		lifePayloads: [
			{ tick: 0, expectedBytes: buildV150NpcRefreshStatePayload( gid0, 0, 2 ) },
			{ tick: 1, expectedBytes: buildV150NpcRefreshStatePayload( gid0, 0, 1 ) }
		],
		despawnPayloads: [ { index: 0, expectedBytes: buildV150NpcDespawnPayload( gid0 ) } ]
	};
}

// game-ready push payloads.
vectors.gameReady = {
	gameTimeBytes: buildV150GameTimePayload(),
	vitals: [
		{
			objectId: 100001,
			currentHp: 100,
			currentMp: 100,
			expectedBytes: buildV150VitalsRefreshPayload( 100001, 100, 100 )
		},
		{
			objectId: 100042,
			currentHp: 3210,
			currentMp: 12345,
			expectedBytes: buildV150VitalsRefreshPayload( 100042, 3210, 12345 )
		}
	]
};

const outPath = path.join( path.dirname( fileURLToPath( import.meta.url ) ), "movement_parity.json" );
writeFileSync( outPath, JSON.stringify( vectors, null, 1 ) );
console.log( `wrote ${outPath}` );
