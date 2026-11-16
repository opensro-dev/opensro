/*
===========================================================================

presentation-scene.mjs - a small invented scene the presentation replay runs without built assets

The presentation replay (tools/perf/presentation/harness.mjs) reads the
published tree. This helper makes up a scene of its own instead: a
catalogue of two player bodies, two monsters, a tower, a horse and a few
worn items, four skill records, and the smallest character GLB the model
decoder admits for each model (four nodes, one triangle, one clip per
name with an invented duration). sceneSources() serves those bytes to the harness by path
(options.source); sceneSession() is the session the scene plays. Nothing
here is cut from the game's data: the names follow the client's naming
rules only where the client parses them (a player body's CHAR_<race>_
<sex>_ prefix, an item's type word).

===========================================================================
*/
import {
	FRAME_HZ,
	REGION,
	CENTER,
	LOCAL_GID,
	FIRST_PEER_GID,
	MOVEMENT_SEATED,
	MOVEMENT_RUN,
	ALIVE,
	DEAD,
	seeded,
	wear,
	pose,
	createSession,
	playerRow,
	npcRow,
	grid,
	walk
} from "../../tools/perf/presentation/session-builder.mjs";
import { goldDropModels } from "./gold-drop-models.mjs";

const GLB_MAGIC = 0x46546c67, GLB_JSON = 0x4e4f534a, GLB_BINARY = 0x004e4942;
const FLOAT = 5126, UNSIGNED_INT = 5125;
// A player body's clips and their invented lengths in seconds: the
// locomotion, posture, hit, death and idle fidget names the presentation
// asks a body for (presentation-catalog.ts clipFacts, random-idle.ts).
const PLAYER_CLIPS = {
	stand: 2,
	walk: 1.2,
	run: .8,
	ride: 2,
	pick: .9,
	attack1: 1.1,
	hit1: .5,
	down: .6,
	downwait: 1,
	wakeup: .7,
	death: 1.3,
	deathloop: 1,
	deathquick: .8,
	idle61: 1.6,
	idle81: 1.4,
	idle122: 1.8,
	"charselect-state13": .6,
	"charselect-state14": 1.5,
	"charselect-state15": .7,
	"attached-default-188": 1,
	emote1: 1.2
};
const MONSTER_CLIPS = { stand: 1.5, walk: 1, run: .7, attack1: .9, hit1: .4, death: 1.1, deathloop: 1 };
const HORSE_CLIPS = { stand: 1.7, walk: 1.1, run: .6 };
// A worn part has its own clip list; the body drives it.
const PART_CLIPS = { stand: 2 };
// item typeFlags bits 11..15: 2 is a sword (animation-metadata.ts weaponAnimationSet).
const SWORD_FLAGS = 2 << 11;
const BODIES = {
	CHAR_CH_MAN_PROBE: { refObjId: 9001, glb: "/assets/char/scene/man.glb" },
	CHAR_EU_WOMAN_PROBE: { refObjId: 9002, glb: "/assets/char/scene/woman.glb" }
};
// The worn items: id, codename, slot, typeFlags, the part mesh they draw.
// Slot 8 is the job suit (equipment-visuals.ts JOB_SLOT).
/** @type {[number, string, number, number, string][]} */
const ITEMS = [
	[ 9101, "ITEM_CH_M_PROBE_TUNIC", 1, 0, "tunic" ],
	[ 9102, "ITEM_CH_M_PROBE_BOOTS", 5, 0, "boots" ],
	[ 9103, "ITEM_CH_M_PROBE_ROBE", 1, 0, "robe" ],
	[ 9104, "ITEM_CH_SWORD_PROBE", 6, SWORD_FLAGS, "blade" ],
	[ 9105, "ITEM_EU_W_PROBE_DRESS", 1, 0, "dress" ],
	[ 9106, "ITEM_CH_M_PROBE_THIEF_SUIT", 8, 0, "suit" ],
	[ 9107, "ITEM_CH_M_PROBE_TRADER_SUIT", 8, 0, "pack" ]
];
const THIEF_SUIT = 9106;
const BODY_KEYS = { CH_M: "ITEM_CH_", EU_W: "ITEM_EU_" };
// The crowd slots that wear the woman's body.
const WOMEN = [ 1, 3, 8 ];
// The crowd slot of the other arena team, the one in a thief suit and the
// one in a trader's suit.
const ARENA_PEER = 13, THIEF_PEER = 14, TRADER_PEER = 17;
// The standing peers whose presentation changes with no new row: a pet
// resolve's stack window ends (2), and a berserk ends and its scale
// eases back (3).
const RESOLVE_PEER = 2, BERSERK_PEER = 3;
// The scene's skills. SKILL has a ready, a wait and a shot, each its
// attack clip (effects.ts animTable0..2), so a cast lays action layers, a
// WAIT and the combat stance; HIDING_SKILL is a shot alone that hides the
// caster's weapon while it casts (hideWeapon). Both deal their damage at
// the attack's first motion event (eventCode 1, at 300 ms).
const SKILL = 2, HIDING_SKILL = 3;
// SILENT_SKILL has no motion: its cast only turns the caster to its
// target. HOST_SKILL, attached to a body, plays the body's attached motion
// default 188 (effects.ts hostMotions).
const SILENT_SKILL = 4, HOST_SKILL = 5;
// The scene's one effect program: an anchor with nothing to draw, the
// smallest program the effect decoder admits (program.ts: a leaf node that
// never ends, renders nothing and emits nothing). The beast and the ghost
// carry it as an ambient particle on their neck (model-emission.ts).
const GLOW = "scene/glow.efp";
const GLOW_MODIFIERS = [ {
	kind: 2,
	stateId: -1,
	animationSetName: "ambient",
	entries: [ {
		effectPath: GLOW,
		boneName: "Bip01 Neck",
		vector3c: [ 0, 0, 0 ],
		field00: 0,
		field4c: 0,
		flag53: 0,
		flags50: [ 0, 0, 0 ]
	} ]
} ];
const STATE = {
	loop: true,
	durationMs: 1000,
	soundEvents: [],
	trackEvents: [],
	timeWarpCurve: { scale: 0, records: [] }
};
const ATTACK = {
	loop: false,
	durationMs: 1100,
	soundEvents: [],
	trackEvents: [ { cursorMs: 300, eventCode: 1, param0: 0, param1: 0 } ],
	timeWarpCurve: { scale: 0, records: [] }
};
const DAMAGE_STAGE = {
	startOffset: [ 0, 0, 0 ],
	startEvent: 1,
	actionType: "",
	animationPhase: "SHOT",
	damageEvent: true,
	startKeepRotation: false,
	startAddHeight: false,
	targetKeepRotation: false,
	targetAddHeight: false
};
// The garments a Chinese body wears on a bare chest or legs
// (equipment-visuals.ts selectDefaultWear): family_part.
const DEFAULT_WEAR = [ "clothes_BA", "clothes_LA", "light_BA", "light_LA" ];

