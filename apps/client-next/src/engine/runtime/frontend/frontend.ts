/*
===========================================================================

frontend.ts - title, dock and character creation flow coordination

===========================================================================
*/
import { createFrontendFlow } from "./flow/flow";
import { createFrontendCamera } from "./camera/camera";
import { createFrontendStage } from "./stage/stage";
import { createCharacterDialog } from "./dialog/dialog";
import { createCreation } from "./creation/creation";
import { characterStatus } from "@/engine/foundation/ui/character-status";
import {
	createEntryVisibility,
	entryVisibilityBegin,
	entryVisibilityEnd,
	entryVisibilitySetHidden,
	entryVisibilityTotal
} from "@/engine/foundation/session/entry-visibility";
import { selectedDockCamera } from "@/engine/foundation/rendering/dock-camera";
import type { AssetOwner } from "@/engine/contracts/assets";
import type { Renderer } from "@/engine/contracts/runtime";
import type { SessionState, CharacterOperationCommand, SessionCommand } from "@/engine/contracts/session";
import type { FrontendSnapshot } from "@/engine/contracts/frontend";
/*
================
LOADING_STALL_LIMIT_SECONDS

How long one of the post-login loading phases (loading-create,
loading-race, loading-dock) may sit with none of the facts its
completion gate waits on changing - the displayed progress, the stage
status, the stage/dock/creation readiness, the preview actor count -
before the wait is declared failed. The gate polls those owners and a
poll that stalls after every asset has arrived reports no error
(BR-261108-1908-A644: the bar full, the asset queue idle for minutes,
the session still at character-select, clicks dead), so without a
bound the loading screen is permanent. The bound runs on the step
clock (flow.elapsed), so a hidden tab does not spend it, and it only
runs while nothing changes at all: a slow load that keeps moving is
never cut. The worker's admission deadline (session/world.ts) is the
model; this is its frontend counterpart.
================
*/
const LOADING_STALL_LIMIT_SECONDS = 60;
// A refused world entry shows its native error code's message
// (characterStatus, SRO_Client 0x73EE40). 1 is the accepted answer and
// 0 names no code, so either falls back to the generic (S2).
const ENTRY_ACCEPTED_CODE = 1;
const ENTRY_GENERIC_REFUSAL = 2;
/*
================
pageHidden

Whether the page is hidden right now; a runtime without a document
(tests) is never hidden.
================
*/
function pageHidden(): boolean {
	return globalThis.document?.hidden === true;
}
/*
================
createFrontend
================
*/
export function createFrontend(
	assets: AssetOwner,
	renderer: Renderer,
	base: string,
	send: (
		command: CharacterOperationCommand | { kind: "logout"; } | Extract<SessionCommand, { kind: "client-report"; }>
	) => void = () => {},
	messageSound: () => void = () => {}
) {
	const flow = createFrontendFlow(),
		camera = createFrontendCamera(),
		stage = createFrontendStage( assets, renderer, base );
	const dialog = createCharacterDialog( send );
	// How long the page was hidden during the current entry attempt
	// (entry-visibility.ts), for the world_entry_rejected incident (#2558).
	const visibility = createEntryVisibility();
	let status: ReturnType<typeof characterStatus> | undefined, statusUntil = 0;
	const creation = createCreation( assets, base, send, messageSound );
	let race: 0 | 1 = 0;
	let entryPending = false, entering = false, entryRevision = -1, sessionRevision = 0;
	let last = 0,
		manifestInstalled = false,
		stageName = "title",
		lastPhase = "",
		disposed = false,
		authenticated = false,
		selectedCharacter = "",
		cameraMoving = false;
	let roster: NonNullable<SessionState["characters"]> = [];
	let pointer: readonly [number, number] | null = null;
	// The loading gate's last seen shape (stepLoadingWatch below): the
	// phase generation it belongs to, the gate facts as one signature,
	// and the phase elapsed at which that signature was first seen.
	let loadingWatch: { generation: number; signature: string; at: number; } | null = null;
	// Native CPSTitle 0x748630 disables terrain LOD; 0x741B80 restores it on release.
	stage.install( "/assets/title/constantinople/manifest.json", "full", "settle" );
	/*
	================
	stepLoadingWatch

	One step of the loading stall watch (LOADING_STALL_LIMIT_SECONDS).
	Outside the post-login loading phases the watch is cleared. Inside
	one, any change in the facts the completion gate waits on restarts
	the bound; a full bound with no change at all means the gate's
	owners have stalled without reporting an error, so the stall is
	reported with the facts the gate was missing and the load fails
	like a stage failure: the player gets the "Unable to finish
	loading" screen and its way back instead of a permanent bar.
	True when this step failed the flow.
	================
	*/
	function stepLoadingWatch(
		state: FrontendSnapshot,
		progress: number,
		stageReady: boolean,
		dockReady: boolean,
		creationReady: boolean
	): boolean {
		if ( ![ "loading-create", "loading-race", "loading-dock" ].includes( state.phase ) ) {
			loadingWatch = null;
			return false;
		}
		const signature = [
			progress,
			stageReady,
			dockReady,
			creationReady,
			stageName === "creation" ? renderer.characterStats().actors : -1,
			stage.status() ?? ""
		].join( "|" );
		if (
			!loadingWatch || loadingWatch.generation !== state.generation ||
			loadingWatch.signature !== signature
		) {
			loadingWatch = { generation: state.generation, signature, at: state.elapsed };
			return false;
		}
		if ( state.elapsed - loadingWatch.at < LOADING_STALL_LIMIT_SECONDS ) return false;
		loadingWatch = null;
		// The player sees only the failed screen: report the stage and
		// the gate's facts, so a stalled entry is never silent (the
		// world_entry_rejected report above is the model).
		send( {
			kind: "client-report",
			incidentKind: "runtime",
			code: "frontend_loading_stalled",
			message: `Loading stalled at ${state.phase}: stage ready ${stageReady}, dock ready ${dockReady}, ` +
				`creation ready ${creationReady}, progress ${progress}, status ${stage.status() ?? "none"}`
		} );
		flow.fail( "Loading stalled: " + (stage.status() ?? state.phase) );
		return true;
	}
	return {
		/*
		================
		reveal
		================
		*/
		reveal() {
			flow.reveal();
		},
		/*
		================
		failure

		The stage failure as facts for the incident report (#2559): the
		text the flow failed with, and the caught error's stack when the
		stage threw one.
		================
		*/
		failure() {
			const error = stage.error();
			if ( !error ) return null;
			const cause = stage.cause();
			return {
				message: error,
				stack: cause instanceof Error && cause.stack ? cause.stack.slice( 0, 8192 ) : undefined
			};
		},
		/*
		================
		create
		================
		*/
		create() {
			if ( flow.snapshot().phase !== "dock" || selectedCharacter || cameraMoving || dialog.snapshot() ) return;
			if ( roster.length < 4 ) flow.create();
			else {
				status = { key: "UIO_MSG_ERROR_CHARACTER_OVER_3", suffix: "", args: [ 4 ] };
				statusUntil = last + 15000;
				messageSound();
			}
		},
		/*
		================
		back
		================
		*/
		back() {
			if ( creation.snapshot() && ![ "editing", "checking" ].includes( creation.snapshot()!.phase ) ) return;
			creation.reset();
			flow.back();
		},
		/*
		================
		start
		================
		*/
		start() {
			if (
				entryPending || flow.snapshot().phase !== "dock" || cameraMoving || dialog.snapshot() ||
				!roster.some( row => row.name === selectedCharacter && !row.deletePending )
			) return false;
			entryRevision = sessionRevision;
			entryPending = true;
			return true;
		},
		/*
		================
		leave
		================
		*/
		leave() {
			if ( !selectedCharacter && !cameraMoving && !dialog.snapshot() ) flow.leave();
		},
		/*
		================
		race
		================
		*/
		race( value: 0 | 1, protectorFloor: 0 | 1 ) {
			if ( flow.snapshot().phase !== "create" ) return;
			race = value;
			creation.open( value, protectorFloor );
			flow.race();
		},
		/*
		================
		creationAction
		================
		*/
		creationAction( id: string, value?: string ) {
			if ( flow.snapshot().phase === "customize" ) creation.action( id, value );
		},
		/*
		================
		dialogAction
		================
		*/
		dialogAction( action: string ) {
			if ( action === "dock:warning-cancel" ) {
				dialog.cancel();
				return;
			}
			if ( action === "dock:warning-accept" ) {
				dialog.accept();
				return;
			}
			if ( flow.snapshot().phase !== "dock" || cameraMoving ) return;
			const row = roster.find( row => row.name === selectedCharacter );
			if ( row ) {
				const warning = dialog.open( row );
				if ( warning ) {
					status = { key: warning, suffix: "" };
					statusUntil = last + 15000;
					messageSound();
				}
			}
		},
		/*
		================
		pointer
		================
		*/
		pointer( value: readonly [number, number] | null ) {
			pointer = value;
		},
		/*
		================
		select
		================
		*/
		select( name: string ) {
			if ( flow.snapshot().phase !== "dock" || cameraMoving || selectedCharacter ) return;
			const index = roster.findIndex( row => row.name === name );
			if ( index < 0 || index >= 4 ) return;
			selectedCharacter = name;
			camera.returnTo( selectedDockCamera( roster[index]!, index, roster.length ) );
			cameraMoving = true;
		},
		/*
		================
		deselect
		================
		*/
		deselect() {
			if (
				entryPending || flow.snapshot().phase !== "dock" || cameraMoving || !selectedCharacter ||
				dialog.snapshot()
			) return;
			const rest = stage.manifest()?.camera.at( -1 );
			if ( !rest ) return;
			selectedCharacter = "";
			camera.returnTo( rest );
			cameraMoving = true;
		},
		/*
		================
		step
		================
		*/
		step( session: SessionState | null, now: number, dockReady = true, creationReady = true ) {
			if ( disposed ) return flow.snapshot();
			// Non-fatal asset faults (an unacknowledged cancellation) reach the
			// Agent as degraded reports instead of passing unseen. The frontend
			// steps every frame and owns the session sender; runtime.ts is sealed.
			for ( const degradation of assets.takeDegradations?.() ?? [] ) {
				send( {
					kind: "client-report",
					incidentKind: "degraded",
					code: degradation.code,
					message: degradation.message
				} );
			}
			// Held character clocks reach the Agent the same way: the renderer
			// owns the snapshot rows that hold them, this owner owns the sender.
			for ( const degradation of renderer.takeDegradations?.() ?? [] ) {
				send( {
					kind: "client-report",
					incidentKind: "degraded",
					code: degradation.code,
					message: degradation.message
				} );
			}
			const delta = last ? Math.max( 0, (now - last) / 1000 ) : 0;
			last = now;
			if ( authenticated && session?.phase === "signed-out" ) {
				entryPending = false;
				entering = false;
				authenticated = false;
				selectedCharacter = "";
				cameraMoving = false;
				dialog.reset();
				creation.reset();
				status = undefined;
				flow.reset();
				camera.dispose();
				stageName = "title";
				manifestInstalled = false;
				lastPhase = "";
				stage.install( "/assets/title/constantinople/manifest.json", "full", "settle" );
			}
			if ( session?.phase === "character-select" ) {
				if ( flow.snapshot().phase === "world" ) {
					entryPending = false;
					entering = false;
					selectedCharacter = "";
					cameraMoving = false;
					dialog.reset();
					creation.reset();
					status = undefined;
					camera.dispose();
					flow.returnedToDock();
				}
				authenticated = true;
			}
			sessionRevision = session?.revision ?? 0;
			roster = session?.characters ?? [];
			const result = dialog.step( delta, session?.characterOperation, roster );
			if ( result?.status === "failed" ) {
				status = characterStatus( result.nativeErrorCode );
				statusUntil = now + 15000;
				messageSound();
			}
			if ( result?.status === "succeeded" ) {
				const index = roster.findIndex( row => row.name === selectedCharacter );
				if ( index >= 0 ) {
					camera.returnTo( selectedDockCamera( roster[index]!, index, roster.length ), 1 );
					cameraMoving = true;
				}
				status = undefined;
			}
			if ( now >= statusUntil ) status = undefined;
			if ( session?.phase === "character-select" ) flow.authenticated();
			if (
				session?.restoringWorld &&
				[ "connecting", "entering-world", "world", "reconnecting" ].includes( session.phase )
			) {
				authenticated = true;
				flow.resumeWorld();
			}
			if ( session?.phase === "connecting" || session?.phase === "entering-world" ) entering = true;
			// The entry's hidden time runs on the step clock: steps stop
			// while the page is hidden, so a span closes at the first
			// visible step - within a frame of the truth, and exact
			// enough to tell a backgrounded tab from a stalled device.
			if ( (entering || entryPending) && !visibility.tracking ) {
				entryVisibilityBegin( visibility, now, pageHidden() );
			} else if ( !(entering || entryPending) && visibility.tracking ) {
				entryVisibilityEnd( visibility, now );
			}
			if ( visibility.tracking ) entryVisibilitySetHidden( visibility, pageHidden(), now );
			if (
				(entering || entryPending || flow.snapshot().phase === "loading-world") && session?.error &&
				session.revision > entryRevision && session.phase === "character-select"
			) {
				// The player sees one status line: report the stage and the
				// session's reason, so a refused entry is never silent
				// (client-report). A refusal that named its native code shows
				// that code's message; anything else is the generic (S2).
				// A maintenance refusal is a designed answer, not an incident:
				// the typed refusal the mint carried files it apart (#2557).
				const named = session.entryFailure?.nativeErrorCode ?? 0;
				const nativeError = named > ENTRY_ACCEPTED_CODE ? named : ENTRY_GENERIC_REFUSAL;
				send( {
					kind: "client-report",
					incidentKind: "runtime",
					code: session.entryFailure?.refusal === "MAINTENANCE" ?
						"world_entry_maintenance" :
						"world_entry_rejected",
					message:
						`Entry rejected (S${nativeError}) at ${flow.snapshot().phase}/${
							entryPending ? "pending" : entering ? "entering" : "loading"
						}: ${session.error}` +
						(session.entryFailure ?
							` (stage ${session.entryFailure.stage}; ${session.entryFailure.bytesReceived} bytes in ${session.entryFailure.framesReceived} frames received)` :
							""),
					tabHiddenMs: Math.round( entryVisibilityTotal( visibility, now ) )
				} );
				entryVisibilityEnd( visibility, now );
				entryPending = false;
				entering = false;
				flow.entryRejected();
				status = characterStatus( nativeError );
				statusUntil = now + 15000;
				messageSound();
			}
			if ( session?.phase === "world" ) {
				entering = false;
				if ( entryPending ) {
					entryPending = false;
					flow.start();
				} else if ( ![ "departing", "failed" ].includes( flow.snapshot().phase ) ) {
					// The session owns world entry: an enter-world this dock did not
					// start (a launcher, a script) still binds a world. Left on the
					// dock, the runtime never resets the world for it, and the
					// loading screen waits forever on a scene it never streams.
					flow.resumeWorld();
				}
			}
			let state = flow.snapshot();
			if ( creation.step( delta, session?.characterOperation ) && state.phase === "customize" ) {
				creation.reset();
				flow.created();
				state = flow.snapshot();
			}
			if ( state.phase === "loading-create" && stageName !== "creation" ) {
				stageName = "creation";
				manifestInstalled = false;
				stage.install(
					"/assets/character-select/create-world-" + (race === 0 ? "europe" : "china") + "-manifest.json",
					"distance"
				);
			}
			if ( state.phase === "loading-race" && stageName !== "race" ) {
				stageName = "race";
				manifestInstalled = false;
				stage.install(
					"/assets/character-select/world-manifest.json#props=/assets/character-select/interface-models.json",
					"distance"
				);
			}
			if ( state.phase === "loading-dock" && stageName !== "dock" ) {
				stageName = "dock";
				manifestInstalled = false;
				stage.install(
					"/assets/character-select/world-manifest.json#props=/assets/character-select/interface-models.json",
					"distance"
				);
			}
			if ( state.phase === "loading-world" && stageName !== "world" ) {
				stage.clear();
				renderer.setWorld( null );
				stageName = "world";
			}
			if ( state.phase !== "world" && state.phase !== "loading-world" ) stage.step();
			if ( stageName === "title" && stage.ready() ) {
				stage.preload(
					"/assets/character-select/world-manifest.json#props=/assets/character-select/interface-models.json"
				);
			}
			// A selected race is authoritative demand; prepare it during the camera
			// transition, not before the user has chosen a race.
			if ( state.phase === "race-zoom" ) {
				stage.preload(
					"/assets/character-select/create-world-" + (race === 0 ? "europe" : "china") + "-manifest.json"
				);
			}
			const manifest = stage.manifest();
			if ( manifest && !manifestInstalled ) {
				const chosen = roster.findIndex( row => row.name === selectedCharacter );
				const keys = stageName === "dock" && chosen >= 0 ?
					[ { ...selectedDockCamera( roster[chosen]!, chosen, roster.length ), timeSeconds: 0 } ] :
					stageName === "race" ?
					manifest.createCamera!.slice( -1 ).map( key => ({ ...key, timeSeconds: 0 }) ) :
					manifest.camera;
				camera.install( {
					keys,
					target: stageName === "race" || stageName === "dock" && chosen >= 0 ?
						0 :
						manifest.cameraControllerTargetTimeSeconds,
					mode: stageName === "title" ? "intro" : "transition"
				} );
				manifestInstalled = true;
			}
			if ( state.phase !== lastPhase && manifest ) {
				if ( state.phase === "create-arrival" && manifest.createCamera ) {
					camera.install( {
						keys: manifest.createCamera,
						target: manifest.createCameraControllerTargetTimeSeconds ?? 5,
						mode: "transition"
					} );
				}
				if ( state.phase === "create-return" && manifest.createCamera ) {
					const duration = manifest.createCameraControllerTargetTimeSeconds ?? 5;
					camera.install( {
						keys: [ ...manifest.createCamera ].reverse().map( key => ({
							...key,
							timeSeconds: duration - key.timeSeconds
						}) ),
						target: duration,
						mode: "transition"
					} );
				}
				if ( state.phase === "race-zoom" ) {
					const key = manifest.createCamera!.at( -1 )!;
					camera.returnTo( {
						...key,
						position: race === 0 ?
							{ x: 168.5, y: -34.9000015, z: 639.099976 } :
							{ x: 165.800003, y: -35.0999985, z: 636.700012 }
					}, 1 );
				}
				if ( state.phase === "title-exit" ) {
					camera.install( {
						keys: [ ...manifest.camera ].reverse().map( key => ({
							...key,
							timeSeconds: 2 - key.timeSeconds
						}) ),
						target: 2,
						mode: "transition"
					} );
				}
			}
			// Departure owns one request; rendering the waiting phase must not restart it.
			if ( state.phase === "title-logout" && lastPhase !== state.phase ) send( { kind: "logout" } );
			lastPhase = state.phase;
			if ( state.phase === "customize" && manifest ) {
				const key = manifest.camera[0]!;
				camera.install( {
					keys: [ { ...key, rotation: { ...key.rotation, y: key.rotation.y + state.elapsed / 30 } } ],
					target: 0,
					mode: "transition"
				} );
			}
			const playing = [
				"intro",
				"login-reveal",
				"login",
				"login-accepted",
				"dock-arrival",
				"create-arrival",
				"create-return",
				"race-zoom",
				"title-exit"
			].includes( state.phase );
			const frame = camera.step( playing || cameraMoving ? delta : 0 );
			if ( frame?.complete ) cameraMoving = false;
			if ( frame && state.phase !== "world" && state.phase !== "loading-world" ) {
				renderer.setWorldCamera( frame.camera );
			}
			const stageReady = stage.ready();
			if ( stage.error() ) flow.fail( stage.error()! );
			// creationReady is the dock preview's own fact: exactly one preview
			// actor and every wardrobe path resident. The renderer's actor count
			// is not: the create worlds' EFP emitters and item glows share that
			// list, so "renderer draws one actor" held the load shut whenever an
			// emitter was ready in view (BR-261108-1908-A644, #3011).
			if (
				stageReady && (stageName !== "dock" || dockReady) && (stageName !== "creation" || creationReady)
			) flow.ready( state.generation );
			if ( state.phase === "intro" && frame?.complete ) flow.reveal();
			flow.advance( delta, frame?.complete );
			state = flow.snapshot();
			const progress = state.phase === "loading-create" ?
				(stage.progress() + Number( creationReady )) / 2 :
				state.phase === "loading-dock" ?
				(stage.progress() + Number( dockReady )) / 2 :
				stage.progress();
			if ( stepLoadingWatch( state, progress, stageReady, dockReady, creationReady ) ) {
				state = flow.snapshot();
			}
			return {
				...state,
				loadingStatus: stage.status(),
				loadingProgress: progress,
				entryPending,
				selectedCharacter,
				cameraMoving,
				dialog: dialog.snapshot(),
				creation: creation.snapshot(),
				race,
				status,
				hoveredRace: state.phase === "create" && pointer ?
					renderer.pickFrontendRace( pointer[0], pointer[1] ) :
					null,
				raceCenters: state.phase === "create" ? renderer.frontendRaceCenters() : [],
				...(frame ? { camera: frame.camera } : {}),
				hoveredCharacter: state.phase === "dock" && !selectedCharacter && !cameraMoving && pointer ?
					renderer.pickFrontendCharacter(
						pointer[0],
						pointer[1],
						roster.slice( 0, 4 ).map( row => row.id )
					) :
					null
			};
		},
		/*
		================
		worldReady
		================
		*/
		worldReady() {
			flow.worldReady();
		},
		/*
		================
		isRace
		================
		*/
		isRace() {
			return flow.snapshot().phase === "create";
		},
		/*
		================
		isDock
		================
		*/
		isDock() {
			return flow.snapshot().phase === "dock";
		},
		snapshot: flow.snapshot,
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( disposed ) return;
			disposed = true;
			creation.dispose();
			dialog.dispose();
			stage.dispose();
			camera.dispose();
			flow.dispose();
		}
	};
}
