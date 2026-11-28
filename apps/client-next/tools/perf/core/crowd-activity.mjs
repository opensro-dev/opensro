/*
===========================================================================

crowd-activity.mjs - a held crowd that walks and chats, scripted from a seed

A standing crowd is the best case for any "write only what changed" work
(#3563): nothing changes. This owner gives a share of the peers human
input instead. Walkers click ground points near their grid spot at human
intervals, chatters send local (All) chat lines, the rest stand. Each peer
draws its whole script from its own generator seeded by (seed, index), so
the same seed gives every peer the same sequence of clicks and lines no
matter how the timers interleave.

Roamers never stand: they re-target before they arrive, so every roaming
peer is mid-walk on every frame (the moving crowd a camera-drag bench
needs, where walkers idle most of the time). Busy peers roam too and, on
their own clocks, take their chest garment off into the bag and put it
back, and recast the cold imbue each time its cooldown has passed: the
appearance changes and skill effects a living town draws.

A busy crowd is created qualified for that script (busyCreation,
BUSY_LOADOUT): clothes from character creation, and a loadout the
benchmark fixture only teaches once the character meets the imbue's
requirements (its cold mastery, raised by the fixture) with the level and
intellect to pay its MP. A refusal still happens and is counted, never
hidden: the evidence keeps every accepted and refused move and cast.

Frames are what the replacement client sends: chat goes through the
client's own chat owner (chat.ts createChat, with its acknowledgement and
deadline), a ground click through the movement owner's predicted envelope
(movement.ts request: 0x0009 [u8 1][u32 id] then the native 0x7738 body).

Fighting is not here: it needs monsters beside the crowd and each peer
decoding their spawns for a target gid (see the lane report).

===========================================================================
*/
import "../../../tests/helpers/native-source-loader.mjs";

const { createChat } = await import(
	"../../../src/engine/runtime/simulation/worker/session/world/gameplay/chat/chat.ts"
);
const { REGION_SIZE } = await import( "../../../src/engine/foundation/world/world-space.ts" );
const { skillRequest } = await import( "../../../src/engine/foundation/gameplay/skill-request.ts" );
const { inventoryMoveRequest } = await import( "../../../src/engine/foundation/gameplay/inventory-move-request.ts" );

// The replacement client's predicted-movement envelope (movement.ts
// OP_PREDICTED_MOVE, transport.OpPredictedMove) and its player kind byte;
// the server answers each id with one 0x000A JSON receipt.
const OP_PREDICTED_MOVE = 0x0009, OP_PREDICTED_MOVE_RESULT = 0x000a, ENVELOPE_PLAYER = 1;
// The native 0x7738 ground click (moverequest.go DecodeClientMovementRequest):
// mode 1, region, then the int16 region-local x, height and z.
const MOVE_DESTINATION_MODE = 1, GROUND_CLICK_BYTES = 9;
// Chat: the All channel (nearby players) and its acknowledgement (chat.ts).
const CHAT_ALL_CHANNEL = 1, OP_CHAT_ACK = 0xb367, CHAT_ACK_OK = 1;
// A walker clicks a point within WALK_RADIUS of its grid spot (15 units
// apart, crowd.mjs crowdLayout, which keeps this reach inside the
// observer's interest area) every 1.5 to 6 s: a player clicking around
// while waiting, not a bot pinned to one rate.
export const WALK_RADIUS = 25;
const WALK_MIN_MS = 1500, WALK_MAX_MS = 6000;
// A roamer clicks every 0.8 to 1.4 s, a point ROAM_MIN_DISTANCE to
// WALK_RADIUS from its spot: two such points lie further apart than a
// player runs in that time on average, so it re-targets mid-walk.
const ROAM_MIN_MS = 800, ROAM_MAX_MS = 1400, ROAM_MIN_DISTANCE = 12;
// A chatter speaks every 8 to 30 s.
const CHAT_MIN_MS = 8000, CHAT_MAX_MS = 30000;
const CHAT_LINES = Object.freeze( [
	"anyone for a party?",
	"selling blades, pm me",
	"lfg tiger girl",
	"where is the stable?",
	"brb",
	"thanks!",
	"hello all",
	"how much for elixirs?"
] );
// Busy peers: SKILL_CH_COLD_GIGONGTA_A_01 (skilldata id 90, the Cold Force
// weapon imbue: enterworld/skillimbue.go admits the row; cold mastery 273 at
// 5, no prerequisite, 5000 ms cooldown, any weapon) recast every 5.5 to 8 s
// with no target (an instant self effect: targetinteract.go admits only an
// untargeted press, and the walk goes on, casthalt.go), and the chest garment (socket 1, inventory/slots.go SocketBody) taken off
// or put back every 3 to 6 s. The take-off asks for a bag slot; the server
// answers where it put the garment (0xB06D [1][0][src][dst]...), and the
// put-back moves it from there. A refused take-off asks for the next slot.
export const BUSY_IMBUE_SKILL = 90;
const IMBUE_MIN_MS = 5500, IMBUE_MAX_MS = 8000, DRESS_MIN_MS = 3000, DRESS_MAX_MS = 6000;
const CHEST_SOCKET = 1, BAG_SLOTS = Object.freeze( [ 40, 39, 38, 37, 36, 35, 34, 33, 32, 31, 30 ] );
const OP_ITEM_MOVE_RESULT = 0xb06d, ITEM_MOVE_OK = 1, ITEM_MOVE_DESTINATION = 3;
const OP_SKILL_CAST_RESULT = 0xb245, CAST_OK = 1, CAST_SUCCESS_BYTES = 10;

