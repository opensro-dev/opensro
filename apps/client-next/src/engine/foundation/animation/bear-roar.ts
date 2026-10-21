/*
===========================================================================

bear-roar.ts - port-only attack-pet clips authored on the retail skeletons

Each clip is authored from the neutral stance by pitching (and yawing) named
biped bones; no attack motion is reused. Counter-rotation at the hips keeps
the hind legs planted while the torso moves. Bear Roar, Wolf Hunting Howl and
Wolf Savage Rend live here.

===========================================================================
*/
import type { CharacterModel, CharacterChannel } from "@/engine/contracts/character";
import { multiplyQuaternion } from "@/engine/foundation/math/pose-math";

/*
================
withPitchClip

Adds clip `name` keyed at `times`; `pitch` returns a bone's model-space pitch
in degrees for a key frame, or [pitch, yaw] to also turn it about the up axis. Returns the model unchanged when the clip already
exists or a required bone or the stand clip is missing.
================
*/
function withPitchClip(
	model: CharacterModel,
	name: string,
	times: readonly number[],
	required: readonly string[],
	pitch: ( bone: string, frame: number ) => number | readonly [number, number]
): CharacterModel {
	const DEGREES = Math.PI / 180;
	const stand = model.clips.find( clip => clip.name === "stand" );
	const byName = new Map( model.nodes.map( ( node, index ) => [ node.name, index ] ) );
	if ( !stand || model.clips.some( clip => clip.name === name ) ) return model;
	for ( const bone of required ) {
		if ( !byName.has( bone ) ) return model;
	}
	const translations = model.nodes.map( node => [ ...node.translation ] );
	const rotations = model.nodes.map( node => new Float32Array( node.rotation ) );
	for ( const channel of stand.channels ) {
		if ( channel.path === "translation" ) translations[channel.node] = [ ...channel.values.slice( 0, 3 ) ];
		if ( channel.path === "rotation" ) rotations[channel.node] = channel.values.slice( 0, 4 );
	}
	const channels: CharacterChannel[] = [];
	const keys = model.nodes.map( () => new Float32Array( times.length * 4 ) );
	const identity = new Float32Array( [ 0, 0, 0, 1 ] );
	for ( let frame = 0; frame < times.length; frame++ ) {
		const globals = new Map<number, Float32Array>();
		/*
		================
		poseBone

		Pitch in model space, then return to the parent's local space. The native
		biped uses different local axes for its torso and left/right limbs.
		================
		*/
		function poseBone( index: number ): Float32Array {
			const existing = globals.get( index );
			if ( existing ) return existing;
			const node = model.nodes[index]!;
			const parent = node.parent < 0 ? identity : poseBone( node.parent );
			const global = new Float32Array( 4 );
			multiplyQuaternion( parent, rotations[index]!, global );
			const angles = pitch( node.name, frame );
			const [x, y] = typeof angles === "number" ? [ angles, 0 ] : angles;
			const half = x * DEGREES / 2, turn = y * DEGREES / 2;
			const tilted = new Float32Array( 4 ), pitched = new Float32Array( 4 );
			multiplyQuaternion( [ Math.sin( half ), 0, 0, Math.cos( half ) ], global, tilted );
			multiplyQuaternion( [ 0, Math.sin( turn ), 0, Math.cos( turn ) ], tilted, pitched );
			const local = keys[index]!.subarray( frame * 4, frame * 4 + 4 );
			multiplyQuaternion( [ -parent[0]!, -parent[1]!, -parent[2]!, parent[3]! ], pitched, local );
			globals.set( index, pitched );
			return pitched;
		}
		for ( let index = 0; index < model.nodes.length; index++ ) poseBone( index );
	}
	for ( let index = 0; index < model.nodes.length; index++ ) {
		channels.push( {
			node: index,
			path: "rotation",
			interpolation: "LINEAR",
			times: new Float32Array( times ),
			values: keys[index]!
		}, {
			node: index,
			path: "translation",
			interpolation: "LINEAR",
			times: new Float32Array( times ),
			values: new Float32Array( times.flatMap( () => translations[index]! ) )
		} );
	}
	return { ...model, clips: [ ...model.clips, { name, duration: times[times.length - 1]!, channels } ] };
}

