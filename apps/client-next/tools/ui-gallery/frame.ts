/*
===========================================================================
frame.ts - one owned production UI/renderer/platform fixture per document

Reset replaces this iframe and disposes every owner. Only Stall uses real
request/reply reducers; no outgoing command reaches a game or report server.
===========================================================================
*/
import { createUi } from "../../src/engine/runtime/ui/ui";
import { createAssets } from "../../src/engine/runtime/assets/assets";
import { createRenderer } from "../../src/engine/runtime/renderer/renderer";
import { createPlatform } from "../../src/engine/runtime/platform/platform";
import { createBugReportDialog } from "../../src/engine/runtime/bug-report/dialog";
import { stallFrame, stallRequest, type StallCommand } from "../../src/engine/foundation/gameplay/stall";
import { emptySocial } from "../../src/engine/foundation/gameplay/social";
import type { SessionCommand } from "../../src/engine/contracts/session";
import type { UiEvent, UiQuad, UiSemantics, UiView } from "../../src/engine/contracts/ui";
import { guildSnapshot, journeys, seedJourney, stallOffers } from "./scenarios";
import "../../src/engine/runtime/platform/loading.css";
import "../../src/engine/runtime/bug-report/bug-report.css";
import productionHtml from "../../index.html?raw";
import { OPENERS, SERVICE_WINDOWS } from "./entry-recipes.mjs";
import { entryEvidence } from "./entry-evidence.mjs";
import { extendedRecipes } from "./extended";
import { createFrontendFixture } from "./frontend-fixture";
import { createStageFixture } from "./frontend-stage-fixture";
import { createPresentationRandom } from "../../src/engine/runtime/random/random";

const parameters = new URLSearchParams( location.search );
// Old direct fixture links resolve to the actual owner; the duplicate is retired from navigation.
const requestedId = parameters.get( "scenario" ) === "frontend:race" ?
	"frontend:stage-race" :
	parameters.get( "scenario" );
const id = journeys.find( row => row.id === requestedId )?.id ?? "hud-tools";
const canvas = document.querySelector<HTMLCanvasElement>( "canvas" )!;
// Read the authored production chrome through an inert template. Only the
// FPS subtree is inserted; no game bootstrap or loading-page nodes execute.
const shell = document.createElement( "template" );
shell.innerHTML = productionHtml;
const fpsChip = shell.content.querySelector( "#fps-chip" );
if ( !fpsChip ) throw Error( "Production FPS shell is missing" );
document.body.append( fpsChip.cloneNode( true ) );
if ( id.startsWith( "dom:" ) ) {
	for ( const name of [ "startup-loading", "loading-transfer", "update-notice" ] ) {
		const authored = shell.content.querySelector( "#" + name );
		if ( !authored ) throw Error( `Production DOM shell missing: ${name}` );
		document.body.append( authored.cloneNode( true ) );
	}
}
// This dedicated loopback origin owns only QA data. Retire game preferences
// on every fixture document.
for ( const key of Object.keys( localStorage ) ) {
	if ( key.startsWith( "sro:" ) || key.startsWith( "sro." ) ) localStorage.removeItem( key );
}
const state = seedJourney( id );
const assets = createAssets(), renderer = createRenderer( canvas, createPresentationRandom( 1 ) );
// The asset worker holds native texture reads until the page names the
// device's texture formats (texture-variants.ts), as runtime.ts does.
void renderer.textureFeatures().then( features => assets.textureFeatures( features ) );
const trace: { time: number; kind: string; detail: unknown; }[] = [];
const queue: SessionCommand[] = [];
const pending: number[] = [];
let semantics: UiSemantics | null = null, clock = 1000, ready = false, stable = 0;
let stopped = false, animation = 0, lastReport = 0, error: string | null = null;
let playing = false;
let toolsVisible = true, recording = false, petMode = 0;
let entryBaseline: Set<string> | null = null;
let entryFrames = 0;
let entryBusy = false;
let entryStep: { frames: number; resolve: () => void; } | null = null;
let sceneTextures: string[] = [];

