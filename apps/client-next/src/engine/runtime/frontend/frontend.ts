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
import { selectedDockCamera } from "@/engine/foundation/rendering/dock-camera";
import type { AssetOwner } from "@/engine/contracts/assets";
import type { Renderer } from "@/engine/contracts/runtime";
import type { SessionState, CharacterOperationCommand } from "@/engine/contracts/session";
/*
================
createFrontend
================
*/
export function createFrontend(
	assets: AssetOwner,
	renderer: Renderer,
	base: string,
	send: ( command: CharacterOperationCommand | { kind: "logout"; } ) => void = () => {},
	messageSound: () => void = () => {}
) {
	const flow = createFrontendFlow(),
		camera = createFrontendCamera(),
		stage = createFrontendStage( assets, renderer, base );
	const dialog = createCharacterDialog( send );
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
	// Native CPSTitle 0x748630 disables terrain LOD; 0x741B80 restores it on release.
	stage.install( "/assets/title/constantinople/manifest.json", "full" );
	return {
		setTerrainNormals: stage.setTerrainNormals,
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
				stage.install( "/assets/title/constantinople/manifest.json", "full" );
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
			if (
				(entering || entryPending || flow.snapshot().phase === "loading-world") && session?.error &&
				session.revision > entryRevision && session.phase === "character-select"
			) {
				entryPending = false;
				entering = false;
				flow.entryRejected();
				status = characterStatus( 2 );
				statusUntil = now + 15000;
				messageSound();
			}
			if ( session?.phase === "world" ) {
				entering = false;
				if ( entryPending ) {
					entryPending = false;
					flow.start();
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
			if ( stage.error() ) flow.fail( stage.error()! );
			if (
				stage.ready() && (stageName !== "dock" || dockReady) &&
				(stageName !== "creation" || creationReady && renderer.characterStats().actors === 1)
			) flow.ready( state.generation );
			if ( state.phase === "intro" && frame?.complete ) flow.reveal();
			flow.advance( delta, frame?.complete );
			state = flow.snapshot();
			return {
				...state,
				loadingStatus: stage.status(),
				loadingProgress: state.phase === "loading-create" ?
					(stage.progress() + Number( creationReady )) / 2 :
					state.phase === "loading-dock" ?
					(stage.progress() + Number( dockReady )) / 2 :
					stage.progress(),
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
