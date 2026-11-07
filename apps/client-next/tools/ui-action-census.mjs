/*
===========================================================================

ui-action-census.mjs - every published control id, activated and recorded

The action half of the ui.ts wave-0 census (#2515; the static half is
tools/ui-census.mjs). For every scene in the catalogue below - the world
HUD, every registered panel, and the service windows the panel sweep
opens - the census settles a fresh fixture, reads the published control
ids, and activates each one, recording the outcome: the session commands
sent, the panel after, the focus after, and the next scene's control
list (as a hash, with the added and removed ids spelled out). Ids that
legitimately do nothing record as `null`: the no-op is a census row too,
so a move that wakes a dead control (or puts a live one to sleep) turns
the census red. The keyboard paths (Escape, Enter, F1-F4, R) and the
wheel targets are recorded per scene the same way.

One fixture serves a whole scene: after any activation whose outcome is
not a no-op, the fixture is rebuilt and the scene re-driven, so every
row is measured from the scene's baseline state, exactly as if its
fixture were fresh. A no-op row provably changed none of the recorded
observables, so it cannot poison the next row. (An activation that arms
an effect invisible to commands, panel, focus and the published control
list for longer than the settle window would break that argument; the
settle runs six seconds of synthetic time, and no control in the file
is known to do this.)

The census is regenerated, never edited: `--write` rewrites the
checked-in table, `--check` (the default) rebuilds it and fails on any
difference. Scenes build in parallel worker processes (`--scene <name>`
runs one scene and prints its row), capped by censusWorkers.mjs so two
checks on one PC cannot fan out into memory exhaustion (#2766); nothing
in ui.ts moves under this tool.

===========================================================================
*/

import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { CLIENT_PUBLIC_ROOT } from "../../../scripts/lib/generatedRoot.mjs";
import "../tests/helpers/native-source-loader.mjs";
import { censusWorkerCount } from "./censusWorkers.mjs";
import { root } from "./project.mjs";

const { createUi } = await import( "../src/engine/runtime/ui/ui.ts" );
const { uiPanels } = await import( "../src/engine/foundation/ui/panels.ts" );
const { emptyStall, stallRequest } = await import( "../src/engine/foundation/gameplay/stall.ts" );
const { fortressPacket } = await import( "../src/engine/foundation/gameplay/fortress.ts" );
const { fortressProductionQuery } = await import(
	"../src/engine/foundation/gameplay/fortress-production.ts"
);

const CENSUS_PATH = path.join( root, "tests/runtime/ui-action-census.json" );
const KEYS = [ "Escape", "Enter", "F1", "F2", "F3", "F4", "KeyR" ];
const WORKERS = censusWorkerCount();

// ============================================================================
// The fixture: ui-fixture.mjs's asset delivery without the text-run
// expansion - the census reads controls, not glyph quads, and expansion
// is most of a cold fixture's cost.
// ============================================================================

/*
================
censusFixture

A production HUD fed synchronously from the generated tree, recording
the session commands it sends.
================
*/
function censusFixture( commands ) {
	const pending = new Map();
	let nextId = 0;
	const ui = createUi( {
		assets: {
			available: () => 8,
			take: id => {
				const bytes = pending.get( id );
				pending.delete( id );
				return bytes?.absent ?
					{ kind: "error", id, error: "absent", absent: true } :
					bytes?.image ?
					{ kind: "image", id, image: bytes.image } :
					bytes ?
					{
						kind: "bytes",
						id,
						buffer: bytes.buffer.slice( bytes.byteOffset, bytes.byteOffset + bytes.byteLength )
					} :
					null;
			},
			request: url => {
				const id = ++nextId, path = decodeURIComponent( new URL( url ).pathname );
				try {
					const bytes = readFileSync( CLIENT_PUBLIC_ROOT + path );
					if ( path.endsWith( ".json" ) || path.endsWith( ".txt" ) || path.endsWith( ".texture" ) ) {
						pending.set( id, bytes );
					} else if ( path.endsWith( ".png" ) ) {
						pending.set( id, {
							image: {
								width: bytes.readUInt32BE( 16 ),
								height: bytes.readUInt32BE( 20 ),
								/*
								================
								close
								================
								*/
								close() {}
							}
						} );
					}
				} catch {
					if ( path.endsWith( ".json" ) ) pending.set( id, { absent: true } );
				}
				return id;
			},
			cancel: id => pending.delete( id )
		},
		commands,
		publish: () => {},
		texture: () => {},
		base: "https://fixture.invalid/",
		apiBase: "https://fixture.invalid/",
		sound: () => {},
		saveOptions: () => {},
		audioPreference: () => {},
		saveSight: () => {},
		saveBindings: () => {},
		saveVideo: () => {}
	} );
	const entity = { gid: 1, regionId: 1, x: 0, y: 0, z: 0, heading: 0, kind: "player", name: "Player", mountedOn: 0 };
	const state = {
		session: { phase: "world", revision: 1, character: "Player" },
		gameplay: { localGid: 1, pose: { ...entity, angle: 0 }, vitals: [], inventory: [], target: 0 },
		entities: [ entity ],
		width: 1600,
		height: 900,
		worldReady: true
	};
	return { ui, state, dispose: () => ui.dispose() };
}