/*
================
glb

The smallest character GLB the model decoder admits: a root, a neck and
a neck end (the sockets a worn part links under), one triangle named
after its part and skinned to the root (a worn part's joints must be
the body's), and one clip per name whose only channel moves the root
from time 0 to the clip's length.
================
*/
export function glb( part, clips ) {
	const names = Object.keys( clips ), floats = [];
	const push = values => {
		const offset = floats.length * 4;
		floats.push( ...values );
		return offset;
	};
	const positions = push( [ 0, 0, 0, 1, 0, 0, 0, 1, 0 ] ), still = push( [ 0, 0, 0, 0, 0, 0 ] );
	const joints = push( new Array( 12 ).fill( 0 ) ), weights = push( [ 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 ] );
	const times = names.map( name => push( [ 0, clips[name] ] ) );
	const indices = floats.length * 4, binaryLength = indices + 12;
	const accessors = [
		{ bufferView: 0, byteOffset: positions, componentType: FLOAT, count: 3, type: "VEC3" },
		{ bufferView: 0, byteOffset: indices, componentType: UNSIGNED_INT, count: 3, type: "SCALAR" },
		{ bufferView: 0, byteOffset: still, componentType: FLOAT, count: 2, type: "VEC3" },
		{ bufferView: 0, byteOffset: joints, componentType: FLOAT, count: 3, type: "VEC4" },
		{ bufferView: 0, byteOffset: weights, componentType: FLOAT, count: 3, type: "VEC4" },
		...times.map( byteOffset => ({ bufferView: 0, byteOffset, componentType: FLOAT, count: 2, type: "SCALAR" }) )
	];
	const json = {
		asset: { version: "2.0" },
		buffers: [ { byteLength: binaryLength } ],
		bufferViews: [ { buffer: 0, byteLength: binaryLength } ],
		accessors,
		nodes: [
			{ name: "root", children: [ 1, 3 ] },
			{ name: "Bip01 Neck", translation: [ 0, 140, 0 ], children: [ 2 ] },
			{ name: "Bip01 Neck1", translation: [ 0, 10, 0 ] },
			{ name: part, mesh: 0, skin: 0 }
		],
		skins: [ { joints: [ 0 ] } ],
		meshes: [ { primitives: [ { attributes: { POSITION: 0, JOINTS_0: 3, WEIGHTS_0: 4 }, indices: 1 } ] } ],
		animations: names.map( ( name, i ) => ({
			name,
			samplers: [ { input: 5 + i, output: 2 } ],
			channels: [ { sampler: 0, target: { node: 0, path: "translation" } } ]
		}) )
	};
	const text = new TextEncoder().encode( JSON.stringify( json ) );
	const jsonLength = Math.ceil( text.length / 4 ) * 4, total = 12 + 8 + jsonLength + 8 + binaryLength;
	const bytes = new Uint8Array( total ), view = new DataView( bytes.buffer );
	view.setUint32( 0, GLB_MAGIC, true );
	view.setUint32( 4, 2, true );
	view.setUint32( 8, total, true );
	view.setUint32( 12, jsonLength, true );
	view.setUint32( 16, GLB_JSON, true );
	bytes.fill( 0x20, 20, 20 + jsonLength );
	bytes.set( text, 20 );
	const binary = 20 + jsonLength;
	view.setUint32( binary, binaryLength, true );
	view.setUint32( binary + 4, GLB_BINARY, true );
	new Float32Array( bytes.buffer, binary + 8, floats.length ).set( floats );
	new Uint32Array( bytes.buffer, binary + 8 + indices, 3 ).set( [ 0, 1, 2 ] );
	return bytes;
}

