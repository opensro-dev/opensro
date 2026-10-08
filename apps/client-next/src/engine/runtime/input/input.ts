/*
===========================================================================

input.ts - the display thread's input owner

Accepts raw input from the platform. Pointer drags and the wheel move the
follow camera here, at once, without waiting for the worker; keys and
focus release are queued as sequenced commands for the simulation worker
(contracts/input.ts). A camera drag therefore sends nothing across the
thread boundary.

===========================================================================
*/
import {
	initialCameraPitch,
	initialCameraYaw,
	sightMode,
	thirdPersonYaw,
	type SightMode
} from "@/engine/foundation/rendering/camera-options";
import type { InputOwner, InputCommand, RawInput } from "@/engine/contracts/input";
import { zoomCamera } from "@/engine/foundation/rendering/camera-wheel";
import { virtualKey } from "@/engine/foundation/ui/input-options";

const MAX_QUEUED_COMMANDS = 512;
// Native camera drag: radians per pixel, and the pitch rails.
const DRAG_RADIANS_PER_PIXEL = .005;
const PITCH_LIMIT = 1.0707963705062866;
const DEFAULT_DROP_KEY = 90;
const DEFAULT_BLIND_KEY = 86;
const ALT_VIRTUAL_KEY = 18;

/*
================
createInput
================
*/
export function createInput(): InputOwner {
	let queue: InputCommand[] = [], sequence = 0, failure: string | null = null;
	let dropKey = DEFAULT_DROP_KEY, dropHeld = false, blindKey = DEFAULT_BLIND_KEY, blindHeld = false;
	// Either Alt key, as GetKeyState(VK_MENU) reads it (6FCD50).
	let altLeft = false, altRight = false;
	let sight: SightMode = 0, mouseMode: 0 | 1 = 0;
	let pointer: { x: number; y: number; } | null = null,
		buttons = 0,
		yaw = initialCameraYaw(),
		pitch = initialCameraPitch(),
		distance = 80;
	return {
		/*
		================
		dropNameBinding
		================
		*/
		dropNameBinding( value: number ) {
			dropKey = value;
			dropHeld = false;
		},
		dropNamesHeld: () => dropHeld,
		/*
		================
		blindBinding
		================
		*/
		blindBinding( value: number ) {
			blindKey = value;
			blindHeld = false;
		},
		blindHeld: () => blindHeld,
		altHeld: () => altLeft || altRight,
		/*
		================
		mouseMode
		================
		*/
		mouseMode( value: 0 | 1 ) {
			if ( value !== 0 && value !== 1 ) throw Error( "Invalid mouse mode" );
			mouseMode = value;
			pointer = null;
			buttons = 0;
		},
		/*
		================
		sight
		================
		*/
		sight( value: SightMode ) {
			sight = sightMode( value );
		},
		/*
		================
		camera
		================
		*/
		camera( playerYaw?: number ) {
			if ( sight === 1 && playerYaw !== undefined ) yaw = thirdPersonYaw( playerYaw );
			return { yaw, pitch, distance };
		},
		/*
		================
		accept
		================
		*/
		accept( event: RawInput ) {
			if ( failure ) return;
			if (
				!Number.isFinite( event.timeMs ) ||
				event.kind === "pointer" &&
					(!Number.isFinite( event.x ) || !Number.isFinite( event.y ) ||
						!Number.isInteger( event.buttons )) ||
				event.kind === "wheel" && !Number.isFinite( event.delta )
			) {
				failure = "Invalid camera/input event";
				return;
			}
			// Platform capture has already filtered UI-owned events. Camera
			// response belongs to this display-thread owner, not worker ticks.
			if ( event.kind === "pointer" ) {
				const drag = mouseMode === 0 ? 2 : 1;
				if ( pointer && buttons & drag && event.buttons & drag ) {
					yaw += (event.x - pointer.x) * DRAG_RADIANS_PER_PIXEL;
					if ( sight !== 2 ) {
						pitch = Math.max(
							-PITCH_LIMIT,
							Math.min(
								PITCH_LIMIT,
								pitch + (event.y - pointer.y) * Math.fround( DRAG_RADIANS_PER_PIXEL )
							)
						);
					}
				}
				pointer = { x: event.x, y: event.y };
				buttons = event.buttons;
				return;
			}
			if ( event.kind === "wheel" ) {
				distance = zoomCamera( distance, event.delta );
				return;
			}
			if ( queue.length >= MAX_QUEUED_COMMANDS ) {
				failure = "Input queue exceeded 512 commands";
				return;
			}
			if ( event.kind === "release" ) {
				pointer = null;
				buttons = 0;
				dropHeld = false;
				blindHeld = false;
				altLeft = altRight = false;
			} else {
				if ( event.code === "AltLeft" ) altLeft = event.down;
				else if ( event.code === "AltRight" ) altRight = event.down;
				// Alt remains bindable: its native VK_MENU state must update both
				// the PvP modifier and an assigned hold action, including two keys.
				const key = virtualKey( event.code ), held = key === ALT_VIRTUAL_KEY ? altLeft || altRight : event.down;
				if ( dropKey && key === dropKey ) dropHeld = held;
				else if ( blindKey && key === blindKey ) blindHeld = held;
			}
			queue.push( { ...event, sequence: ++sequence } );
		},
		/*
		================
		drain
		================
		*/
		drain() {
			if ( !queue.length ) return null;
			const commands = queue;
			queue = [];
			return { first: commands[0]!.sequence, last: commands[commands.length - 1]!.sequence, commands };
		},
		error: () => failure
	};
}
