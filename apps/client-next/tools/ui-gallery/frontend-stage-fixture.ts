/*
===========================================================================
frontend-stage-fixture.ts - production frontend flow, stage and camera admission

Synthetic session publications feed production stage, camera, character assembly
and readiness owners. No character-operation success or server is emulated.
===========================================================================
*/
import { createFrontend } from "../../src/engine/runtime/frontend/frontend";
import { createCharacterPresentation } from "../../src/engine/runtime/characters/characters";
import { createPresentationRandom } from "../../src/engine/runtime/random/random";
import type { CharacterRecord } from "../../src/engine/contracts/session";
import type { AssetOwner } from "../../src/engine/contracts/assets";
import type { Renderer } from "../../src/engine/contracts/runtime";
import type { UiEvent } from "../../src/engine/contracts/ui";
import type { RecipeContext } from "./extended";
import type { FrontendFixtureState } from "./fixture-state";

/*
================
createStageFixture
================
*/
export function createStageFixture(
	assets: AssetOwner,
	renderer: Renderer,
	state: FrontendFixtureState,
	record: ( kind: string, detail: unknown ) => void
) {
	let owner: ReturnType<typeof createFrontend> | null = null;
	let characters: ReturnType<typeof createCharacterPresentation> | null = null;
	let worldActor = false;
	const roster: CharacterRecord[] = [ {
		id: 1,
		name: "Reviewer",
		level: 40,
		raceIndex: 0,
		gender: 0,
		figureIndex: 0,
		heightIndex: 1,
		volumeIndex: 1,
		weaponIndex: 1,
		protectorIndex: 1,
		armorSelected: true,
		weaponSelected: true,
		deletePending: false,
		maxHp: 1000,
		maxMp: 1000,
		visualLoadout: {
			modelCodename: "CHAR_EU_MAN_ADVENTURER",
			items: [],
			avatars: [],
			animationSetName: "",
			heightScale: 1,
			volumeScale: 1
		}
	} ];
	return {
		active: () => !!owner,
		pointer: ( point: readonly [number, number] | null ) => owner?.pointer( point ),
		error: () => owner?.snapshot().error ?? characters?.error() ?? null,
		readiness: () =>
			characters ?
				({
					dock: characters.dockReady(),
					preview: characters.previewReady(),
					actors: renderer.characterActors().map( row => ({
						gid: row.gid,
						pose: row.pose,
						opacity: row.opacity,
						scale: row.scale,
						height: row.height
					}) ),
					camera: renderer.presentationCamera(),
					stats: renderer.characterStats()
				}) :
				null,
		/*
		================
		openWorldActor

		Only the local synthetic entity is presented; the production renderer uses it
		for Inventory, Skin and Mall paperdolls over the diagnostic backdrop.
		================
		*/
		async openWorldActor( context: Pick<RecipeContext, "step" | "advance"> ) {
			if ( !state.gameplay ) throw Error( "Actor recipe requires a world snapshot" );
			const game = state.gameplay;
			characters?.dispose();
			characters = createCharacterPresentation(
				assets,
				renderer,
				location.origin,
				() => {},
				createPresentationRandom( 2 )
			);
			worldActor = true;
			// An empty world still supplies the production renderer's region basis.
			renderer.setWorld( { id: "qa-actor-stage", originRegion: 0x6a48, groups: [], warnings: [] } );
			renderer.setWorldCamera( {
				originRegion: 0x6a48,
				eye: [ 1000, 80, 850 ],
				target: [ 1000, 0, 1000 ],
				up: [ 0, 1, 0 ],
				fov: Math.PI / 4,
				near: 1,
				far: 500000
			} );
			state.entities = state.entities.map( ( row ) =>
				row.gid === game.localGid ?
					{ ...row, refObjId: 1907, bodyShape: 0x22, equipment: [], appearanceState: [ 0, 0, 0 ] } :
					row
			);
			for ( let i = 0; i < 1800; i++ ) {
				context.advance( 100 );
				await context.step();
				if ( characters.error() ) throw Error( characters.error()! );
				if ( characters.ready( state.gameplay.localGid ) ) {
					context.advance( 1000 );
					await context.step();
					return;
				}
			}
			throw Error( "Local production actor did not become ready" );
		},
		mallOutfit( ...args: Parameters<ReturnType<typeof createCharacterPresentation>["mallOutfit"]> ) {
			if ( worldActor ) characters?.mallOutfit( ...args );
		},
		mallPreviewState: () => worldActor ? characters?.mallPreviewState() : undefined,
		/*
		================
		open
		================
		*/
		async open( mode: boolean | string, context: Pick<RecipeContext, "step" | "advance"> ) {
			worldActor = false;
			owner?.dispose();
			characters?.dispose();
			characters = createCharacterPresentation(
				assets,
				renderer,
				location.origin,
				() => {},
				createPresentationRandom( 2 )
			);
			owner = createFrontend(
				assets,
				renderer,
				location.origin,
				command => record( "unsupported integration", command )
			);
			state.gameplay = null;
			state.entities = [];
			const populated = typeof mode === "string" &&
				[ "populated", "selected", "delete", "restore" ].includes( mode );
			state.session = {
				phase: mode === "login" ? "signed-out" : "character-select",
				revision: 3,
				characters: populated ? roster.map( row => ({ ...row, deletePending: mode === "restore" }) ) : []
			};
			for ( let i = 0; i < 1800; i++ ) {
				context.advance( 200 );
				await context.step();
				const phase = owner.snapshot().phase;
				if ( characters.error() ) throw Error( characters.error()! );
				if ( phase === "failed" ) throw Error( owner.snapshot().error ?? "Frontend stage failed" );
				if ( mode === "login" && phase === "login" ) {
					context.advance( 1000 );
					await context.step();
					return;
				}
				if ( phase === "dock" ) {
					if ( !renderer.worldStats().residentGroups ) throw Error( "Dock has no resident production scene" );
					if ( mode === false || mode === "populated" ) return;
					if ( populated ) {
						if ( !state.frontend.selectedCharacter ) owner.select( "Reviewer" );
						else if ( !state.frontend.cameraMoving ) {
							if ( mode === "delete" || mode === "restore" ) owner.dialogAction( "dock:" + mode );
							await context.step();
							return;
						}
					} else owner.create();
				}
				if ( phase === "create" ) {
					if ( state.frontend.raceCenters?.length !== 2 ) {
						throw Error( "Production race figures are not both projected" );
					}
					if ( mode === true ) return;
					owner.race( mode === "china" ? 1 : 0, 1 );
				}
				if ( phase === "customize" && characters.previewReady() ) {
					context.advance( 1000 );
					await context.step();
					return;
				}
			}
			throw Error( "Frontend stage admission timed out: " + owner.snapshot().phase );
		},
		/*
		================
		step
		================
		*/
		step( now: number ) {
			if ( worldActor && characters && state.gameplay ) {
				const game = state.gameplay;
				characters.step(
					state.entities.filter( ( row ) => row.gid === game.localGid ),
					state.gameplay,
					now / 1000,
					now
				);
				return;
			}
			if ( !owner || !characters ) return;
			state.frontend = owner.step( state.session, now, characters.dockReady(), characters.previewReady() );
			const front = owner.snapshot();
			const preview = [ "loading-create", "customize", "create-exit" ].includes( front.phase );
			const dock = [
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
			].includes( front.phase );
			renderer.setCharacterPreview( preview ? front.creation?.camera ?? null : null );
			characters.step(
				[],
				null,
				now / 1000,
				now,
				undefined,
				dock ? state.session.characters ?? [] : undefined,
				preview ? front.creation : null,
				[ "create", "create-return", "race-zoom" ].includes( front.phase )
			);
		},
		/*
		================
		event
		Same frontend dispatch as runtime.ts; outgoing operations remain captured.
		================
		*/
		event( event: UiEvent ) {
			if ( !owner ) return false;
			if ( event.kind === "activate" ) {
				if ( event.id === "frontend:create" ) owner.create();
				else if ( event.id === "frontend:back" ) owner.back();
				else if ( event.id === "frontend:leave" ) owner.leave();
				else if ( event.id === "dock:back" ) owner.deselect();
				else if ( event.id.startsWith( "dock:" ) ) owner.dialogAction( event.id );
				else if ( event.id.startsWith( "create:" ) ) owner.creationAction( event.id );
				else if ( event.id === "enter" ) {
					if ( owner.start() ) record( "unsupported integration", "Character entry requested" );
				} else if ( event.id === "frontend:race-europe" || event.id === "frontend:race-china" ) {
					owner.race( event.id.endsWith( "europe" ) ? 0 : 1, 1 );
				} else return false;
				return true;
			}
			if ( event.kind === "edit" && event.id.startsWith( "create:" ) ) {
				if ( !event.composing ) {
					owner.creationAction(
						event.id === "create:name" ? event.id : event.id + ":" + event.value,
						event.id === "create:name" ? event.value : undefined
					);
				}
				return true;
			}
			if ( event.kind === "hover" && event.id?.startsWith( "create:" ) ) {
				owner.creationAction( "create:explain", event.id.split( ":" )[1] );
			}
			if ( event.kind === "key" && event.code === "Escape" ) owner.dialogAction( "dock:warning-cancel" );
			return false;
		},
		/*
		================
		click
		================
		*/
		click( x: number, y: number ) {
			if ( owner?.isDock() ) {
				const rows = state.session.characters ?? [];
				const gid = renderer.pickFrontendCharacter( x, y, rows.map( ( row: CharacterRecord ) => row.id ) );
				const row = rows.find( ( row: CharacterRecord ) => row.id === gid );
				if ( row ) owner.select( row.name );
				return true;
			}
			if ( !owner?.isRace() ) return false;
			const race = renderer.pickFrontendRace( x, y );
			if ( race !== null ) {
				owner.race( race, 1 );
			}
			return true;
		},
		dispose() {
			owner?.dispose();
			owner = null;
			characters?.dispose();
			characters = null;
		}
	};
}
