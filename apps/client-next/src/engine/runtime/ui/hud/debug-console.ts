/*
===========================================================================

debug-console.ts - the debug console window: its lines and debug bytes

The console family of CGInterface_OnCommandMessage (690C40): the lines
the console shows, the debug flags each command flips and the camera
waypoint store. Moved out of ui.ts unchanged (#2530, wave 3 of #2513):
ui.ts keeps the console's text field, its slide-in phase and the
history, and reads this window's state for the overhead boards (/id),
the HUD tree's visibility (/uioff) and the sampler's debug clock.

The bytes each command flips reach their owners through the extensions
exactly as before; this module only holds the interface's copies.

===========================================================================
*/
import type { WorldClockSeed } from "@/engine/contracts/gameplay";
import type { SessionCommand } from "@/engine/contracts/session";
import {
	GRAPHIC_OPTION_CHARACTERS,
	GRAPHIC_OPTION_GROUND,
	GRAPHIC_OPTION_LEVEL_OF_DETAIL,
	GRAPHIC_OPTION_MAP_OBJECTS,
	GRAPHIC_OPTION_SKY,
	GRAPHIC_OPTION_WIRE_FRAME
} from "@/engine/foundation/rendering/graphic-options";
import {
	cameraWaypointSaveText,
	createCameraWaypointStore,
	resetCameraWaypoints,
	type CameraWaypointStore
} from "@/engine/foundation/ui/camera-waypoints";
import {
	commandFloat,
	commandInteger,
	DEBUG_COMMAND_CAMERA_SPEED,
	DEBUG_COMMAND_CLEAR,
	DEBUG_COMMAND_COLON,
	DEBUG_COMMAND_COLON_OFF,
	DEBUG_COMMAND_CURSOR,
	DEBUG_COMMAND_DAY,
	DEBUG_COMMAND_DEBUG,
	DEBUG_COMMAND_EQUIP,
	DEBUG_COMMAND_FAST,
	DEBUG_COMMAND_FRAME,
	DEBUG_COMMAND_FREE_CAMERA,
	DEBUG_COMMAND_GET_POSITION,
	DEBUG_COMMAND_HIDE_ME,
	DEBUG_COMMAND_ID,
	DEBUG_COMMAND_ITEM,
	DEBUG_COMMAND_MAKE_OBJECT,
	DEBUG_COMMAND_MESSAGE_CLEAR,
	DEBUG_COMMAND_NIGHT,
	DEBUG_COMMAND_NULL,
	DEBUG_COMMAND_PLAYER_COUNT,
	DEBUG_COMMAND_POSITION,
	DEBUG_COMMAND_POWER,
	DEBUG_COMMAND_RAIN,
	DEBUG_COMMAND_SHOW_ME,
	DEBUG_COMMAND_SNOW,
	DEBUG_COMMAND_SOUND,
	DEBUG_COMMAND_TIME_MINUS,
	DEBUG_COMMAND_TIME_OFF,
	DEBUG_COMMAND_TIME_ON,
	DEBUG_COMMAND_TIME_PLUS,
	DEBUG_COMMAND_TIME_SET,
	DEBUG_COMMAND_UI_OFF,
	DEBUG_COMMAND_UI_ON,
	DEBUG_COMMAND_WAYPOINT_CLEAR,
	DEBUG_COMMAND_WAYPOINT_GO,
	DEBUG_COMMAND_WAYPOINT_POP,
	DEBUG_COMMAND_WAYPOINT_SAVE,
	DEBUG_COMMAND_WAYPOINT_STOP,
	DEBUG_COMMAND_ZOOM,
	DEBUG_COMMAND_WIRE_FRAME,
	DEBUG_COMMAND_GROUND,
	DEBUG_COMMAND_SKY,
	DEBUG_COMMAND_MAP_OBJECTS,
	DEBUG_COMMAND_CHARACTERS,
	DEBUG_COMMAND_LEVEL_OF_DETAIL,
	DEBUG_COMMAND_SET_FIELD_OF_VIEW,
	DEBUG_COMMAND_SET_SPEED,
	debugCameraSpeedValue,
	debugFastSpeedValue,
	debugFieldOfView,
	debugPosition,
	debugSetSpeedValue,
	type DebugCommand
} from "@/engine/foundation/ui/debug-commands";
import {
	createDebugClock,
	DEBUG_CLOCK_STEP_MINUTES,
	debugClockHold,
	debugClockResume,
	debugClockSetFraction,
	debugClockSetHours,
	debugClockStep,
	type DebugClock
} from "@/engine/foundation/gameplay/debug-clock";

