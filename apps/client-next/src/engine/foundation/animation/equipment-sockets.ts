/*
===========================================================================

equipment-sockets.ts - native compound attachment bind frames and private markers

Equipment branches and independently animated resources share the native
attach-root rule. Keep the wearer translation and cancel its bind rotation;
ordinary sockets retain the sampled bone orientation.

===========================================================================
*/
import type { CharacterNode } from "@/engine/contracts/character";
import { compose, multiply } from "@/engine/foundation/math/pose-math";
import { identity } from "@/engine/foundation/rendering/world-math";
/*
================
EquipmentBranch
================
*/
export interface EquipmentBranch {
	readonly part: string;
	readonly attachBone: string;
	readonly nodes: readonly CharacterNode[];
}
/*
================
equipmentSocket
================
*/
export function equipmentSocket( slot: number, part: string, bone: string ) {
	return `equipment:${slot}:${part}:${bone}`;
}
/*
================
validateEquipmentBranches
================
*/
export function validateEquipmentBranches( branches: readonly EquipmentBranch[] | undefined ) {
	if ( branches === undefined ) return;
	if ( !Array.isArray( branches ) || branches.length > 16 ) throw Error( "Invalid equipment branches" );
	for ( const b of branches ) {
		if (
			!b || typeof b.part !== "string" || typeof b.attachBone !== "string" || !b.attachBone ||
			!Array.isArray( b.nodes ) || b.nodes.length > 256
		) throw Error( "Invalid equipment branch" );
		const names = new Set<string>();
		for ( let i = 0; i < b.nodes.length; i++ ) {
			const n = b.nodes[i]!;
			if (
				!n || typeof n.name !== "string" || names.has( n.name ) || !Number.isInteger( n.parent ) ||
				n.parent < -1 || n.parent >= i || !Array.isArray( n.translation ) || n.translation.length !== 3 ||
				!Array.isArray( n.rotation ) || n.rotation.length !== 4 || !Array.isArray( n.scale ) ||
				n.scale.length !== 3 || ![ ...n.translation, ...n.rotation, ...n.scale ].every( Number.isFinite ) ||
				[ ...n.scale ].some( ( v: number ) => v !== 1 ) || n.matrix
			) throw Error( "Invalid private socket node" );
			names.add( n.name );
		}
	}
}
/*
================
createAttachmentBindPose

AB5870 takes the parent skin rotation and restores its sampled translation.
Embedded branches already share the body's import adapter; separate resources
carry their own adapter and must include it in the cancelled bind frame.
================
*/
export function createAttachmentBindPose( nodes: readonly CharacterNode[], includeResourceBasis = true ) {
	const rest = new Map<number, Float32Array>();
	/*
================
bind

Cache the authored frame independently of the current animation sample.
================
	*/
	function bind( index: number ): Float32Array {
		const old = rest.get( index );
		if ( old ) return old;
		const n = nodes[index]!;
		if ( !n ) throw Error( "Missing attachment parent" );
		if ( !includeResourceBasis && n.name === "__gltf_left_handed__" ) return identity();
		const local = new Float32Array( 16 );
		if ( n.matrix ) local.set( n.matrix );
		else compose( n.translation, n.rotation, n.scale, local );
		const value = new Float32Array( 16 );
		if ( n.parent < 0 ) value.set( local );
		else multiply( bind( n.parent ), local, value );
		rest.set( index, value );
		return value;
	}
	return bind;
}

/*
================
appendEquipmentSockets

ABC680 links a handle-local branch. Cancel its parent bind rotation while
keeping the body import adapter, which these embedded nodes share.
================
*/
export function appendEquipmentSockets(
	body: readonly CharacterNode[],
	branches: readonly EquipmentBranch[],
	slot: number
): CharacterNode[] {
	const nodes = [ ...body ], bind = createAttachmentBindPose( body, false );
	for ( const branch of branches ) {
		const parent = body.findIndex( n => n.name === branch.attachBone );
		if ( parent < 0 ) throw Error( "Missing native wearer socket " + branch.attachBone );
		const world = bind( parent ), cancel = identity();
		for ( let c = 0; c < 3; c++ ) for ( let r = 0; r < 3; r++ ) cancel[c * 4 + r] = world[r * 4 + c]!;
		const anchor = nodes.length;
		nodes.push( {
			name: equipmentSocket( slot, branch.part, "$root" ),
			parent,
			translation: [ 0, 0, 0 ],
			rotation: [ 0, 0, 0, 1 ],
			scale: [ 1, 1, 1 ],
			matrix: [ ...cancel ]
		} );
		const start = nodes.length;
		for ( const n of branch.nodes ) {
			nodes.push( {
				...n,
				name: equipmentSocket( slot, branch.part, n.name ),
				parent: n.parent < 0 ? anchor : start + n.parent
			} );
		}
	}
	if ( nodes.length > 1024 ) throw Error( "Equipment socket budget exceeded" );
	return nodes;
}