// The level and intellect a busy peer is taught at: past the imbue's
// mastery 5, with MP for a cast every cooldown.
export const BUSY_LOADOUT = Object.freeze( {
	level: 40,
	intellect: 200,
	skills: Object.freeze( [ BUSY_IMBUE_SKILL ] )
} );
// China protector choice 3 (enterworld/loadout.go chinaProtectorKinds):
// the CLOTHES set, chest legs and feet.
const CLOTHES_PROTECTOR = 3;
export const ACTIVITY_ROLES = Object.freeze( [ "walk", "chat", "roam", "busy" ] );
const STILL = "still";

/*
================
parseActivity

"walk=0.5,chat=0.2,roam=0.3" (or "" / "still") into shares of the crowd. The shares
are disjoint roles; whatever they leave stands still. label is canonical,
so two spellings of the same activity key the same held crowd.
================
*/
export function parseActivity( text ) {
	const shares = noShares();
	const spec = String( text ?? "" ).trim();
	if ( spec && spec !== STILL ) {
		for ( const part of spec.split( "," ) ) {
			const [name, value, extra] = part.split( "=" ).map( item => item.trim() );
			const share = Number( value );
			if ( !ACTIVITY_ROLES.includes( name ) || extra !== undefined || !(share >= 0 && share <= 1) ) {
				throw Error(
					`bad --act part "${part}": expected ${ACTIVITY_ROLES.join( "|" )}=SHARE, SHARE in [0, 1]`
				);
			}
			shares[name] = share;
		}
	}
	if ( sharedOut( shares ) > 1 + 1e-9 ) throw Error( `--act shares sum past 1: ${spec}` );
	const label =
		ACTIVITY_ROLES.filter( role => shares[role] > 0 ).map( role => `${role}=${shares[role]}` ).join( "," ) ||
		STILL;
	return { ...shares, label };
}

/*
================
noShares, sharedOut

The shares of a standing crowd, and the part of the crowd a set of shares
gives a role (the rest stands).
================
*/
/** @returns {Record<"walk" | "chat" | "roam" | "busy", number>} */
function noShares() {
	return Object.fromEntries( ACTIVITY_ROLES.map( role => [ role, 0 ] ) );
}

function sharedOut( shares ) {
	return ACTIVITY_ROLES.reduce( ( sum, role ) => sum + (shares[role] ?? 0), 0 );
}

