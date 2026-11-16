/*
===========================================================================

particle-slots.mjs - read a particle graph's flat slots as elements

The particle graph keeps its elements in flat slot storage
(src/engine/foundation/animation/particle-graph-slots.ts). Tests that
assert on one element's state read it here as a plain object, with its
vectors as arrays: the values the tick wrote, copied.

===========================================================================
*/
import "./native-source-loader.mjs";

const { SLOT_ALIVE, SLOT_OCCUPIED, SLOT_RELEASED } = await import(
	"../../src/engine/foundation/animation/particle-graph-slots.ts"
);

/*
================
particleElement

Slot s of graph as { slot, serial, born, clockBirth, age, alive, released,
children, rootLink, parent (its parent's slot), frame, position, velocity,
scale, previousPosition, origin, matrix }.
================
*/
export function particleElement( graph, s ) {
	const flags = graph.flags[s], vector = array => Array.from( array.subarray( s * 3, s * 3 + 3 ) );
	return {
		slot: s,
		serial: graph.serials[s],
		born: graph.birth[s],
		clockBirth: graph.clockBirth[s],
		age: graph.age[s],
		alive: (flags & SLOT_ALIVE) !== 0,
		released: (flags & SLOT_RELEASED) !== 0,
		children: graph.children[s],
		rootLink: graph.link[s],
		parent: graph.parent[s],
		frame: graph.frames[s],
		position: vector( graph.position ),
		velocity: vector( graph.velocity ),
		scale: vector( graph.scale ),
		previousPosition: vector( graph.previousPosition ),
		origin: vector( graph.origin ),
		matrix: graph.matrix.slice( s * 16, s * 16 + 16 )
	};
}

/*
================
particleElements

Emitter n's slots in use, each its element (particleElement) or undefined
while it holds none (typed as present: a test reads the slots it knows
hold an element, and filters with ?. where some may not).
================
*/
export function particleElements( graph, n ) {
	const base = graph.plan.base[n];
	/** @type {ReturnType<typeof particleElement>[]} */
	const elements = [];
	for ( let b = 0; b < graph.counts[n]; b++ ) {
		const occupied = graph.flags[base + b] & SLOT_OCCUPIED;
		elements.push( occupied ? particleElement( graph, base + b ) : /** @type {any} */ (undefined) );
	}
	return elements;
}
