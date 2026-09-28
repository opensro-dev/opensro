/*
===========================================================================

creation.ts - character draft, validation and confirmation lifecycle

Owns the editable draft and serializes name checks and creation requests.
Equipment choices follow race and weapon compatibility without discarding
a still-valid clothing choice when a weapon changes.

===========================================================================
*/
import {
	initialCreation,
	creationLoadout,
	creationRange,
	creationProtectors,
	creationNameRules,
	creationNameError
} from "@/engine/foundation/ui/character-create";
import { characterStatus } from "@/engine/foundation/ui/character-status";
import { frontendCameraView } from "@/engine/foundation/rendering/frontend-camera";
import type { CreationSnapshot, CreationSelection } from "@/engine/contracts/frontend";
import type { AssetOwner } from "@/engine/contracts/assets";
import type { CharacterOperationCommand, CharacterOperationResult } from "@/engine/contracts/session";
const NAME_MAX_LENGTH = 12;
const NAME_FILTER_MAX_BYTES = 4 << 20;
const NAME_FILTER_RETRY_SECONDS = 2;
const STATUS_SECONDS = 15;
const FADE_SECONDS = .5;
const ROTATION_IMPULSE = 50;
const ROTATION_DAMPING = .5;
const ROTATION_STOP_SPEED = 30;
const ZOOM_SECONDS = 1;
const CAMERA_INITIAL = [ .5, 9.80000019, 28.5 ];
const CAMERA_REST = [ .5, 9.80000019, 28 ];
const CAMERA_ZOOM_X = 2;
const CAMERA_ZOOM_DISTANCE = 11;
const CAMERA_ZOOM_HEIGHT = [ 15, 14 ];
const BODY_SCALE_BASE = .94;
const BODY_SCALE_STEP = .03;
const CAMERA_PITCH = .200000003;
const CAMERA_FOV = Math.PI / 4;
const CAMERA_FAR = 500000;
const DEGREES_TO_RADIANS = Math.PI / 180;