/*
================
settle

Step until the published control list is unchanged for two steps (six
seconds of synthetic time at most). An idle HUD republishes nothing, so
the last semantics are carried in and back out; the clock only advances.
================
*/
function settle( held ) {
	let prev = "", stable = 0;
	for ( let i = 0; i < 160; i++ ) {
		held.sem = held.fixture.ui.step( held.fixture.state, held.now += 100 ) ?? held.sem;
		const ids = held.sem ? held.sem.controls.map( control => control.id ).join( "\n" ) : "";
		stable = ids === prev && ids !== "" ? stable + 1 : 0;
		if ( stable >= 2 ) break;
		prev = ids;
	}
	if ( !held.sem ) throw new Error( "the fixture published nothing" );
	const ids = held.sem.controls.map( control => control.id );
	if ( new Set( ids ).size !== ids.length ) throw new Error( "duplicate control ids published" );
	return held.sem;
}

// ============================================================================
// The scene catalogue. Openers mirror tests/runtime/ui-panel-sweep.test.mjs:
// a window must actually open, through the flow that opens it in game.
// ============================================================================

const OFFICIAL = 17;
const GLOBAL_CHAT_TYPE = 0x29ec;
const BAG_SLOT = 13;
const TRADE_GOODS_TYPE = 0x46c;
const TRADER = 18;
const TRANSPORT = 7;

/*
================
talkTo

An open talk menu with a fortress official.
================
*/
function talkTo( f ) {
	f.state.entities.push( { ...f.state.entities[0], gid: OFFICIAL, refObjId: 2011, kind: "npc", name: "Official" } );
	Object.assign( f.state.gameplay, { target: OFFICIAL, npcConversation: { phase: "menu", gid: OFFICIAL } } );
}

/*
================
tradeWithTransport

A trade-goods shop opened with a transport out.
================
*/
function tradeWithTransport( f ) {
	f.state.entities.push( { ...f.state.entities[0], gid: TRADER, refObjId: 2011, kind: "npc", name: "Trader" } );
	f.state.gameplay.cosRecords = [
		{ gid: TRANSPORT, refObjId: 100, band: 4, hp: 100, mp: 0, status: 0, dead: false, inventory: [] }
	];
	Object.assign( f.state.gameplay, {
		target: TRADER,
		shop: { npc: TRADER, name: "Trader", offers: [], cosGid: TRANSPORT }
	} );
	f.ui.event( { kind: "activate", id: "open-window:Shop" } );
}