/*
================
apportionRoles

How many of count peers take each role: largest remainder over the roles
and still, so the counts always sum to count (rounding each share alone
can give 2 + 2 of 3). Ties go to the earlier role.
================
*/
export function apportionRoles( count, shares ) {
	const roles = [ ...ACTIVITY_ROLES, STILL ];
	const wanted = [
		...ACTIVITY_ROLES.map( role => (shares[role] ?? 0) * count ),
		Math.max( 0, 1 - sharedOut( shares ) ) * count
	];
	const counts = wanted.map( Math.floor );
	let left = count - counts.reduce( ( a, b ) => a + b, 0 );
	const order = roles.map( ( _, i ) => i ).sort( ( a, b ) =>
		(wanted[b] - counts[b]) - (wanted[a] - counts[a]) || a - b
	);
	for ( let i = 0; left > 0; i = (i + 1) % order.length, left-- ) counts[order[i]]++;
	return Object.fromEntries( roles.map( ( role, i ) => [ role, counts[i] ] ) );
}

/*
================
createRandom

mulberry32: a small fixed generator, so a script never depends on
Math.random or on the order other peers drew in.
================
*/
function createRandom( seed ) {
	let state = seed >>> 0;
	return () => {
		state = state + 0x6d2b79f5 >>> 0;
		let t = Math.imul( state ^ state >>> 15, 1 | state );
		t = t + Math.imul( t ^ t >>> 7, 61 | t ) ^ t;
		return ((t ^ t >>> 14) >>> 0) / 0x100000000;
	};
}

/*
================
streamSeed

One generator per (seed, stream): stream 0 deals the roles, stream
index + 1 is peer index's script.
================
*/
function streamSeed( seed, stream ) {
	return Math.imul( (seed >>> 0) ^ 0x9e3779b9, 0x85ebca6b ) + Math.imul( stream, 0xc2b2ae35 ) >>> 0;
}

/*
================
assignRoles

Each peer's role: a seeded shuffle of the indices, cut by apportionRoles.
================
*/
export function assignRoles( count, shares, seed ) {
	const random = createRandom( streamSeed( seed, 0 ) );
	const order = Array.from( { length: count }, ( _, i ) => i );
	for ( let i = count - 1; i > 0; i-- ) {
		const j = Math.floor( random() * (i + 1) );
		[order[i], order[j]] = [ order[j], order[i] ];
	}
	const counts = apportionRoles( count, shares ), roles = new Array( count ).fill( STILL );
	let at = 0;
	for ( const role of ACTIVITY_ROLES ) {
		for ( let n = 0; n < counts[role]; n++ ) roles[order[at++]] = role;
	}
	return roles;
}

/*
================
peerScript

Peer index's endless script as a next() function: each call gives the
wait before the next action and the action. The first wait is drawn
within one interval, so the crowd does not act in step.
================
*/
export function peerScript( { role, index, seed, spot } ) {
	const random = createRandom( streamSeed( seed, index + 1 ) );
	const [min, max] = role === "walk" ?
		[ WALK_MIN_MS, WALK_MAX_MS ] :
		role === "roam" || role === "busy" ?
		[ ROAM_MIN_MS, ROAM_MAX_MS ] :
		[ CHAT_MIN_MS, CHAT_MAX_MS ];
	const [near, far] = role === "roam" || role === "busy" ? [ ROAM_MIN_DISTANCE, WALK_RADIUS ] : [ 0, WALK_RADIUS ];
	let first = true;
	return () => {
		const delayMs = Math.round( first ? random() * max : min + random() * (max - min) );
		first = false;
		if ( role === "chat" ) {
			return { delayMs, kind: "chat", text: CHAT_LINES[Math.floor( random() * CHAT_LINES.length )] };
		}
		const angle = random() * Math.PI * 2,
			distance = Math.sqrt( near * near + random() * (far * far - near * near) );
		return {
			delayMs,
			kind: "walk",
			to: {
				...spot,
				x: regionLocal( spot.x + Math.cos( angle ) * distance ),
				z: regionLocal( spot.z + Math.sin( angle ) * distance )
			}
		};
	};
}

/*
================
busyScript

A busy peer's second and third clocks, from their own streams of the
peer's seed so adding them leaves its walk script unchanged: the waits
between imbue casts and between dress changes.
================
*/
export function busyScript( { index, seed } ) {
	const imbue = createRandom( streamSeed( seed, 0x10000 + index ) );
	const dress = createRandom( streamSeed( seed, 0x20000 + index ) );
	const wait = ( random, min, max ) => Math.round( min + random() * (max - min) );
	return {
		imbueMs: () => wait( imbue, IMBUE_MIN_MS, IMBUE_MAX_MS ),
		dressMs: () => wait( dress, DRESS_MIN_MS, DRESS_MAX_MS )
	};
}