/*
================
sceneCatalogs

The scene's catalogue, in the shapes the presentation admits and
session-builder.mjs reads: the roster with its dress table, the item
rows, the NPC manifest, the codename index and the bodies.
================
*/
export function sceneCatalogs() {
	const models = Object.entries( BODIES ).map( ( [codename, body] ) => ({
		codename,
		refObjId: body.refObjId,
		walkSpeed: 16,
		runSpeed: 50,
		glb: body.glb,
		clips: Object.keys( PLAYER_CLIPS ),
		animationStates: { attack1: ATTACK, "attached-default-188": STATE }
	}) );
	const equipment = {}, items = {};
	for ( const [id, codename, slot, typeFlags, part] of ITEMS ) {
		const key = Object.keys( BODY_KEYS ).find( body => codename.startsWith( BODY_KEYS[body] ) );
		if ( !key ) throw Error( `No scene body wears ${codename}` );
		const entry = { glb: `/assets/char/scene/${part}.glb`, parts: [ part ] };
		equipment[id] = { slot, bodies: { [key]: entry } };
		items[id] = { codename, typeFlags };
	}
	// The thief suit's Moonlit Ash variant, worn under that server rule.
	equipment[THIEF_SUIT].thiefSuit = true;
	equipment[THIEF_SUIT].bodies.CH_M.moonlit = { glb: "/assets/char/scene/moonlit.glb", parts: [ "moonlit" ] };
	const npcs = {
		MOB_PROBE_BEAST: {
			codename: "MOB_PROBE_BEAST",
			refObjId: 9201,
			kind: "monster",
			glb: "/assets/npc/scene/beast.glb",
			clips: Object.keys( MONSTER_CLIPS ),
			particleModifiers: GLOW_MODIFIERS,
			// Its stand clip drives a modifier set, so its actor carries a
			// model animation frame (model-animation.ts) every frame.
			animationBindings: [ { set: "default", stateId: 0, clip: "stand" } ],
			modifierSets: [ { kind: 1, stateId: 0, animationSetName: "default", count: 0, firstBaseWord4: 0 } ]
		},
		MOB_PROBE_GHOST: {
			codename: "MOB_PROBE_GHOST",
			refObjId: 9203,
			kind: "monster",
			glb: "/assets/npc/scene/beast.glb",
			clips: Object.keys( MONSTER_CLIPS ),
			particleModifiers: GLOW_MODIFIERS
		},
		STRUCTURE_PROBE_TOWER: {
			codename: "STRUCTURE_PROBE_TOWER",
			refObjId: 9204,
			kind: "structure",
			glb: "/assets/npc/scene/tower.glb",
			clips: [ "stand" ],
			structureStages: {
				0: { glb: "/assets/npc/scene/tower.glb" },
				1: { glb: "/assets/npc/scene/tower-damaged.glb" }
			}
		},
		COS_PROBE_HORSE: {
			codename: "COS_PROBE_HORSE",
			refObjId: 9202,
			kind: "cos",
			glb: "/assets/npc/scene/horse.glb",
			clips: Object.keys( HORSE_CLIPS )
		}
	};
	const defaultWear = Object.fromEntries( DEFAULT_WEAR.map( key => [
		"CH_M_" + key,
		{ glb: `/assets/char/scene/${key}.glb`, parts: [ key ] }
	] ) );
	// The clothes a Chinese player wears in an arena: the local team's
	// (actor-appearance.ts fortressAppearance index 3) and the other's (4).
	const fortressWear = {
		CH_M_3: { glb: "/assets/char/scene/ally.glb", parts: [ "ally" ] },
		CH_M_4: { glb: "/assets/char/scene/arena.glb", parts: [ "arena" ] }
	};
	// The hair a Chinese body in berserk (Hwan) wears on its neck.
	const hwan = { CH_M: { glb: "/assets/char/scene/hwan.glb", parts: [ "hwan" ], clip: "stand", bone: "Bip01 Neck" } };
	return {
		roster: { models, dress: { equipment, defaultWear, fortressWear, hwan } },
		items,
		npcs,
		byCode: new Map( ITEMS.map( ( [id, codename] ) => [ codename, id ] ) ),
		bodies: new Map( models.map( model => [ model.codename, model ] ) )
	};
}