const OPENERS = {
	"COS inventory"( f ) {
		f.state.gameplay.cosRecords = [ { gid: 7, refObjId: 100, band: 4, hp: 100, mp: 0, status: 0, dead: false } ];
		f.ui.event( { kind: "activate", id: "open-window:COS inventory" } );
	},
	"Storage"( f ) {
		f.state.gameplay = {
			...f.state.gameplay,
			storage: { npc: OFFICIAL, phase: "open", capacity: 150, gold: "0", items: [] }
		};
		f.ui.event( { kind: "activate", id: "open-window:Storage" } );
	},
	"Academy Matching"( f ) {
		f.state.gameplay.academy = { member: false, rows: [], request: null, page: 0 };
		f.ui.event( { kind: "activate", id: "open-window:Academy Matching" } );
	},
	"Academy honor"( f ) {
		f.state.gameplay.academy = {
			member: false,
			rows: [],
			request: null,
			page: 0,
			pages: 0,
			result: null,
			honorRanks: {
				rows: [ { ranking: 1, rank: 1, name: "Mentor", level: 60, maxLevel: 80, graduates: 3, guild: "" } ],
				sequence: 1
			}
		};
	},
	"Stall network"( f ) {
		f.state.gameplay.stall = stallRequest( emptyStall(), { kind: "stall-network-open", open: true } ).state;
		f.ui.event( { kind: "activate", id: "open-window:Stall network" } );
	},
	"Skin change"( f, step ) {
		Object.assign( f.state.entities[0], { refObjId: 1907, bodyShape: 0x22 } );
		Object.assign( f.state.gameplay, {
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			playerModels: [ { refObjId: 1907, sex: 1 }, { refObjId: 1920, sex: 0 } ],
			inventory: [ {
				slot: 13,
				refObjId: 1,
				typeFlags: (3 << 2) | (3 << 5) | (13 << 7) | (9 << 11),
				quantity: 1,
				name: "Skin scroll",
				icon: "item/etc/hp_potion_01.ddj"
			} ]
		} );
		f.ui.event( { kind: "key", code: "KeyI" } );
		step();
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
	},
	"Fortress war application"( f, step ) {
		talkTo( f );
		step();
		f.ui.event( { kind: "activate", id: "npc-fortress-war" } );
		f.state.gameplay.fortressApplication = { warStart: null, applied: null, sequence: 1 };
	},
	"Job ranking"( f ) {
		f.state.gameplay.jobRanks = {
			lists: [ { job: 1, kind: 0, rows: [ { rank: 1, alias: "Trader", grade: 2, value: 40 } ] } ],
			opened: { job: 1, kind: 0, sequence: 1 }
		};
	},
	"Fortress war schedule"( f, step ) {
		talkTo( f );
		Object.assign( f.state.gameplay, {
			fortress: {
				...f.state.gameplay.fortress,
				worldId: 7,
				worlds: [ { id: 7, code: "FORTRESS_JANGAN" } ],
				fortresses: [ { id: 1, code: "FORTRESS_JANGAN", nameStrId: "FORTRESS_NAME" } ],
				wars: [],
				registered: [],
				serviceSequence: 0
			}
		} );
		step();
		f.ui.event( { kind: "activate", id: "npc-fortress-schedule" } );
		f.state.gameplay.fortress = {
			...f.state.gameplay.fortress,
			serviceSequence: 1,
			service: { action: 5, result: 1, applicants: [] }
		};
	},
	"Fortress tax"( f, step ) {
		talkTo( f );
		Object.assign( f.state.gameplay, {
			fortress: {
				...f.state.gameplay.fortress,
				worldId: 7,
				worlds: [ { id: 7, code: "FORTRESS_JANGAN" } ],
				fortresses: [ { id: 1, code: "FORTRESS_JANGAN", nameStrId: "FORTRESS_NAME", taxTargets: 63 } ],
				wars: [],
				registered: [],
				serviceSequence: 0
			}
		} );
		step();
		f.ui.event( { kind: "activate", id: "npc-fortress-tax" } );
		f.state.gameplay.fortress = {
			...f.state.gameplay.fortress,
			serviceSequence: 1,
			service: { action: 0, result: 1, fortress: 1, taxRate: 10, gold: "1234" }
		};
	},
	"Fortress map"( f, step ) {
		talkTo( f );
		Object.assign( f.state.gameplay, {
			fortress: {
				...f.state.gameplay.fortress,
				worldId: 7,
				worlds: [ { id: 7, code: "FORTRESS_JANGAN" } ],
				fortresses: [ { id: 1, code: "FORTRESS_JANGAN", nameStrId: "FORTRESS_NAME" } ],
				wars: [],
				registered: [],
				serviceSequence: 0
			}
		} );
		step();
		f.ui.event( { kind: "activate", id: "npc-fortress-structure" } );
		f.state.gameplay.fortress = {
			...f.state.gameplay.fortress,
			serviceSequence: 1,
			service: { action: 0x18, result: 1, fortress: 1, structures: [] }
		};
	},
	"Fortress production"( f, step ) {
		talkTo( f );
		Object.assign( f.state.gameplay, {
			targetCapabilities: 0x4000000,
			fortressForge: [ { refObjId: 9001, gold: 1, gp: 1, minutes: 1, staff: "trainer", name: "Cart" } ],
			fortress: {
				...f.state.gameplay.fortress,
				worldId: 7,
				worlds: [ { id: 7, code: "FORTRESS_JANGAN" } ],
				fortresses: [ { id: 1, code: "FORTRESS_JANGAN", nameStrId: "FORTRESS_NAME", taxTargets: 63 } ],
				wars: [],
				registered: [],
				serviceSequence: 0
			}
		} );
		step();
		f.ui.event( { kind: "activate", id: "npc-fortress-production:trainer" } );
		f.state.gameplay.fortress.production = fortressProductionQuery( undefined, "trainer", 1, 1 );
		f.state.gameplay.fortress = fortressPacket( f.state.gameplay.fortress, {
			opcode: 0xb1e1,
			payload: Uint8Array.from( [ 0x11, 1, 1, 0, 0, 0, 0 ] )
		} );
	}
};

