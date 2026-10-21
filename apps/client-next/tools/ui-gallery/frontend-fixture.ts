/*
===========================================================================
frontend-fixture.ts - mount production creation and character dialog owners

The fixture provides coherent session/frontend snapshots, not a second flow
state machine. Stage/camera navigation is intentionally reported unsupported.
===========================================================================
*/
import { createCreation } from "../../src/engine/runtime/frontend/creation/creation";
import { createCharacterDialog } from "../../src/engine/runtime/frontend/dialog/dialog";
import type { AssetOwner } from "../../src/engine/contracts/assets";
import type { CharacterRecord } from "../../src/engine/contracts/session";
import type { UiEvent } from "../../src/engine/contracts/ui";
import type { FrontendFixtureState } from "./fixture-state";

/*
================
createFrontendFixture
================
*/
export function createFrontendFixture(
	assets: AssetOwner,
	state: FrontendFixtureState,
	record: ( kind: string, detail: unknown ) => void
) {
	const creation = createCreation( assets, location.origin, command => record( "unsupported integration", command ) );
	const dialog = createCharacterDialog( command => record( "unsupported integration", command ) );
	let active = false, last = 0;
	let roster: CharacterRecord[] = [];
	const front = { generation: 2, elapsed: 1, alpha: 1, logoAlpha: 0, error: null };
	return {
		/*
		================
		open
		Snapshot entry is explicit; production owners handle subsequent edits.
		================
		*/
		open( mode: string, now: number ) {
			active = true;
			last = now;
			creation.reset();
			dialog.reset();
			state.gameplay = null;
			state.entities = [];
			state.frontend = { ...front, phase: "login" };
			state.session = {
				phase: mode === "authenticating" ? "authenticating" : "signed-out",
				revision: 2,
				servers: mode === "servers-empty" ? [] : [
					{
						id: "qa",
						name: "Local fixture",
						onlinePlayers: 10,
						capacity: 100,
						nativeServerId: 1,
						nativeFarmId: 1,
						isTest: true,
						operating: true,
						transportUrl: ""
					},
					{
						id: "full",
						name: "Full server",
						onlinePlayers: 100,
						capacity: 100,
						nativeServerId: 2,
						nativeFarmId: 1,
						isTest: true,
						operating: true,
						transportUrl: ""
					},
					{
						id: "offline",
						name: "Unavailable",
						onlinePlayers: 0,
						capacity: 100,
						nativeServerId: 3,
						nativeFarmId: 1,
						isTest: true,
						operating: false,
						transportUrl: ""
					}
				]
			};
			if ( [ "login", "servers-empty", "authenticating" ].includes( mode ) ) return;
			roster = mode === "dock-empty" ? [] : [ {
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
				deletePending: mode === "restore",
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
			state.session = { phase: "character-select", revision: 3, characters: roster };
			state.frontend = { ...front, phase: "dock", selectedCharacter: roster[0]?.name ?? "", cameraMoving: false };
			if ( mode === "delete" || mode === "restore" ) {
				dialog.open( roster[0]! );
				dialog.step( 1, undefined, roster );
				state.frontend.dialog = dialog.snapshot();
			} else if ( mode.startsWith( "customize-" ) ) {
				const race = mode.endsWith( "china" ) ? 1 : 0;
				creation.open( race, 1 );
				creation.step( 1, undefined );
				state.frontend = { ...front, phase: "customize", race, creation: creation.snapshot() };
			}
		},
		/*
		================
		event
		Same owner calls as runtime.ts; no character/session mutation is emulated.
		================
		*/
		event( value: UiEvent ) {
			if ( !active ) return false;
			if ( value.kind === "activate" && value.id.startsWith( "create:" ) ) {
				creation.action( value.id );
				return true;
			}
			if ( value.kind === "edit" && value.id.startsWith( "create:" ) && !value.composing ) {
				creation.action( value.id === "create:name" ? value.id : value.id + ":" + value.value, value.value );
			}
			if ( value.kind === "hover" && value.id?.startsWith( "create:" ) ) {
				creation.action( "create:explain", value.id.split( ":" )[1] );
			}
			if ( value.kind === "key" && value.code === "Escape" ) dialog.cancel();
			if ( value.kind === "activate" && value.id.startsWith( "dock:" ) ) {
				if ( value.id === "dock:warning-accept" ) dialog.accept();
				else if ( value.id === "dock:warning-cancel" ) dialog.cancel();
				else if ( value.id === "dock:delete" || value.id === "dock:restore" ) {
					const warning = roster[0] && dialog.open( roster[0] );
					if ( warning ) state.frontend = { ...state.frontend, status: { key: warning, suffix: "" } };
				} else record( "unsupported integration", `Frontend stage navigation: ${value.id}` );
				return true;
			}
			if (
				value.kind === "activate" && (value.id.startsWith( "frontend:" ) || value.id.startsWith( "character:" ))
			) {
				record(
					"unsupported integration",
					`Frontend stage navigation: ${value.id}. Select the corresponding gallery entry.`
				);
				return true;
			}
			return false;
		},
		/*
		================
		step
		================
		*/
		step( now: number ) {
			if ( !active ) return;
			const delta = Math.max( 0, now - last ) / 1000;
			last = now;
			creation.step( delta, undefined );
			dialog.step( delta, undefined, roster );
			state.frontend = { ...state.frontend, creation: creation.snapshot(), dialog: dialog.snapshot() };
		},
		dispose() {
			creation.dispose();
			dialog.dispose();
		}
	};
}