/*
================
withBearRoar

Called once at model admission. Original nodes, skins and clips remain intact.
The existing attack-pet server flag owns whether a roar emote can be emitted.
================
*/
export function withBearRoar( model: CharacterModel ): CharacterModel {
	const RISE = [ 0, .08, .8, 1, 1, .96, 1, .9, .45, 0 ];
	const JAW = [ 0, 0, 4, 25, 36, 24, 34, 16, 0, 0 ];
	return withPitchClip(
		model,
		"pet-roar",
		[ 0, .2, .65, .85, 1, 1.18, 1.38, 1.6, 1.85, 2.2 ],
		[ "Bip01 Pelvis", "Bip01 L Thigh", "Bip01 R Thigh", "Bip01 Head", "Bone01" ],
		( bone, frame ) => {
			if ( bone === "Bip01 Pelvis" ) return -70 * RISE[frame]!;
			if ( /Bip01 [LR] Thigh/.test( bone ) ) return 70 * RISE[frame]!;
			if ( bone === "Bip01 Neck" ) return 35 * RISE[frame]!;
			if ( bone === "Bip01 Head" ) return 20 * RISE[frame]!;
			if ( /Bip01 [LR] UpperArm/.test( bone ) ) return -20 * RISE[frame]!;
			return bone === "Bone01" ? JAW[frame]! : 0;
		}
	);
}

/*
================
withWolfHowl

Wolf Hunting Howl (emote of skill IDs 1..3): all four paws stay planted and
the chest dips while the neck and head throw the muzzle straight at the sky
(unlike the bear, which rears up). The lower jaw (Ponytail1 on the wolf rigs)
opens in two pulses; ears (Bone01/Bone03) fold back. 1.6 s, matching the
server's 1.4 s hold.
================
*/
export function withWolfHowl( model: CharacterModel ): CharacterModel {
	const RISE = [ 0, .35, .9, 1, 1, 1, 1, .85, .35, 0 ];
	const JAW = [ 0, 0, 10, 26, 22, 28, 24, 14, 2, 0 ];
	return withPitchClip(
		model,
		"pet-howl",
		[ 0, .15, .4, .55, .7, .85, 1, 1.15, 1.35, 1.6 ],
		[ "Bip01 Pelvis", "Bip01 L Thigh", "Bip01 R Thigh", "Bip01 Neck1", "Bip01 Head", "Bip01 Ponytail1" ],
		( bone, frame ) => {
			if ( bone === "Bip01 Pelvis" ) return 6 * RISE[frame]!;
			if ( /Bip01 [LR] (Thigh|UpperArm)/.test( bone ) ) return -6 * RISE[frame]!;
			if ( bone === "Bip01 Neck1" ) return -55 * RISE[frame]!;
			if ( bone === "Bip01 Head" ) return -25 * RISE[frame]!;
			if ( bone === "Bone01" || bone === "Bone03" ) return 25 * RISE[frame]!;
			return bone === "Bip01 Ponytail1" ? JAW[frame]! : 0;
		}
	);
}

/*
================
withWolfRend

Wolf Savage Rend (emote of skill IDs 4..6 at the bite's windup): crouch,
lunge with the forelegs reaching, the jaw snapping shut at 45 % and a
head-shaking tear. It replaces attack1 for that one cast, so it keeps
attack1's duration and the native hit timing.
================
*/
export function withWolfRend( model: CharacterModel ): CharacterModel {
	const attack = model.clips.find( clip => clip.name === "attack1" );
	if ( !attack ) return model;
	const CROUCH = [ 0, .7, 1, .3, 0, 0, 0, 0, 0, 0 ];
	const LUNGE = [ 0, 0, 0, .8, 1, .9, .8, .6, .25, 0 ];
	const SHAKE = [ 0, 0, 0, 0, .4, -1, 1, -.7, .3, 0 ];
	const JAW = [ 0, 0, 12, 30, 6, 14, 10, 12, 4, 0 ];
	return withPitchClip(
		model,
		"pet-rend",
		[ 0, .12, .25, .35, .45, .55, .65, .75, .87, 1 ].map( f => f * attack.duration ),
		[ "Bip01 Pelvis", "Bip01 L Thigh", "Bip01 R Thigh", "Bip01 Neck1", "Bip01 Head", "Bip01 Ponytail1" ],
		( bone, frame ) => {
			const crouch = CROUCH[frame]!, lunge = LUNGE[frame]!, shake = SHAKE[frame]!;
			if ( bone === "Bip01 Pelvis" ) return 8 * crouch - 10 * lunge;
			if ( /Bip01 [LR] Thigh/.test( bone ) ) return -8 * crouch + 10 * lunge;
			if ( /Bip01 [LR] UpperArm/.test( bone ) ) return -30 * lunge;
			if ( bone === "Bip01 Neck1" ) return [ 12 * crouch + 10 * lunge, 18 * shake ];
			if ( bone === "Bip01 Head" ) return [ 8 * lunge, 25 * shake ];
			if ( bone === "Bone01" || bone === "Bone03" ) return 30 * Math.max( crouch, lunge );
			return bone === "Bip01 Ponytail1" ? JAW[frame]! : 0;
		}
	);
}