// Published anchor witnesses remain distinct from a renderer/screenshot review.
let sceneAnchors: number[] = [];
let loadingPreview: { error: boolean; } | null = null;
const extendedRecipe = extendedRecipes.find( row => row.id === id );
const frontendFixture = createFrontendFixture( assets, state, record );
const stageFixture = createStageFixture( assets, renderer, state, record );

/*
================
openEntry

Record the settled HUD before the production entry so its controls cannot count
as evidence that the requested panel painted.
================
*/
async function openEntry() {
	if ( entryBusy ) return;
	entryBusy = true;
	entryBaseline = new Set( semantics?.controls.map( control => control.id ) ?? [] );
	entryFrames = 0;
	const name = id.slice( id.indexOf( ":" ) + 1 );
	try {
		const step = () => {
			// Production observes immutable publications. Changing a social
			// field on the old gameplay object can otherwise be retained away.
			if ( state.gameplay ) state.gameplay = { ...state.gameplay };
			return new Promise<void>( resolve => {
				entryStep = { frames: 0, resolve };
			} );
		};
		const recipe = OPENERS[name] ?? SERVICE_WINDOWS[name];
		if ( extendedRecipe ) {
			record( "synthetic admission", extendedRecipe.source );
			await extendedRecipe.open( {
				state,
				ui: { event },
				controls: () => semantics?.controls ?? [],
				step,
				actor: () =>
					stageFixture.openWorldActor( {
						step,
						advance: milliseconds => {
							clock += milliseconds;
						}
					} ),
				stage: race =>
					stageFixture.open( race, {
						step,
						advance: milliseconds => {
							clock += milliseconds;
						}
					} ),
				platform,
				report: dialog,
				advance: milliseconds => {
					clock += milliseconds;
				},
				frontend: mode => frontendFixture.open( mode, clock ),
				loading: failed => {
					loadingPreview = { error: failed };
				}
			} );
		} else if ( recipe ) {
			record( "synthetic admission", `Shared production sweep recipe: ${name}` );
			await recipe( { state, ui: { event } }, step );
		} else activate( "open-window:" + name );
	} catch ( failure ) {
		error = String( failure );
	} finally {
		if ( state.gameplay ) state.gameplay = { ...state.gameplay };
		entryFrames = 0;
		entryBusy = false;
	}
}

/*
================
record
================
*/
function record( kind: string, detail: unknown ) {
	trace.push( { time: clock, kind, detail } );
	if ( trace.length > 100 ) trace.shift();
}

/*
================
backdrop

Flat colors expose alpha; the grid is an explicit test background, not world.
================
*/
function backdrop( width: number, height: number ): UiQuad[] {
	const mode = parameters.get( "backdrop" );
	const base: UiQuad = {
		rect: [ 0, 0, width, height ],
		clip: [ 0, 0, width, height ],
		texture: "",
		uv: [ 0, 0, 1, 1 ],
		color: mode === "light" ? [ .86, .88, .82, 1 ] : [ .035, .055, .085, 1 ]
	};
	const rows = [ base ];
	if ( mode === "contrast" ) {
		for ( let y = 0; y < height; y += 80 ) {
			for ( let x = 0; x < width; x += 80 ) {
				rows.push( {
					...base,
					rect: [ x, y, 80, 80 ],
					color: (x + y) % 160 === 0 ? [ .8, .7, .22, 1 ] : [ .1, .38, .55, 1 ]
				} );
			}
		}
	}
	return rows;
}

const ui = createUi(
	assets,
	command => {
		if ( command.kind === "gameplay" && command.command.kind.startsWith( "stall-" ) ) {
			queue.push( command );
			record( "command queued", command );
		} else record( "unsupported integration", command );
	},
	scene => {
		sceneTextures = [ ...new Set( scene?.quads.map( quad => quad.texture ) ?? [] ) ];
		sceneAnchors = [
			...new Set(
				scene?.quads.flatMap( quad => quad.characterAnchor === undefined ? [] : [ quad.characterAnchor ] ) ?? []
			)
		];
		renderer.setUi(
			scene ?
				{
					...scene,
					quads: [
						...(stageFixture.active() || extendedRecipe?.anchor !== undefined ?
							[] :
							backdrop( scene.width, scene.height )),
						...scene.quads
					]
				} :
				null
		);
	},
	( name, texture ) => renderer.setUiTexture( name, texture ),
	location.origin,
	"https://fixture.invalid"
);