/*
================
DebugConsoleState

The window's record. ui.ts reads consoleRows, debugShowIds,
debugUiVisible, debugClock and cameraWaypoints through it.
================
*/
export interface DebugConsoleState {
	consoleRows: string[];
	// +0x7BC, the /Debug flag: the native debug-message switch (690C40 case 0).
	debugMessages: boolean;
	// The interface timer's debug state (690C40 ids 100-109): the sampler
	// reads it through the handle's debugClockSeed.
	debugClock: DebugClock;
	// The camera family's debug bytes (690C40 ids 110-135): the
	// interface's zoom rail (+0x3CF, standing from its constructor), its
	// free-camera byte (+0x3CE), its camera-apply byte (+0x3CC, also
	// standing from its constructor) and the cursor manager's visibility
	// (+0x20). Each owner applies its own through the extensions.
	debugZoomRail: boolean;
	debugFreeCamera: boolean;
	debugCursorVisible: boolean;
	// The camera-waypoint family (690C40 ids 130-134, #1716): the
	// interface's keyframe track (+0x3FC) and its play byte (+0x3CD).
	// The store is this window's, as natively; the camera owner gets a
	// copy of the records and the byte through the extensions, and the
	// playback stepping is its own. The records arrive with the HUD's
	// cameradata asset (cameraWaypointsLoaded in ui.ts): nothing native
	// ever loads the track, so the published rows are the port's
	// content source (issue #1716).
	cameraWaypoints: CameraWaypointStore;
	// The camera family's kept values: the free-camera move speed
	// (+0x7B0, seeded 100, case 136; the camera owner holds a copy for
	// the free-fly arm, #1787), the snapped second speed (+0x7C0,
	// seeded 50, case 111), the frame byte (engine global +0x4A, seeded
	// clear, case 117), the /id byte (0xCCB5D8, seeded standing,
	// case 137) and the local
	// player's written speed (+0x658, case 600; 100 is the scale's
	// neutral point, the value the word 10 writes). The movement speed
	// is the server's, so it and the second speed are kept as written
	// for the consumers in #1717. The /id byte
	// is read: while it is clear the overhead boards and their kind-4
	// pass are skipped (85F5DC), and the character owner holds a copy
	// for the hit damage numbers (8D5856, #1785).
	debugCameraSpeed: number;
	debugFast: number;
	debugFrameStats: boolean;
	debugShowIds: boolean;
	debugMoveSpeed: number;
	// The render family's SWorld graphic options (690C40 ids 150-155):
	// SWorld_Construct seeds wireframe clear and the five others standing,
	// each command negates one, and the renderer applies the result. The
	// copies here are the interface's negation state, as with the zoom
	// rail; the applied state lives with the renderer.
	debugWireFrame: boolean;
	debugGround: boolean;
	debugSky: boolean;
	debugMapObjects: boolean;
	debugCharacters: boolean;
	debugLevelOfDetail: boolean;
	// The mission window's visible flag (CIFWnd +0x6D, written through
	// CIFWnd_SetVisibleFlag 9FCD70 by 690C40 cases 115/116): while it is
	// down the frame carries only the console's own quads and controls -
	// the console belongs to the interface, not the hidden window tree,
	// exactly as the native stays able to read /uion.
	debugUiVisible: boolean;
}

/*
================
DebugConsoleExtensions

The renderer and camera hooks the console drives: the members of ui.ts's
UiExtensions it calls, restated here because a child module never imports
its owner (tools/verify-ownership.mjs). ui.ts passes its own extensions,
which match this shape structurally.
================
*/
export interface DebugConsoleExtensions {
	playSoundByName?: ( name: string ) => void;
	saveCameraWaypoints?: ( text: string ) => void;
	setCameraCollision?: ( collision: boolean ) => void;
	setCameraPlaying?: ( playing: boolean ) => void;
	setCameraWaypoints?: (
		keyframes: readonly import("@/engine/foundation/ui/camera-waypoints").CameraWaypoint[]
	) => void;
	setCameraZoomRail?: ( capped: boolean ) => void;
	setCursorVisible?: ( visible: boolean ) => void;
	setDamageNumbers?: ( shown: boolean ) => void;
	setFreeCamera?: ( free: boolean ) => void;
	setFreeCameraSpeed?: ( speed: number ) => void;
	setRenderMode?: ( mode: number, enabled: boolean ) => void;
	setWorldFieldOfView?: ( degrees: number ) => void;
}

