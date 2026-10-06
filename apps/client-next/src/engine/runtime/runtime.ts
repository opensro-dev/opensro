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
import { sampleWorldClock } from "@/engine/foundation/gameplay/world-clock";
import { createWorldDoubleClick } from "@/engine/foundation/gameplay/world-double-click";
import type { WorldClickInput } from "@/engine/contracts/input";
import { createNavigationStream } from "./navigation/navigation";
import { createFrontend } from "./frontend/frontend";
import { createUi } from "./ui/ui";
import { createAudio } from "./audio/audio";
import { createCharacterPresentation } from "./characters/characters";
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
import { createSimulationHost } from "./simulation/host";
import type { RuntimeControl } from "@/engine/contracts/runtime";

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
		const input = createInput();
		const frameWork = createFrameWork();
		const framePacing = createFramePacing();
		const simulation = own( createSimulationHost() );
		const renderer = own(
			createRenderer( canvas, random, audio.enqueue, diagnostics )
		);
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
			simulation.session( command );
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
					frontend.race( event.id.endsWith( "europe" ) ? 0 : 1 );
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
				bugReport
			)
		);
		let lastDockPick = "none";
		// Deliberate deviation (see world-double-click.ts): a drifted second
		// press on the same monster still attacks. Do not remove.
		const worldDoubleClick = createWorldDoubleClick();
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
				if ( race !== null ) frontend.race( race );
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
				const corpse = renderer.pickEntity( x, y, 0, input.blindHeld() );
				if ( corpse === game.localGid ) ui.event( { kind: "world-select", gid: corpse } );
				return;
			}
			const gid = renderer.pickEntity( x, y, game.localGid, input.blindHeld() ),
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
					simulation.session( { kind: "gameplay", command: { kind: "select", gid: entity.gid } } );
				}
				if ( doubleClick || attack ) {
					if ( attackable && attack ) {
						simulation.session( {
							kind: "gameplay",
							command: { kind: local?.mountedOn ? "cos-attack" : "attack", gid: entity.gid }
						} );
					}
				} else if ( entity.kind === "ground-item" ) {
					simulation.session( { kind: "gameplay", command: { kind: "pickup", gid: entity.gid } } );
				}
				return;
			}
			if ( doubleClick ) return;
			const query = renderer.pickGround( x, y );
			if ( query && game.pose ) {
				simulation.session( { kind: "gameplay", command: { kind: "ground-move", query } } );
			}
		}
		let worldPointer: readonly [number, number] | null = null, hoveredEntity: number | null = null;
		let readySent = false;
		const world = own( createWorldStream( assets, renderer, new URL( "/", import.meta.url ).href, random ) );
		const navigation = own(
			createNavigationStream(
				assets,
				command => simulation.session( { kind: "gameplay", command } ),
				new URL( "/", import.meta.url ).href
			)
		);
		const characters = own(
			createCharacterPresentation(
				assets,
				renderer,
				new URL( "/", import.meta.url ).href,
				audio.enqueue,
				random,
				world.soundSurface,
				presentation
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
		================
		*/
		function displayFrame( now: number ): void {
			if ( disposed ) {
				return;
			}
			const visible = globalThis.document?.visibilityState !== "hidden";
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
			void frame( now );
		}
		/*
		================
		frame
		================
		*/
		async function frame( now: number ): Promise<void> {
			if ( disposed ) return;
			const visible = globalThis.document?.visibilityState !== "hidden";
			// Whichever of RAF and a hidden delivery ran this frame, retire the other.
			frameToken++;
			const cpuStart = performance.now();
			frameWork.begin( now, globalThis.document?.visibilityState !== "hidden" );
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
								audio.reset();
								readySent = false;
							} else pendingWorldReset = true;
						}
						characters.receiveLifecycle( batch.events );
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
						characters.simulationOrigin( snapshot.clock.originMs - performance.timeOrigin );
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
							audio.buffEnded( at, [
								listener.x + (listener.regionId & 255) * 1920,
								listener.y,
								listener.z + (listener.regionId >>> 8) * 1920
							] );
						}
					} else if ( cue.kind === "item-sound" ) audio.nativeItem( cue.cue, at );
					else {audio.nativeUi(
							cue.handle,
							at,
							listener ?
								[
									listener.x + (listener.regionId & 255) * 1920,
									listener.y,
									listener.z + (listener.regionId >>> 8) * 1920
								] :
								undefined
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
				renderer.setSelectionDecal(
					frontendState.phase !== "world" || !decal ?
						null :
						decal.kind === "ground" ?
						{ pose: decal.pose, slot: 0 } :
						followed ?
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
				renderer.setWorldClock( sampleWorldClock( presentation.gameplay()?.worldClock, simulationTimeMs ) );
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
					true,
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
						// UI pixels: the chosen screen size, or CSS pixels when native.
						width: platform.canvasSize().width / platform.displayScale(),
						height: platform.canvasSize().height / platform.displayScale(),
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
				releaseWatch.step( now, phaseTrigger || platform.visibilityReturned() );
				// A server's 426 is the same news, learned from a refused request.
				platform.presentUpdate( releaseWatch.newerAvailable() || sessionState?.releaseOutdated === true );
				markStage( "ui" );
				renderer.setDamageText( characters.damageText() );
				// Hidden maintenance still acknowledges every ordered publication, but
				// cannot display a frame. Avoid GPU preparation and visibility queries.
				const rendered = visible ?
					renderer.frame( platform.readViewport(), now / 1000, frameId, frameProbe() ) :
					undefined;
				if ( rendered ) {
					await rendered;
				}
				waitMs = visible ? renderer.readbackWaitMs?.() ?? 0 : 0;
				if ( disposed ) return;
				markStage( "render-preparation-submit" );
				renderer.setTeleportGates( worldPresented ? presentation.entities() : [] );
				const hoverLocal = presentation.gameplay()?.localGid,
					hoverGid = visible && diagnostics.hoverPicking !== false && worldPointer &&
							frontendState.phase === "world" &&
							hoverLocal &&
							!ui.blocks(
								worldPointer[0] * platform.canvasSize().width / platform.displayScale(),
								worldPointer[1] * platform.canvasSize().height / platform.displayScale()
							) ?
						renderer.pickEntity( worldPointer[0], worldPointer[1], hoverLocal, input.blindHeld() ) :
						null;
				hoveredEntity = hoverGid;
				platform.presentWorldCursor(
					ui.cursor() ?? worldCursor(
						hoverGid === null ? undefined : presentation.read( hoverGid ),
						hoverLocal ? presentation.read( hoverLocal ) : undefined
					)
				);
				markStage( "hover" );
				const soundListener = renderer.audioListener();
				audio.step(
					now / 1000,
					soundListener?.position ?? (listener ?
						[
							listener.x + (listener.regionId & 255) * 1920,
							listener.y,
							listener.z + (listener.regionId >>> 8) * 1920
						] :
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
					runtimeErrors.update( "Navigation", navigation.error() );
					runtimeErrors.update( "World", world.error() );
					runtimeErrors.update( "Navigation", navigation.error() );
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
		one instead; a visible document keeps waiting for its RAF. The frame
		token retires this waiter once either path has run a frame.
		================
		*/
		function hiddenFrame( token: number, lastFrameAt: number ): void {
			simulation.delivery().then( () => {
				if ( disposed || token !== frameToken ) return;
				const now = performance.now();
				if ( globalThis.document?.visibilityState !== "hidden" || now - lastFrameAt < HIDDEN_FRAME_MS ) {
					hiddenFrame( token, lastFrameAt );
					return;
				}
				cancelAnimationFrame( raf );
				displayFrame( now );
			} );
		}
		raf = requestAnimationFrame( displayFrame );
		hiddenFrame( frameToken, performance.now() );
		return {
			dispose,
			audioSnapshot: () => audio.snapshot(),
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
			camera: () => input.camera(),
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