/*
================
sceneSources

Every published path the scene's presentation asks for, as bytes: the
eight presentation manifests, the skill records, the effect program
catalogue and the GLBs. A path not here is absent, as a missing file is.
================
*/
export function sceneSources() {
	const catalogs = sceneCatalogs(), sources = new Map();
	const json = ( pathname, value ) => sources.set( pathname, new TextEncoder().encode( JSON.stringify( value ) ) );
	json( "/assets/char/roster.json", catalogs.roster );
	json( "/assets/npc/manifest.json", { models: catalogs.npcs } );
	json( "/assets/data/missionPresentation.json", { itemsByRefObjId: catalogs.items } );
	json( "/assets/audio/effectsound.json", { rules: [] } );
	json( "/assets/anim/manifest.json", { models: {} } );
	json( "/assets/itemdrop/manifest.json", { format: "sro-mission-itemdrop-models", models: goldDropModels() } );
	json( "/assets/data/skillAudioData.json", { skillAudioRows: [] } );
	json( "/assets/data/characterActionData.json", { characterActionEffectRows: [] } );
	json( "/assets/skillfx/manifest.json", { format: "sro-skill-stage-models", models: {} } );
	json( "/assets/skill/effectRecords.json", {
		[SKILL]: {
			authoredStages: [ DAMAGE_STAGE ],
			animTable0: [ "ANI_ATTACK1" ],
			animTable1: [ "ANI_ATTACK1" ],
			animTable2: [ "ANI_ATTACK1" ]
		},
		[HIDING_SKILL]: { authoredStages: [ DAMAGE_STAGE ], animTable2: [ "ANI_ATTACK1" ], hideWeapon: 1 },
		[SILENT_SKILL]: { authoredStages: [] },
		[HOST_SKILL]: { authoredStages: [], attachedMotion: { set: "default", id: 188 } }
	} );
	json( "/assets/skill/namedEffectRecords.json", {} );
	const anchor = {
		name: "anchor",
		lifeCommand: { name: "NeverExtinct" },
		viewCommand: { name: "ViewNone" },
		renderCommand: { name: "RenderNone" },
		emitterProgram: [],
		preProgram: [],
		postEmitterProgram: [],
		renderProgram: [],
		children: [],
		globalData: { totalFrames: 20, parameters: [] }
	};
	json( "/assets/effects/programs.json", { framesPerSecond: 20, effects: { [GLOW]: { scale: 1, root: anchor } } } );
	for ( const body of Object.values( BODIES ) ) sources.set( body.glb, glb( "body", PLAYER_CLIPS ) );
	for ( const part of [ ...ITEMS.map( item => item[4] ), ...DEFAULT_WEAR, "moonlit", "ally", "arena", "hwan" ] ) {
		sources.set( `/assets/char/scene/${part}.glb`, glb( part, PART_CLIPS ) );
	}
	sources.set( catalogs.npcs.MOB_PROBE_BEAST.glb, glb( "body", MONSTER_CLIPS ) );
	sources.set( catalogs.npcs.COS_PROBE_HORSE.glb, glb( "body", HORSE_CLIPS ) );
	sources.set( "/assets/npc/scene/tower.glb", glb( "body", { stand: 1 } ) );
	sources.set( "/assets/npc/scene/tower-damaged.glb", glb( "body", { stand: 1 } ) );
	for ( const model of Object.values( goldDropModels() ) ) sources.set( model.glb, glb( "heap", { stand: 1 } ) );
	return sources;
}