const SERVICE_WINDOWS = {
	"Global chat"( f ) {
		Object.assign( f.state.gameplay, {
			inventorySlotCount: 45,
			inventory: [ { slot: BAG_SLOT, refObjId: 24600, typeFlags: GLOBAL_CHAT_TYPE, quantity: 3 } ]
		} );
		f.ui.event( { kind: "double-activate", id: "slot:" + BAG_SLOT } );
	},
	"Exchange"( f ) {
		f.state.gameplay.exchange = {
			open: true,
			partner: 2,
			own: [],
			theirs: [],
			ownGold: 0,
			theirGold: 0,
			ownLocked: false,
			theirLocked: false,
			approved: false,
			requesting: false
		};
	},
	"Stall"( f ) {
		f.state.gameplay.stall = { ...emptyStall(), phase: "owner", title: "Stall" };
	},
	"Shop with a transport out"( f ) {
		tradeWithTransport( f );
	}
};

/*
================
sceneNames

The catalogue: the world HUD, every registered panel, the service
windows. Order is the census's own and never changes under a move.
================
*/
function sceneNames() {
	return [ "World", ...uiPanels(), ...Object.keys( SERVICE_WINDOWS ) ];
}

/*
================
driveScene

Build a fixture and drive it to the named scene's baseline.
================
*/
function driveScene( name, commands ) {
	const fixture = censusFixture( command => commands.push( command ) );
	const held = { fixture, sem: null, now: 0 };
	try {
		const step = () => settle( held );
		step();
		if ( name !== "World" ) {
			const open = OPENERS[name] ?? SERVICE_WINDOWS[name] ??
				(() => fixture.ui.event( { kind: "activate", id: "open-window:" + name } ));
			open( fixture, step );
			step();
		}
		return held;
	} catch ( error ) {
		fixture.dispose();
		throw error;
	}
}

// ============================================================================
// Recording
// ============================================================================

/*
================
stableJson

A command as stable JSON text: sorted keys at every level, so the same
command always records byte-identically.
================
*/
function stableJson( value ) {
	if ( Array.isArray( value ) ) return value.map( stableJson );
	if ( value && typeof value === "object" ) {
		const out = {};
		for ( const key of Object.keys( value ).sort() ) {
			if ( value[key] !== undefined ) out[key] = stableJson( value[key] );
		}
		return out;
	}
	return value;
}

/*
================
listHash

The short hash of a sorted control-id list.
================
*/
function listHash( ids ) {
	return createHash( "sha256" ).update( ids.join( "\n" ) ).digest( "hex" ).slice( 0, 16 );
}

/*
================
observe

The recorded observables of one settled fixture.
================
*/
function observe( fixture, sem ) {
	return {
		ids: sem.controls.map( control => control.id ).sort(),
		panel: fixture.ui.stats().panel,
		focus: sem.focusRequest?.id ?? null
	};
}

/*
================
outcomeOf

One activation's outcome against the baseline, or null for a no-op.
================
*/
function outcomeOf( baseline, after, commands ) {
	const sent = commands.map( stableJson );
	const added = after.ids.filter( id => !baseline.ids.includes( id ) );
	const removed = baseline.ids.filter( id => !after.ids.includes( id ) );
	if (
		sent.length === 0 && added.length === 0 && removed.length === 0 &&
		after.panel === baseline.panel && after.focus === baseline.focus
	) {
		return null;
	}
	return {
		commands: sent,
		panel: after.panel,
		focus: after.focus,
		hash: listHash( after.ids ),
		added,
		removed
	};
}

/*
================
censusScene

The whole census of one scene. One fixture serves until an activation
changes an observable; then the scene is re-driven, so every row is
measured from the baseline.
================
*/
function censusScene( name ) {
	const commands = [];
	let held = driveScene( name, commands );
	let baseline = observe( held.fixture, held.sem );
	let dirty = false;
	const rebuild = () => {
		held.fixture.dispose();
		commands.length = 0;
		held = driveScene( name, commands );
		baseline = observe( held.fixture, held.sem );
		dirty = false;
	};
	const probe = event => {
		if ( dirty ) rebuild();
		commands.length = 0;
		held.fixture.ui.event( event );
		const outcome = outcomeOf( baseline, observe( held.fixture, settle( held ) ), commands );
		if ( outcome !== null ) dirty = true;
		return outcome;
	};

	const rows = {};
	for ( const id of baseline.ids ) rows[id] = probe( { kind: "activate", id } );
	const keys = {};
	for ( const code of KEYS ) keys[code] = probe( { kind: "key", code } );
	const wheelChanged = {};
	let wheelNoop = 0;
	const targets = held.sem.controls.filter( control => control.rect && control.rect[2] > 0 && control.rect[3] > 0 );
	for ( const control of targets ) {
		const [x, y, w, h] = control.rect;
		const outcome = probe( {
			kind: "scroll",
			x: Math.round( x + w / 2 ),
			y: Math.round( y + h / 2 ),
			delta: -3
		} );
		if ( outcome === null ) wheelNoop++;
		else wheelChanged[control.id] = outcome;
	}
	held.fixture.dispose();
	return {
		controls: baseline.ids,
		panel: baseline.panel,
		focus: baseline.focus,
		hash: listHash( baseline.ids ),
		rows,
		keys,
		wheel: { changed: wheelChanged, noop: wheelNoop }
	};
}