/*
================
event
================
*/
function event( value: UiEvent ) {
	record( "UI event", value );
	if ( stageFixture.event( value ) ) return;
	if ( frontendFixture.event( value ) ) return;
	if ( value.kind === "activate" && value.id === "world-load-retry" ) {
		record( "unsupported integration", "World reload is not mounted" );
	}
	ui.event( value );
}

const platform = createPlatform(
	canvas,
	document.querySelector<HTMLOutputElement>( "output" )!,
	() => record( "unsupported", "session close" ),
	() => {},
	() => {},
	event,
	ui.blocks,
	( x, y ) => {
		if ( !stageFixture.click( x, y ) ) record( "world click-through", { x, y } );
	},
	point => stageFixture.pointer( point )
);
const dialog = createBugReportDialog( {
	launch: openReport,
	record: toggleRecording,
	stopRecording: toggleRecording,
	deliver: async () => {
		record( "unsupported", "Report delivery is disabled" );
		return { ok: false, message: "UI gallery: report delivery is not available offline." };
	},
	still: async () => null,
	saved: async () =>
		id === "dom:report-archive" ?
			[ {
				id: "QA-SYNTHETIC",
				createdAt: "2026-01-01T12:00:00Z",
				description: "Synthetic archive entry; no real report stored",
				bytes: 2000000,
				delivered: false
			} ] :
			[],
	exportZip: async () => {
		record( "unsupported integration", "Archive export disabled; no report exists" );
		return null;
	},
	forget: async () => {
		record( "unsupported integration", "Synthetic archive has no persisted report to remove" );
	}
} );
dialog.showLauncher( true );
dialog.showRecording( "idle", 0, 30 );
window.sroDebug?.setDiagnostics( true );
platform.presentTelemetry( {
	fps: 60,
	pingMs: 25,
	frameMs: 16,
	p95FrameMs: 20,
	cpuMs: 5,
	p95CpuMs: 7,
	actors: 2,
	draws: 10,
	visibleGroups: 1,
	build: { lines: [ "Synthetic UI fixture", "No game connection" ], detail: "Fixed presentation telemetry" }
} );

/*
================
openReport
================
*/
function openReport() {
	dialog.open( {
		text: "Local UI review only. No report will be sent.",
		replay: null,
		maxBytes: 1000000,
		replayState: "unsupported",
		replayError: "Capture and delivery are not connected in this fixture."
	} );
	record( "presentation", "Production report dialog opened; transport unavailable" );
}

/*
================
toggleRecording
================
*/
function toggleRecording() {
	recording = !recording;
	dialog.showRecording( recording ? "recording" : "idle", recording ? 12 : 0, 30 );
	record( "presentation only", `Recording display ${recording ? "on" : "off"}; no recorder running` );
}

/*
================
activate
================
*/
function activate( control: string ) {
	event( { kind: "activate", id: control } );
}

/*
================
activateControl

Checkpoint shortcuts honor the published control's admission just like the
DOM button. A shortcut cannot bypass a permission-disabled control.
================
*/
function activateControl( id: string ) {
	const control = semantics?.controls.find( row => row.id === id );
	if ( !control || control.disabled ) return record( "unavailable control", id );
	activate( id );
}