/*
================
DebugConsolePorts

What the console reaches outside itself: the gameplay lane, the bag's
double click, the extensions, and the two per-frame values it reads
(the world clock seed and the quickslot clock).
================
*/
export interface DebugConsolePorts {
	readonly sendGameplay: ( command: Extract<SessionCommand, { kind: "gameplay"; }>["command"] ) => void;
	readonly useInventorySlot: ( slot: number ) => boolean;
	readonly extensions: DebugConsoleExtensions;
	readonly worldClock: () => WorldClockSeed | undefined;
	readonly quickslotTime: () => number;
}

/*
================
createDebugConsole
================
*/
export function createDebugConsole( ports: DebugConsolePorts ) {
	// Plain property reads, as createUi takes its ports: the execution-flow
	// analyzer follows these, not destructuring.
	const sendGameplay = ports.sendGameplay,
		useInventorySlot = ports.useInventorySlot,
		extensions = ports.extensions;
	const state: DebugConsoleState = {
		consoleRows: [],
		debugMessages: false,
		debugClock: createDebugClock(),
		debugZoomRail: true,
		debugFreeCamera: false,
		debugCursorVisible: true,
		cameraWaypoints: createCameraWaypointStore(),
		debugCameraSpeed: 100,
		debugFast: 50,
		debugFrameStats: false,
		debugShowIds: true,
		debugMoveSpeed: 100,
		debugWireFrame: false,
		debugGround: true,
		debugSky: true,
		debugMapObjects: true,
		debugCharacters: true,
		debugLevelOfDetail: true,
		debugUiVisible: true
	};
	/*
	================
	appendConsoleLine
	================
	*/
	function appendConsoleLine( value: string ) {
		state.consoleRows = [ ...state.consoleRows.slice( -99 ), value ];
	}
	/*
	================
	runDebugCommand

	CGInterface_OnCommandMessage (690C40) for the console family, the position
	pair, bag-slot use and FOV. The weather rows write the worker's weather
	slot (the sampler's state, not this window's); the time rows drive the
	local debug clock the sampler reads through debugClockSeed. The camera
	rows flip this window's debug bytes and hand each to its owner; the
	shown byte (112/113) rides the gameplay lane to the entity owner, and
	the mission window's visible flag (115/116) is this window's own. 137
	gates this window's overhead boards and hands the byte to the
	character owner for the damage numbers (#1785). The
	remaining camera rows (111/117/136) and the 600 family keep their
	values on this window for consumers the port does not have yet (#1717);
	601 frames the named item as a packet the client hands itself (see
	the case); the port builds nothing. 602 is the native table's shared exit.
	The camera-waypoint rows (130-134) drive this window's waypoint store
	and forward the play byte to the camera owner (#1716). Every row the
	table names is consumed here, as natively.
	================
	*/
	function runDebugCommand( command: DebugCommand ) {
		switch ( command.id ) {
			case DEBUG_COMMAND_DEBUG:
				// Case 0 flips +0x7BC and reports the new state.
				state.debugMessages = !state.debugMessages;
				appendConsoleLine( state.debugMessages ? "DebugMsg On" : "DebugMsg Off" );
				break;
			case DEBUG_COMMAND_MESSAGE_CLEAR:
				// Case 1 empties the console's lines, the command's echo included.
				state.consoleRows = [];
				break;
			case DEBUG_COMMAND_POSITION: {
				// Case 2: "/Pos region,x,y,z" walks the local player (or its
				// mount) to the destination, the ground-click walk, not a
				// teleport. The native destination carries no heading.
				const destination = debugPosition( command.args );
				if ( destination ) sendGameplay( { kind: "move", destination: { ...destination, angle: 0 } } );
				break;
			}
			case DEBUG_COMMAND_GET_POSITION:
				// Case 3 flips the +CEAD84 byte and writes it as the local
				// player's body visual state (85EC00): berserk look on or off.
				sendGameplay( { kind: "debug-body-visual" } );
				break;
			case DEBUG_COMMAND_NULL:
				// Port-only, not native: case 4 writes through a null pointer to
				// crash the client on purpose; the port ignores the line.
				break;
			case DEBUG_COMMAND_PLAYER_COUNT:
				// Id 0 is below the 691CC4 table: 690C40 does nothing with it.
				break;
			case DEBUG_COMMAND_ITEM:
				// The 0x190 branch: exactly one word, read by wcstol, then
				// CIFInventory_ExecuteItemAction(slot, -1, -1) as a double click.
				if ( command.args.length === 1 ) useInventorySlot( commandInteger( command.args[0]! ) );
				break;
			case DEBUG_COMMAND_POWER:
				// 99: one word; the weather intensity alone (mode kept).
				if ( command.args.length === 1 ) {
					sendGameplay( { kind: "debug-weather", amount: commandInteger( command.args[0]! ) } );
				}
				break;
			case DEBUG_COMMAND_RAIN:
				// 105: one word; rain at the word's intensity.
				if ( command.args.length === 1 ) {
					sendGameplay( { kind: "debug-weather", mode: 2, amount: commandInteger( command.args[0]! ) } );
				}
				break;
			case DEBUG_COMMAND_SNOW:
				// 106: one word; snow at the word's intensity.
				if ( command.args.length === 1 ) {
					sendGameplay( { kind: "debug-weather", mode: 3, amount: commandInteger( command.args[0]! ) } );
				}
				break;
			case DEBUG_COMMAND_CLEAR:
				// 107: clear weather at full intensity, no word read.
				sendGameplay( { kind: "debug-weather", mode: 1, amount: 255 } );
				break;
			case DEBUG_COMMAND_TIME_OFF:
				// 100: hold the interface timer where it stands (+0x7AD).
				state.debugClock = debugClockHold( state.debugClock, ports.worldClock(), ports.quickslotTime() );
				break;
			case DEBUG_COMMAND_TIME_ON:
				// 101: let the timer run again from the held time.
				state.debugClock = debugClockResume( state.debugClock, ports.quickslotTime() );
				break;
			case DEBUG_COMMAND_TIME_SET:
				// 102: one word, an atof hour the timer restarts from.
				if ( command.args.length === 1 ) {
					state.debugClock = debugClockSetHours(
						state.debugClock,
						ports.worldClock(),
						ports.quickslotTime(),
						commandFloat( command.args[0]! )
					);
				}
				break;
			case DEBUG_COMMAND_DAY:
				// 103: the day fraction set to 0.5.
				state.debugClock = debugClockSetFraction(
					state.debugClock,
					ports.worldClock(),
					ports.quickslotTime(),
					0.5
				);
				break;
			case DEBUG_COMMAND_NIGHT:
				// 104: the day fraction set to 1.0.
				state.debugClock = debugClockSetFraction(
					state.debugClock,
					ports.worldClock(),
					ports.quickslotTime(),
					1.0
				);
				break;
			case DEBUG_COMMAND_TIME_PLUS:
				// 108: "/+" steps a held clock BACK (the native sign quirk).
				state.debugClock = debugClockStep( state.debugClock, -DEBUG_CLOCK_STEP_MINUTES );
				break;
			case DEBUG_COMMAND_TIME_MINUS:
				// 109: "/-" steps a held clock forward.
				state.debugClock = debugClockStep( state.debugClock, DEBUG_CLOCK_STEP_MINUTES );
				break;
			case DEBUG_COMMAND_ZOOM:
				// Case 0x10 flips +0x3CF: the 150-unit wheel rail (67CC3C)
				// stands or falls. The native prints nothing.
				state.debugZoomRail = !state.debugZoomRail;
				extensions.setCameraZoomRail?.( state.debugZoomRail );
				break;
			case DEBUG_COMMAND_FAST:
				// Case 17 (0x691367): snap +0x7C0 between its 50 and 240
				// presets. Nothing native reads the field; the value is
				// kept as the handler leaves it. The native prints nothing.
				state.debugFast = debugFastSpeedValue( state.debugFast );
				break;
			case DEBUG_COMMAND_SHOW_ME:
				// Case 0x12 writes the local player's shown byte
				// (CICPlayer +0xB8) to 1. The native prints nothing.
				sendGameplay( { kind: "debug-local-visible", visible: true } );
				break;
			case DEBUG_COMMAND_HIDE_ME:
				// Case 0x13 writes the shown byte to 0: the local
				// presentation (body and name board) is skipped. The
				// native prints nothing.
				sendGameplay( { kind: "debug-local-visible", visible: false } );
				break;
			case DEBUG_COMMAND_FREE_CAMERA:
				// Case 0x14 flips +0x3CE and reports the new state: while it
				// stands the camera keeps its last eye and target (68FAD0,
				// 68F830); clearing it re-aims at the player.
				state.debugFreeCamera = !state.debugFreeCamera;
				extensions.setFreeCamera?.( state.debugFreeCamera );
				appendConsoleLine( state.debugFreeCamera ? "Free Camera On" : "Free Camera Off" );
				break;
			case DEBUG_COMMAND_UI_OFF:
				// Case 0x15 writes the mission window's visible flag
				// (CIFWnd +0x6D) to 0 through 9FCD70: the HUD tree
				// hides and the hovered window is released; the console
				// stays, so /uion can be typed. The native prints nothing.
				state.debugUiVisible = false;
				break;
			case DEBUG_COMMAND_UI_ON:
				// Case 0x16 writes the flag back to 1. The native
				// prints nothing.
				state.debugUiVisible = true;
				break;
			case DEBUG_COMMAND_FRAME:
				// Case 23 flips the engine global's frame byte (+0x4A),
				// the frame-statistics switch; a GM string arm flips the
				// same byte. The port has no readout for it to gate yet
				// (#1717); the byte is kept. The native prints nothing.
				state.debugFrameStats = !state.debugFrameStats;
				break;
			case DEBUG_COMMAND_COLON:
				// Case 0x76 writes +0x3CC back to 1: the clamp casts
				// the eye-to-target segment again (68FAD0). The native
				// prints nothing.
				extensions.setCameraCollision?.( true );
				break;
			case DEBUG_COMMAND_COLON_OFF:
				// Case 0x77 writes +0x3CC to 0: the clamp takes its
				// no-hit arm and the camera keeps the requested
				// distance; the follow still tracks. Prints nothing.
				extensions.setCameraCollision?.( false );
				break;
			case DEBUG_COMMAND_WAYPOINT_CLEAR:
				// Case 130: the store reset (CRenderContext_Reset on
				// the +0x3FC track), and the same block's next write
				// clears the play byte (+0x3CD): the stop row (133)
				// enters at that write, so clear and stop share it.
				// The records, the clock and the byte all go. The
				// native prints nothing.
				resetCameraWaypoints( state.cameraWaypoints );
				extensions.setCameraWaypoints?.( state.cameraWaypoints.keyframes );
				extensions.setCameraPlaying?.( false );
				break;
			case DEBUG_COMMAND_WAYPOINT_POP:
			case DEBUG_COMMAND_WAYPOINT_GO:
				// Cases 131/132 are one native block: the free-camera
				// byte (+0x3CE) must stand and the store be non-empty;
				// then the play byte (+0x3CD) is written and the track
				// clock zeroed. Either gate failing leaves the byte
				// clear. The camera owner holds the byte's copy. The
				// native prints nothing.
				if ( state.debugFreeCamera && state.cameraWaypoints.keyframes.length > 0 ) {
					state.cameraWaypoints.clockSeconds = 0;
					extensions.setCameraPlaying?.( true );
				}
				break;
			case DEBUG_COMMAND_WAYPOINT_STOP:
				// Case 133: the play byte is cleared; the camera stays
				// where playback left it until the free-camera byte
				// also falls. The native prints nothing.
				extensions.setCameraPlaying?.( false );
				break;
			case DEBUG_COMMAND_WAYPOINT_SAVE:
				// Case 134: the whole store written in the native line
				// form - natively to c:\cw.txt, here the downloaded
				// cw.txt the platform writes (#1716). Prints nothing.
				extensions.saveCameraWaypoints?.( cameraWaypointSaveText( state.cameraWaypoints ) );
				break;
			case DEBUG_COMMAND_CURSOR:
				// Case 0x1E: SetVisible(![cursor+0x20]) - the pointer shows
				// or goes away (A15A70), silently.
				state.debugCursorVisible = !state.debugCursorVisible;
				extensions.setCursorVisible?.( state.debugCursorVisible );
				break;
			case DEBUG_COMMAND_CAMERA_SPEED: {
				// Case 136 writes +0x7B0 from one word clamped to
				// 1..500: the free-fly arm's translation speed (690146
				// reads it at 6902AD and 69040B), forwarded to the
				// camera owner (#1787). No print.
				const speed = debugCameraSpeedValue( command.args );
				if ( speed !== null ) {
					state.debugCameraSpeed = speed;
					extensions.setFreeCameraSpeed?.( speed );
				}
				break;
			}
			case DEBUG_COMMAND_ID:
				// Case 137 flips the /id byte (0xCCB5D8, sete at 691678).
				// Its readers draw while it stands: 85F5DC (the name
				// board and the kind-4 overhead pass, read below in the
				// overhead composition) and 8D5856 (a hit's damage
				// number, the character owner's). The native prints
				// nothing.
				state.debugShowIds = !state.debugShowIds;
				extensions.setDamageNumbers?.( state.debugShowIds );
				break;
			case DEBUG_COMMAND_WIRE_FRAME:
				// 150 (691686) negates graphic option 0x12: while it stands
				// SWorld_Render fills wireframe (8B9C28). The port's device
				// has no line topology, so the byte is carried unapplied.
				// The native prints nothing.
				state.debugWireFrame = !state.debugWireFrame;
				extensions.setRenderMode?.( GRAPHIC_OPTION_WIRE_FRAME, state.debugWireFrame );
				break;
			case DEBUG_COMMAND_GROUND:
				// 151 (6916BF) negates option 0x13: cleared, the terrain
				// and ground-decal pass returns whole (8ABFDE).
				state.debugGround = !state.debugGround;
				extensions.setRenderMode?.( GRAPHIC_OPTION_GROUND, state.debugGround );
				break;
			case DEBUG_COMMAND_SKY:
				// 152 (6916F8) negates option 0x14: cleared, the sky pass
				// in CWorld_RenderScenePasses is skipped (8B32F9, 8B3489).
				state.debugSky = !state.debugSky;
				extensions.setRenderMode?.( GRAPHIC_OPTION_SKY, state.debugSky );
				break;
			case DEBUG_COMMAND_MAP_OBJECTS:
				// 153 (691731) negates option 0x15: cleared, the model
				// pass returns whole (SWorld_RenderPostModelPass_8AA750).
				state.debugMapObjects = !state.debugMapObjects;
				extensions.setRenderMode?.( GRAPHIC_OPTION_MAP_OBJECTS, state.debugMapObjects );
				break;
			case DEBUG_COMMAND_CHARACTERS:
				// 154 (69176A) negates option 0x16: its proven readers are
				// the character shadow and dungeon passes (8B130B,
				// 8B1B4E); the port clears every character draw with it.
				state.debugCharacters = !state.debugCharacters;
				extensions.setRenderMode?.( GRAPHIC_OPTION_CHARACTERS, state.debugCharacters );
				break;
			case DEBUG_COMMAND_LEVEL_OF_DETAIL:
				// 155 (6917A3) negates option 0x17: cleared, every cell
				// takes LOD 0 in place of its distance band (8B7F1D).
				state.debugLevelOfDetail = !state.debugLevelOfDetail;
				extensions.setRenderMode?.( GRAPHIC_OPTION_LEVEL_OF_DETAIL, state.debugLevelOfDetail );
				break;
			case DEBUG_COMMAND_SET_FIELD_OF_VIEW: {
				const degrees = debugFieldOfView( command.args );
				if ( degrees !== null ) extensions.setWorldFieldOfView?.( degrees );
				break;
			}
			case DEBUG_COMMAND_SET_SPEED:
				// Case 600 writes the local player's (or its mount's)
				// speed field (+0x658) as the word times ten, clamped
				// to 10..500. The port's movement is the server's, so
				// the value is kept, not applied (#1717); the GM
				// family's /SETSPEED stays the server-side speed lever.
				// The native prints nothing.
				state.debugMoveSpeed = debugSetSpeedValue( command.args );
				break;
			case DEBUG_COMMAND_EQUIP:
				// Case 601 builds the named item's descriptor, frames
				// it as a packet stream headed by opcode 0x3314 and
				// self-dispatches it through the client's own packet
				// handler (690BD0); the stream is destroyed after and
				// nothing is transmitted. The port's inventory is the
				// server's snapshot, so there is nothing for the
				// crafted packet to land on; the line is consumed and
				// nothing is built (#1717).
				break;
			case DEBUG_COMMAND_MAKE_OBJECT:
				// 602 has no native case: the over-400 table sends it
				// to the shared exit, so the handler does nothing at
				// all. The line is consumed the same way here.
				break;
			case DEBUG_COMMAND_SOUND:
				// Case 603 hands its first word to the sound manager's
				// play-by-name; the audio owner answers the name (a
				// name with no authored row stays silent, as a native
				// label-map miss does). Further words are not read.
				// With no word the native still calls through, with an
				// unset argument - whatever the stack held becomes the
				// name - so there is no defined sound to reproduce;
				// the port deliberately plays nothing for a wordless
				// line instead of inventing one.
				if ( command.args.length >= 1 ) extensions.playSoundByName?.( command.args[0]! );
				break;
		}
	}
	return { state, appendConsoleLine, runDebugCommand };
}
