/*
===========================================================================

action-refresh.ts - native synchronous action transitions and movement rates

Presentation owns the state mask. This reducer preserves callback ordering
without creating a second simulation clock. Native dispatch: CCC9F8.

===========================================================================
*/
/** Synchronous callback reduction, owned by character presentation. This is
 * not a second ticking state machine. Masks are the u16 records at CCC9F8+32.
 * Exits clear their bit BEFORE calling nested callbacks (857950/8565D0). */
import type { Pose } from "@/engine/contracts/gameplay";
import { poseDistance } from "@/engine/foundation/gameplay/native-movement";
/** 86CE00 initializes +8C from the two path distances; 8E5DD0 caps the
 * animation installation rate at 1.3. This is not the actor's speed buff. */
/*
================
movementEntryRate
================
*/
export function movementEntryRate(
	rendered: Pose,
	predicted: Pose,
	path: { readonly from: Pose; readonly to: Pose; } | undefined,
	newPath: boolean
): number {
	if (
		!path ||
		((rendered.regionId | predicted.regionId | path.from.regionId | path.to.regionId) & 0x8000) &&
			![ predicted, path.from, path.to ].every( p => p.regionId === rendered.regionId )
	) return 1;
	const f = Math.fround;
	if ( newPath ) {
		const error = f( poseDistance( rendered, path.from ) ** 2 ),
			distance = f( poseDistance( path.from, path.to ) ** 2 );
		return Math.min( f( 1.3 ), error > distance ? distance ? f( error / distance ) : 3 : 1 );
	}
	// 86CF50: within five units keep1; otherwise use the correction/forward
	// direction dot product, with the ten-degree threshold tested in ASM.
	const distance = poseDistance( rendered, predicted );
	if ( distance < 5 ) return 1;
	const vector = ( a: Pose, b: Pose ) => {
		const outdoor = !(a.regionId & 0x8000);
		return [
			b.x - a.x + (outdoor ? ((b.regionId & 255) - (a.regionId & 255)) * 1920 : 0),
			b.z - a.z + (outdoor ? ((b.regionId >>> 8) - (a.regionId >>> 8)) * 1920 : 0)
		] as const;
	};
	const a = vector( rendered, predicted ), b = vector( path.from, path.to ), length = Math.hypot( ...b );
	if ( !length ) return 1;
	const dot = f( (a[0] * b[0] + a[1] * b[1]) / (distance * length) );
	return dot < 0 ? f( .9 ) : dot > f( Math.cos( .17453292012214661 ) ) ? f( 1.1 ) : 1;
}
/*
================
BaseAction
================
*/
export type BaseAction = 3 | 6 | 8 | 9 | 15;
/*
================
ActionEffect
================
*/
export type ActionEffect = { kind: "enter" | "leave"; state: number; previous: number; } | { kind: "commit"; } | {
	kind: "feet";
} | { kind: "navigation"; enabled: boolean; };
/*
================
ActionRefresh
================
*/
export interface ActionRefresh {
	mask: number;
	navigation: boolean;
	effects: readonly ActionEffect[];
}
/*
================
actionMasks
================
*/
function actionMasks( state: BaseAction ): readonly [number, number, number] {
	switch ( state ) {
		case 3:
			return [ 0, 2, 0x8050 ];
		case 6:
			return [ 0, 0x40, 8 ];
		case 8:
			return [ 8, 0x102, 0x200 ];
		case 9:
			return [ 8, 0x200, 0x21a0 ];
		case 15:
			return [ 0, 0, 0xfffe ];
	}
}
/*
================
transitionActionStates
================
*/
export function transitionActionStates(
	initialMask: number,
	navigationAvailable: boolean,
	commands: readonly { kind: "enter" | "leave" | "refresh"; state?: BaseAction; }[]
): ActionRefresh {
	let mask = initialMask & 0xffff, navigation = !!(mask & 0x200);
	const effects: ActionEffect[] = [];
	/*
================
leave
================
	*/
	function leave( state: BaseAction ) {
		if ( !(mask & (1 << state)) ) return;
		const previous = mask;
		mask &= ~(1 << state);
		effects.push( { kind: "leave", state, previous } );
		if ( state === 9 ) {
			enter( 8 );
			effects.push( { kind: "feet" } );
			navigation = false;
			effects.push( { kind: "navigation", enabled: false } );
		} else if ( state === 3 ) {
			leave( 8 );
			leave( 9 );
		} else if ( state === 8 ) enter( 9 );
	}
	/*
================
enter
================
	*/
	function enter( state: BaseAction ) {
		const [required, excluded, clear] = actionMasks( state );
		if ( state === 9 && !navigationAvailable || required && !(mask & required) || mask & excluded ) return;
		const previous = mask;
		for ( let id = 0; id < 16; id++ ) {
			if ( clear & mask & (1 << id) ) {
				if ( id === 3 || id === 6 || id === 8 || id === 9 || id === 15 ) leave( id );
				else {
					const before = mask;
					mask &= ~(1 << id);
					effects.push( { kind: "leave", state: id, previous: before } );
				}
			}
		}
		mask |= 1 << state;
		effects.push( { kind: "enter", state, previous } );
		if ( state === 3 ) enter( 8 );
		else if ( state === 9 ) {
			navigation = true;
			effects.push( { kind: "navigation", enabled: true } );
		}
	}
	for ( const command of commands ) {
		if ( command.kind === "enter" ) {
			enter( command.state! );
			continue;
		}
		if ( command.kind === "leave" ) {
			leave( command.state! );
			continue;
		}
		let saved = 0;
		for ( const id of [ 9, 3, 6 ] as const ) {
			if ( mask & (1 << id) ) {
				leave( id );
				saved |= 1 << id;
			}
		}
		effects.push( { kind: "commit" } );
		for ( const id of [ 9, 3, 6 ] as const ) if ( saved & (1 << id) ) enter( id );
	}
	return { mask, navigation, effects };
}
/*
================
refreshActionStates
================
*/
export function refreshActionStates(
	initialMask: number,
	navigationAvailable: boolean,
	changed: boolean
): ActionRefresh {
	return transitionActionStates( initialMask, navigationAvailable, changed ? [ { kind: "refresh" } ] : [] );
}