// ============================================================================
// Building, checking, and the worker protocol
// ============================================================================

/*
================
buildTable

Every scene's census, keyed by scene name.
================
*/
async function buildTable( names ) {
	const scenes = {};
	let next = 0;
	async function worker() {
		while ( next < names.length ) {
			const name = names[next++];
			scenes[name] = await runSceneWorker( name );
		}
	}
	await Promise.all( Array.from( { length: Math.min( WORKERS, names.length ) }, worker ) );
	const ordered = {};
	for ( const name of names ) ordered[name] = scenes[name];
	return {
		about: "Generated by tools/ui-action-census.mjs. Do not edit: regenerate with --write. " +
			"A null row is a recorded no-op; any routing change that alters a row fails --check (#2515).",
		scenes: ordered
	};
}

/*
================
runSceneWorker

One scene in a child process, so scenes build in parallel and one
scene's cost never serialises the rest.
================
*/
function runSceneWorker( name ) {
	return new Promise( ( resolvePromise, rejectPromise ) => {
		const child = spawn(
			process.execPath,
			[ fileURLToPath( import.meta.url ), "--scene", name ],
			{ stdio: [ "ignore", "pipe", "pipe" ] }
		);
		let out = "", err = "";
		child.stdout.on( "data", chunk => out += chunk );
		child.stderr.on( "data", chunk => err += chunk );
		child.on( "error", rejectPromise );
		child.on( "close", code => {
			if ( code !== 0 ) {
				rejectPromise( new Error( `scene ${name} failed (${code}): ${err.slice( -2000 )}` ) );
				return;
			}
			try {
				resolvePromise( JSON.parse( out ) );
			} catch {
				rejectPromise( new Error( `scene ${name} printed no census: ${out.slice( -500 )}` ) );
			}
		} );
	} );
}

/*
================
canonical
================
*/
function canonical( value ) {
	if ( Array.isArray( value ) ) return value.map( canonical );
	if ( value && typeof value === "object" ) {
		const out = {};
		for ( const key of Object.keys( value ).sort() ) out[key] = canonical( value[key] );
		return out;
	}
	return value;
}

/*
================
main
================
*/
async function main() {
	const args = process.argv.slice( 2 );
	const sceneAt = args.indexOf( "--scene" );
	if ( sceneAt >= 0 ) {
		const name = args[sceneAt + 1];
		process.stdout.write( JSON.stringify( censusScene( name ) ) );
		return;
	}
	const table = canonical( await buildTable( sceneNames() ) );
	const text = JSON.stringify( table, null, "\t" ) + "\n";
	const sceneCount = Object.keys( table.scenes ).length;
	const rowCount = Object.values( table.scenes )
		.reduce( ( sum, scene ) => sum + Object.keys( scene.rows ).length, 0 );
	if ( args.includes( "--write" ) ) {
		fs.writeFileSync( CENSUS_PATH, text );
		console.log( `ui-action-census: wrote ${sceneCount} scenes, ${rowCount} control rows` );
		return;
	}
	const current = fs.existsSync( CENSUS_PATH ) ? fs.readFileSync( CENSUS_PATH, "utf8" ) : "";
	if ( current === text ) {
		console.log( `ui-action-census: up to date (${sceneCount} scenes, ${rowCount} control rows)` );
		return;
	}
	console.error(
		"ui-action-census: tests/runtime/ui-action-census.json is stale or differs; " +
			"run node tools/ui-action-census.mjs --write and review the row diff"
	);
	if ( current ) {
		const before = JSON.parse( current );
		for ( const name of Object.keys( table.scenes ) ) {
			if ( JSON.stringify( canonical( before.scenes?.[name] ) ) !== JSON.stringify( table.scenes[name] ) ) {
				console.error( `first differing scene: ${name}` );
				break;
			}
		}
	}
	process.exit( 1 );
}

await main();