/*
================
createCreation
================
*/
export function createCreation(
	assets: AssetOwner,
	base: string,
	send: ( command: CharacterOperationCommand ) => void,
	messageSound: () => void = () => {}
) {
	let selection: CreationSelection | null = null,
		phase: CreationSnapshot["phase"] = "editing",
		alpha = 0,
		serial = 0,
		operation = 0,
		submitted = false,
		status: CreationSnapshot["status"],
		statusAge = 0;
	let explain: CreationSnapshot["explain"] = "weapon";
	let retry = 0;
	let job: number | null = null, rules: ReturnType<typeof creationNameRules> | null = null;
	let yaw = 0, velocity = 0, zoom = false, zoomTime = ZOOM_SECONDS, pose = CAMERA_INITIAL, from = pose, to = pose;
	/*
================
reset
================
	*/
	function reset() {
		if ( phase === "checking" || phase === "submitting" ) send( { kind: "cancel-character-operation" } );
		if ( job !== null ) {
			assets.cancel( job );
			job = null;
		}
		selection = null;
		operation = 0;
		submitted = false;
		phase = "editing";
		status = undefined;
	}
	/*
================
show
================
	*/
	function show( key: string ) {
		status = { key, suffix: "" };
		statusAge = 0;
		messageSound();
	}
	/*
================
validate
================
	*/
	function validate() {
		if ( !selection || !rules ) return false;
		const error = creationNameError( selection.name, rules );
		if ( error ) show( error );
		return !error;
	}
	return {
		/*
================
open
================
		*/
		open( race: 0 | 1 ) {
			reset();
			selection = initialCreation( race );
			explain = "weapon";
			yaw = velocity = 0;
			zoom = false;
			pose = CAMERA_INITIAL;
			from = to = pose;
			zoomTime = ZOOM_SECONDS;
			alpha = 0;
		},
		/*
================
action
================
		*/
		action( id: string, value?: string ) {
			if ( !selection ) return;
			if ( id === "create:confirm-cancel" && phase === "confirming" ) {
				phase = "dismissing";
				return;
			}
			if ( id === "create:confirm" && phase === "confirming" && alpha === 1 ) {
				phase = "submitting";
				submitted = false;
				return;
			}
			if ( phase !== "editing" ) return;
			if (
				id === "create:explain" &&
				[ "figure", "height", "volume", "weapon", "protector" ].includes( value ?? "" )
			) {
				explain = value as typeof explain;
				return;
			}
			if ( id === "create:name" ) {
				selection = { ...selection, name: (value ?? "").slice( 0, NAME_MAX_LENGTH ) };
				status = undefined;
				return;
			}
			if ( id === "create:check" ) {
				if ( validate() ) {
					operation = ++serial;
					phase = "checking";
					send( { kind: "check-name", operationId: operation, characterName: selection.name } );
				}
				return;
			}
			if ( id === "create:ok" ) {
				if ( selection.race === 0 && !selection.protector ) {
					show( "UIO_MSG_ERROR_CHARACTER_SELECTARMOR" );
					messageSound();
					return;
				}
				if ( !selection.weapon ) {
					show( "UIO_MSG_ERROR_CHARACTER_SELECTWEAPON" );
					messageSound();
					return;
				}
				if ( validate() ) {
					phase = "confirming";
					alpha = 0;
				}
				return;
			}
			if ( id === "create:male" || id === "create:female" ) {
				selection = {
					...initialCreation( selection.race ),
					name: selection.name,
					gender: id === "create:male" ? 0 : 1
				};
				explain = "weapon";
				return;
			}
			if ( id === "create:left" || id === "create:right" ) {
				velocity += id === "create:left" ? ROTATION_IMPULSE : -ROTATION_IMPULSE;
				return;
			}
			if ( id === "create:zoom" ) {
				zoom = !zoom;
				from = pose;
				to = zoom ?
					[
						CAMERA_ZOOM_X,
						CAMERA_ZOOM_HEIGHT[selection.gender]! * (BODY_SCALE_BASE + selection.height * BODY_SCALE_STEP),
						CAMERA_ZOOM_DISTANCE
					] :
					[ .5, 9.80000019, 28 ];
				zoomTime = 0;
				return;
			}
			const match = /^create:(figure|height|volume|weapon|protector):(prev|next|\d+)$/.exec( id );
			if ( !match ) return;
			const key = match[1] as "figure" | "height" | "volume" | "weapon" | "protector",
				[min, max] = creationRange( selection, key ),
				n = match[2] === "prev" ?
					selection[key] - 1 :
					match[2] === "next" ?
					selection[key] + 1 :
					Number( match[2] );
			const armor = creationProtectors( selection )[selection.protector - 1];
			selection = { ...selection, [key]: Math.max( min, Math.min( max, n ) ) };
			if ( key === "weapon" ) {
				// Protector indices are weapon-relative. Preserve the player's
				// chosen type when admitted by the new weapon, not its old index.
				selection = {
					...selection,
					protector: armor ? creationProtectors( selection ).indexOf( armor ) + 1 : 0
				};
			}
		},
		/*
================
step
================
		*/
		step( delta: number, result: CharacterOperationResult | undefined ) {
			if ( !selection ) return false;
			retry = Math.max( 0, retry - delta );
			if ( job !== null ) {
				const result = assets.take( job );
				if ( result ) {
					job = null;
					try {
						if ( result.kind !== "bytes" ) throw Error( "Native name filter unavailable" );
						rules = creationNameRules( result.buffer );
					} catch {
						retry = NAME_FILTER_RETRY_SECONDS;
						status = characterStatus( 2 );
						statusAge = 0;
					}
				}
			}
			if ( !rules && job === null && retry === 0 && assets.available() ) {
				job = assets.request( new URL( "/assets/textdata/abusefilter.txt", base ).href, NAME_FILTER_MAX_BYTES );
			}
			statusAge += delta;
			if ( statusAge >= STATUS_SECONDS ) status = undefined;
			if ( phase === "dismissing" ) {
				alpha = Math.max( 0, alpha - delta / FADE_SECONDS );
				if ( alpha === 0 ) phase = "editing";
			}
			if ( phase === "confirming" ) alpha = Math.min( 1, alpha + delta / FADE_SECONDS );
			if ( phase === "submitting" && !submitted ) {
				alpha = Math.max( 0, alpha - delta / FADE_SECONDS );
				if ( alpha === 0 ) {
					submitted = true;
					operation = ++serial;
					const s = selection;
					send( {
						kind: "create-character",
						operationId: operation,
						draft: {
							characterName: s.name,
							modelCodename: creationLoadout( s ).modelCodename,
							heightIndex: s.height,
							volumeIndex: s.volume,
							weaponIndex: s.weapon,
							protectorIndex: s.protector,
							armorSelected: s.protector > 0,
							weaponSelected: s.weapon > 0
						}
					} );
				}
			}
			if (
				result && operation === result.operationId && result.status !== "pending" &&
				((phase === "checking" && result.kind === "check-name") ||
					(phase === "submitting" && submitted && result.kind === "create-character"))
			) {
				operation = 0;
				if ( result.status === "failed" ) {
					phase = "editing";
					status = characterStatus( result.nativeErrorCode );
					statusAge = 0;
					messageSound();
				} else if ( phase === "checking" ) {
					phase = "editing";
					show( "UIO_MSG_ERROR_ADMISSON" );
				} else {
					phase = "accepted";
					alpha = 0;
				}
			}
			if ( phase === "accepted" ) {
				alpha = Math.min( 1, alpha + delta / FADE_SECONDS );
				if ( alpha === 1 ) return true;
			}
			yaw += velocity * delta;
			velocity -= velocity * delta * ROTATION_DAMPING;
			if ( Math.abs( velocity ) < ROTATION_STOP_SPEED ) velocity = 0;
			zoomTime = Math.min( 1, zoomTime + delta / ZOOM_SECONDS );
			pose = from.map( ( v, i ) => v + (to[i]! - v) * zoomTime );
			return false;
		},
		/*
================
snapshot
================
		*/
		snapshot(): CreationSnapshot | null {
			return selection ?
				{
					selection,
					explain,
					phase,
					alpha,
					status,
					ready: !!rules,
					yaw: yaw * DEGREES_TO_RADIANS,
					zoom,
					camera: {
						...frontendCameraView( {
							timeSeconds: 0,
							sectorX: 0,
							sectorY: 0,
							position: { x: pose[0]!, y: pose[1]!, z: 0 },
							rotation: { x: CAMERA_PITCH, y: 0, z: 0 },
							mode: pose[2]!
						} ),
						fov: CAMERA_FOV,
						far: CAMERA_FAR
					}
				} :
				null;
		},
		reset,
		/*
================
dispose
================
		*/
		dispose() {
			if ( job !== null ) assets.cancel( job );
			job = null;
			selection = null;
			rules = null;
		}
	};
}
