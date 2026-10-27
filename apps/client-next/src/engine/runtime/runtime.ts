/*
===========================================================================

runtime.ts - the client runtime: owners, frame loop and session flow

Creates every runtime owner (assets, platform, simulation host, world,
characters, audio, UI, renderer), runs the frame loop in a fixed stage
order, and moves the session between title, loading and world. When the
world first becomes ready it starts the background install of the combat
presentation set (assets/worker/install.ts).

===========================================================================
*/

import { nativeHeadingYaw } from "@/engine/foundation/math/angles";
import { createRuntimeErrors } from "./runtime-errors";
import { createPresentationRandom } from "./random/random";
import { worldCursor } from "@/engine/foundation/ui/world-cursor";
import { experimentalGameplay, experimentalVideo } from "@/engine/foundation/ui/experimental-options";
import { sampleWorldClock } from "@/engine/foundation/gameplay/world-clock";
import { createWorldDoubleClick } from "@/engine/foundation/gameplay/world-double-click";
import type { RawInput, WorldClickInput } from "@/engine/contracts/input";
import { createNavigationStream } from "./navigation/navigation";
import { createFrontend } from "./frontend/frontend";
import { createUi } from "./ui/ui";
import { createAudio } from "./audio/audio";
import { createCharacterPresentation } from "./characters/characters";
import { creationProtectorFloor } from "@/engine/foundation/ui/character-create";
import { createWorldStream } from "./world/world";
import { createPresentation } from "./presentation/presentation";
import type { SessionState } from "@/engine/contracts/session";
import { createAssets } from "./assets/assets";
import { createReleaseWatch } from "./release/release-watch";
import { createInput } from "./input/input";
import { createPlatform } from "./platform/platform";
import { createBuildInfo } from "./build-info/build-info";
import { createBugReport } from "./bug-report/bug-report";
import type { BugReportField } from "@/engine/contracts/bug-report";
import { frameProbe } from "./frame-probes";
import { createFrameWork } from "./frame-work";
import { createFramePacing } from "./frame-pacing";
import { createRenderer } from "./renderer/renderer";
import { createViewer } from "./viewer/viewer";
import { createSimulationHost } from "./simulation/host";
import { ARROW_TURN_WORDS_PER_MS } from "@/engine/foundation/gameplay/direction-movement";
import type { FreeCameraKeys } from "@/engine/foundation/rendering/free-camera";
import { createLocalMover, type LocalMoverFrame, type LocalMoverWorld } from "./movement/local-mover";
import { poseDistance } from "@/engine/foundation/gameplay/native-movement";
import { approachGoal } from "@/engine/foundation/gameplay/approach-goal";
import type { Pose } from "@/engine/contracts/gameplay";
import type { EntityState } from "@/engine/contracts/world";
import type { RuntimeControl } from "@/engine/contracts/runtime";
import { worldAudioPosition } from "@/engine/foundation/world/world-space";

// The build's ordered background install list (buildBackgroundInstallAsset.mjs).
const BACKGROUND_INSTALL_LIST = "/assets/delivery/background-install.json";

// The live page a release check compares against (release-watch.ts).
const RELEASE_PAGE = "/play";