/*
================
applyRequests

All queued Stall commands reduce before one immutable publication, preserving
the real namingSequence path across Cancel -> Stall. Unsupported commands log.
================
*/
function applyRequests() {
	let next = state.gameplay.stall;
	for ( const command of queue.splice( 0 ) ) {
		if ( command.kind !== "gameplay" || !command.command.kind.startsWith( "stall-" ) ) {
			record( "unsupported integration", command );
			continue;
		}
		try {
			const result = stallRequest( next, command.command as StallCommand );
			next = result.state;
			for ( const frame of result.frames ) {
				record( "production request frame (not sent)", {
					opcode: frame.opcode,
					bytes: Array.from( frame.payload )
				} );
				// Greeting accompanying creation is intentionally not a separate fake reply.
				if ( frame.opcode !== 0x71a8 ) pending.push( frame.opcode );
				else record( "unsupported reply", "Stall edit acknowledgement is not supplied by this fixture" );
			}
		} catch ( failure ) {
			record( "production request rejected", String( failure ) );
		}
	}
	state.gameplay = { ...state.gameplay, stall: next };
	record( "publication", { phase: next.phase, namingSequence: next.namingSequence } );
}

/*
================
reply

Only response payloads with an independently exercised production recipe are
offered. Purchase success would require inventory ownership, so stays absent.
================
*/
function reply( accepted: boolean ) {
	const opcode = pending[0];
	if ( opcode === undefined ) return record( "no pending response", "Apply a queued request first" );
	if ( ![ 0x7049, 0x742c, 0x76e7, 0x73f9 ].includes( opcode ) || accepted && opcode === 0x73f9 ) {
		return record( "unsupported response", { opcode, accepted } );
	}
	pending.shift();
	const frame = { opcode: opcode + 0x4000, payload: Uint8Array.from( accepted ? [ 1 ] : [ 2, 0x3b ] ) };
	const result = stallFrame( state.gameplay.stall, frame, { localGid: 1, refs: new Map() } );
	if ( result ) {
		state.gameplay = { ...state.gameplay, stall: result.state };
		record( "production reply", { opcode: frame.opcode, accepted, notice: result.notice ?? null } );
		if ( result.notice ) {
			record(
				"coverage limit",
				"Refusal is recorded; the full inventory-owner notification pipeline is not mounted"
			);
		}
	}
}

/*
================
action

Checkpoint buttons either invoke production events or visibly labeled seed
changes. They do not reach into a production owner's private closure.
================
*/
function action( name: string ) {
	if ( !ready ) return;
	record( "checkpoint", name );
	const game = state.gameplay;
	switch ( name ) {
		case "open-extended":
			void openEntry();
			break;
		case "open-entry":
			if ( id.startsWith( "surface:" ) || id.startsWith( "service:" ) ) void openEntry();
			break;
		case "escape":
			event( { kind: "key", code: "Escape" } );
			break;
		case "advance":
			clock += 1000;
			break;
		case "play":
			playing = !playing;
			break;
		case "guild":
			activate( "open-window:Guild" );
			break;
		case "inventory":
			activate( "open-window:Inventory" );
			break;
		case "relations":
			activateControl( "guild-tab:1" );
			break;
		case "authority":
			activateControl( "guild-dialog:authority" );
			break;
		case "notice":
			activateControl( "guild-notice-read" );
			break;
		case "notice-edit":
			activateControl( "guild-dialog:notice" );
			break;
		case "transfer":
			activateControl( "npc-guild:master-leave" );
			break;
		case "guild-toggle":
			game.social = game.social.guild ? emptySocial( "Reviewer" ) : guildSnapshot();
			break;
		case "remove-member":
			if ( game.social.guild ) {
				game.social = {
					...game.social,
					guild: {
						...game.social.guild,
						members: game.social.guild.members.filter( member => member.id !== 102 )
					}
				};
			}
			break;
		case "permission":
			if ( game.social.guild ) {
				game.social = {
					...game.social,
					guild: {
						...game.social.guild,
						members: game.social.guild.members.map( member =>
							member.name === "Reviewer" ?
								{
									...member,
									grade: member.grade === 0 ? 1 : 0,
									permissions: member.grade === 0 ? 0 : 31
								} :
								member
						)
					}
				};
			}
			break;
		case "title":
			activateControl( "stall-change-title" );
			break;
		case "offers":
			game.stall = { ...game.stall, offers: game.stall.offers.length ? [] : stallOffers() };
			break;
		case "trading":
			game.stall = { ...game.stall, open: !game.stall.open };
			break;
		case "apply":
			applyRequests();
			break;
		case "accept":
			reply( true );
			break;
		case "refuse":
			reply( false );
			break;
		case "reentry":
			activate( "stall-prompt-cancel" );
			event( { kind: "double-activate", id: "action:1009" } );
			break;
		case "report":
			openReport();
			break;
		case "record":
			toggleRecording();
			break;
		case "tools":
			toolsVisible = !toolsVisible;
			recording = false;
			dialog.showRecording( "idle", 0, 30 );
			dialog.showLauncher( toolsVisible );
			break;
		case "target":
			game.target = game.target ? 0 : 8;
			break;
		case "pets": {
			petMode = (petMode + 1) % 4;
			const pet = seedJourney( "hud-tools" ).gameplay.cosRecords[0]!;
			game.cosRecords = petMode === 3 ?
				[] :
				petMode === 1 ?
				[ { ...pet, refObjId: 6117, band: 3 } ] :
				petMode === 2 ?
				[ pet, { ...pet, gid: 10, refObjId: 6117, band: 3 } ] :
				[ pet ];
			break;
		}
	}
	if ( state.gameplay ) state.gameplay = { ...state.gameplay };
}