/*
================
busyCreation

What a crowd's peers are created with: a crowd with busy peers wears the
clothes set it takes off and puts on, any other keeps the bare starter.
================
*/
export function busyCreation( activity ) {
	return activity.busy > 0 ?
		{ protectorIndex: CLOTHES_PROTECTOR, armorSelected: true } :
		{ protectorIndex: 0, armorSelected: false };
}

/*
================
regionLocal

A click keeps its spot's region: a point drawn past the region's edge
stays on it. The server refuses a local coordinate outside
[0, REGION_SIZE), which would count a script's own draw as a refusal.
================
*/
function regionLocal( value ) {
	return Math.min( REGION_SIZE - 1, Math.max( 0, value ) );
}

/*
================
groundClickBody

The native 0x7738 mode-1 body for spot, in the client's integer
region-local form.
================
*/
export function groundClickBody( spot ) {
	const payload = new Uint8Array( GROUND_CLICK_BYTES ), view = new DataView( payload.buffer );
	payload[0] = MOVE_DESTINATION_MODE;
	view.setUint16( 1, spot.regionId, true );
	view.setInt16( 3, Math.trunc( spot.x ), true );
	view.setInt16( 5, Math.trunc( spot.y ), true );
	view.setInt16( 7, Math.trunc( spot.z ), true );
	return payload;
}

/*
================
predictedMoveBody

The player envelope the movement owner sends for a ground click: kind,
the session's next command id (strictly increasing from 1, or the server
drops it as stale), then the native body.
================
*/
function predictedMoveBody( id, spot ) {
	const payload = new Uint8Array( 5 + GROUND_CLICK_BYTES );
	payload[0] = ENVELOPE_PLAYER;
	new DataView( payload.buffer ).setUint32( 1, id, true );
	payload.set( groundClickBody( spot ), 5 );
	return payload;
}

/*
================
startActivity

Runs the scripts on an admitted crowd ({ send, listen }, crowd.mjs).
spotFor( index ) is the peer's grid spot. Returns { evidence, stop }:
evidence is plain JSON (what ran, with its seed, and what the server
answered), updated in place while the activity runs.
================
*/
export function startActivity( { crowd, count, activity, seed, spotFor } ) {
	const roles = activity.label === STILL ? new Array( count ).fill( STILL ) : assignRoles( count, activity, seed );
	const evidence = {
		act: activity.label,
		seed: activity.label === STILL ? null : seed,
		peers: apportionRoles( count, activity.label === STILL ? noShares() : activity ),
		clicks: 0,
		moveAccepted: 0,
		moveRefused: 0,
		chats: 0,
		chatAcked: 0,
		chatRefused: 0,
		// A line due while the previous one still waits for its receipt.
		chatBusy: 0,
		// Busy peers: dress changes sent and answered, imbue presses, the
		// caster's refusals, and the distinct casters seen succeeding (every
		// peer hears its neighbours' 0xB245, so successes are by caster).
		dressMoves: 0,
		dressAccepted: 0,
		dressRefused: 0,
		undressed: 0,
		imbues: 0,
		imbueRefused: 0,
		// Refusal codes by count ("0x11": 3), the server's reason in its words.
		imbueRefusals: {},
		dressRefusals: {},
		imbueCasters: []
	};
	const casters = new Set();
	const timers = new Set(), started = performance.now();
	let running = true;
	/*
	================
	schedule
	================
	*/
	const schedule = ( next, act ) => {
		const step = next();
		const timer = setTimeout( () => {
			timers.delete( timer );
			if ( !running ) return;
			act( step );
			schedule( next, act );
		}, step.delayMs );
		timer.unref?.();
		timers.add( timer );
	};
	for ( let index = 0; index < count; index++ ) {
		const role = roles[index];
		if ( role === STILL ) continue;
		const next = peerScript( { role, index, seed, spot: spotFor( index ) } );
		if ( role === "busy" ) startBusy( { crowd, index, seed, evidence, casters, schedule } );
		if ( role === "walk" || role === "roam" || role === "busy" ) {
			let id = 0;
			crowd.listen( index, frame => {
				if ( frame.opcode !== OP_PREDICTED_MOVE_RESULT ) return;
				const receipt = JSON.parse( new TextDecoder().decode( frame.payload ) );
				if ( receipt.accepted ) evidence.moveAccepted++;
				else evidence.moveRefused++;
			} );
			schedule( next, step => {
				crowd.send( index, OP_PREDICTED_MOVE, predictedMoveBody( ++id, step.to ) );
				evidence.clicks++;
			} );
			continue;
		}
		const chat = createChat( frame => crowd.send( index, frame.opcode, frame.payload ) );
		crowd.listen( index, frame => {
			if ( frame.opcode !== OP_CHAT_ACK ) return;
			if ( frame.payload[0] === CHAT_ACK_OK ) evidence.chatAcked++;
			else evidence.chatRefused++;
			chat.receive( frame, 0 );
		} );
		schedule( next, step => {
			const now = performance.now() - started;
			chat.step( now );
			if ( chat.state().pending ) {
				evidence.chatBusy++;
				return;
			}
			chat.request( CHAT_ALL_CHANNEL, step.text, "", now );
			evidence.chats++;
		} );
	}
	return {
		evidence,
		stop: () => {
			running = false;
			for ( const timer of timers ) clearTimeout( timer );
			timers.clear();
		}
	};
}