/*
================
cast

One cast as the worker publishes it: received, shot with its impact 30
frames later (the hit result first), finished 50 frames after that. The
gameplay cast list is replaced whole: the scene's casts do not overlap.
================
*/
function cast( session, at, caster, target, impact, skill = SKILL ) {
	const base = { token: at, caster, target, skill, damage: impact.damage, fatal: impact.fatal };
	const received = { ...base, receivedAtMs: session.simulationMs( at ) };
	session.gameplay( at, { casts: [ received ] } );
	session.event( at + 30, { kind: "hp-result", gid: target, key: `${at}:${target}:0`, fatal: impact.fatal } );
	session.gameplay( at + 30, {
		casts: [ {
			...received,
			shotAtMs: session.simulationMs( at + 30 ),
			results: [ { target, impacts: [ { flags: 0, secondaryAmount: 0, ...impact } ] } ]
		} ]
	} );
	session.gameplay( at + 80, { casts: [] } );
}

/*
================
sceneSession

The scene: the local player and a standing crowd that stays settled to
the end (peers 0..3), with one peer per input that must unsettle it:
4 walks, 5 changes its equipment, 6 sits and stands, 7 dies, 8 rides,
9 is frozen then slowed, 10 leaves view for one frame, 11 and 12 attack
the beast (the second cast hides the weapon, a third kills it), 13 is of
the other arena team, 14 and 17 wear job suits, 15 casts with no motion,
16 hosts two attached motions, one after the other ends, 18 emotes, 19
picks up. Of the standing four, 2's pet resolve runs out with no new
row and 3 goes berserk and back (its scale held at 1.1 by the effects
owner, then eased back). The beast and the ghost carry a glow particle,
and the beast a model animation frame: the ghost dies by its vitals
alone and leaves, its particle handed to its fading copy, and the camera
pulls back at the end until the dead beast's particle is hidden by
distance. The tower changes its stage on its own timer.
================
*/
export function sceneSession( seed ) {
	const catalogs = sceneCatalogs(), rng = seeded( seed ), session = createSession( "scene", seed );
	const peers = Array.from( { length: 20 }, ( _, i ) => FIRST_PEER_GID + i );
	const beast = FIRST_PEER_GID + 20, horse = FIRST_PEER_GID + 21, ghost = FIRST_PEER_GID + 22;
	const tower = FIRST_PEER_GID + 23, towerHp = 10000;
	const vitals = new Map( [ LOCAL_GID, ...peers, beast, ghost, tower ].map( gid => {
		const hp = gid === tower ? towerHp : 500;
		return [ gid, { gid, hp, maxHp: hp, abnormal: 0 } ];
	} ) );
	const setVital = ( at, gid, changes ) => {
		vitals.set( gid, { ...vitals.get( gid ), ...changes } );
		session.gameplay( at, { vitals: [ ...vitals.values() ] } );
	};
	const man = ( ...codes ) => codes.map( code => wear( catalogs, code, "CH_M" ) );
	const armed = man( "ITEM_CH_M_PROBE_TUNIC", "ITEM_CH_M_PROBE_BOOTS", "ITEM_CH_SWORD_PROBE" );
	const local = pose( CENTER, CENTER );
	session.event( 0, { kind: "reset", epoch: 1 } );
	session.spawn(
		0,
		playerRow( catalogs, LOCAL_GID, "CHAR_CH_MAN_PROBE", local, {
			kind: "local-player",
			name: "local",
			equipment: armed
		} )
	);
	session.event( 0, {
		kind: "gameplay",
		state: {
			localGid: LOCAL_GID,
			pose: local,
			moving: false,
			movementRevision: 0,
			inventory: armed,
			vitals: [ ...vitals.values() ],
			casts: [],
			attachedEffects: []
		}
	} );
	session.view( 0, { x: CENTER, y: 60, z: CENTER - 120 }, -0.35 );
	peers.forEach( ( gid, i ) => {
		const woman = WOMEN.includes( i );
		const extra = i === ARENA_PEER ?
			{ arenaTeam: 1 } :
			i === THIEF_PEER ?
			{ equipment: man( "ITEM_CH_M_PROBE_TUNIC", "ITEM_CH_M_PROBE_THIEF_SUIT" ) } :
			i === TRADER_PEER ?
			{ equipment: man( "ITEM_CH_M_PROBE_TUNIC", "ITEM_CH_M_PROBE_TRADER_SUIT" ) } :
			i === RESOLVE_PEER ?
			{ petResolve: { untilMs: session.simulationMs( 650 ), stacks: 3 } } :
			{};
		session.spawn(
			0,
			playerRow( catalogs, gid, woman ? "CHAR_EU_WOMAN_PROBE" : "CHAR_CH_MAN_PROBE", grid( rng, i ), {
				equipment: woman ? [ wear( catalogs, "ITEM_EU_W_PROBE_DRESS", "EU_W" ) ] : armed,
				...extra
			} )
		);
	} );
	session.spawn( 0, npcRow( catalogs, beast, "MOB_PROBE_BEAST", grid( rng, 20 ) ) );
	session.spawn( 0, npcRow( catalogs, horse, "COS_PROBE_HORSE", grid( rng, 22 ), { ownerGid: peers[8] } ) );
	// A monster row with no appearance state: it dies by its vitals alone.
	const { appearanceState: _state, ...ghostRow } = npcRow( catalogs, ghost, "MOB_PROBE_GHOST", grid( rng, 24 ) );
	session.spawn( 0, ghostRow );
	session.spawn(
		0,
		npcRow( catalogs, tower, "STRUCTURE_PROBE_TOWER", grid( rng, 26 ), { maxHp: towerHp, structureState: 0 } )
	);
	// Only the beast's hp is seeded: a seeded hp stands over the vitals'.
	session.event( 0, { kind: "hp-seed", gid: beast, hp: 500 } );
	// peers 0..3 stand to the end: the skipped case.
	walk( session, peers[4], 240, { x: CENTER + 40, z: CENTER + 90 } );
	session.state( 300, peers[5], { equipment: man( "ITEM_CH_M_PROBE_ROBE", "ITEM_CH_SWORD_PROBE" ) } );
	session.state( 620, peers[5], { equipment: [] } );
	session.state( 330, peers[6], { movementMode: MOVEMENT_SEATED } );
	session.state( 480, peers[6], { movementMode: MOVEMENT_RUN } );
	session.state( 360, peers[7], { appearanceState: [ DEAD, 0, 0, 0, 0, 0, 0 ] } );
	setVital( 360, peers[7], { hp: 0 } );
	session.state( 390, peers[8], { mountedOn: horse } );
	session.state( 540, peers[8], { mountedOn: undefined } );
	setVital( 420, peers[9], { abnormal: 0x1 } );
	setVital( 520, peers[9], { abnormal: 0x2 } );
	session.state( 560, peers[9], { pickupRevision: 1 } );
	setVital( 660, peers[9], { abnormal: 0 } );
	session.state( 580, peers[19], { pickupRevision: 1 } );
	// An emote empties an armed player's hands while it plays (emote.ts).
	session.state( 380, peers[18], { emote: { action: 1, revision: 1, atMs: session.simulationMs( 380 ) } } );
	const home = session.row( peers[10] );
	session.state( 450, peers[10], { regionId: REGION + 1, x: 400, z: 400 } );
	session.state( 451, peers[10], { regionId: home.regionId, x: home.x, z: home.z } );
	// A cast with no motion: only the caster's facing shows it.
	session.gameplay( 240, {
		casts: [ {
			token: 240,
			caster: peers[15],
			target: beast,
			skill: SILENT_SKILL,
			receivedAtMs: session.simulationMs( 240 )
		} ]
	} );
	session.gameplay( 280, { casts: [] } );
	// An attached effect plays its host motion on a standing body.
	// A second, joining while the first plays, leaves first.
	const host = { gid: peers[16], skill: HOST_SKILL, token: 900, phase: 1 };
	const second = { gid: peers[16], skill: HOST_SKILL, token: 901, phase: 1 };
	session.gameplay( 300, { attachedEffects: [ host ] } );
	session.gameplay( 330, { attachedEffects: [ host, second ] } );
	session.gameplay( 390, { attachedEffects: [ host ] } );
	session.gameplay( 420, { attachedEffects: [] } );
	// The tower's damage turns its stage model on its own timer, with no new row.
	setVital( 440, tower, { hp: towerHp / 5 } );
	cast( session, 500, peers[11], beast, { type: 0, damage: 40, fatal: false } );
	cast( session, 600, peers[12], beast, { type: 0, damage: 40, fatal: false }, HIDING_SKILL );
	// Berserk (body state 1) on a standing body, no cast near it.
	session.state( 560, peers[BERSERK_PEER], { appearanceState: [ ALIVE, 0, 1, 0, 0, 0, 0 ] } );
	session.state( 680, peers[BERSERK_PEER], { appearanceState: [ ALIVE, 0, 0, 0, 0, 0, 0 ] } );
	setVital( 640, ghost, { hp: 0 } );
	session.despawn( 690, ghost );
	cast( session, 700, peers[11], beast, { type: 0, damage: 420, fatal: true } );
	// The dress inputs no row of the wearer carries: the local player joins
	// and leaves an arena team (fortress clothes on a peer of the other
	// team), and the server's job-suit and Moonlit Ash rules turn.
	session.state( 740, LOCAL_GID, { arenaTeam: 0 } );
	session.state( 800, LOCAL_GID, { arenaTeam: 255 } );
	session.gameplay( 760, { jobSuitHidesArmor: true } );
	session.gameplay( 820, { moonlitThiefSuit: true } );
	session.gameplay( 860, { jobSuitHidesArmor: false, moonlitThiefSuit: false } );
	// The camera pulls back past the distance that hides a particle
	// (entity-lod.ts: 720 units) while the dead beast lies still.
	for ( let at = 900; at < 1000; at++ ) {
		const back = 120 + 1100 * Math.min( 1, (at - 900) / 60 );
		session.view( at, { x: CENTER, y: 60 + back * .3, z: CENTER - back }, -0.35 );
	}
	// 16 s of standing lets the idle fidget timers fire (random-idle.ts: 15 s).
	return session.finish( 17 * FRAME_HZ );
}
