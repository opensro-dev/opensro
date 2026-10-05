/*
===========================================================================

residency.ts - which world scenes and terrain parts the renderer holds

The world renderer holds up to three kinds of scene at once: the drawn
one, the one being admitted and retired ones awaiting release. An outdoor
scene is its own groups plus shared terrain parts (world-admission.ts),
and one part can belong to several of those scenes. This owner counts
every scene's own bytes and every held part once, and on release names
only the groups no remaining scene still uses, so a region crossing keeps
the GPU resources of the regions it retains.

===========================================================================
*/
import type { WorldGroup, WorldScene } from "@/engine/contracts/scene";
import type { WorldTerrainPart } from "@/engine/contracts/world-admission";

/*
================
createWorldResidency
================
*/
export function createWorldResidency() {
	const sizes = new Map<WorldScene, number>();
	const sceneParts = new Map<WorldScene, readonly WorldTerrainPart[]>();
	const holders = new Map<WorldTerrainPart, number>();

	/*
	================
	unheld

	The bytes of the parts in terrain that no held scene holds yet.
	================
	*/
	function unheld( terrain: readonly WorldTerrainPart[] ): number {
		let bytes = 0;
		for ( const part of terrain ) if ( !holders.has( part ) ) bytes += part.bytes;
		return bytes;
	}

	/*
	================
	drop

	Forgets scene and lets go of its parts.
	================
	*/
	function drop( scene: WorldScene ): void {
		for ( const part of sceneParts.get( scene ) ?? [] ) {
			const count = holders.get( part )! - 1;
			if ( count ) holders.set( part, count );
			else holders.delete( part );
		}
		sceneParts.delete( scene );
		sizes.delete( scene );
	}

	return {
		/*
		================
		incoming

		What admitting a scene of own bytes with these parts adds.
		================
		*/
		incoming: ( own: number, terrain: readonly WorldTerrainPart[] = [] ) => own + unheld( terrain ),
		/*
		================
		retained
		================
		*/
		retained(): number {
			let bytes = 0;
			for ( const size of sizes.values() ) bytes += size;
			for ( const part of holders.keys() ) bytes += part.bytes;
			return bytes;
		},
		/*
		================
		hold
		================
		*/
		hold( scene: WorldScene, own: number, terrain: readonly WorldTerrainPart[] = [] ): void {
			if ( sizes.has( scene ) ) throw new Error( "World scene already held" );
			sizes.set( scene, own );
			sceneParts.set( scene, terrain );
			for ( const part of terrain ) holders.set( part, (holders.get( part ) ?? 0) + 1 );
		},
		/*
		================
		release

		Lets go of scene and returns its groups that none of the live scenes
		uses. live never includes scene itself.
		================
		*/
		release( scene: WorldScene, live: readonly (WorldScene | null)[] ): WorldGroup[] {
			drop( scene );
			const kept = new Set<WorldGroup>();
			for ( const other of live ) {
				if ( other && other !== scene ) { for ( const group of other.groups ) kept.add( group ); }
			}
			return scene.groups.filter( group => !kept.has( group ) );
		},
		/*
		================
		keepOnly

		Device recovery re-admits one scene and forgets every other.
		================
		*/
		keepOnly( scene: WorldScene | null ): void {
			for ( const held of [ ...sizes.keys() ] ) if ( held !== scene ) drop( held );
		},
		/*
		================
		clear
		================
		*/
		clear(): void {
			sizes.clear();
			sceneParts.clear();
			holders.clear();
		}
	};
}