/*
================
startBusy

A busy peer's dress and imbue clocks. One dress move is in flight at a
time; its 0xB06D answers it (a peer receives only its own). An accepted
take-off leaves the garment at that bag slot for the put-back; a refused
one tries the next bag slot.
================
*/
function startBusy( { crowd, index, seed, evidence, casters, schedule } ) {
	const clocks = busyScript( { index, seed } );
	let bag = 0, stored = 0, worn = true, pending = null;
	const tally = ( counts, code ) => {
		const key = `0x${(code ?? 0).toString( 16 )}`;
		counts[key] = (counts[key] ?? 0) + 1;
	};
	crowd.listen( index, frame => {
		if ( frame.opcode === OP_ITEM_MOVE_RESULT && pending ) {
			if ( frame.payload[0] === ITEM_MOVE_OK ) {
				evidence.dressAccepted++;
				worn = pending === "on";
				if ( !worn ) {
					evidence.undressed++;
					stored = frame.payload[ITEM_MOVE_DESTINATION] ?? BAG_SLOTS[bag];
				}
			} else {
				evidence.dressRefused++;
				tally( evidence.dressRefusals, frame.payload[1] );
				if ( pending === "off" ) bag = (bag + 1) % BAG_SLOTS.length;
			}
			pending = null;
			return;
		}
		if ( frame.opcode !== OP_SKILL_CAST_RESULT ) return;
		// A refusal ([2][code]) reaches only its caster; a success reaches
		// every peer in sight and names its caster.
		if ( frame.payload[0] !== CAST_OK ) {
			evidence.imbueRefused++;
			tally( evidence.imbueRefusals, frame.payload[1] );
			return;
		}
		if ( frame.payload.length < CAST_SUCCESS_BYTES ) return;
		const view = new DataView( frame.payload.buffer, frame.payload.byteOffset, frame.payload.byteLength );
		if ( view.getUint32( 2, true ) !== BUSY_IMBUE_SKILL ) return;
		const caster = view.getUint32( 6, true );
		if ( !casters.has( caster ) ) {
			casters.add( caster );
			evidence.imbueCasters.push( caster );
		}
	} );
	schedule( () => ({ delayMs: clocks.dressMs() }), () => {
		if ( pending ) return;
		pending = worn ? "off" : "on";
		const frame = worn ?
			inventoryMoveRequest( CHEST_SOCKET, BAG_SLOTS[bag], 1 ) :
			inventoryMoveRequest( stored, CHEST_SOCKET, 1 );
		crowd.send( index, frame.opcode, frame.payload );
		evidence.dressMoves++;
	} );
	schedule( () => ({ delayMs: clocks.imbueMs() }), () => {
		const frame = skillRequest( BUSY_IMBUE_SKILL );
		crowd.send( index, frame.opcode, frame.payload );
		evidence.imbues++;
	} );
}
