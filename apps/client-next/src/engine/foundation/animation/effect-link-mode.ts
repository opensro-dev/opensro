/*
===========================================================================

effect-link-mode.ts - an effect element's links to its ancestors

Each EFP element follows its ancestors' particles through four link depths
and two keep flags (the element definition's +0x154..+0x168, read by
CEFElement_ResolveLinkAncestors AF1DD0 and CEFEffect_UpdateElementMatrices
AF13D0). The element header supplies them (B298F0), and a LinkMode
controller then overrides them as CEFCLinkMode's apply (B1F6F0) does. The
particle graph (particle-graph.ts) runs the links; this module only decides
them, so the effect decoder and its tests share one rule.

===========================================================================
*/

/*
================
EffectLinkNode

The parts of a published effect element the links read: its header bytes
and ints, its controllers and its children.
================
*/
export interface EffectLinkNode {
	readonly byte0?: number;
	readonly byte1?: number;
	readonly int0?: number;
	readonly int1?: number;
	readonly int2?: number;
	readonly int3?: number;
	readonly controllers?: readonly { readonly name: string; readonly value?: unknown; }[];
	readonly children: readonly EffectLinkNode[];
}

/*
================
EffectLinks

One element's resolved links, in the particle graph's names:
positionDepth (+0x158) turns the position about an ancestor,
matrixDepth (+0x15C, -1 drops the inherited rotation) multiplies the
ancestor's turn into the matrix, velocityDepth (+0x160) turns the velocity,
followDepth (+0x168) carries the position with an ancestor's movement.
keepMatrix (+0x154) and keepOrigin (+0x155) make the element keep the
turn and the previous position its descendants link to.
================
*/
export interface EffectLinks {
	positionDepth: number;
	matrixDepth: number;
	velocityDepth: number;
	followDepth: number;
	keepMatrix: boolean;
	keepOrigin: boolean;
}

// The four LinkMode words, in file order (B1F6F0 reads controller +0x14..+0x20).
const LINK_FOLLOW = 0;
const LINK_POSITION = 1;
const LINK_MATRIX = 2;
const LINK_VELOCITY = 3;
// B1F751: a matrix link of -1 keeps no ancestor and drops the rotation.
const LINK_MATRIX_RESET = -1;

/*
================
linkModeWords

The LinkMode controller's four words as signed 32-bit values (the native
compares them signed), or null when the element has none. The last
LinkMode wins, as each apply overwrites the depths.
================
*/
function linkModeWords( node: EffectLinkNode ): number[] | null {
	let words: number[] | null = null;
	for ( const controller of node.controllers ?? [] ) {
		if ( controller.name !== "LinkMode" ) continue;
		const value = controller.value;
		if (
			!Array.isArray( value ) || value.length !== 4 ||
			value.some( word => !Number.isInteger( word ) || word < -0x80000000 || word > 0xffffffff )
		) {
			throw new Error( "Invalid effect LinkMode" );
		}
		words = value.map( word => (word as number) | 0 );
	}
	return words;
}

/*
================
walkUp

B1F6F0's ancestor walk for one word: a positive word climbs that many
parents and marks the element reached; the steps taken are the stored
depth. Each step counts before it tests for a parent (B1F713 add, then
B1F718 test), so a walk past the root stops there with one step more
than the parents it climbed: from [root, a, self], a word of 5 stores 3
and marks the root. A negative word marks the element itself and stores
0; zero marks nothing.
================
*/
function walkUp(
	chain: readonly EffectLinkNode[],
	word: number
): { depth: number; marked: EffectLinkNode | undefined; } {
	const self = chain[chain.length - 1]!;
	if ( word === 0 ) return { depth: 0, marked: undefined };
	if ( word < 0 ) return { depth: 0, marked: self };
	let index = chain.length - 1, depth = 0;
	do {
		depth++;
		if ( index === 0 ) break;
		index--;
	} while ( depth < word );
	return { depth, marked: chain[index]! };
}

/*
================
effectLinks

Every element's links for one effect tree. Headers come first (B298F0);
then each LinkMode, in load order (parents before children), overrides its
element's depths and marks the ancestors it reaches: the follow word sets
the reached ancestor's keepOrigin, the others its keepMatrix (B1F6F0).
================
*/
export function effectLinks( root: EffectLinkNode ): Map<EffectLinkNode, EffectLinks> {
	const links = new Map<EffectLinkNode, EffectLinks>();
	const stack: { node: EffectLinkNode; chain: readonly EffectLinkNode[]; }[] = [ { node: root, chain: [ root ] } ];
	while ( stack.length ) {
		const { node, chain } = stack.pop()!;
		links.set( node, {
			positionDepth: node.int0 ?? 0,
			matrixDepth: node.int1 ?? 0,
			velocityDepth: node.int2 ?? 0,
			followDepth: node.int3 ?? 0,
			keepMatrix: !!node.byte0,
			keepOrigin: !!node.byte1
		} );
		const words = linkModeWords( node );
		if ( words ) {
			const own = links.get( node )!;
			const follow = walkUp( chain, words[LINK_FOLLOW]! );
			if ( follow.marked ) links.get( follow.marked )!.keepOrigin = true;
			own.followDepth = follow.depth;
			const position = walkUp( chain, words[LINK_POSITION]! );
			if ( position.marked ) links.get( position.marked )!.keepMatrix = true;
			own.positionDepth = position.depth;
			if ( words[LINK_MATRIX] === LINK_MATRIX_RESET ) own.matrixDepth = LINK_MATRIX_RESET;
			else {
				const matrix = walkUp( chain, words[LINK_MATRIX]! );
				if ( matrix.marked ) links.get( matrix.marked )!.keepMatrix = true;
				own.matrixDepth = matrix.depth;
			}
			const velocity = walkUp( chain, words[LINK_VELOCITY]! );
			if ( velocity.marked ) links.get( velocity.marked )!.keepMatrix = true;
			own.velocityDepth = velocity.depth;
		}
		// Children load after their parent, so each sees its ancestors' marks.
		for ( let i = node.children.length - 1; i >= 0; i-- ) {
			const child = node.children[i]!;
			stack.push( { node: child, chain: [ ...chain, child ] } );
		}
	}
	return links;
}