// Frame-timing window for the FPS chip. Two seconds at 60 Hz keeps the readout
// responsive without letting one stall dominate the published percentile.
const TELEMETRY_SAMPLES = 120, TELEMETRY_INTERVAL_MS = 500;
// A hidden tab gets no animation frames. Native keeps its game loop running
// while minimized; this port keeps consuming the world journal (which fails
// the session when unacknowledged) at this pace, driven by worker messages.
const HIDDEN_FRAME_MS = 100;
// A visible window the compositor stopped drawing (occluded, off-screen)
// gets no animation frames either, without being reported hidden. A frame
// this long overdue after the last one finished is starved, not slow: even a
// throttled display callback arrives several times a second.
const STARVED_FRAME_MS = 250;
const MOVEMENT_FRAME_SPIKE_MS = 25;
/*
================
startRuntime
================
*/
export function startRuntime(
	canvas: HTMLCanvasElement,
	status: HTMLOutputElement,
	presentationSeed = Math.trunc( performance.now() ) >>> 0,
	diagnostics: import("@/engine/contracts/runtime").RuntimeDiagnostics = {}
): RuntimeControl {
	let disposed = false, raf = 0, frameToken = 0, lastReport = 0;
	let loadingVisible = false, loadingTitle = "Preparing your journey";
	let pendingWorldReset = false, effectDetail = 2, normalFortressClothes = false;
	/*
	================
	Cleanup
	================
	*/
	type Cleanup = { dispose(): void; previous: Cleanup | null; };
	let cleanups: Cleanup | null = null;
	/*
	================
	own
	================
	*/
	function own<T extends { dispose(): void; }>( owner: T ): T {
		cleanups = { dispose: () => owner.dispose(), previous: cleanups };
		return owner;
	}
	/*
	================
	dispose
	================
	*/
	function dispose(): void {
		if ( disposed ) return;
		disposed = true;
		const errors: unknown[] = [];
		try {
			cancelAnimationFrame( raf );
		} catch ( error ) {
			errors.push( error );
		}
		while ( cleanups ) {
			const cleanup: Cleanup = cleanups;
			cleanups = cleanup.previous;
			try {
				cleanup.dispose();
			} catch ( error ) {
				errors.push( error );
			}
		}
		if ( errors.length ) throw new AggregateError( errors, "Runtime cleanup failed" );
	}
	try {
		const random = createPresentationRandom( presentationSeed );
		const runtimeErrors = createRuntimeErrors( message => {
			console.error( "[SRO runtime] " + message );
			bugReport.note( message );
			ui.runtimeError( message );
		} );
		const presentation = own( createPresentation() );
		let sessionState: SessionState | null = null;
		const assets = own( createAssets() );
		const audio = own(
			createAudio( assets, new URL( "/", import.meta.url ).href, random, Math.trunc( performance.now() ) >>> 0 )
		);
		const input = createInput( event => {
			arrowInput( event );
			freeCameraKey( event );
		} );
		const frameWork = createFrameWork();
		const framePacing = createFramePacing();
		const simulation = own( createSimulationHost() );
		// Port-only (#1440): the local player's ground-click walk stepped on the
		// main thread, behind the Experimental "Instant own movement" row.
		const localMover = createLocalMover();
		let mainThreadOwnWalk = false;
		// Simulation time zero on the frame clock, from the latest snapshot:
		// the mover places a worker publication's pose on the frame clock with
		// it (#1441b).
		let simulationOriginMs: number | undefined;
		const renderer = own(
			createRenderer( canvas, random, audio.enqueue, diagnostics )
		);
		// Port-only: the asset owner learns the device's texture features once,
		// to serve re-encoded textures where BC is missing (texture-variants.ts).
		void renderer.textureFeatures().then( features => assets.textureFeatures( features ) );
		const frontend = own(
			createFrontend(
				assets,
				renderer,
				new URL( "/", import.meta.url ).href,
				command => simulation.session( command ),
				() => audio.uiSound( "message" )
			)
		);
		/*
		================
		sessionCommand
		================
		*/
		function sessionCommand( command: import("@/engine/contracts/session").SessionCommand ) {
			if ( frontend && command.kind === "enter-world" ) {
				if ( frontend.start() ) simulation.session( command );
				return;
			}
			if ( command.kind === "gameplay" && command.command.kind === "skill" && worldPointer ) {
				const query = renderer.pickGround( worldPointer[0], worldPointer[1] );
				if ( query ) command = { ...command, command: { ...command.command, query } };
			}
			// 6FCD50 and 6A2350 case 2 read the Alt state when the press executes.
			if (
				command.kind === "gameplay" &&
				(command.command.kind === "skill" || command.command.kind === "cos-pet-attack") && input.altHeld()
			) {
				command = { ...command, command: { ...command.command, alt: true } };
			}
			const seq = simulation.session( command );
			// A move with a known destination (the GM /Pos walk; map
			// auto-move sends the same kind) goes through the main-thread
			// mover like a ground click (#1571): from the drawn pose,
			// under the worker's own ground-walk gate. A foreign-region
			// goal keeps the presenter: the mover's navigation view covers
			// one region, and the worker walks those exactly as today.
			if (
				seq !== undefined && mainThreadOwnWalk &&
				command.kind === "gameplay" && command.command.kind === "move"
			) {
				const game = presentation.gameplay();
				const drawn = game ? characters.drawnPose( game.localGid ) : undefined;
				const world = moverWorld();
				if (
					drawn && world.walkPredictable && world.speed !== undefined &&
					command.command.destination.regionId === drawn.regionId
				) {
					localMover.predict( {
						seq,
						aim: { kind: "goal", destination: command.command.destination },
						speed: world.speed,
						drawn,
						now: performance.now()
					}, world );
				}
			}
		}
		/*
		================
		uiEvent
		================
		*/
		function uiEvent( event: import("@/engine/contracts/ui").UiEvent ) {
			if ( event.kind === "video-preferences" ) {
				framePacing.setFrameLimit( event.value.frameLimit );
				renderer.videoOptions( event.value );
				effectDetail = event.value.records[event.value.active][13] ?? 2;
				normalFortressClothes = event.value.records[event.value.active][15] === 1;
			}
			if ( event.kind === "input-preferences" ) {
				input.mouseMode( event.value.mouseMode );
				input.dropNameBinding( event.value.keys[10]! );
				input.blindBinding( event.value.keys[30]! );
			}
			if ( event.kind === "camera-preferences" ) input.sight( event.value );
			if ( event.kind === "experimental-preferences" ) {
				mainThreadOwnWalk = event.value.mainThreadOwnWalk === true;
				if ( !mainThreadOwnWalk ) localMover.reset();
				renderer.experimentalVideo( experimentalVideo( event.value ) );
				simulation.session( { kind: "experimental-gameplay", value: experimentalGameplay( event.value ) } );
			}
			if ( event.kind === "audio-preferences" ) audio.options( event.value );
			if ( event.kind === "chat-blocks" ) simulation.session( { kind: "chat-blocks", value: event.value } );
			if ( event.kind === "preferences" ) {
				simulation.session( { kind: "game-options", value: event.value } );
				characters.options( event.value );
			}
			if ( event.kind === "activate" && event.id === "world-load-retry" ) {
				world.retry();
				navigation.retry();
				return;
			}
			if ( frontend && event.kind === "activate" ) {
				if ( event.id === "frontend:reveal" ) {
					frontend.reveal();
					return;
				}
				if ( event.id === "frontend:create" ) {
					audio.uiClick();
					frontend.create();
					return;
				}
				if ( event.id === "frontend:leave" ) {
					audio.uiClick();
					frontend.leave();
					return;
				}
				if ( event.id === "frontend:race-europe" || event.id === "frontend:race-china" ) {
					audio.uiClick();
					chooseRace( event.id.endsWith( "europe" ) ? 0 : 1 );
					return;
				}
				if ( event.id.startsWith( "create:" ) ) {
					audio.uiClick();
					frontend.creationAction( event.id );
					return;
				}
				if ( event.id === "frontend:back" ) {
					audio.uiClick();
					frontend.back();
					return;
				}
				if ( event.id === "dock:back" ) {
					audio.uiClick();
					frontend.deselect();
					return;
				}
				if (
					event.id.startsWith( "dock:warning" ) || event.id === "dock:delete" || event.id === "dock:restore"
				) {
					audio.uiClick();
					frontend.dialogAction( event.id );
					return;
				}
				if ( event.id.startsWith( "character:" ) ) {
					frontend.select( event.id.slice( 10 ) );
					return;
				}
			}
			if ( event.kind === "key" && event.code === "Escape" ) frontend.dialogAction( "dock:warning-cancel" );
			if ( event.kind === "hover" && event.id?.startsWith( "create:" ) ) {
				frontend.creationAction( "create:explain", event.id.split( ":" )[1] );
			}
			if ( event.kind === "edit" && event.id.startsWith( "create:" ) ) {
				if ( !event.composing ) {
					if ( event.id === "create:name" ) frontend.creationAction( event.id, event.value );
					else frontend.creationAction( event.id + ":" + event.value );
				}
			}
			ui.event( event );
		}
		// Hosted deployments route /api to Agent. A build-time override supports a
		// separate same-site API origin; query overrides are restricted to development.
		const apiBase = new URL(
			(import.meta.env.DEV ? new URLSearchParams( location.search ).get( "apiBase" ) : null) ||
				import.meta.env.VITE_AGENT_API_BASE || "/api",
			location.origin
		).href.replace( /\/$/, "" );
		const buildInfo = own(
			createBuildInfo( apiBase, import.meta.env.SRO_CLIENT_REVISION, import.meta.env.SRO_CLIENT_SUBJECT )
		);
		const bugReport = own(
			createBugReport( {
				canvas,
				apiBase,
				gpu: () => renderer.adapterFacts(),
				network: () => simulation.netJournal(),
				context: (): readonly BugReportField[] => {
					const game = presentation.gameplay();
					const local = game?.localGid ? presentation.read( game.localGid ) : undefined;
					return [
						{ name: "Phase", value: sessionState?.phase ?? "starting" },
						...(sessionState?.incidentID ?
							[ { name: "Incident", value: sessionState.incidentID }, {
								name: "Incident delivery",
								value: sessionState.incidentDelivery ?? "not sent"
							} ] :
							[]),
						...(sessionState?.nativeServerName ?
							[ { name: "Shard", value: sessionState.nativeServerName } ] :
							[]),
						...(local ?
							[ {
								name: "Position",
								value: `region 0x${local.regionId.toString( 16 ).toUpperCase()} ` +
									`(${local.x.toFixed( 1 )}, ${local.y.toFixed( 1 )}, ${local.z.toFixed( 1 )})`
							} ] :
							[]),
						{ name: "Build", value: platform.runningEntry() ?? "development" }
					];
				},
				sound: () => audio.captureAudio(),
				state: () => {
					const gameplay = presentation.gameplay();
					return {
						session: sessionState,
						gameplay,
						target: gameplay?.target ? presentation.read( gameplay.target ) : undefined,
						entities: presentation.entities().length
					};
				}
			} )
		);
		const ui = own(
			createUi(
				assets,
				sessionCommand,
				renderer.setUi,
				renderer.setUiTexture,
				new URL( "/", import.meta.url ).href,
				apiBase,
				audio.uiClick,
				audio.uiSound,
				() => random.range( 1, 3 ),
				count => random.range( 0, count ),
				value => platform.saveGameOptions( value ),
				( value, commit ) => {
					audio.options( value );
					if ( commit ) platform.saveAudioOptions( value );
				},
				value => platform.saveSightMode( value ),
				value => platform.saveInputOptions( value ),
				value => platform.saveVideoOptions( value ),
				value => platform.saveChatBlocks( value ),
				value => platform.saveQuickslotOptions( value ),
				{
					bugReport,
					setRenderMode: ( mode, enabled ) => renderer.setWorldRenderMode( mode, enabled ),
					setWorldFieldOfView: degrees => world.setFieldOfView( degrees ),
					setCameraZoomRail: capped => input.zoomRail( capped ),
					setFreeCamera: free => {
						freeCameraOn = free;
						world.setFreeCamera( free );
					},
					setFreeCameraSpeed: speed => world.setFreeCameraSpeed( speed ),
					setCameraCollision: collision => world.setCameraCollision( collision ),
					setCameraWaypoints: keyframes => world.setCameraWaypoints( keyframes ),
					setCameraPlaying: playing => world.setCameraPlaying( playing ),
					setCursorVisible: visible => platform.setCursorVisible( visible ),
					setDamageNumbers: shown => characters.setDamageNumbers( shown ),
					saveCameraWaypoints: text => platform.saveCameraWaypoints( text ),
					playSoundByName: name => audio.playByName( name ),
					saveExperimental: value => platform.saveExperimentalOptions( value ),
					saveWindowPositions: value => platform.saveWindowPositions( value )
				}
			)
		);
		let lastDockPick = "none";
		// Deliberate deviation (see world-double-click.ts): a drifted second
		// press on the same monster still attacks. Do not remove.
		const worldDoubleClick = createWorldDoubleClick();
		/*
		================
		chooseRace

		Opens creation for a race with the login shard's protector floor
		(GameConfig +0x129, 72C780/7302B0).
		================
		*/
		function chooseRace( race: 0 | 1 ) {
			frontend.race( race, creationProtectorFloor( characters.uncensored( sessionState?.nativeServerName ) ) );
		}
		// The arrow keys held now (#1574): a press is sent once, a release only
		// for a key whose press was sent, and losing focus releases them all.
		const ARROW_KEYS: Readonly<Record<string, "up" | "left" | "right">> = {
			ArrowUp: "up",
			ArrowLeft: "left",
			ArrowRight: "right"
		};
		const arrowsHeld = new Set<"up" | "left" | "right">();
		// The interface's free-camera byte (+0x3CE) as the console last wrote
		// it, and the keys the free-fly arm reads (free-camera.ts, #1787).
		let freeCameraOn = false;
		const freeCameraHeld = new Set<string>();
		/*
		================
		freeCameraKey

		Tracks the held keys by code; a focus release lets every key go.
		================
		*/
		function freeCameraKey( event: RawInput ) {
			if ( event.kind === "release" ) {
				freeCameraHeld.clear();
				return;
			}
			if ( event.kind !== "key" ) return;
			if ( event.down ) freeCameraHeld.add( event.code );
			else freeCameraHeld.delete( event.code );
		}
		/*
		================
		freeCameraKeys

		The free-fly arm's keys for this frame (690146 reads them with
		CCursorManager_IsKeyDown), or null while a text field has focus:
		native returns before the keys when the chat edit holds focus.
		================
		*/
		function freeCameraKeys(): FreeCameraKeys | null {
			const focus = document.activeElement;
			if ( focus instanceof HTMLInputElement || focus instanceof HTMLTextAreaElement ) return null;
			const held = ( code: string ) => freeCameraHeld.has( code );
			return {
				forward: held( "KeyW" ),
				back: held( "KeyS" ),
				strafeRight: held( "KeyD" ),
				strafeLeft: held( "KeyA" ),
				turnLeft: held( "ArrowLeft" ),
				turnRight: held( "ArrowRight" ),
				pitchUp: held( "ArrowUp" ),
				pitchDown: held( "ArrowDown" ),
				rollPositive: held( "KeyQ" ),
				rollNegative: held( "KeyE" )
			};
		}
		/*
		================
		arrowInput

		Game_OnKeyDown 0x680A30 / Game_OnKeyUp 0x688290: Up walks the facing,
		Left and Right turn it; Down is not a movement key. A press typed into
		a text field is the field's. The worker owns the walk and the wire
		(arrow-keys.ts); the main-thread mover predicts the Up arrow's start
		and stop.
		================
		*/
		function arrowInput( event: RawInput ) {
			// 0x86D480: Left turns positive and wins over Right.
			const turnRate = () =>
				arrowsHeld.has( "left" ) ?
					ARROW_TURN_WORDS_PER_MS :
					arrowsHeld.has( "right" ) ?
					-ARROW_TURN_WORDS_PER_MS :
					0;
			const predicted = ( key: "up" | "left" | "right", down: boolean, seq: number | undefined ) => {
				const drawn = mainThreadOwnWalk && seq !== undefined ?
					characters.drawnPose( presentation.gameplay()?.localGid ?? 0 ) :
					undefined;
				if ( seq === undefined || !drawn ) return;
				if ( key === "up" ) localMover.arrow( seq, down, drawn, performance.now(), moverWorld() );
				else localMover.turn( seq, turnRate(), drawn, performance.now(), moverWorld() );
			};
			const release = ( key: "up" | "left" | "right" ) => {
				arrowsHeld.delete( key );
				predicted(
					key,
					false,
					simulation.session( { kind: "gameplay", command: { kind: "arrow-key", key, down: false } } )
				);
			};
			if ( event.kind === "release" ) {
				for ( const key of [ ...arrowsHeld ] ) release( key );
				return;
			}
			if ( event.kind !== "key" ) return;
			const key = ARROW_KEYS[event.code];
			if ( !key ) return;
			if ( !event.down ) {
				if ( arrowsHeld.has( key ) ) release( key );
				return;
			}
			// Game_OnKeyDown starts no arrow walk or turn while the free-camera
			// byte stands: the arrows fly the camera instead. A walk already
			// held still ends on its release.
			if ( freeCameraOn ) return;
			const game = presentation.gameplay();
			if ( arrowsHeld.has( key ) || frontend.snapshot().phase !== "world" || sessionState?.phase !== "world" ) {
				return;
			}
			if ( !game?.localGid || !game.pose ) return;
			const focus = document.activeElement;
			if ( focus instanceof HTMLInputElement || focus instanceof HTMLTextAreaElement ) return;
			arrowsHeld.add( key );
			predicted(
				key,
				true,
				simulation.session( { kind: "gameplay", command: { kind: "arrow-key", key, down: true } } )
			);
		}
		/*
		================
		worldClick
		================
		*/
		function worldClick( x: number, y: number, click: WorldClickInput = {} ) {
			const doubleClick = click.double ?? false, shift = click.shift ?? false;
			if ( doubleClick && frontend.snapshot().phase !== "world" ) return;
			if ( frontend.isRace() ) {
				const race = renderer.pickFrontendRace( x, y );
				if ( race !== null ) chooseRace( race );
				return;
			}
			if ( frontend.isDock() ) {
				const gid = renderer.pickFrontendCharacter(
						x,
						y,
						(sessionState?.characters ?? []).slice( 0, 4 ).map( row => row.id )
					),
					row = sessionState?.characters?.find( row => row.id === gid );
				lastDockPick = `${x},${y}: ${gid}`;
				if ( row ) frontend.select( row.name );
				return;
			}
			const game = presentation.gameplay();
			if ( frontend.snapshot().phase !== "world" || sessionState?.phase !== "world" || !game?.localGid ) return;
			const local = presentation.read( game.localGid );
			// 6989A5 -> 6813E0: a dead player's own corpse remains clickable;
			// selecting it reopens confirmation type 3 without a gameplay packet.
			if ( local?.appearanceState?.[0] === 2 ) {
				const corpse = renderer.pickEntity( x, y, 0, input.blindHeld(), true );
				if ( corpse === game.localGid ) ui.event( { kind: "world-select", gid: corpse } );
				return;
			}
			// 698740: SHIFT sets 692680's filter flag, which also takes dead
			// players (a corpse to select, or to whisper).
			const gid = renderer.pickEntity( x, y, game.localGid, input.blindHeld(), shift ),
				entity = gid === null ? null : presentation.read( gid ),
				attackable = entity?.kind === "monster" && entity.appearanceState?.[0] !== 2,
				now = performance.now();
			// A browser dblclick after a promoted pair would attack a second time.
			const attack = doubleClick ?
				worldDoubleClick.double( attackable ? entity!.gid : null, now ) :
				worldDoubleClick.press( attackable ? entity!.gid : null, now );
			if ( entity ) {
				// 698924 retains VK_SHIFT; 698BFD gates whisper prefill after selection.
				if ( shift && !doubleClick && entity.kind === "player" ) {
					ui.event( { kind: "whisper-target", gid: entity.gid } );
				}
				// 698740 -> 693E50: a press on another player may attack it; the
				// worker decides against the selection this press is about to
				// replace, so this precedes the select (player-attack.ts).
				if ( entity.kind === "player" && !doubleClick ) {
					simulation.session( {
						kind: "gameplay",
						command: { kind: "player-interact", gid: entity.gid, alt: click.alt ?? false }
					} );
				}
				// Native 67AA60 reactivates the shared decal even for the same GID.
				// The targeting owner deduplicates the wire request, not the click.
				if ( entity.kind !== "ground-item" && !game.targetPending ) {
					const seq = simulation.session( {
						kind: "gameplay",
						command: { kind: "select", gid: entity.gid }
					} );
					const drawn = mainThreadOwnWalk && seq !== undefined ?
						characters.drawnPose( game.localGid ) :
						undefined;
					if ( seq !== undefined && drawn ) {
						predictApproachWalk( seq, entity, drawn, now, game.walkPredictable === true );
					}
				}
				// 67CCA0 -> 6935F0: a double click on another player's pet may
				// attack it; the worker applies the pet's admission (player-attack.ts).
				if ( doubleClick && entity.kind === "cos" && entity.appearanceState?.[0] !== 2 ) {
					simulation.session( {
						kind: "gameplay",
						command: { kind: "cos-interact", gid: entity.gid, alt: click.alt ?? false }
					} );
				}
				if ( doubleClick || attack ) {
					if ( attackable && attack ) {
						simulation.session( {
							kind: "gameplay",
							command: { kind: local?.mountedOn ? "cos-attack" : "attack", gid: entity.gid }
						} );
					}
				} else if ( entity.kind === "ground-item" ) {
					const seq = simulation.session( {
						kind: "gameplay",
						command: { kind: "pickup", gid: entity.gid }
					} );
					const drawn = mainThreadOwnWalk && seq !== undefined ?
						characters.drawnPose( game.localGid ) :
						undefined;
					if ( seq !== undefined && drawn ) {
						predictApproachWalk( seq, entity, drawn, now, game.pickupPredictable === true );
					}
				}
				return;
			}
			if ( doubleClick ) return;
			const query = renderer.pickGround( x, y );
			if ( query && game.pose ) {
				const seq = simulation.session( { kind: "gameplay", command: { kind: "ground-move", query } } );
				const drawn = mainThreadOwnWalk && seq !== undefined ?
					characters.drawnPose( game.localGid ) :
					undefined;
				if ( seq !== undefined && drawn ) localMover.click( { seq, query, drawn, now }, moverWorld() );
			}
		}
		let worldPointer: readonly [number, number] | null = null, hoveredEntity: number | null = null;
		let readySent = false;
		// The display stopped asking for frames while the document stays visible.
		let starved = false;
		const world = own( createWorldStream( assets, renderer, new URL( "/", import.meta.url ).href, random ) );
		const navigation = own(
			createNavigationStream(
				assets,
				command => simulation.session( { kind: "gameplay", command } ),
				new URL( "/", import.meta.url ).href
			)
		);
		/*
		================
		moverWorld

		What the main-thread mover reads: the worker's latest publication and
		the main thread's navigation view (#1439).
		================
		*/
		function moverWorld(): LocalMoverWorld {
			const game = presentation.gameplay();
			// The worker's own cast displacement reaches the mover as a fixed
			// aim on the frame clock (#1569): the publication's start is on the
			// simulation clock, so place it by the same origin as poseAtMs.
			const displacement = game?.movementDisplacement;
			const aim: LocalMoverWorld["aim"] = displacement && simulationOriginMs !== undefined ?
				{
					kind: "fixed",
					segment: {
						from: displacement.from,
						to: displacement.to,
						start: displacement.startedAtMs + simulationOriginMs,
						duration: displacement.durationMs,
						fixedTiming: true
					}
				} :
				game?.movementAim;
			return {
				pose: game?.pose ?? null,
				poseAtMs: game?.poseAtMs,
				revision: game?.movementRevision ?? 0,
				moving: game?.moving ?? false,
				aim,
				speedFactor: game?.movementSpeedFactor ?? 1,
				turnRate: game?.movementTurnRate ?? 0,
				walkPredictable: game?.walkPredictable === true,
				walkHeld: game?.walkHeld === true,
				holding: game?.moveHeld === true,
				speed: game?.movementSpeed,
				inputSeq: game?.movementInputSeq ?? 0,
				navigationRequestId: game?.navigationRequestId,
				navigationOwner: game?.navigationOwner,
				originMs: simulationOriginMs,
				view: navigation.view()
			};
		}
		/*
	================
	moverFrame

	The mover's frame, starting the prediction on the worker's own cast
	displacement when one is published: the cast's segment is known only
	once the worker walks it, so the mover adopts it from the publication
	and steps it on its own clock. The saved copy carries the
	publication's own sequence, so it drops at once; rebuilds take the
	segment from the publication again. A live segment only: one the lag
	already finished stays with the presenter (#1569).
	================
	*/
		function moverFrame( now: number ): LocalMoverFrame {
			const world = moverWorld();
			if (
				!localMover.active() && world.aim?.kind === "fixed" &&
				now < world.aim.segment.start + world.aim.segment.duration
			) {
				const drawn = characters.drawnPose( presentation.gameplay()?.localGid ?? 0 ),
					segment = world.aim.segment;
				if ( drawn ) {
					localMover.predict( {
						seq: world.inputSeq,
						aim: world.aim,
						speed: poseDistance( segment.from, segment.to ) / segment.duration * 1000,
						drawn,
						now
					}, world );
				}
			}
			return localMover.frame( now, world );
		}
		/*
		================
		predictApproachWalk

		An entity click the worker answers with a walk first (#1567): a far
		select approaches its NPC, gate or stall keeper, a far pickup walks
		onto the item. Predict that leg from this frame, the way a ground
		click predicts (localMover.click): approachGoal names the worker's
		own destination from its latest pose. Each arm asks the worker's
		own gate for its walk - a select the ground click's
		(walkPredictable), a pickup the run-up's (pickupPredictable: a
		live cast incl. a held wall or chain root, or any mount, closes it
		where the ground gate may not) - so a state the worker would not
		walk in at once predicts nothing and keeps the presenter, as
		before. Arrival hands back through the mover's own settle: the
		worker's interaction owns what happens there.
		================
		*/
		function predictApproachWalk(
			seq: number,
			entity: EntityState,
			drawn: Pose,
			now: number,
			gateOpen: boolean
		): void {
			const world = moverWorld();
			if ( !gateOpen || !world.pose || !world.speed ) return;
			const destination = approachGoal( world.pose, entity );
			if ( !destination ) return;
			localMover.predict(
				{ seq, aim: { kind: "goal", destination }, speed: world.speed, drawn, now },
				world
			);
		}
		const characters = own(
			createCharacterPresentation(
				assets,
				renderer,
				new URL( "/", import.meta.url ).href,
				audio.enqueue,
				random,
				world.soundSurface,
				presentation,
				audio.effectsEnabled
			)
		);
		// Platform installation publishes saved preferences synchronously. Install
		// it after every recipient exists, before accepting browser input.
		const platform = own(
			createPlatform(
				canvas,
				status,
				dispose,
				input.accept,
				audio.unlock,
				uiEvent,
				ui.blocks,
				worldClick,
				point => {
					worldPointer = point;
					frontend.pointer( point );
				}
			)
		);
		platform.setMovementDump( bugReport.dumpMovement );
		platform.setAssetDump( () => ({ assets: assets.snapshot?.(), audio: audio.snapshot() }) );
		// Offers a refresh once a newer client release is live (release-watch.ts).
		const releaseWatch = own(
			createReleaseWatch( assets, new URL( RELEASE_PAGE, location.origin ).href, platform.runningEntry() )
		);
		let releasePhase: string | undefined;
		let simulationTimeMs = 0;
		let workerDebtMs = 0;
		const frameHistory: number[] = [], cpuHistory: number[] = [];
		let lastFrameAt = 0, lastTelemetry = 0;
		const stageTotals: Record<string, number> = {};
		let stageAt = 0, stageFrames = 0;
		/*
		================
		markStage
		================
		*/
		function markStage( name: string ) {
			frameProbe()?.mark( name );
			if ( !diagnostics.stages ) return;
			const at = performance.now();
			stageTotals[name] = (stageTotals[name] ?? 0) + at - stageAt;
			stageAt = at;
		}
		/*
		================
		sample
		================
		*/
		function sample( history: number[], value: number ): void {
			history.push( value );
			if ( history.length > TELEMETRY_SAMPLES ) history.shift();
		}
		/*
		================
		average
		================
		*/
		function average( history: readonly number[] ): number {
			return history.reduce( ( total, value ) => total + value, 0 ) / history.length;
		}
		/*
		================
		percentile
		================
		*/
		function percentile( history: readonly number[], fraction: number ): number {
			const sorted = [ ...history ].sort( ( first, second ) => first - second );
			return sorted[Math.min( sorted.length - 1, Math.floor( sorted.length * fraction ) )] ?? 0;
		}
		let latestSequence = 0, acceptedInput = 0, frameId = 0;
		/*
		================
		displayFrame

		Deliver input on each display opportunity, then admit presentation work.
		A maintenance frame (hiddenFrame's, run by a worker delivery instead of
		RAF) is paced and budgeted as hidden whatever the document reports: no
		display asked for it.
		================
		*/
		function displayFrame( now: number, maintenance = false ): void {
			if ( disposed ) {
				return;
			}
			if ( !maintenance ) starved = false;
			const visible = !maintenance && globalThis.document?.visibilityState !== "hidden";
			try {
				// Input is delivered even when this display refresh needs no new draw.
				const commands = input.drain();
				if ( commands ) simulation.sendInput( commands );
				if ( !framePacing.admit( now, visible ) ) {
					raf = requestAnimationFrame( displayFrame );
					return;
				}
			} catch ( error ) {
				platform.report( `Runtime failed: Input: ${String( error )}`, error );
				dispose();
				return;
			}
			void frame( now, visible );
		}
		/*
		================
		frame
		================
		*/
		async function frame( now: number, visible: boolean ): Promise<void> {
			if ( disposed ) return;
			// Whichever of RAF and a hidden delivery ran this frame, retire the other.
			frameToken++;
			const cpuStart = performance.now();
			frameWork.begin( now, visible );
			renderer.setFrameWork?.( frameWork );
			characters.frameWork( frameWork );
			let waitMs = 0;
			frameId++;
			stageAt = cpuStart;
			frameProbe()?.begin( frameId );
			if ( diagnostics.stages ) stageFrames++;
			try {
				// Worker death is terminal for this runtime. Reload creates fresh owners;
				// silently retrying a terminated worker cannot restore its resource state.
				const assetHealth = assets.health();
				if ( assetHealth.phase === "failed" ) {
					throw new Error( `Assets failed: ${assetHealth.error}. Reload to restart.` );
				}
				const inputError = input.error();
				if ( inputError ) {
					platform.report( `Runtime failed: Input: ${inputError}` );
					dispose();
					return;
				}
				const state = simulation.pollSession();
				if ( state ) sessionState = state;
				const batch = simulation.pollWorld();
				if ( batch ) {
					try {
						if ( presentation.apply( batch ) ) {
							// An attempted connection publishes a world clear before admission.
							// Keep the dock's shared renderer resources until entry is accepted.
							if ( [ "world", "loading-world" ].includes( frontend.snapshot().phase ) ) {
								world.reset();
								navigation.reset();
								characters.reset();
								localMover.reset();
								audio.reset();
								readySent = false;
							} else pendingWorldReset = true;
						}
						characters.receiveLifecycle( batch.events );
						for ( const event of batch.events ) {
							if ( event.kind === "movement-diagnostic" ) {
								bugReport.movement( {
									...event,
									worldSequence: batch.sequence,
									localGid: presentation.gameplay()?.localGid
								} );
							} else if ( event.kind === "reset" ) {
								bugReport.movement( {
									event: "world-reset",
									epoch: event.epoch,
									worldSequence: batch.sequence
								} );
							}
						}
						simulation.ackWorld( batch.sequence );
					} catch ( error ) {
						platform.report( `Runtime failed: World publication: ${String( error )}`, error );
						dispose();
						return;
					}
				}
				const snapshot = simulation.poll();
				if ( snapshot ) {
					workerDebtMs = snapshot.clock?.debtMs ?? 0;
					latestSequence = snapshot.sequence;
					simulationTimeMs = snapshot.timeMs;
					acceptedInput = snapshot.acceptedInputSequence;
					// Main-clock milliseconds of simulation time zero: poses carry their
					// simulation time, and presentation needs it on the frame clock.
					if ( snapshot.clock?.originMs ) {
						bugReport.movementClock( snapshot.clock.originMs );
						simulationOriginMs = snapshot.clock.originMs - performance.timeOrigin;
						characters.simulationOrigin( simulationOriginMs );
					}
				}
				const simulationError = simulation.error();
				if ( simulationError ) {
					platform.report( `Simulation failed: ${simulationError}` );
					dispose();
					return;
				}
				// The simulation clock starts at zero and can pause independently of RAF.
				// Preserve event age, not its absolute value, across this clock boundary.
				const listener = presentation.gameplay()?.pose;
				for ( const cue of presentation.takeSounds() ) {
					const at = (now - Math.max( 0, simulationTimeMs - cue.at )) / 1000;
					if ( cue.kind === "buff-ended" ) {
						if ( listener ) {
							audio.buffEnded( at, worldAudioPosition( listener ) );
						}
					} else if ( cue.kind === "item-sound" ) audio.nativeItem( cue.cue, at );
					else {audio.nativeUi(
							cue.handle,
							at,
							listener ? worldAudioPosition( listener ) : undefined
						);}
				}
				const frontendState = frontend?.step(
					sessionState,
					now,
					characters.dockReady(),
					characters.previewReady()
				);
				if ( pendingWorldReset && frontendState.phase === "loading-world" ) {
					pendingWorldReset = false;
					world.reset();
					navigation.reset();
					characters.reset();
					localMover.reset();
					audio.reset();
					readySent = false;
				}
				renderer.setCharacterPreview(
					[ "loading-create", "customize", "create-exit" ].includes( frontendState.phase ) ?
						frontendState.creation?.camera ?? null :
						null
				);
				const decal = presentation.gameplay()?.selectionDecal;
				const followed = decal?.kind === "target" ? presentation.read( decal.gid ) : undefined;
				// Port-only, not native (Experimental "Dungeon character PVS"): the
				// ring belongs to the published body, so a target standing in a
				// room this frame's room does not draw shows no ring either.
				const followedMount = followed?.mountedOn !== undefined ?
					presentation.read( followed.mountedOn ) :
					undefined;
				const followedRoom = followedMount?.dungeonBlock ?? followed?.dungeonBlock;
				const followedHidden = followedRoom !== undefined &&
					renderer.dungeonBlockHides?.(
							followedRoom,
							presentation.gameplay()?.navigationBlock
						) === true;
				renderer.setSelectionDecal(
					frontendState.phase !== "world" || !decal ?
						null :
						decal.kind === "ground" ?
						{ pose: decal.pose, slot: 0 } :
						followed && !followedHidden ?
						{
							pose: {
								regionId: followed.regionId,
								x: followed.x,
								y: followed.y,
								z: followed.z,
								angle: followed.heading
							},
							slot: decal.slot
						} :
						null
				);
				renderer.setWorldClock(
					sampleWorldClock(
						ui.debugClockSeed( presentation.gameplay()?.worldClock, simulationTimeMs ),
						simulationTimeMs
					)
				);
				// World presentation outlives its socket; logout/bootstrap owns teardown.
				const worldPresented = frontendState.phase === "world" || sessionState?.phase === "world";
				audio.world(
					worldPresented ? presentation.gameplay()?.pose ?? null : null,
					presentation.gameplay()?.worldClock,
					simulationTimeMs,
					presentation.gameplay()?.musicMode ?? 0
				);
				const dockActive = [
					"loading-dock",
					"dock-arrival",
					"dock",
					"departing",
					"create-arrival",
					"create",
					"create-return",
					"loading-race",
					"race-zoom",
					"title-exit"
				].includes( frontendState.phase );
				characters.receiveFeedback( presentation.takeFeedback(), presentation.entities() );
				markStage( "input-state-frontend" );
				world.pumpCameraScripts( now );
				presentation.step( simulationTimeMs );
				characters.profile( frameProbe() );
				characters.mallOutfit(
					worldPresented ? ui.mallPreview() : null,
					worldPresented ? ui.skinPreview() : null
				);
				const moverGid = presentation.gameplay()?.localGid ?? 0;
				characters.ownMover(
					moverGid,
					mainThreadOwnWalk ? moverFrame( now ) : { kind: "idle" },
					presentation.entities().find( entity => entity.gid === moverGid )?.mountedOn || undefined
				);
				characters.step(
					presentation.entities(),
					presentation.gameplay(),
					now / 1000,
					simulationTimeMs,
					input.camera()?.pitch,
					dockActive ? sessionState?.characters ?? [] : undefined,
					[ "loading-create", "customize", "create-exit" ].includes( frontendState.phase ) ?
						frontendState.creation :
						null,
					[ "create", "create-return", "race-zoom" ].includes( frontendState.phase ),
					effectDetail,
					worldPresented && input.blindHeld(),
					sessionState?.nativeServerName,
					normalFortressClothes
				);
				markStage( "character-presentation" );
				ui.mallPreviewState( characters.mallPreviewState() );
				const eventRain = characters.eventRain();
				renderer.setWeather(
					worldPresented ?
						{ ...(presentation.gameplay()?.weather ?? { mode: 1, amount: 0 }), eventRain } :
						null
				);
				// CGInterface_UpdateCameraOrbitTarget (68F830) locks third person
				// behind the drawn body's turning yaw (+0x88, written per step by
				// 86CBA0), not the logical heading, which snaps on each click.
				const cameraFollow = characters.cameraTarget();
				const movement = presentation.gameplay();
				if ( visible && lastFrameAt && now - lastFrameAt > MOVEMENT_FRAME_SPIKE_MS && movement?.pose ) {
					bugReport.movement( {
						event: "frame-spike",
						frameAtMs: now,
						durationMs: now - lastFrameAt,
						workerDebtMs,
						revision: movement.movementRevision,
						localGid: movement.localGid,
						logical: { ...movement.pose },
						displayed: cameraFollow?.pose ? { ...cameraFollow.pose } : undefined,
						bodyYaw: cameraFollow?.yaw,
						pending: movement.pendingMoves,
						visibility: document.visibilityState
					} );
				}
				if ( movement?.pose ) {
					frameProbe()?.movement?.( {
						atMs: now,
						workerAtMs: movement.poseAtMs ?? simulationTimeMs,
						workerDebtMs,
						revision: movement.movementRevision ?? 0,
						transition: movement.movementTransition,
						logical: movement.pose,
						displayed: cameraFollow?.pose ?? null,
						pending: movement.pendingMoves,
						acknowledged: movement.acknowledgedMove
					} );
				}
				world.setFreeCameraKeys( freeCameraKeys() );
				world.step(
					[ "loading-world", "world" ].includes( frontendState.phase ) ?
						presentation.gameplay()?.pose ?? null :
						null,
					input.camera(
						cameraFollow?.yaw ??
							(presentation.gameplay()?.pose ?
								nativeHeadingYaw( presentation.gameplay()!.pose!.angle ) :
								undefined)
					),
					cameraFollow,
					presentation.gameplay()?.navigationBlock,
					now,
					characters.takeCameraScripts()
				);
				// Dungeon visibility depends on admitted navigation ownership. Waiting
				// for the first visible draw here deadlocks first entry into a dungeon.
				// Retain admitted navigation while offline, but do not emit a late
				// navigation command into the disconnected simulation.
				if ( sessionState?.phase === "world" || !worldPresented ) {
					navigation.step(
						worldPresented ? presentation.gameplay()?.pose ?? null : null,
						presentation.gameplay()?.navigationRegion,
						presentation.gameplay()?.navigationFailure,
						presentation.gameplay()?.navigationRequestId
					);
				}

				audio.music(
					![ "loading-title", "loading-world", "world", "failed" ].includes( frontendState.phase ),
					worldPresented
				);
				audio.prepareUi( frontendState.phase !== "world" );
				audio.prepareCombat( worldPresented ? presentation.gameplay() : null, presentation.entities() );
				const worldStats = renderer.worldStats(),
					localReady = characters.ready( presentation.gameplay()?.localGid ?? 0 ),
					navReady = navigation.phase() === "ready",
					baselineReady = characters.entryReady() && ui.entryReady();
				const worldReady = world.ready() && worldStats.visibleGroups > 0 && worldStats.pendingGroups === 0 &&
					worldStats.pendingTextures === 0 && localReady && navReady && audio.ready() && baselineReady;
				const loadingProgress = sessionState?.phase !== "world" || !presentation.gameplay()?.localGid ?
					0 :
					worldReady ?
					1 :
					(Number( localReady ) + Number( navReady ) + world.progress() + Number( audio.ready() ) +
						Number( baselineReady )) / 5;
				markStage( "world-stream" );
				const semantics = ui.step(
					{
						worldError: world.error() ?? navigation.error(),
						worldRetrying: world.error() ? world.reconnecting() : navigation.reconnecting(),
						resourceError: (!localReady || !baselineReady ? characters.error() : null) ??
							(!audio.ready() ? audio.error() : null),
						simulationTimeMs,
						hoveredEntity,
						dropNamesHeld: input.dropNamesHeld(),
						blindHeld: worldPresented && input.blindHeld(),
						travel: readySent ? null : presentation.travel(),
						worldTransitionRegion: world.loadingRegion(),
						loadingProgress,
						berserkGauge: characters.orbGauge(),
						frontend: frontendState,
						session: sessionState,
						gameplay: presentation.gameplay(),
						entities: presentation.entities(),
						// Layout and picking share logical UI pixels; the world retains its full backing resolution.
						width: platform.readUiViewport().width,
						height: platform.readUiViewport().height,
						worldReady: readySent || worldReady
					},
					now,
					frameProbe()
				);
				if ( semantics ) {
					platform.presentUi( semantics );
					loadingVisible = semantics.loadingVisible === true;
					loadingTitle = semantics.loadingStatus ?? "Preparing your journey";
				}
				// A refused release protocol stops every request, so the scene can
				// never finish: name the remedy instead of showing scene progress.
				const outdatedTitle = sessionState?.releaseOutdated === true ? sessionState.error : undefined;
				platform.presentLoading( {
					visible: loadingVisible || frontendState.entryPending === true,
					title: outdatedTitle ?? (frontendState.entryPending ? "Connecting to server" : loadingTitle),
					progress: frontendState.entryPending ? null : assets.progress()
				} );
				if ( worldReady && sessionState?.phase === "world" && !readySent ) {
					frontend?.worldReady();
					simulation.session( { kind: "world-ready", travelRevision: presentation.travel()?.revision ?? 0 } );
					readySent = true;
					// Foreground loading is done: make combat sounds and effects local
					// before their first play (worker/install.ts). Runs once per worker.
					assets.install( new URL( BACKGROUND_INSTALL_LIST, location.origin ).href );
				}
				// Check for a newer release when the title opens or the connection
				// drops (the moments a refresh costs the player nothing), and when
				// the tab comes back into view.
				const phase = sessionState?.phase ?? "signed-out";
				const phaseTrigger = phase !== releasePhase && (phase === "signed-out" || phase === "disconnected");
				releasePhase = phase;
				// The server holds a world session only while it hears from this
				// tab; a frozen background tab would let it expire (keep-awake.ts).
				platform.setKeepAwake(
					phase === "connecting" || phase === "entering-world" || phase === "world" ||
						phase === "reconnecting"
				);
				const visibleAgain = platform.visibilityReturned(), onlineAgain = platform.connectionReturned();
				if ( visibleAgain || onlineAgain ) {
					world.retryTransient();
					navigation.retryTransient();
				}
				releaseWatch.step( now, phaseTrigger || visibleAgain );
				// A server's 426 is the same news, learned from a refused request.
				platform.presentUpdate( releaseWatch.newerAvailable() || sessionState?.releaseOutdated === true );
				markStage( "ui" );
				renderer.setDamageText( characters.damageText() );
				// Hidden maintenance still acknowledges every ordered publication, but
				// cannot display a frame, so it skips GPU preparation. A pending world
				// entry is the exception: scene admission happens only inside the
				// renderer frame, and 0x3012 game-ready waits on it. The native client
				// keeps updating while inactive and only stops drawing: Application_PumpIdleFrame
				// (A0F430) runs OnUpdate every idle frame, CGFXMainFrame_RenderFrame (A11650)
				// sleeps 20 ms instead of drawing while g_bIsWindowActive is 0, and the loading
				// screen finishes in CPSMission_OnUpdate (728E10, sends 0x324C). A background
				// tab must finish its entry too.
				const admitting = !readySent && sessionState?.phase === "world";
				const rendered = visible || admitting ?
					renderer.frame( platform.readViewport(), now / 1000, frameId, frameProbe() ) :
					undefined;
				if ( rendered ) {
					await rendered;
				}
				waitMs = visible || admitting ? renderer.readbackWaitMs?.() ?? 0 : 0;
				if ( disposed ) return;
				markStage( "render-preparation-submit" );
				renderer.setTeleportGates( worldPresented ? presentation.entities() : [] );
				const hoverLocal = presentation.gameplay()?.localGid,
					hoverGid = visible && diagnostics.hoverPicking !== false && worldPointer &&
							frontendState.phase === "world" &&
							hoverLocal &&
							!ui.blocks(
								worldPointer[0] * platform.readUiViewport().width,
								worldPointer[1] * platform.readUiViewport().height
							) ?
						renderer.pickEntity( worldPointer[0], worldPointer[1], hoverLocal, input.blindHeld() ) :
						null;
				hoveredEntity = hoverGid;
				platform.presentWorldCursor(
					ui.cursor() ?? worldCursor(
						hoverGid === null ? undefined : presentation.read( hoverGid ),
						hoverLocal ? presentation.read( hoverLocal ) : undefined,
						false,
						input.altHeld()
					)
				);
				markStage( "hover" );
				const soundListener = renderer.audioListener();
				audio.step(
					now / 1000,
					soundListener?.position ?? (listener ?
						worldAudioPosition( listener ) :
						[ 0, 0, 0 ]),
					soundListener ?? undefined
				);
				markStage( "audio" );
				if ( renderer.phase() === "failed" ) {
					platform.report( `Renderer failed: ${renderer.error()}` );
					dispose();
					return;
				}
				// The frame owner measures its own loop: RAF spacing is the presented
				// frame cost; the callback span is this runtime's share of it.
				if ( lastFrameAt ) sample( frameHistory, now - lastFrameAt );
				lastFrameAt = now;
				const cpuMs = performance.now() - cpuStart - waitMs;
				sample( cpuHistory, cpuMs );
				frameWork.recordCpu( cpuMs );
				frameProbe()?.characterCount( "cpu-ms", cpuMs );
				frameProbe()?.characterCount( "readback-wait-ms", waitMs );
				frameProbe()?.characterCount( "cosmetic-level", frameWork.level() );
				if ( frameHistory.length && now - lastTelemetry >= TELEMETRY_INTERVAL_MS ) {
					lastTelemetry = now;
					const frameMs = average( frameHistory ), drawn = renderer.characterStats();
					platform.presentTelemetry( {
						overload: frameWork.stats(),
						frameId,
						stages: diagnostics.stages ?
							Object.fromEntries(
								Object.entries( stageTotals ).map( ( [name, total] ) => [ name, total / stageFrames ] )
							) :
							undefined,
						gpu: renderer.gpuTiming(),
						fps: frameMs > 0 ? 1000 / frameMs : 0,
						frameMs,
						p95FrameMs: percentile( frameHistory, 0.95 ),
						cpuMs: average( cpuHistory ),
						p95CpuMs: percentile( cpuHistory, 0.95 ),
						actors: drawn.actors,
						draws: drawn.draws,
						visibleGroups: renderer.worldStats().visibleGroups,
						pingMs: sessionState?.phase === "world" ? sessionState.pingMs : null,
						build: buildInfo.readout( now, platform.diagnosticsActive(), sessionState?.phase )
					} );
					for ( const name in stageTotals ) stageTotals[name] = 0;
					stageFrames = 0;
				}
				if ( now - lastReport >= 250 ) {
					lastReport = now;
					runtimeErrors.update( "Characters", characters.error() );
					runtimeErrors.update( "Navigation", navigation.reconnecting() ? null : navigation.error() );
					runtimeErrors.update( "World", world.reconnecting() ? null : world.error() );
					runtimeErrors.update( "Audio", audio.error() );
					runtimeErrors.update( "Interface", ui.resourceError() );
					const assetHealth = assets.health();
					runtimeErrors.update( "Assets", assetHealth.phase === "failed" ? assetHealth.error : null );
					platform.report( `Replacement runtime: ${renderer.phase()}
Simulation tick: ${latestSequence}
Input acknowledged: ${acceptedInput}
Session: ${sessionState?.phase ?? "signed-out"}; entities: ${presentation.count()}
Frontend: ${frontendState.phase}
Frontend error: ${frontendState.error ?? "none"}
Frontend loading: ${frontendState.loadingStatus ?? "none"}; dock ready: ${characters.dockReady()}
Dock pick: ${lastDockPick}
Characters: ${renderer.characterStats().actors} actors, ${renderer.characterStats().draws} draws; ${
						characters.error() ?? "running"
					}
Navigation: ${navigation.error() ?? navigation.phase()}; admitted ${presentation.gameplay()?.navigationRegion ?? "none"}
Audio: ${audio.error() ?? "running"}; music: ${audio.musicStatus()}
UI: ${ui.stats().pending} pending images; ${ui.stats().failed.length} failed images; ${ui.stats().error ?? "running"}
World: ${
						world.error() ??
							`${renderer.worldStats().visibleGroups} visible groups; ${renderer.worldStats().pendingTextures} pending textures`
					}
World admission: ${renderer.worldStats().sceneId ?? "none"}; ${renderer.worldStats().pendingGroups} pending groups` );
				}
				frameProbe()?.end();
				raf = requestAnimationFrame( displayFrame );
				hiddenFrame( frameToken, now );
			} catch ( error ) {
				platform.report( `Runtime failed: ${String( error )}`, error );
				dispose();
			}
		}
		/*
		================
		hiddenFrame

		A hidden document gets no animation frames. While hidden, a worker
		delivery at least HIDDEN_FRAME_MS after the last frame runs the next
		one instead. A visible document waits for its RAF unless none came
		STARVED_FRAME_MS after the last frame finished: a window the browser
		stopped drawing without hiding it, which then runs at the hidden pace
		until a RAF arrives again. The native client keeps updating while it
		draws nothing (A0F430), so loading finishes either way. The frame
		token retires this waiter once either path has run a frame.
		================
		*/
		function hiddenFrame( token: number, lastFrameAt: number, finishedAt = performance.now() ): void {
			simulation.delivery().then( () => {
				if ( disposed || token !== frameToken ) return;
				const now = performance.now();
				const hidden = globalThis.document?.visibilityState === "hidden";
				const due = hidden || starved ?
					now - lastFrameAt >= HIDDEN_FRAME_MS :
					now - finishedAt >= STARVED_FRAME_MS;
				if ( !due ) {
					hiddenFrame( token, lastFrameAt, finishedAt );
					return;
				}
				if ( !hidden ) starved = true;
				cancelAnimationFrame( raf );
				displayFrame( now, true );
			} );
		}
		raf = requestAnimationFrame( displayFrame );
		hiddenFrame( frameToken, performance.now() );
		return {
			dispose,
			audioSnapshot: () => audio.snapshot(),
			gpuTiming: () => renderer.gpuTiming(),
			retryWorld: () => {
				world.retry();
				navigation.retry();
			},
			session: sessionCommand,
			sessionState: () => sessionState,
			entity: gid => presentation.read( gid ),
			gameplay: () => presentation.gameplay(),
			entities: () => presentation.entities(),
			berserkGauge: () => characters.orbGauge(),
			characterActors: () => renderer.characterActors(),
			characterStats: () => renderer.characterStats( true ),
			characterCullCensus: () => renderer.characterStats( true, true ),
			camera: () => input.camera(),
			worldView: () => renderer.worldView(),
			takeNative: () => presentation.takeNative()
		};
	} catch ( error ) {
		try {
			dispose();
		} catch ( cleanupError ) {
			throw new AggregateError( [ error, cleanupError ], "Runtime startup and cleanup failed" );
		}
		throw error;
	}
}