/*
================
snapshot
================
*/
function snapshot() {
	const selectedNode = extendedRecipe?.selector ?
		document.querySelector<HTMLElement>( extendedRecipe.selector ) :
		null;
	const visibleNode = !!selectedNode && selectedNode.getBoundingClientRect().width > 0 &&
		selectedNode.getBoundingClientRect().height > 0 && getComputedStyle( selectedNode ).visibility !== "hidden";
	const result = {
		id,
		ready,
		entry: entryBaseline ?
			{
				requested: id.slice( id.indexOf( ":" ) + 1 ),
				evidence: entryEvidence( id.slice( id.indexOf( ":" ) + 1 ), semantics?.controls ?? [] ),
				frames: entryFrames,
				busy: entryBusy,
				addedControls:
					semantics?.controls.filter( control => !entryBaseline!.has( control.id ) ).map( control =>
						control.id
					) ?? []
			} :
			null,
		clock,
		playing,
		coverage: extendedRecipe ?
			{
				source: extendedRecipe.source,
				unsupported: extendedRecipe.unsupported,
				requiredControls: extendedRecipe.controls ?? (extendedRecipe.control ? [ extendedRecipe.control ] : []),
				selectedControls: extendedRecipe.selectedControls ?? [],
				admitted: !error && !stageFixture.error() && !renderer.error() && !extendedRecipe.unsupported &&
					(extendedRecipe.anchor === undefined || (stageFixture.readiness()?.stats.draws ?? 0) > 0) &&
					(extendedRecipe.selectedControls ?? []).every( id =>
						semantics?.controls.some( control => control.id === id && control.selected )
					) && !!(extendedRecipe.controls ?
						extendedRecipe.controls.every( id =>
							semantics?.controls.some( control => control.id === id )
						) :
						extendedRecipe.selector ?
						visibleNode :
						extendedRecipe.texture ?
						sceneTextures.some( texture => texture.includes( extendedRecipe.texture! ) ) :
						extendedRecipe.control ?
						semantics?.controls.some( control => control.id === extendedRecipe.control ) :
						extendedRecipe.anchor !== undefined ?
						sceneAnchors.includes( extendedRecipe.anchor ) :
						false)
			} :
			null,
		error: error ?? stageFixture.error() ?? renderer.error(),
		stage: stageFixture.active() ? renderer.worldStats() : null,
		characters: stageFixture.readiness(),
		sceneTextures,
		sceneAnchors,
		stats: ui.stats(),
		viewport: { ...platform.readUiViewport(), dpr: devicePixelRatio },
		controls: semantics?.controls.map( control => ({
			id: control.id,
			label: control.label,
			kind: control.kind,
			rect: control.rect,
			disabled: control.disabled,
			selected: control.selected
		}) ) ?? [],
		state: {
			frontend: state.frontend,
			session: state.session,
			stall: state.gameplay?.stall,
			partySelf: state.gameplay?.social?.self,
			guildSelf: state.gameplay?.social?.guild?.members.find( row => row.name === "Reviewer" ),
			target: state.gameplay?.target,
			companions: state.gameplay?.cosRecords?.length,
			queued: queue.length,
			pending
		},
		trace
	};
	// Wire-sized money/serials remain exact decimal strings in portable evidence.
	return JSON.parse(
		JSON.stringify( result, ( _key, value ) => typeof value === "bigint" ? value.toString() : value )
	);
}