/*
================
startViewerRuntime

Port-only, not native (docs/VIEWER.md): the embeddable 3D viewer page. It
creates only the owners one drawn model needs (assets, renderer and the
character presentation, as startRuntime builds them) and drives
viewer/viewer.ts by animation frames only while it asks for them: an idle
interactive view costs nothing until input wakes it. The owners are
disposed when the viewer is done. No session, input, audio or UI.
================
*/
export function startViewerRuntime( canvas: HTMLCanvasElement, picture: HTMLCanvasElement, search: string ): void {
	const random = createPresentationRandom( 1 );
	const assets = createAssets();
	const renderer = createRenderer( canvas, random );
	const characters = createCharacterPresentation(
		assets,
		renderer,
		new URL( "/", import.meta.url ).href,
		() => {},
		random
	);
	let scheduled = false, disposed = false;
	/*
	================
	viewerFrame
	================
	*/
	const viewerFrame = ( now: number ) => {
		scheduled = false;
		const next = viewer.frame( now );
		if ( next === "draw" ) wake();
		else if ( next === "done" ) {
			disposed = true;
			characters.dispose();
			renderer.dispose();
			assets.dispose();
		}
	};
	/*
	================
	wake
	================
	*/
	const wake = () => {
		if ( scheduled || disposed ) return;
		scheduled = true;
		requestAnimationFrame( viewerFrame );
	};
	const viewer = createViewer( { canvas, picture, search, renderer, characters, wake } );
	wake();
}