/*
================
draw
================
*/
async function draw( time: number ) {
	if ( stopped ) return;
	try {
		if ( playing && !entryBusy ) clock += 1000 / 60;
		frontendFixture.step( clock );
		stageFixture.mallOutfit( ui.mallPreview(), ui.skinPreview() );
		stageFixture.step( clock );
		const mallPreview = stageFixture.mallPreviewState();
		if ( mallPreview ) ui.mallPreviewState( mallPreview );
		const next = ui.step( { ...state, ...platform.readUiViewport() } as unknown as UiView, clock );
		if ( next ) {
			semantics = next;
			platform.presentUi( next );
		}
		if ( loadingPreview && semantics ) {
			platform.presentUi( {
				...semantics,
				loading: true,
				loadingVisible: true,
				loadingProgress: .45,
				loadingError: loadingPreview.error ? "Synthetic fixture resource failure" : undefined,
				loadingStatus: "Loading local fixture assets"
			} );
			platform.presentLoading( {
				visible: true,
				title: "Synthetic transfer progress",
				progress: {
					bytesRead: 12000000,
					bytesReceived: 10000000,
					bytesPerSecond: 2000000,
					filesReady: 42,
					filesActive: 3,
					cacheHits: 12,
					currentFile: "fixture/scene.bin"
				}
			} );
		}
		// The production renderer owns an asynchronous second particle submission.
		// Do not advance presentation or schedule another frame before it finishes.
		await renderer.frame( platform.readViewport(), clock / 1000 );
		if ( stopped ) return;
		if ( entryBaseline ) entryFrames++;
		if ( entryStep && ++entryStep.frames >= 3 && ui.stats().pending === 0 && !ui.stats().hudSettling ) {
			const resolve = entryStep.resolve;
			entryStep = null;
			resolve();
		}
		if ( !ready && ui.stats().pending === 0 && !ui.stats().hudSettling && semantics?.controls.length ) {
			if ( ++stable === 3 ) activate( "mall-notice-close" );
			if ( stable === 5 ) {
				ready = true;
				if ( id.startsWith( "guild-" ) ) activate( "open-window:Guild" );
				if ( id === "hud-modal" ) activate( "open-window:Inventory" );
				if ( id.startsWith( "surface:" ) || id.startsWith( "service:" ) ) void openEntry();
				if ( extendedRecipe ) void openEntry();
				record( "ready", "Production assets settled; synthetic seed admitted" );
			}
		}
	} catch ( failure ) {
		error = String( failure );
	}
	if ( time - lastReport > 500 ) {
		lastReport = time;
		parent.postMessage( { type: "gallery-status", report: snapshot() }, location.origin );
	}
	if ( !stopped ) animation = requestAnimationFrame( draw );
}

/*
================
dispose
================
*/
function dispose() {
	if ( stopped ) return;
	stopped = true;
	cancelAnimationFrame( animation );
	dialog.dispose();
	frontendFixture.dispose();
	stageFixture.dispose();
	platform.dispose();
	ui.dispose();
	renderer.dispose();
	assets.dispose();
}

window.addEventListener( "message", message => {
	if ( message.origin !== location.origin || message.source !== parent ) return;
	if ( message.data?.type === "gallery-action" ) action( String( message.data.action ) );
} );
window.addEventListener( "pagehide", dispose, { once: true } );
// Explicit read/action seam for the owned browser probe; never patches source.
Object.assign( window, { __uiGallery: { snapshot, action, event } } );
animation = requestAnimationFrame( draw );
